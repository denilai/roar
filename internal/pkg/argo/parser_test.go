package argo

import (
	"bytes"
	"io"
	"testing"

	"roar/internal/pkg/logger"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func init() {
	logger.Log = logrus.New()
	logger.Log.SetOutput(io.Discard)
}

func TestParseApplicationsWithFilter(t *testing.T) {
	yamlInput := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-master
  annotations: {rawRepository: "repo"}
spec:
  source:
    targetRevision: master
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-dev
  annotations: {rawRepository: "repo"}
spec:
  source:
    targetRevision: dev
---
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: project-ignored
spec: {}
`

	tests := []struct {
		name          string
		filter        string
		expectedNames []string
	}{
		{
			name:          "no filter",
			filter:        "",
			expectedNames: []string{"app-master", "app-dev"},
		},
		{
			name:          "filter equals master",
			filter:        "spec.source.targetRevision==master",
			expectedNames: []string{"app-master"},
		},
		{
			name:          "filter not equals master",
			filter:        "spec.source.targetRevision!=master",
			expectedNames: []string{"app-dev"},
		},
		{
			name:          "filter by name",
			filter:        "metadata.name==app-dev",
			expectedNames: []string{"app-dev"},
		},
		{
			name:          "filter by non-existent field",
			filter:        "spec.source.missing!=foo",
			expectedNames: []string{"app-master", "app-dev"}, // "пусто" != "foo" -> true
		},
		{
			name:          "filter by non-existent field equals",
			filter:        "spec.source.missing==foo",
			expectedNames: []string{}, // "пусто" == "foo" -> false
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			apps, err := ParseApplications([]byte(yamlInput), tc.filter)
			require.NoError(t, err)

			names := []string{}
			for _, app := range apps {
				names = append(names, app.Name)
			}
			require.Equal(t, tc.expectedNames, names)
		})
	}
}

// TestParseApplications_LogSkipped проверяет, что пропущенные приложения логируются
func TestParseApplications_LogSkipped(t *testing.T) {
	var logBuffer bytes.Buffer
	originalOut := logger.Log.Out
	logger.Log.SetOutput(&logBuffer)
	logger.Log.SetLevel(logrus.InfoLevel)
	defer func() {
		logger.Log.SetOutput(originalOut)
	}()

	yamlInput := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-to-keep
  annotations: {rawRepository: "repo"}
spec:
  source:
    targetRevision: master
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-to-skip
  annotations: {rawRepository: "repo"}
spec:
  source:
    targetRevision: dev
`

	apps, err := ParseApplications([]byte(yamlInput), "spec.source.targetRevision==master")
	require.NoError(t, err)

	require.Len(t, apps, 1)
	require.Equal(t, "app-to-keep", apps[0].Name)

	logOutput := logBuffer.String()
	require.Contains(t, logOutput, "Skipped by filter")
	require.Contains(t, logOutput, "app-to-skip")
	require.Contains(t, logOutput, "spec.source.targetRevision == 'master'")
}

// TestParseApplications проверяет высокоуровневую логику парсинга
func TestParseApplications(t *testing.T) {
	testCases := []struct {
		name             string
		inputYAML        string
		expectedAppCount int
		expectError      bool
		errorContains    string
	}{
		{
			name: "should parse a single valid application",
			inputYAML: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: the-real-app
  annotations:
    rawRepository: "repo"
spec:
  source:
    targetRevision: main
`,
			expectedAppCount: 1,
		},
		{
			name: "should ignore non-application resources",
			inputYAML: `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: the-real-app
  annotations:
    rawRepository: "repo"
spec:
  source:
    targetRevision: main
`,
			expectedAppCount: 1,
		},
		{
			name: "should parse multiple valid applications",
			inputYAML: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-one
  annotations: {rawRepository: "repo1"}
spec: {source: {targetRevision: "main"}}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-two
  annotations: {rawRepository: "repo2"}
spec: {source: {targetRevision: "dev"}}
`,
			expectedAppCount: 2,
		},
		{
			name:          "should return error on malformed yaml",
			inputYAML:     `apiVersion: v1: kind: Broken`,
			expectError:   true,
			errorContains: "failed to decode yaml document",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			apps, err := ParseApplications([]byte(tc.inputYAML), "")

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.Len(t, apps, tc.expectedAppCount)
				if tc.expectedAppCount > 0 {
					require.NotEmpty(t, apps[0].Name)
				}
			}
		})
	}
}

