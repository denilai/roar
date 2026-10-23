package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"roar/internal/pkg/argo"
	"roar/internal/pkg/git"
	"roar/internal/pkg/helm"
	"roar/internal/pkg/logger"
)

// maxStandDepth ограничивает вложенность app-of-apps: корень -> стенд -> приложение.
const maxStandDepth = 4

type Config struct {
	ChartPath   string
	ValuesFiles []string
	OutputDir   string
	LogLevel    string
	Filters     []string
	Mirror      bool
	Jobs        int
	tempDir_    string
}

// cloneEntry гарантирует один clone на пару (repo, revision), даже при параллельных воркерах.
type cloneEntry struct {
	once sync.Once
	path string
	err  error
}

type appState struct {
	tempDir   string
	outputDir string
	mirror    bool
	jobs      int

	cloneMu  sync.Mutex
	clones   map[string]*cloneEntry
	cloneSeq int

	indexMu sync.Mutex
	index   []indexEntry
}

// indexEntry фиксирует, какие стенды и приложения есть в корне, и что с каждым случилось.
type indexEntry struct {
	Stand    string `json:"stand"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Env      string `json:"env,omitempty"`
	Instance string `json:"instance,omitempty"`
	RepoURL  string `json:"repoURL"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
	Status   string `json:"status"`
	Output   string `json:"output,omitempty"`
}

func Run(cfg Config) error {
	tempDir := cfg.tempDir_
	if tempDir == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "argo-charts-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tempDir)
	}

	logger.Log.Infof("Using temporary directory for clones: %s", tempDir)

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", cfg.OutputDir, err)
	}

	jobs := cfg.Jobs
	if jobs < 1 {
		jobs = 1
	}
	state := &appState{
		tempDir:   tempDir,
		outputDir: cfg.OutputDir,
		mirror:    cfg.Mirror,
		jobs:      jobs,
		clones:    make(map[string]*cloneEntry),
	}

	if err := renderLevel(state, cfg.ChartPath, cfg.ValuesFiles, cfg.Filters, "", 0); err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	if err := state.writeIndex(); err != nil {
		return err
	}
	logger.Log.Info("All done!")
	return nil
}

// renderLevel рендерит app-of-apps чарт и обрабатывает его Application параллельно (state.jobs воркеров).
func renderLevel(state *appState, chartPath string, valuesFiles, filters []string, parent string, depth int) error {
	if depth > maxStandDepth {
		return fmt.Errorf("app-of-apps nested deeper than %d levels at %s", maxStandDepth, parent)
	}
	logger.Log.Infof("Rendering the 'app-of-apps' chart %s (level %d)...", chartPath, depth)
	opts := helm.RenderOptions{ReleaseName: "app-of-apps", ChartPath: chartPath, ValuesFiles: valuesFiles}
	manifests, err := helm.Template(opts)
	if err != nil {
		return fmt.Errorf("failed to render app-of-apps chart: %w", err)
	}

	applications, err := argo.ParseApplications(manifests, filters)
	if err != nil {
		return fmt.Errorf("failed to parse Argo applications: %w", err)
	}
	logger.Log.Infof("Found %d applications to process.", len(applications))

	work := make(chan argo.Application)
	var wg sync.WaitGroup
	for w := 0; w < state.jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for app := range work {
				if err := processApplication(state, app, parent, depth, filters); err != nil {
					logger.Log.WithField("application", app.Name).Errorf("Could not process application: %v. Skipping.", err)
				}
			}
		}()
	}
	for _, app := range applications {
		work <- app
	}
	close(work)
	wg.Wait()
	return nil
}

