package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertHTTPtoSSH(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
		wantURL  string
		wantErr  bool
	}{
		{
			name:     "standard https url",
			inputURL: "https://gitlab.com/my-org/my-repo.git",
			wantURL:  "git@gitlab.com:my-org/my-repo.git",
			wantErr:  false,
		},
		{
			name:     "url is already ssh",
			inputURL: "git@github.com:another-org/another-repo.git",
			wantURL:  "git@github.com:another-org/another-repo.git",
			wantErr:  false,
		},
		{
			name:     "http url without .git suffix",
			inputURL: "http://gitserver.internal/project/repo",
			wantURL:  "git@gitserver.internal:project/repo",
			wantErr:  false,
		},
		{
			name:     "malformed url",
			inputURL: "://some-broken-url",
			wantURL:  "",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := convertHTTPtoSSH(tt.inputURL)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantURL, gotURL)
			}
		})
	}
}

func TestApplyMirrorTransform(t *testing.T) {
	tests := []struct {
		name            string
		inputRepoURL    string
		inputPath       string
		wantRepoURL     string
		wantPath        string
		wantTransformed bool
	}{
		{
			name:            "standard mirror URL with .git",
			inputRepoURL:    "https://git.nvfn.ru/deploy/a/b/c.git",
			inputPath:       "service",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/a/b/c/service",
			wantTransformed: true,
		},
		{
			name:            "mirror URL without .git suffix",
			inputRepoURL:    "https://git.nvfn.ru/deploy/myproject",
			inputPath:       "app",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/myproject/app",
			wantTransformed: true,
		},
		{
			name:            "mirror URL with empty path",
			inputRepoURL:    "https://git.nvfn.ru/deploy/project.git",
			inputPath:       "",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/project",
			wantTransformed: true,
		},
		{
			name:            "mirror URL with dot path",
			inputRepoURL:    "https://git.nvfn.ru/deploy/project.git",
			inputPath:       ".",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/project",
			wantTransformed: true,
		},
		{
			name:            "non-mirror host - no transformation",
			inputRepoURL:    "https://gitlab.com/org/repo.git",
			inputPath:       "service",
			wantRepoURL:     "https://gitlab.com/org/repo.git",
			wantPath:        "service",
			wantTransformed: false,
		},
		{
			name:            "mirror host but wrong path prefix - no transformation",
			inputRepoURL:    "https://git.nvfn.ru/other/project.git",
			inputPath:       "service",
			wantRepoURL:     "https://git.nvfn.ru/other/project.git",
			wantPath:        "service",
			wantTransformed: false,
		},
		{
			name:            "ssh URL - no transformation",
			inputRepoURL:    "git@git.nvfn.ru:deploy/project.git",
			inputPath:       "service",
			wantRepoURL:     "git@git.nvfn.ru:deploy/project.git",
			wantPath:        "service",
			wantTransformed: false,
		},
		// git.octe.cc/service/* mirror (path gets stable/ prefix like nvfn.ru)
		{
			name:            "octe.cc service mirror with .git",
			inputRepoURL:    "https://git.octe.cc/service/stable/myservice.git",
			inputPath:       ".",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/stable/myservice",
			wantTransformed: true,
		},
		{
			name:            "octe.cc service mirror path without stable in URL",
			inputRepoURL:    "https://git.octe.cc/service/mrkt/search-engine.git",
			inputPath:       ".",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/mrkt/search-engine",
			wantTransformed: true,
		},
		{
			name:            "octe.cc service mirror with subpath",
			inputRepoURL:    "https://git.octe.cc/service/stable/myapp.git",
			inputPath:       ".helm",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/stable/myapp/.helm",
			wantTransformed: true,
		},
		{
			name:            "octe.cc wrong path prefix - no transformation",
			inputRepoURL:    "https://git.octe.cc/other/stable/foo.git",
			inputPath:       ".",
			wantRepoURL:     "https://git.octe.cc/other/stable/foo.git",
			wantPath:        ".",
			wantTransformed: false,
		},
		// Дополнительные граничные случаи
		{
			name:            "octe.cc service mirror empty path",
			inputRepoURL:    "https://git.octe.cc/service/stable/foo.git",
			inputPath:       "",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/stable/foo",
			wantTransformed: true,
		},
		{
			name:            "octe.cc service mirror without .git",
			inputRepoURL:    "https://git.octe.cc/service/mrkt/svc",
			inputPath:       "chart",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/mrkt/svc/chart",
			wantTransformed: true,
		},
		{
			name:            "octe.cc service mirror deep path",
			inputRepoURL:    "https://git.octe.cc/service/omni/core.git",
			inputPath:       ".helm/values.yaml",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/omni/core/.helm/values.yaml",
			wantTransformed: true,
		},
		{
			name:            "nvfn.ru deploy with trailing slash in path segment",
			inputRepoURL:    "https://git.nvfn.ru/deploy/svc.git",
			inputPath:       "subdir",
			wantRepoURL:     "https://git.uis.dev/deploy/product.git",
			wantPath:        "stable/svc/subdir",
			wantTransformed: true,
		},
		{
			name:            "invalid URL returns unchanged",
			inputRepoURL:    "not-a-valid-url",
			inputPath:       "x",
			wantRepoURL:     "not-a-valid-url",
			wantPath:        "x",
			wantTransformed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRepoURL, gotPath, gotTransformed := applyMirrorTransform(tt.inputRepoURL, tt.inputPath)
			require.Equal(t, tt.wantTransformed, gotTransformed)
			require.Equal(t, tt.wantRepoURL, gotRepoURL)
			require.Equal(t, tt.wantPath, gotPath)
		})
	}
}
