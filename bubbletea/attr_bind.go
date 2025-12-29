package bubbletea

import (
	"context"
	"errors"
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

var errNoDataModel = errors.New("no datamodel available for expression")

func bindAttributes(el xmldom.Element, dst any) error {
	return bindAttributesWithEval(context.Background(), el, dst, nil)
}

func bindAttributesWithEval(ctx context.Context, el xmldom.Element, dst any, eval func(context.Context, string) (any, error)) error {
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

		if exprAttr, expr := lookupExprAttribute(el, attr); exprAttr != "" {
			if eval == nil {
				return &attrError{Attr: exprAttr, Value: expr, Cause: errNoDataModel}
			}
			val, err := eval(ctx, expr)
			if err != nil {
				return &attrError{Attr: exprAttr, Value: expr, Cause: err}
			}
			if err := assignEvaluatedValue(value.Field(i), val); err != nil {
				return &attrError{Attr: exprAttr, Value: expr, Cause: err}
			}
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

func lookupExprAttribute(el xmldom.Element, attr string) (name, value string) {
	if attr == "" {
		return "", ""
	}
	candidates := []string{attr + "expr", attr + "-expr"}
	for _, candidate := range candidates {
		raw := strings.TrimSpace(string(el.GetAttribute(xmldom.DOMString(candidate))))
		if raw != "" {
			return candidate, raw
		}
	}
	return "", ""
}

func hasExprAttribute(el xmldom.Element, attr string) bool {
	name, _ := lookupExprAttribute(el, attr)
	return name != ""
}

func assignEvaluatedValue(fv reflect.Value, val any) error {
	if !fv.CanSet() {
		return nil
	}
	if val == nil {
		fv.Set(reflect.Zero(fv.Type()))
		return nil
	}

	if fv.Type() == durationType {
		switch v := val.(type) {
		case time.Duration:
			fv.SetInt(int64(v))
			return nil
		case string:
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			fv.SetInt(int64(parsed))
			return nil
		default:
			return fmt.Errorf("unsupported duration value %T", val)
		}
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(fmt.Sprintf("%v", val))
		return nil
	case reflect.Bool:
		switch v := val.(type) {
		case bool:
			fv.SetBool(v)
			return nil
		case string:
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			fv.SetBool(parsed)
			return nil
		case int, int8, int16, int32, int64:
			fv.SetBool(reflect.ValueOf(v).Int() != 0)
			return nil
		case uint, uint8, uint16, uint32, uint64:
			fv.SetBool(reflect.ValueOf(v).Uint() != 0)
			return nil
		case float32:
			fv.SetBool(v != 0)
			return nil
		case float64:
			fv.SetBool(v != 0)
			return nil
		default:
			return fmt.Errorf("unsupported bool value %T", val)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := val.(type) {
		case int, int8, int16, int32, int64:
			fv.SetInt(reflect.ValueOf(v).Int())
			return nil
		case uint, uint8, uint16, uint32, uint64:
			fv.SetInt(int64(reflect.ValueOf(v).Uint()))
			return nil
		case float32:
			fv.SetInt(int64(v))
			return nil
		case float64:
			fv.SetInt(int64(v))
			return nil
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			fv.SetInt(parsed)
			return nil
		default:
			return fmt.Errorf("unsupported int value %T", val)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch v := val.(type) {
		case uint, uint8, uint16, uint32, uint64:
			fv.SetUint(reflect.ValueOf(v).Uint())
			return nil
		case int, int8, int16, int32, int64:
			fv.SetUint(uint64(reflect.ValueOf(v).Int()))
			return nil
		case float32:
			fv.SetUint(uint64(v))
			return nil
		case float64:
			fv.SetUint(uint64(v))
			return nil
		case string:
			parsed, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return err
			}
			fv.SetUint(parsed)
			return nil
		default:
			return fmt.Errorf("unsupported uint value %T", val)
		}
	case reflect.Float32, reflect.Float64:
		switch v := val.(type) {
		case float32:
			fv.SetFloat(float64(v))
			return nil
		case float64:
			fv.SetFloat(v)
			return nil
		case int, int8, int16, int32, int64:
			fv.SetFloat(float64(reflect.ValueOf(v).Int()))
			return nil
		case uint, uint8, uint16, uint32, uint64:
			fv.SetFloat(float64(reflect.ValueOf(v).Uint()))
			return nil
		case string:
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return err
			}
			fv.SetFloat(parsed)
			return nil
		default:
			return fmt.Errorf("unsupported float value %T", val)
		}
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type %s", fv.Type().String())
		}
		switch v := val.(type) {
		case []string:
			fv.Set(reflect.ValueOf(v))
			return nil
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				out = append(out, fmt.Sprintf("%v", item))
			}
			fv.Set(reflect.ValueOf(out))
			return nil
		case string:
			return setFieldValue(fv, v)
		default:
			return fmt.Errorf("unsupported slice value %T", val)
		}
	default:
		return fmt.Errorf("unsupported field type %s", fv.Type().String())
	}
}
