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

func TestParseFilters(t *testing.T) {
	// Проверка парсинга нескольких фильтров
	raw := []string{"a==1", "b!=2"}
	filters, err := ParseFilters(raw)
	require.NoError(t, err)
	require.Len(t, filters, 2)
	assert.Equal(t, "a", filters[0].Path)
	assert.Equal(t, "b", filters[1].Path)
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
    emptyval: ""
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
			name:      "nil node safety check",
			filterStr: "any==thing",
			wantMatch: false, // Должно быть false, если node nil, но в этом тесте мы передаем &node. См. отдельный тест ниже.
		},
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
			name:      "inequality missing field (should be true: missing != val)",
			filterStr: "metadata.labels.region!=us-east",
			wantMatch: true,
		},
		{
			name:      "equality missing field (should be false: missing is not equal to val)",
			filterStr: "spec.destination.server==https://kubernetes.default.svc",
			wantMatch: false,
		},
		{
			name:      "equality empty string vs missing field",
			filterStr: "missing.field==",
			wantMatch: false, // missing field is NOT equal to empty string
		},
		{
			name:      "equality empty string vs existing empty field",
			filterStr: "metadata.labels.emptyval==",
			wantMatch: true, // existing empty field IS equal to empty string
		},
		{
			name:      "deep nesting missing parent",
			filterStr: "spec.source.helm.parameters.0.name==foo",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nil node safety check" {
				// Специальный кейс для проверки nil node
				f, _ := ParseFilter(tt.filterStr)
				assert.False(t, f.Match(nil), "Should return false for nil node")
				return
			}

			f, err := ParseFilter(tt.filterStr)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMatch, f.Match(&node))
		})
	}
}

// Тест на безопасность функции getNodeValueByPath при nil
func TestGetNodeValueByPath_NilSafety(t *testing.T) {
	val, found := getNodeValueByPath(nil, "some.path")
	assert.Equal(t, "", val)
	assert.False(t, found)
}
