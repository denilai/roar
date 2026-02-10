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
