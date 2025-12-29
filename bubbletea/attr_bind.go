package bubbletea

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/agentflare-ai/go-xmldom"
)

type attrError struct {
	Attr  string
	Value string
	Cause error
}

func (e *attrError) Error() string {
	return fmt.Sprintf("attribute %q invalid: %v", e.Attr, e.Cause)
}

func bindAttributes(el xmldom.Element, dst any) error {
	if dst == nil {
		return fmt.Errorf("destination cannot be nil")
	}
	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("destination must be a non-nil pointer")
	}
	value = value.Elem()
	if value.Kind() != reflect.Struct {
		return fmt.Errorf("destination must point to a struct")
	}

	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		attr := strings.TrimSpace(field.Tag.Get("attr"))
		if attr == "" || attr == "-" {
			continue
		}

		raw := strings.TrimSpace(string(el.GetAttribute(xmldom.DOMString(attr))))
		if raw == "" {
			if def := strings.TrimSpace(field.Tag.Get("default")); def != "" {
				raw = def
			} else {
				continue
			}
		}

		fv := value.Field(i)
		if !fv.CanSet() {
			continue
		}
		if err := setFieldValue(fv, raw); err != nil {
			return &attrError{Attr: attr, Value: raw, Cause: err}
		}
	}
	return nil
}

var durationType = reflect.TypeOf(time.Duration(0))

func setFieldValue(fv reflect.Value, raw string) error {
	if fv.Type() == durationType {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		fv.SetInt(int64(parsed))
		return nil
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
		return nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(parsed)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(parsed)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(parsed)
		return nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(parsed)
		return nil
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type %s", fv.Type().String())
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
		fv.Set(reflect.ValueOf(out))
		return nil
	default:
		return fmt.Errorf("unsupported field type %s", fv.Type().String())
	}
}
