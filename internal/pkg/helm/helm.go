// /roar/internal/pkg/helm/helm.go
package helm

import (
	"bytes"
	"fmt"
	"os/exec"
	"roar/internal/pkg/logger"
	"strings"
)

type RenderOptions struct {
	ReleaseName string
	ChartPath   string
	ValuesFiles []string
	SetValues   map[string]string
}

func Template(opts RenderOptions) ([]byte, error) {
	args := []string{"template"}
	if opts.ReleaseName != "" {
		args = append(args, opts.ReleaseName)
	}
	args = append(args, opts.ChartPath)
	for _, valuesFile := range opts.ValuesFiles {
		args = append(args, "--values", valuesFile)
	}
	for key, value := range opts.SetValues {
		setValue := strings.Join([]string{key, value}, "=")
		args = append(args, "--set", setValue)
	}
	cmd := exec.Command("helm", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	logger.Log.WithField("cmd", cmd.String()).Info("[CMD]")
	err := cmd.Run()
	if stderr.Len() > 0 {
		logger.Log.Warn(strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return nil, fmt.Errorf("helm template failed: %w\nStderr:\n%s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