func processApplication(state *appState, app argo.Application, parent string, depth int, filters []string) error {
	logCtx := logger.Log.WithField("application", app.Name)
	logCtx.Info("Processing application...")

	if state.mirror {
		newRepoURL, newPath, transformed := applyMirrorTransform(app.RepoURL, app.Path)
		if transformed {
			logCtx.Info("Mirror transformation applied")
			logCtx.Infof("  Repository: %s -> %s", app.RepoURL, newRepoURL)
			logCtx.Infof("  Path: %s -> %s", app.Path, newPath)
			app.RepoURL = newRepoURL
			app.Path = newPath
		}
	}

	entry := indexEntry{Stand: parent, Name: app.Name, Kind: "app", Env: app.Env, Instance: app.Instance,
		RepoURL: app.RepoURL, Path: app.Path, Revision: app.TargetRevision}

	sshURL, err := convertHTTPtoSSH(app.RepoURL)
	if err != nil {
		entry.Status = "clone-error"
		state.addIndex(entry)
		return fmt.Errorf("invalid repo URL '%s': %w", app.RepoURL, err)
	}

	repoPath, err := state.ensureClone(sshURL, app.TargetRevision)
	if err != nil {
		entry.Status = "clone-error"
		state.addIndex(entry)
		return fmt.Errorf("failed to clone repo: %w", err)
	}

	// Путь чарта: Path (rawPath или spec.source.path) + SourcePath (spec.source.path внутри зеркала).
	appServicePath := filepath.Join(repoPath, app.Path, app.SourcePath)
	chartDir, err := resolveChartDir(appServicePath)
	if err != nil {
		entry.Status = "render-error"
		state.addIndex(entry)
		return err
	}

	absoluteValuesFiles := make([]string, len(app.ValuesFiles))
	for i, file := range app.ValuesFiles {
		absoluteValuesFiles[i] = filepath.Join(appServicePath, file)
	}

	// Setters из Application: для стенда передаём как есть, env/instance в них не добавляем.
	setters := app.Setters
	if setters == nil {
		setters = map[string]string{}
	}

	renderedApp, err := helm.Template(helm.RenderOptions{ReleaseName: app.Name, ChartPath: chartDir,
		ValuesFiles: absoluteValuesFiles, SetValues: setters})
	if err != nil {
		logCtx.Errorf("Failed to render chart: %v.", err)
		entry.Status = "render-error"
		state.addIndex(entry)
		return nil
	}

	if isAppOfApps(renderedApp) {
		return processStand(state, app, chartDir, absoluteValuesFiles, setters, filters, depth, entry)
	}

	entry.Status = "ok"
	return writeLeaf(state, app, renderedApp, entry)
}

// processStand разворачивает стенд: чарт, который сам порождает Application, рендерится следующим уровнем.
// Слой env (values.<env>.yaml) подключается перед файлами значений стенда, как это делает Argo.
func processStand(state *appState, app argo.Application, chartDir string, valuesFiles []string,
	setters map[string]string, filters []string, depth int, entry indexEntry) error {
	layers := []string{}
	if app.Env != "" {
		envLayer := filepath.Join(chartDir, fmt.Sprintf("values.%s.yaml", app.Env))
		if _, err := os.Stat(envLayer); err == nil {
			layers = append(layers, envLayer)
		}
	}
	layers = append(layers, valuesFiles...)

	entry.Kind = "stand"
	entry.Status = "ok"
	standPath := strings.TrimPrefix(entry.Stand+"/"+app.Name, "/")
	logger.Log.WithField("application", app.Name).Infof("Stand detected: expanding %s", standPath)
	err := renderLevel(state, chartDir, layers, filters, standPath, depth+1)
	if err != nil {
		entry.Status = "render-error"
	}
	state.addIndex(entry)
	return err
}

// writeLeaf пишет манифест приложения в <outputDir>/<env>/<instance>/<name>.yaml.
func writeLeaf(state *appState, app argo.Application, manifest []byte, entry indexEntry) error {
	logCtx := logger.Log.WithField("application", app.Name)
	finalOutputDir := state.outputDir
	if app.Env != "" {
		finalOutputDir = filepath.Join(finalOutputDir, app.Env)
	}
	if app.Instance != "" {
		finalOutputDir = filepath.Join(finalOutputDir, app.Instance)
	}
	if err := os.MkdirAll(finalOutputDir, 0755); err != nil {
		entry.Status = "write-error"
		state.addIndex(entry)
		return fmt.Errorf("failed to create output subdirectory %s: %w", finalOutputDir, err)
	}
	outputFile := filepath.Join(finalOutputDir, fmt.Sprintf("%s.yaml", app.Name))
	if err := os.WriteFile(outputFile, manifest, 0644); err != nil {
		entry.Status = "write-error"
		state.addIndex(entry)
		return fmt.Errorf("failed to write manifest to %s: %w", outputFile, err)
	}
	entry.Output = outputFile
	state.addIndex(entry)
	logCtx.Infof("Successfully rendered and saved manifest to %s", outputFile)
	return nil
}

// resolveChartDir: чарт лежит в <путь>/.helm (как у product-приложений) или в самом <путь> (как applications/).
func resolveChartDir(appServicePath string) (string, error) {
	candidates := []string{filepath.Join(appServicePath, ".helm"), appServicePath}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no Chart.yaml in %s or %s/.helm", appServicePath, appServicePath)
}