// TestNewApplicationFromRaw проверяет внутреннюю логику преобразования
func TestNewApplicationFromRaw(t *testing.T) {
	baseRawApp := func() rawApplication {
		var app rawApplication
		app.ApiVersion = "argoproj.io/v1alpha1"
		app.Kind = "Application"
		app.Metadata.Name = "test-app"
		app.Spec.Source.TargetRevision = "main"
		app.Metadata.Annotations = map[string]string{"rawRepository": "https://default.repo"}
		return app
	}

	testCases := []struct {
		name          string
		inputRawApp   rawApplication
		expectedApp   Application
		expectError   bool
		errorContains string
	}{
		// --- Тесты на разрешение Instance и Env ---
		{
			name: "instance and env from labels only",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Labels = map[string]string{
					"instance": "from-label",
					"env":      "dev-label",
				}
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				Instance:       "from-label",
				Env:            "dev-label",
				TargetRevision: "main",
				RepoURL:        "https://default.repo",
				Path:           ".",
				Setters:        map[string]string{},
				ValuesFiles:    []string{},
			},
		},
		{
			name: "instance and env from plugin.env only",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Spec.Source.Plugin = &struct {
					Env []EnvVar `yaml:"env"`
				}{
					Env: []EnvVar{
						{Name: "WERF_SET_INSTANCE", Value: "global.instance=from-plugin"},
						{Name: "WERF_SET_ENV", Value: "global.env=dev-plugin"},
					},
				}
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				Instance:       "from-plugin",
				Env:            "dev-plugin",
				TargetRevision: "main",
				RepoURL:        "https://default.repo",
				Path:           ".",
				Setters: map[string]string{
					"global.instance": "from-plugin",
					"global.env":      "dev-plugin",
				},
				ValuesFiles: []string{},
			},
		},
		{
			name: "conflict when both instance sources differ",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Labels = map[string]string{"instance": "from-label"}
				app.Spec.Source.Plugin = &struct {
					Env []EnvVar `yaml:"env"`
				}{
					Env: []EnvVar{{Name: "WERF_SET_INSTANCE", Value: "global.instance=from-plugin"}},
				}
				return app
			}(),
			expectError:   true,
			errorContains: "conflicting values for 'instance': label is 'from-label', plugin.env is 'from-plugin'",
		},
		{
			name: "conflict when both env sources differ",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Labels = map[string]string{"env": "from-label"}
				app.Spec.Source.Plugin = &struct {
					Env []EnvVar `yaml:"env"`
				}{
					Env: []EnvVar{{Name: "WERF_SET_ENV", Value: "global.env=from-plugin"}},
				}
				return app
			}(),
			expectError:   true,
			errorContains: "conflicting values for 'env': label is 'from-label', plugin.env is 'from-plugin'",
		},
		{
			name: "no conflict when both sources match",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Labels = map[string]string{"instance": "same-value"}
				app.Spec.Source.Plugin = &struct {
					Env []EnvVar `yaml:"env"`
				}{
					Env: []EnvVar{{Name: "WERF_SET_INSTANCE", Value: "global.instance=same-value"}},
				}
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				Instance:       "same-value",
				TargetRevision: "main",
				RepoURL:        "https://default.repo",
				Path:           ".",
				Setters:        map[string]string{"global.instance": "same-value"},
				ValuesFiles:    []string{},
			},
		},

		// --- Тесты на разрешение Repo и Path ---
		{
			name: "repo and path from annotations have priority",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Annotations["rawRepository"] = "https://anno.repo"
				app.Metadata.Annotations["rawPath"] = "anno/path"
				app.Spec.Source.RepoURL = "https://spec.repo"
				app.Spec.Source.Path = "spec/path"
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				RepoURL:        "https://anno.repo",
				Path:           "anno/path",
				TargetRevision: "main",
				Setters:        map[string]string{},
				ValuesFiles:    []string{},
			},
		},
		{
			name: "fallback to spec.source for repo and path",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Annotations = map[string]string{}
				app.Spec.Source.RepoURL = "https://spec.repo"
				app.Spec.Source.Path = "spec/path"
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				RepoURL:        "https://spec.repo",
				Path:           "spec/path",
				TargetRevision: "main",
				Setters:        map[string]string{},
				ValuesFiles:    []string{},
			},
		},
		{
			name: "path falls back to '.' if all sources are empty",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Spec.Source.Path = ""
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				RepoURL:        "https://default.repo",
				Path:           ".",
				TargetRevision: "main",
				Setters:        map[string]string{},
				ValuesFiles:    []string{},
			},
		},
		{
			name: "error when all repo sources are missing",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Metadata.Annotations = map[string]string{}
				app.Spec.Source.RepoURL = ""
				return app
			}(),
			expectError:   true,
			errorContains: "both 'rawRepository' annotation and 'spec.source.repoURL' are empty",
		},

		// --- Тесты на извлечение Setters и ValuesFiles ---
		{
			name: "extracts and sorts values files correctly",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Spec.Source.Plugin = &struct {
					Env []EnvVar `yaml:"env"`
				}{
					Env: []EnvVar{
						{Name: "WERF_VALUES_2", Value: "values/prod.yaml"},
						{Name: "WERF_VALUES_0", Value: "values/common.yaml"},
						{Name: "SOME_OTHER_VAR", Value: "ignore-me"},
						{Name: "WERF_VALUES_1", Value: "values/overlay.yaml"},
						{Name: "WERF_VALUES_INVALID", Value: "ignore-me-too"},
					},
				}
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				RepoURL:        "https://default.repo",
				Path:           ".",
				TargetRevision: "main",
				ValuesFiles:    []string{"values/common.yaml", "values/overlay.yaml", "values/prod.yaml"},
				Setters:        map[string]string{},
			},
		},
		{
			name: "extracts setters correctly",
			inputRawApp: func() rawApplication {
				app := baseRawApp()
				app.Spec.Source.Plugin = &struct {
					Env []EnvVar `yaml:"env"`
				}{
					Env: []EnvVar{
						{Name: "WERF_SET_IMAGE_TAG", Value: "global.image.tag=v1.2.3"},
						{Name: "WERF_SET_REPLICA_COUNT", Value: "frontend.replicaCount=3"},
						{Name: "WERF_SET_INVALID", Value: "no-equals-sign"},
					},
				}
				return app
			}(),
			expectedApp: Application{
				Name:           "test-app",
				RepoURL:        "https://default.repo",
				Path:           ".",
				TargetRevision: "main",
				Setters: map[string]string{
					"global.image.tag":      "v1.2.3",
					"frontend.replicaCount": "3",
				},
				ValuesFiles: []string{},
			},
		},
	}

	// Создаем логгер-пустышку, который будет использоваться во всех суб-тестах.
	// Его вывод направлен в io.Discard, чтобы не засорять вывод тестов.
	testLogger := logrus.New()
	testLogger.SetOutput(io.Discard)
	logCtx := logrus.NewEntry(testLogger)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanApp, err := newApplicationFromRaw(tc.inputRawApp, logCtx)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedApp, cleanApp)
			}
		})
	}
}
