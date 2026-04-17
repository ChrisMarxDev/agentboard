package data

import (
	"encoding/json"
)

// InferSchema returns the inferred JSON schema for all data keys.
func (s *SQLiteStore) InferSchema() (map[string]Schema, error) {
	all, err := s.GetAll("", nil)
	if err != nil {
		return nil, err
	}

	result := make(map[string]Schema)
	for key, value := range all {
		result[key] = inferType(value)
	}
	return result, nil
}

func inferType(raw json.RawMessage) Schema {
	if len(raw) == 0 {
		return Schema{Type: "null"}
	}

	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Schema{Type: "any"}
	}

	return inferValue(v)
}

func inferValue(v interface{}) Schema {
	if v == nil {
		return Schema{Type: "null"}
	}

	switch val := v.(type) {
	case bool:
		return Schema{Type: "boolean"}
	case float64:
		return Schema{Type: "number"}
	case string:
		return Schema{Type: "string"}
	case []interface{}:
		if len(val) == 0 {
			return Schema{Type: "array"}
		}
		itemSchema := inferValue(val[0])
		return Schema{Type: "array", Items: &itemSchema}
	case map[string]interface{}:
		props := make(map[string]Schema)
		for k, child := range val {
			props[k] = inferValue(child)
		}
		return Schema{Type: "object", Properties: props}
	default:
		return Schema{Type: "any"}
	}
}
