package config

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func SetValue[V any](input *V, path string, value string) error {
	pathParts := strings.Split(path, ".")

	return updateValue(reflect.ValueOf(input), pathParts, value, false)
}

func UnsetValue[V any](input *V, path string) error {
	pathParts := strings.Split(path, ".")

	return updateValue(reflect.ValueOf(input), pathParts, "", true)
}

//nolint:gocyclo,maintidx
func updateValue(input reflect.Value, path []string, value string, unset bool) error {
	// Just don't want to deal with pointers later.
	actualInput := input
	if input.Kind() == reflect.Pointer {
		actualInput = input.Elem()
	}

	// TODO: handle more Kinds as needed by the config structs
	switch actualInput.Kind() {
	case reflect.Struct:
		if len(path) == 0 && unset {
			actualInput.Set(reflect.New(actualInput.Type()).Elem())
			return nil
		}

		if len(path) == 0 {
			return errors.New("can not set struct")
		}

		step := path[0]
		path = path[1:]

		for fieldIndex := range actualInput.NumField() {
			field := actualInput.Field(fieldIndex)
			fieldType := actualInput.Type().Field(fieldIndex)
			yamlName := strings.Split(fieldType.Tag.Get("yaml"), ",")[0]

			if yamlName != step {
				continue
			}

			if len(path) == 0 && unset {
				newValue := reflect.New(field.Type()).Elem()
				field.Set(newValue)
				return nil
			}

			if field.Kind() == reflect.Map && field.IsNil() {
				newValue := reflect.MakeMap(field.Type())
				field.Set(newValue)
			}

			if field.Kind() == reflect.Pointer && field.IsNil() {
				newValue := reflect.New(field.Type().Elem())
				field.Set(newValue)
			}

			return updateValue(field, path, value, unset)
		}

		return fmt.Errorf("unable to locate path %#v under %s", step, actualInput.Type())
	case reflect.Map:
		if len(path) == 0 {
			return errors.New("can not set map")
		}

		step := path[0]
		path = path[1:]

		mapKey := reflect.ValueOf(step)
		currMapValue := actualInput.MapIndex(mapKey)

		if len(path) == 0 && unset {
			actualInput.SetMapIndex(mapKey, reflect.Value{})
			return nil
		}

		// Handle leaf string values in maps directly via SetMapIndex,
		// since map values obtained via MapIndex are not addressable.
		if len(path) == 0 && actualInput.Type().Elem().Kind() == reflect.String {
			if unset {
				actualInput.SetMapIndex(mapKey, reflect.Value{})
				return nil
			}
			actualInput.SetMapIndex(mapKey, reflect.ValueOf(value))
			return nil
		}

		mapEntryDoesNotExist := currMapValue.Kind() == reflect.Invalid
		if mapEntryDoesNotExist {
			if unset {
				// Nothing to unset; don't create an empty entry as a side effect.
				return nil
			}
			elemType := actualInput.Type().Elem()
			switch elemType.Kind() {
			case reflect.Pointer:
				currMapValue = reflect.New(elemType.Elem()).Elem().Addr()
			case reflect.Map:
				currMapValue = reflect.MakeMap(elemType)
			default:
				currMapValue = reflect.New(elemType).Elem()
			}
			actualInput.SetMapIndex(mapKey, currMapValue)
		}

		// Guard: a map entry may exist but hold a nil map (e.g. `providers.slo: null`
		// in YAML). SetMapIndex on a nil map panics, so handle this explicitly.
		if currMapValue.Kind() == reflect.Map && currMapValue.IsNil() {
			if unset {
				// Nothing to unset inside a nil map; leave the entry as-is.
				return nil
			}
			// Initialize a new map, reattach it to the parent, then proceed.
			newMap := reflect.MakeMap(currMapValue.Type())
			actualInput.SetMapIndex(mapKey, newMap)
			currMapValue = newMap
		}

		// For nested maps, operate on the value and then re-set the map index
		// to ensure changes propagate for non-reference value types.
		err := updateValue(currMapValue, path, value, unset)
		if err != nil {
			return err
		}

		// Re-set map index to propagate changes for value types that are copies.
		// For maps (reference types), this is a no-op but harmless.
		actualInput.SetMapIndex(mapKey, currMapValue)
		return nil
	case reflect.String:
		if len(path) != 0 {
			return fmt.Errorf("more steps after string: %s", strings.Join(path, "."))
		}

		if unset {
			actualInput.SetString("")
			return nil
		}

		actualInput.SetString(value)
	case reflect.Slice:
		if len(path) != 0 {
			return fmt.Errorf("more steps after slice: %s", strings.Join(path, "."))
		}

		// String slices (orgs, stacks, assume-server-dry-run) take
		// comma-separated values; byte slices (TLS data) take raw bytes.
		if actualInput.Type().Elem().Kind() == reflect.String {
			if unset {
				actualInput.Set(reflect.Zero(actualInput.Type()))
				return nil
			}
			var items []string
			for item := range strings.SplitSeq(value, ",") {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					items = append(items, trimmed)
				}
			}
			actualInput.Set(reflect.ValueOf(items))
			return nil
		}

		if unset {
			actualInput.SetBytes(nil)
			return nil
		}

		actualInput.SetBytes([]byte(value))
	case reflect.Bool:
		if len(path) != 0 {
			return fmt.Errorf("more steps after bool: %s", strings.Join(path, "."))
		}

		if unset {
			actualInput.SetBool(false)
			return nil
		}

		boolValue, err := toBool(value)
		if err != nil {
			return err
		}

		actualInput.SetBool(boolValue)
	case reflect.Int64:
		if len(path) != 0 {
			return fmt.Errorf("more steps after int64: %s", strings.Join(path, "."))
		}

		if unset {
			actualInput.SetInt(0)
			return nil
		}

		intValue, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("can not parse value as int64: %s", value)
		}

		actualInput.SetInt(intValue)
	default:
		return fmt.Errorf("unhandled kind %v", actualInput.Kind())
	}

	return nil
}

func toBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}

	return strconv.ParseBool(value)
}
