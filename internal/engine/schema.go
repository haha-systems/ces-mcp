package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
)

func validateUniqueJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if err := readJSONValue(decoder); err != nil {
		return err
	}

	if _, err := decoder.Token(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}

		return fmt.Errorf("read trailing JSON: %w", err)
	}

	return fmt.Errorf("artifact contains trailing JSON")
}

func readJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := map[string]struct{}{}

		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("read object key: %w", err)
			}

			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}

			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key %q", key)
			}

			seen[key] = struct{}{}

			if err := readJSONValue(decoder); err != nil {
				return err
			}
		}

		_, err := decoder.Token()

		return err
	case '[':
		for decoder.More() {
			if err := readJSONValue(decoder); err != nil {
				return err
			}
		}

		_, err := decoder.Token()

		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func validateAgainstSchema(value any, schema map[string]any, path string) []string {
	var failures []string

	typeName, _ := schema["type"].(string)
	if typeName != "" && !matchesType(value, typeName) {
		return []string{fmt.Sprintf("%s must be %s", path, typeName)}
	}

	if enum, ok := schema["enum"].([]any); ok {
		matched := false

		for _, allowed := range enum {
			if reflect.DeepEqual(value, allowed) {
				matched = true
				break
			}
		}

		if !matched {
			return []string{fmt.Sprintf("%s has a value outside the allowed set", path)}
		}
	}

	if minimum, ok := numericValue(schema["minimum"]); ok {
		if number, valid := numericValue(value); valid && number < minimum {
			failures = append(failures, fmt.Sprintf("%s must be at least %v", path, minimum))
		}
	}

	switch object := value.(type) {
	case map[string]any:
		required := stringSlice(schema["required"])
		properties, _ := schema["properties"].(map[string]any)

		for _, field := range required {
			if _, exists := object[field]; !exists {
				failures = append(failures, fmt.Sprintf("%s.%s is required", path, field))
			}
		}

		additionalAllowed, hasAdditional := schema["additionalProperties"].(bool)

		for key, fieldValue := range object {
			child, exists := properties[key]
			if !exists {
				if hasAdditional && !additionalAllowed {
					failures = append(failures, fmt.Sprintf("%s.%s is not allowed", path, key))
				}

				continue
			}

			childSchema, _ := child.(map[string]any)
			failures = append(
				failures,
				validateAgainstSchema(fieldValue, childSchema, path+"."+key)...)
		}
	case []any:
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range object {
				failures = append(
					failures,
					validateAgainstSchema(item, itemSchema, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}

	return failures
}

func ValidateSchemaValue(value any, schema map[string]any) []string {
	return validateAgainstSchema(value, schema, "arguments")
}

func ValidateJSONDocument(data []byte, schema map[string]any) []string {
	if err := validateUniqueJSONKeys(data); err != nil {
		return []string{err.Error()}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	return validateAgainstSchema(value, schema, "document")
}

func matchesType(value any, expected string) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		if number, ok := numericValue(value); ok {
			return math.Trunc(number) == number
		}

		return false
	case "number":
		switch value.(type) {
		case json.Number, float32, float64, int, int32, int64, uint, uint32, uint64:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

func stringSlice(value any) []string {
	values, _ := value.([]any)

	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}

	return result
}

func canonicalJSON(value any) ([]byte, error) {
	var data bytes.Buffer

	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(value); err != nil {
		return nil, err
	}

	return bytes.TrimSpace(data.Bytes()), nil
}

func trimmedErrors(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}

	return result
}
