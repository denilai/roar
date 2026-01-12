package argo

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// FilterCriteria описывает разобранное условие фильтрации
type FilterCriteria struct {
	Path     string
	Operator string // "==" или "!="
	Value    string
}

// ParseFilter разбирает строку вида "path.to.field!=value"
func ParseFilter(filterStr string) (*FilterCriteria, error) {
	if filterStr == "" {
		return nil, nil
	}

	var op string
	if strings.Contains(filterStr, "!=") {
		op = "!="
	} else if strings.Contains(filterStr, "==") {
		op = "=="
	} else if strings.Contains(filterStr, "=") {
		op = "==" // поддержка одинарного равно для удобства
	} else {
		return nil, fmt.Errorf("invalid filter format: operator not found (supported: ==, !=)")
	}

	parts := strings.SplitN(filterStr, op, 2)
	path := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	return &FilterCriteria{
		Path:     path,
		Operator: op,
		Value:    val,
	}, nil
}

// MatchNode проверяет, соответствует ли YAML-узел критериям фильтра
func (f *FilterCriteria) Match(node *yaml.Node) bool {
	// Получаем значение из YAML по пути (например, "spec.source.targetRevision")
	nodeVal := getNodeValueByPath(node, f.Path)

	switch f.Operator {
	case "==":
		return nodeVal == f.Value
	case "!=":
		return nodeVal != f.Value
	default:
		return true
	}
}

// getNodeValueByPath ищет строковое значение в yaml.Node по dot-notation пути
func getNodeValueByPath(node *yaml.Node, path string) string {
	parts := strings.Split(path, ".")
	current := node

	// Если это DocumentNode, переходим к его контенту (обычно MappingNode)
	if current.Kind == yaml.DocumentNode && len(current.Content) > 0 {
		current = current.Content[0]
	}

	for _, part := range parts {
		if current.Kind != yaml.MappingNode {
			return ""
		}

		found := false
		// В MappingNode Content лежит плоско: [Key1, Val1, Key2, Val2, ...]
		for i := 0; i < len(current.Content); i += 2 {
			keyNode := current.Content[i]
			valNode := current.Content[i+1]

			if keyNode.Value == part {
				current = valNode
				found = true
				break
			}
		}

		if !found {
			return "" // Путь не найден
		}
	}

	return current.Value
}
