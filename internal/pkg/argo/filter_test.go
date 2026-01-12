package argo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantPath    string
		wantOp      string
		wantVal     string
		expectError bool
	}{
		{
			name:     "standard equality",
			input:    "spec.source.targetRevision==master",
			wantPath: "spec.source.targetRevision",
			wantOp:   "==",
			wantVal:  "master",
		},
		{
			name:     "standard inequality",
			input:    "metadata.name!=my-app",
			wantPath: "metadata.name",
			wantOp:   "!=",
			wantVal:  "my-app",
		},
		{
			name:     "spaces around operator",
			input:    "  spec.source.path  ==  .  ",
			wantPath: "spec.source.path",
			wantOp:   "==",
			wantVal:  ".",
		},
		{
			name:        "invalid operator",
			input:       "key~=value",
			expectError: true,
		},
		{
			name:        "missing value",
			input:       "key==",
			expectError: false, // Это валидно: пустая строка
			wantPath:    "key",
			wantOp:      "==",
			wantVal:     "",
		},
		{
			name:        "single equal sign (not supported)",
			input:       "key=value",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFilter(tt.input)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPath, got.Path)
				assert.Equal(t, tt.wantOp, got.Operator)
				assert.Equal(t, tt.wantVal, got.Value)
			}
		})
	}
}

func TestFilterMatch(t *testing.T) {
	yamlStr := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
  labels:
    env: production
    tier: frontend
spec:
  source:
    path: charts/my-chart
    plugin:
      name: werf
`
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &node))

	tests := []struct {
		name      string
		filterStr string
		wantMatch bool
	}{
		{
			name:      "match simple field",
			filterStr: "metadata.name==test-app",
			wantMatch: true,
		},
		{
			name:      "match nested field",
			filterStr: "spec.source.path==charts/my-chart",
			wantMatch: true,
		},
		{
			name:      "mismatch value",
			filterStr: "metadata.labels.env==dev",
			wantMatch: false,
		},
		{
			name:      "match inequality",
			filterStr: "metadata.labels.tier!=backend",
			wantMatch: true,
		},
		{
			name:      "missing field in inequality (should be true)",
			filterStr: "metadata.labels.region!=us-east",
			wantMatch: true, // "пусто" != "us-east" -> true
		},
		{
			name:      "missing field in equality (should be false)",
			filterStr: "spec.destination.server==https://kubernetes.default.svc",
			wantMatch: false,
		},
		{
			name:      "deep nesting missing parent",
			filterStr: "spec.source.helm.parameters.0.name==foo",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ParseFilter(tt.filterStr)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMatch, f.Match(&node))
		})
	}
}

