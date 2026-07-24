package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// setByPath 通过点分隔路径设置配置结构体的标量叶字段。
// 只接受固定结构体路径；数组下标、动态 Map key 或非标量目标视为非法。
func setByPath(cfg *Config, path string, value any) error {
	keys := strings.Split(path, ".")
	if len(keys) == 0 || keys[0] == "" {
		return fmt.Errorf("empty flag path")
	}
	v := reflect.ValueOf(cfg).Elem()
	return navigateAndSet(v, keys, value, path)
}

// navigateAndSet 逐层导航到目标标量字段并赋值。
func navigateAndSet(v reflect.Value, keys []string, value any, fullPath string) error {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("path %s: expected struct at %s, got %s", fullPath, keys[0], v.Kind())
	}
	field, ok := lookupFieldByYAMLTag(v, keys[0])
	if !ok {
		return fmt.Errorf("path %s: unknown field %q", fullPath, keys[0])
	}
	if !field.CanSet() {
		return fmt.Errorf("path %s: field %q is not settable", fullPath, keys[0])
	}

	if len(keys) == 1 {
		return assignScalar(field, value, fullPath)
	}

	switch field.Kind() {
	case reflect.Struct:
		return navigateAndSet(field, keys[1:], value, fullPath)
	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return navigateAndSet(field, keys[1:], value, fullPath)
	default:
		return fmt.Errorf("path %s: %q is not a struct or pointer", fullPath, keys[0])
	}
}

// assignScalar 把命令行字符串值赋给标量字段，按目标类型显式转换。
func assignScalar(field reflect.Value, value any, fullPath string) error {
	target := field.Type()
	if value == nil {
		return fmt.Errorf("path %s: nil flag value", fullPath)
	}

	switch target.Kind() {
	case reflect.String:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("path %s: cannot assign %T to string", fullPath, value)
		}
		field.SetString(s)
	case reflect.Bool:
		b, ok := value.(bool)
		if ok {
			field.SetBool(b)
			return nil
		}
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("path %s: cannot assign %T to bool", fullPath, value)
		}
		v, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("path %s: parse bool %q: %w", fullPath, s, err)
		}
		field.SetBool(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if target == reflect.TypeOf(time.Duration(0)) {
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("path %s: duration expects string, got %T", fullPath, value)
			}
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("path %s: parse duration %q: %w", fullPath, s, err)
			}
			field.SetInt(int64(d))
			return nil
		}
		switch n := value.(type) {
		case int:
			field.SetInt(int64(n))
		case int64:
			field.SetInt(n)
		case string:
			parsed, err := strconv.ParseInt(n, 10, target.Bits())
			if err != nil {
				return fmt.Errorf("path %s: parse int %q: %w", fullPath, n, err)
			}
			field.SetInt(parsed)
		default:
			return fmt.Errorf("path %s: cannot assign %T to %s", fullPath, value, target.Kind())
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch n := value.(type) {
		case int:
			if n < 0 {
				return fmt.Errorf("path %s: negative value for uint", fullPath)
			}
			field.SetUint(uint64(n))
		case string:
			parsed, err := strconv.ParseUint(n, 10, target.Bits())
			if err != nil {
				return fmt.Errorf("path %s: parse uint %q: %w", fullPath, n, err)
			}
			field.SetUint(parsed)
		default:
			return fmt.Errorf("path %s: cannot assign %T to %s", fullPath, value, target.Kind())
		}
	case reflect.Float32, reflect.Float64:
		switch n := value.(type) {
		case float64:
			field.SetFloat(n)
		case string:
			parsed, err := strconv.ParseFloat(n, target.Bits())
			if err != nil {
				return fmt.Errorf("path %s: parse float %q: %w", fullPath, n, err)
			}
			field.SetFloat(parsed)
		default:
			return fmt.Errorf("path %s: cannot assign %T to %s", fullPath, value, target.Kind())
		}
	default:
		return fmt.Errorf("path %s: target %s is not a scalar leaf", fullPath, target)
	}
	return nil
}

// lookupFieldByYAMLTag 按 yaml tag 名查找字段（大小写精确匹配，与 DecodeInto 一致）。
func lookupFieldByYAMLTag(v reflect.Value, name string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag, ok := decodeFieldName(field)
		if ok && tag == name {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}
