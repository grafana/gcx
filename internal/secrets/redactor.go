package secrets

import (
	"reflect"
	"strings"
)

const (
	dataPolicyTag = "datapolicy"
	redacted      = "**REDACTED**"
)

func Redact[V any](value *V) error {
	return redactSecrets(reflect.ValueOf(value), false)
}

func redactSecrets(curr reflect.Value, redact bool) error {
	if !curr.IsValid() {
		return nil
	}

	actualCurrValue := curr
	if curr.Kind() == reflect.Pointer {
		actualCurrValue = curr.Elem()
	}

	switch actualCurrValue.Kind() {
	case reflect.Map:
		for _, k := range actualCurrValue.MapKeys() {
			v := actualCurrValue.MapIndex(k)
			// Map values obtained via MapIndex are not addressable.
			// For string values, redact in-place via SetMapIndex rather than SetString.
			if v.Kind() == reflect.String {
				if redact && !v.IsZero() {
					actualCurrValue.SetMapIndex(k, reflect.ValueOf(redacted))
				}
				continue
			}
			if err := redactSecrets(v, redact); err != nil {
				return err
			}
		}
		return nil

	case reflect.String:
		if redact && !actualCurrValue.IsZero() {
			actualCurrValue.SetString(redacted)
		}
		return nil

	case reflect.Slice:
		if actualCurrValue.Type() == reflect.TypeFor[[]byte]() && redact {
			if !actualCurrValue.IsNil() {
				actualCurrValue.SetBytes([]byte(redacted))
			}
			return nil
		}
		for i := range actualCurrValue.Len() {
			err := redactSecrets(actualCurrValue.Index(i), false)
			if err != nil {
				return err
			}
		}
		return nil

	case reflect.Struct:
		for fieldIndex := range actualCurrValue.NumField() {
			currFieldValue := actualCurrValue.Field(fieldIndex)
			currFieldType := actualCurrValue.Type().Field(fieldIndex)
			policyTag := currFieldType.Tag.Get(dataPolicyTag)
			policy := strings.Split(policyTag, ",")[0]

			if policy == "secret" {
				err := redactSecrets(currFieldValue, true)
				if err != nil {
					return err
				}
			} else {
				err := redactSecrets(currFieldValue, false)
				if err != nil {
					return err
				}
			}
		}
		return nil

	default:
		return nil
	}
}
