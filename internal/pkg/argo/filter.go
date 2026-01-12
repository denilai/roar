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

// Filters - коллекция критериев фильтрации
type Filters []FilterCriteria

// ParseFilters разбирает список строк фильтров
func ParseFilters(rawFilters []string) (Filters, error) {
	var filters Filters
	for _, raw := range rawFilters {
		if raw == "" {
			continue
		}
		f, err := ParseFilter(raw)
		if err != nil {
			return nil, err
		}
		filters = append(filters, *f)
	}
	return filters, nil
}

// ParseFilter разбирает одну строку вида "path.to.field==value"
func ParseFilter(filterStr string) (*FilterCriteria, error) {
	if filterStr == "" {
		return nil, nil
	}

	var op string

	if strings.Contains(filterStr, "!=") {
		op = "!="
	} else if strings.Contains(filterStr, "==") {
		op = "=="
	} else {
		return nil, fmt.Errorf("invalid filter format: operator not found (supported: ==, !=) in '%s'", filterStr)
	}

	parts := strings.SplitN(filterStr, op, 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid filter format: expected 'key%svalue'", op)
	}

	path := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	return &FilterCriteria{
		Path:     path,
		Operator: op,
		Value:    val,
	}, nil
}

// Match проверяет, соответствует ли YAML-узел критерию
func (f *FilterCriteria) Match(node *yaml.Node) bool {
	// Добавлена проверка на nil для безопасности
	if node == nil {
		return false
	}

	// Получаем значение и флаг существования поля
	nodeVal, found := getNodeValueByPath(node, f.Path)

	switch f.Operator {
	case "==":
		// При строгом равенстве поле ОБЯЗАНО существовать
		return found && nodeVal == f.Value
	case "!=":
		// При неравенстве: либо поля нет совсем, либо оно есть и не совпадает
		return !found || nodeVal != f.Value
	default:
		return true
	}
}

// MatchAll проверяет, соответствует ли YAML-узел ВСЕМ критериям в списке.
// Возвращает true, если все фильтры прошли успешно.
// Если какой-то фильтр не прошел, возвращает false и сам критерий, на котором произошел сбой.
func (fs Filters) MatchAll(node *yaml.Node) (bool, *FilterCriteria) {
	for _, f := range fs {
		if !f.Match(node) {
			return false, &f
		}
	}
	return true, nil
}

// getNodeValueByPath ищет строковое значение в yaml.Node по dot-notation пути
func getNodeValueByPath(node *yaml.Node, path string) (string, bool) {
	if node == nil {
		return "", false
	}

	parts := strings.Split(path, ".")
	current := node

	// Если это DocumentNode, переходим к его контенту (обычно MappingNode)
	if current.Kind == yaml.DocumentNode {
		if len(current.Content) == 0 {
			return "", false
		}
		current = current.Content[0]
	}

	for _, part := range parts {
		if current.Kind != yaml.MappingNode {
			return "", false
		}

		found := false
		// В MappingNode Content лежит плоско: [Key1, Val1, Key2, Val2, ...]
		for i := 0; i < len(current.Content); i += 2 {
			// Защита от выхода за границы массива (на случай битого YAML)
			if i+1 >= len(current.Content) {
				break
			}

			keyNode := current.Content[i]
			valNode := current.Content[i+1]

			if keyNode.Value == part {
				current = valNode
				found = true
				break
			}
		}

		if !found {
			return "", false
		}
	}

	return current.Value, true
}
