package providers

// Legacy parsing is deliberately isolated to the explicit offline migration.
// Registry reads, validation, API requests and immutable run snapshots use JSON.
import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/theo-bnts/aiosources/internal/model"
)

var migrationDecimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
var migrationYAMLDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func convertLegacySource(raw []byte) ([]byte, error) {
	if len(raw) > MaxDocumentBytes || !utf8.Valid(raw) {
		return nil, fmt.Errorf("legacy definition must be UTF-8 and at most 128 KiB")
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	var document, extra yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("legacy definition contains invalid YAML")
	}
	if err := decoder.Decode(&extra); err != io.EOF || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("legacy definition must contain exactly one YAML mapping")
	}
	if err := checkLegacySchema(document.Content[0], reflect.TypeOf(model.Provider{})); err != nil {
		return nil, err
	}
	value, err := legacyValue(document.Content[0], 0)
	if err != nil {
		return nil, err
	}
	// The legacy connector accepts integral float64 category IDs. Canonicalize
	// only this integer option, exactly: never round arbitrary request values.
	root := value.(map[string]any)
	if options, ok := root["options"].(map[string]any); ok {
		if categories, ok := options["local_categories"].([]any); ok {
			for index, category := range categories {
				if number, ok := category.(json.Number); ok && strings.ContainsAny(string(number), ".eE") {
					if integer, ok := exactLegacyInteger(string(number)); ok {
						categories[index] = json.Number(integer)
					}
				}
			}
		}
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("legacy definition cannot be represented as exact JSON")
	}
	return append(data, '\n'), nil
}

func legacyValue(node *yaml.Node, depth int) (any, error) {
	invalid := func() (any, error) {
		return nil, fmt.Errorf("unsupported or ambiguous legacy YAML at line %d", node.Line)
	}
	if depth > 1000 || node.Kind == yaml.AliasNode || node.Tag == "!!merge" || strings.ContainsRune(node.Value, '\x00') {
		return invalid()
	}
	switch node.Kind {
	case yaml.MappingNode:
		if node.Tag != "!!map" {
			return invalid()
		}
		object := make(map[string]any, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || strings.ContainsRune(key.Value, '\x00') {
				return invalid()
			}
			if _, exists := object[key.Value]; exists {
				return nil, fmt.Errorf("duplicate legacy YAML property at line %d", key.Line)
			}
			value, err := legacyValue(node.Content[index+1], depth+1)
			if err != nil {
				return nil, err
			}
			object[key.Value] = value
		}
		return object, nil
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return invalid()
		}
		array := make([]any, len(node.Content))
		for index, child := range node.Content {
			value, err := legacyValue(child, depth+1)
			if err != nil {
				return nil, err
			}
			array[index] = value
		}
		return array, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			var value bool
			if node.Decode(&value) != nil {
				return invalid()
			}
			return value, nil
		case "!!int":
			number := strings.ReplaceAll(node.Value, "_", "")
			integer, ok := new(big.Int).SetString(number, 0)
			if !ok {
				return invalid()
			}
			return json.Number(integer.String()), nil
		case "!!float":
			number := strings.ReplaceAll(node.Value, "_", "")
			if !migrationYAMLDecimal.MatchString(number) {
				return nil, fmt.Errorf("legacy number is not finite or cannot be represented exactly at line %d", node.Line)
			}
			number = strings.TrimPrefix(number, "+")
			mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(number), "e")
			if strings.HasPrefix(mantissa, ".") {
				mantissa = "0" + mantissa
			} else if strings.HasPrefix(mantissa, "-.") {
				mantissa = "-0" + mantissa[1:]
			}
			if strings.HasSuffix(mantissa, ".") {
				mantissa += "0"
			}
			// Leading zeros are legal YAML decimals but not JSON numbers.
			sign := ""
			if strings.HasPrefix(mantissa, "-") {
				sign, mantissa = "-", mantissa[1:]
			}
			whole, fraction, hasFraction := strings.Cut(mantissa, ".")
			whole = strings.TrimLeft(whole, "0")
			if whole == "" {
				whole = "0"
			}
			number = sign + whole
			if hasFraction {
				number += "." + fraction
			}
			if hasExponent {
				number += "e" + exponent
			}
			if !migrationDecimal.MatchString(number) {
				return nil, fmt.Errorf("legacy number is not finite or cannot be represented exactly at line %d", node.Line)
			}
			return json.Number(number), nil
		case "!!timestamp":
			var value time.Time
			if node.Decode(&value) != nil {
				return invalid()
			}
			return value.Format(time.RFC3339Nano), nil
		}
	}
	return invalid()
}

func exactLegacyInteger(number string) (string, bool) {
	// Category IDs fit int64. Bound exponents before big.Rat parsing so an
	// adversarial exponent cannot force an enormous allocation during preflight.
	if exponent := strings.IndexAny(number, "eE"); exponent >= 0 {
		value, ok := new(big.Int).SetString(number[exponent+1:], 10)
		if !ok || !value.IsInt64() || value.Int64() < -MaxDocumentBytes || value.Int64() > 19 {
			return "", false
		}
	}
	value, ok := new(big.Rat).SetString(number)
	if !ok || !value.IsInt() || !value.Num().IsInt64() {
		return "", false
	}
	return value.Num().String(), true
}

// Preserve the old schema's scalar typing before conversion. In particular a
// timestamp is not a declared string and a YAML float is not a declared int.
func checkLegacySchema(node *yaml.Node, expected reflect.Type) error {
	invalid := func() error { return fmt.Errorf("legacy schema mismatch at line %d", node.Line) }
	switch expected.Kind() {
	case reflect.Interface:
		return nil
	case reflect.Pointer:
		return checkLegacySchema(node, expected.Elem())
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return invalid()
		}
		for index := 0; index < len(node.Content); index += 2 {
			found := false
			for fieldIndex := range expected.NumField() {
				field := expected.Field(fieldIndex)
				name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name == node.Content[index].Value {
					if err := checkLegacySchema(node.Content[index+1], field.Type); err != nil {
						return err
					}
					found = true
					break
				}
			}
			if !found {
				return invalid()
			}
		}
	case reflect.Map:
		if node.Tag == "!!null" {
			return nil
		}
		if node.Kind != yaml.MappingNode {
			return invalid()
		}
		for index := 1; index < len(node.Content); index += 2 {
			if err := checkLegacySchema(node.Content[index], expected.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Tag == "!!null" {
			return nil
		}
		if node.Kind != yaml.SequenceNode {
			return invalid()
		}
		for _, child := range node.Content {
			if err := checkLegacySchema(child, expected.Elem()); err != nil {
				return err
			}
		}
	case reflect.String:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			return invalid()
		}
	case reflect.Bool:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			return invalid()
		}
	case reflect.Int:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			return invalid()
		}
	}
	return nil
}
