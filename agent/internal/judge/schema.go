package judge

import (
	"encoding/json"
	"fmt"
	"slices"
)

// answerSchema reads the rules from schema.json, the same contract sent to the provider.
// It supports the schema's object, array, string and number rules, and fails closed on an
// unsupported type. No model runtime or external schema dependency enters the launcher.
type answerSchema struct {
	Type                 string                  `json:"type"`
	Properties           map[string]answerSchema `json:"properties"`
	Required             []string                `json:"required"`
	AdditionalProperties bool                    `json:"additionalProperties"`
	Items                *answerSchema           `json:"items"`
	Enum                 []string                `json:"enum"`
	Minimum              *float64                `json:"minimum"`
	Maximum              *float64                `json:"maximum"`
}

func validateAnswer(raw []byte) error {
	var schema answerSchema
	if err := json.Unmarshal([]byte(Schema), &schema); err != nil {
		return fmt.Errorf("the judge answer schema is invalid: %v", err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("the judge printed no JSON answer: %v", err)
	}
	if err := schema.check(value, "answer"); err != nil {
		return fmt.Errorf("the judge answer breaks the schema: %v", err)
	}
	return nil
}

func (s answerSchema) check(value any, path string) error {
	switch s.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, name := range s.Required {
			if _, ok := object[name]; !ok {
				return fmt.Errorf("%s is missing %s", path, name)
			}
		}
		for name, value := range object {
			property, ok := s.Properties[name]
			if !ok {
				if !s.AdditionalProperties {
					return fmt.Errorf("%s has unknown property %s", path, name)
				}
				continue
			}
			if err := property.check(value, path+"."+name); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok || s.Items == nil {
			return fmt.Errorf("%s must be an array with an item schema", path)
		}
		for i, item := range array {
			if err := s.Items.check(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if len(s.Enum) > 0 && !slices.Contains(s.Enum, text) {
			return fmt.Errorf("%s must be one of %v", path, s.Enum)
		}
	case "number":
		number, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		if s.Minimum != nil && number < *s.Minimum || s.Maximum != nil && number > *s.Maximum {
			return fmt.Errorf("%s is outside the allowed range", path)
		}
	default:
		return fmt.Errorf("%s has unsupported schema type %q", path, s.Type)
	}
	return nil
}