// isAppOfApps: отрендеренный чарт содержит Application, значит это уровень стенда.
func isAppOfApps(manifest []byte) bool {
	text := string(manifest)
	return strings.HasPrefix(text, "kind: Application") || strings.Contains(text, "\nkind: Application\n")
}

// ensureClone клонирует (repo, revision) один раз; параллельные воркеры ждут тот же результат.
func (s *appState) ensureClone(sshURL, revision string) (string, error) {
	key := fmt.Sprintf("%s@%s", sshURL, revision)
	s.cloneMu.Lock()
	entry, ok := s.clones[key]
	if !ok {
		s.cloneSeq++
		entry = &cloneEntry{path: filepath.Join(s.tempDir, fmt.Sprintf("clone-%d", s.cloneSeq))}
		s.clones[key] = entry
	}
	s.cloneMu.Unlock()

	entry.once.Do(func() {
		logger.Log.Infof("Cloning %s to %s", key, entry.path)
		entry.err = git.Clone(sshURL, revision, entry.path)
	})
	return entry.path, entry.err
}

func (s *appState) addIndex(e indexEntry) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	s.index = append(s.index, e)
}

// writeIndex пишет <outputDir>/index.json: все стенды и приложения, их ревизии и статусы.
func (s *appState) writeIndex() error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	sort.SliceStable(s.index, func(i, j int) bool {
		if s.index[i].Stand != s.index[j].Stand {
			return s.index[i].Stand < s.index[j].Stand
		}
		return s.index[i].Name < s.index[j].Name
	})
	data, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode index: %w", err)
	}
	path := filepath.Join(s.outputDir, "index.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write index %s: %w", path, err)
	}
	logger.Log.Infof("Index written: %s (%d entries)", path, len(s.index))
	return nil
}

func convertHTTPtoSSH(httpURL string) (string, error) {
	if strings.HasPrefix(httpURL, "git@") {
		return httpURL, nil
	}
	parsedURL, err := url.Parse(httpURL)
	if err != nil {
		return "", fmt.Errorf("could not parse URL: %w", err)
	}
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return httpURL, nil
	}
	path := strings.TrimPrefix(parsedURL.Path, "/")
	sshURL := fmt.Sprintf("git@%s:%s", parsedURL.Host, path)
	return sshURL, nil
}

const (
	mirrorSourceHost  = "git.nvfn.ru"
	mirrorSourceHost2 = "git.octe.cc"
	mirrorTargetRepo  = "https://git.uis.dev/deploy/product.git"
	mirrorDeployPath  = "/deploy/"
	mirrorServicePath = "/service/"
)

// applyMirrorTransform transforms mirror URLs to use git.uis.dev/deploy/product.git.
// Supports two sources:
//   - git.nvfn.ru/deploy/* -> product.git, path "stable/{path after /deploy/}/..."
//   - git.octe.cc/service/* -> product.git, path "stable/{path after /service/}/..."
//
// Returns transformed (repoURL, path, wasTransformed).
func applyMirrorTransform(repoURL, path string) (string, string, bool) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return repoURL, path, false
	}

	// git.nvfn.ru/deploy/* -> product.git, prefix "stable/"
	if parsedURL.Host == mirrorSourceHost && strings.HasPrefix(parsedURL.Path, mirrorDeployPath) {
		afterDeploy := strings.TrimPrefix(parsedURL.Path, mirrorDeployPath)
		afterDeploy = strings.TrimSuffix(afterDeploy, ".git")
		var newPath string
		if path == "" || path == "." {
			newPath = filepath.Join("stable", afterDeploy)
		} else {
			newPath = filepath.Join("stable", afterDeploy, path)
		}
		return mirrorTargetRepo, newPath, true
	}

	// git.octe.cc/service/* -> product.git; path = stable/ + (path after /service/), same layout as product repo
	if parsedURL.Host == mirrorSourceHost2 && strings.HasPrefix(parsedURL.Path, mirrorServicePath) {
		afterService := strings.TrimPrefix(parsedURL.Path, mirrorServicePath)
		afterService = strings.TrimSuffix(afterService, ".git")
		afterService = strings.Trim(afterService, "/")
		var newPath string
		if path == "" || path == "." {
			newPath = filepath.Join("stable", afterService)
		} else {
			newPath = filepath.Join("stable", afterService, path)
		}
		return mirrorTargetRepo, newPath, true
	}

	return repoURL, path, false
}
