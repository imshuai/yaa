package config

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
)

var (
	durationType   = reflect.TypeOf(time.Duration(0))
	jsonNumberType = reflect.TypeOf(json.Number(""))
)

type decodeIssue struct {
	path    string
	message string
}

// DecodeInto applies only fields present in raw to an already-defaulted Config.
func DecodeInto(raw map[string]any, dst *Config) error {
	if dst == nil {
		return fmt.Errorf("config decode: nil destination")
	}
	if raw == nil {
		return nil
	}
	if err := prepareDecodeTarget(raw, dst); err != nil {
		return err
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           dst,
		TagName:          "yaml",
		ZeroFields:       false,
		ErrorUnused:      true,
		WeaklyTypedInput: false,
		MatchName: func(mapKey, fieldName string) bool {
			return mapKey == fieldName
		},
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			strictScalarDecodeHook(),
			strictDurationDecodeHook(),
		),
	})
	if err != nil {
		return fmt.Errorf("config decode: %w", err)
	}
	if err := decoder.Decode(raw); err != nil {
		return fmt.Errorf("config decode: %w", err)
	}
	return nil
}

// prepareDecodeTarget validates presence-sensitive input and schedules the
// target resets that mapstructure cannot perform with ZeroFields disabled.
func prepareDecodeTarget(raw map[string]any, dst *Config) error {
	var issues []decodeIssue
	var resets []reflect.Value
	inspectDecodeValue(raw, reflect.ValueOf(dst).Elem(), "", &resets, &issues)
	if len(issues) > 0 {
		sort.Slice(issues, func(i, j int) bool {
			if issues[i].path != issues[j].path {
				return issues[i].path < issues[j].path
			}
			return issues[i].message < issues[j].message
		})
		parts := make([]string, len(issues))
		for i, issue := range issues {
			path := issue.path
			if path == "" {
				path = "<root>"
			}
			parts[i] = path + ": " + issue.message
		}
		return fmt.Errorf("config decode: %s", strings.Join(parts, "; "))
	}
	for _, value := range resets {
		if value.IsValid() && value.CanSet() {
			value.Set(reflect.Zero(value.Type()))
		}
	}
	return nil
}

func inspectDecodeValue(raw any, dst reflect.Value, path string, resets *[]reflect.Value, issues *[]decodeIssue) {
	if !dst.IsValid() {
		return
	}
	if rawIsNil(raw) {
		switch dst.Kind() {
		case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface:
			if dst.CanSet() {
				*resets = append(*resets, dst)
			}
		default:
			addDecodeIssue(issues, path, "null is not allowed for "+dst.Type().String())
		}
		return
	}

	if dst.Kind() == reflect.Interface {
		return
	}
	if dst.Kind() == reflect.Ptr {
		elem := reflect.New(dst.Type().Elem()).Elem()
		if !dst.IsNil() {
			elem = dst.Elem()
		}
		inspectDecodeValue(raw, elem, path, resets, issues)
		return
	}

	switch dst.Kind() {
	case reflect.Struct:
		if dst.Type() == durationType {
			validateDurationInput(raw, path, issues)
			return
		}
		inspectDecodeStruct(raw, dst, path, resets, issues)
	case reflect.Slice, reflect.Array:
		inspectDecodeSequence(raw, dst, path, resets, issues)
	case reflect.Map:
		inspectDecodeMap(raw, dst, path, resets, issues)
	default:
		validateDecodeScalar(raw, dst.Type(), path, issues)
	}
}

func inspectDecodeStruct(raw any, dst reflect.Value, path string, resets *[]reflect.Value, issues *[]decodeIssue) {
	entries, ok := decodeMapEntries(raw)
	if !ok {
		addDecodeIssue(issues, path, "expected object, got "+rawTypeName(raw))
		return
	}

	fields := make(map[string]reflect.Value, dst.NumField())
	for i := 0; i < dst.NumField(); i++ {
		field := dst.Type().Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, included := decodeFieldName(field)
		if !included {
			continue
		}
		fields[name] = dst.Field(i)
	}

	for _, entry := range entries {
		field, found := fields[entry.key]
		child := joinDecodePath(path, entry.key)
		if !found {
			addDecodeIssue(issues, child, "unknown field")
			continue
		}
		inspectDecodeValue(entry.value, field, child, resets, issues)
	}
}

func inspectDecodeSequence(raw any, dst reflect.Value, path string, resets *[]reflect.Value, issues *[]decodeIssue) {
	value := reflect.ValueOf(raw)
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		addDecodeIssue(issues, path, "expected array, got "+rawTypeName(raw))
		return
	}
	if dst.Kind() == reflect.Slice && dst.CanSet() {
		*resets = append(*resets, dst)
	}
	for i := 0; i < value.Len(); i++ {
		element := reflect.New(dst.Type().Elem()).Elem()
		inspectDecodeValue(value.Index(i).Interface(), element, arrayDecodePath(path, i), resets, issues)
	}
}

func inspectDecodeMap(raw any, dst reflect.Value, path string, resets *[]reflect.Value, issues *[]decodeIssue) {
	entries, ok := decodeMapEntries(raw)
	if !ok {
		addDecodeIssue(issues, path, "expected object, got "+rawTypeName(raw))
		return
	}
	if dst.Type().Key().Kind() != reflect.String {
		addDecodeIssue(issues, path, "unsupported map key type "+dst.Type().Key().String())
		return
	}
	if rawIsNil(raw) {
		if dst.CanSet() {
			*resets = append(*resets, dst)
		}
		return
	}
	if dst.Type().Elem().Kind() == reflect.Interface {
		return
	}
	for _, entry := range entries {
		element := reflect.New(dst.Type().Elem()).Elem()
		inspectDecodeValue(entry.value, element, joinDecodePath(path, entry.key), resets, issues)
	}
}

type decodeMapEntry struct {
	key   string
	value any
}

func decodeMapEntries(raw any) ([]decodeMapEntry, bool) {
	value := reflect.ValueOf(raw)
	if !value.IsValid() || value.Kind() != reflect.Map {
		return nil, false
	}
	entries := make([]decodeMapEntry, 0, value.Len())
	for _, rawKey := range value.MapKeys() {
		key := indirectDecodeValue(rawKey)
		if !key.IsValid() || key.Kind() != reflect.String {
			return nil, false
		}
		item := value.MapIndex(rawKey)
		if !item.IsValid() {
			return nil, false
		}
		entries = append(entries, decodeMapEntry{key: key.String(), value: item.Interface()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	return entries, true
}

func indirectDecodeValue(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func decodeFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("yaml")
	name := strings.SplitN(tag, ",", 2)[0]
	if name == "-" {
		return "", false
	}
	if name == "" {
		return field.Name, true
	}
	return name, true
}

func validateDecodeScalar(raw any, target reflect.Type, path string, issues *[]decodeIssue) {
	value := indirectDecodeValue(reflect.ValueOf(raw))
	if !value.IsValid() {
		addDecodeIssue(issues, path, "null is not allowed for "+target.String())
		return
	}

	switch target.Kind() {
	case reflect.String:
		if value.Type() == jsonNumberType || value.Kind() != reflect.String {
			addDecodeIssue(issues, path, "expected string, got "+rawTypeName(raw))
		}
	case reflect.Bool:
		if value.Kind() != reflect.Bool && !(value.Kind() == reflect.String && value.Type() != jsonNumberType && canParseBool(value.String())) {
			addDecodeIssue(issues, path, "expected bool, got "+rawTypeName(raw))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if target == durationType {
			validateDurationInput(raw, path, issues)
			return
		}
		if !validateSignedInput(value, target.Bits()) {
			addDecodeIssue(issues, path, "expected integer, got "+rawTypeName(raw))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if !validateUnsignedInput(value, target.Bits()) {
			addDecodeIssue(issues, path, "expected unsigned integer, got "+rawTypeName(raw))
		}
	case reflect.Float32, reflect.Float64:
		if !validateFloatInput(value, target.Bits()) {
			addDecodeIssue(issues, path, "expected number, got "+rawTypeName(raw))
		}
	default:
		if !value.Type().AssignableTo(target) {
			addDecodeIssue(issues, path, "expected "+target.String()+", got "+rawTypeName(raw))
		}
	}
}

func validateSignedInput(value reflect.Value, bits int) bool {
	if value.Type() == jsonNumberType {
		_, err := strconv.ParseInt(string(value.Interface().(json.Number)), 10, bits)
		return err == nil
	}
	switch value.Kind() {
	case reflect.String:
		if value.Type() == jsonNumberType {
			return false
		}
		_, err := strconv.ParseInt(value.String(), 10, bits)
		return err == nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := value.Int()
		return n >= -(int64(1)<<(bits-1)) && n <= (int64(1)<<(bits-1))-1
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		max := uint64(1)<<(bits-1) - 1
		return value.Uint() <= max
	default:
		return false
	}
}

func validateUnsignedInput(value reflect.Value, bits int) bool {
	if value.Type() == jsonNumberType {
		_, err := strconv.ParseUint(string(value.Interface().(json.Number)), 10, bits)
		return err == nil
	}
	switch value.Kind() {
	case reflect.String:
		if value.Type() == jsonNumberType {
			return false
		}
		_, err := strconv.ParseUint(value.String(), 10, bits)
		return err == nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() >= 0 && uint64(value.Int()) <= (uint64(1)<<bits)-1
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() <= (uint64(1)<<bits)-1
	default:
		return false
	}
}

func validateFloatInput(value reflect.Value, bits int) bool {
	if value.Type() == jsonNumberType {
		parsed, err := strconv.ParseFloat(string(value.Interface().(json.Number)), bits)
		return err == nil && !math.IsInf(parsed, 0)
	}
	switch value.Kind() {
	case reflect.String:
		if value.Type() == jsonNumberType {
			return false
		}
		parsed, err := strconv.ParseFloat(value.String(), bits)
		return err == nil && !math.IsInf(parsed, 0)
	case reflect.Float32, reflect.Float64:
		if bits == 32 && (value.Float() > math.MaxFloat32 || value.Float() < -math.MaxFloat32) {
			return false
		}
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return false
	}
}

func validateDurationInput(raw any, path string, issues *[]decodeIssue) {
	value := indirectDecodeValue(reflect.ValueOf(raw))
	if !value.IsValid() {
		addDecodeIssue(issues, path, "null is not allowed for time.Duration")
		return
	}
	if value.Type() == durationType {
		return
	}
	if value.Kind() == reflect.String && value.Type() != jsonNumberType {
		if _, err := time.ParseDuration(value.String()); err != nil {
			addDecodeIssue(issues, path, "invalid duration: "+err.Error())
		}
		return
	}
	if numericDecodeValueIsZero(value) {
		return
	}
	addDecodeIssue(issues, path, "duration must be a string or numeric zero")
}

func strictDurationDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if to != durationType {
			return data, nil
		}
		if from == durationType {
			return data, nil
		}
		value := indirectDecodeValue(reflect.ValueOf(data))
		if !value.IsValid() {
			return data, nil
		}
		if value.Kind() == reflect.String && value.Type() != jsonNumberType {
			return time.ParseDuration(value.String())
		}
		if numericDecodeValueIsZero(value) {
			return time.Duration(0), nil
		}
		return data, fmt.Errorf("duration must be a string or numeric zero")
	}
}

func strictScalarDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || from == jsonNumberType || to == durationType {
			return data, nil
		}
		text := reflect.ValueOf(data).String()
		switch to.Kind() {
		case reflect.Bool:
			return strconv.ParseBool(text)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return strconv.ParseInt(text, 10, to.Bits())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return strconv.ParseUint(text, 10, to.Bits())
		case reflect.Float32, reflect.Float64:
			return strconv.ParseFloat(text, to.Bits())
		default:
			return data, nil
		}
	}
}

func canParseBool(value string) bool {
	_, err := strconv.ParseBool(value)
	return err == nil
}

func numericDecodeValueIsZero(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	if value.Type() == jsonNumberType {
		number := value.Interface().(json.Number)
		parsed, err := strconv.ParseFloat(string(number), 64)
		return err == nil && parsed == 0
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	default:
		return false
	}
}

func rawIsNil(raw any) bool {
	if raw == nil {
		return true
	}
	value := reflect.ValueOf(raw)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func rawTypeName(raw any) string {
	if rawIsNil(raw) {
		return "null"
	}
	return reflect.TypeOf(raw).String()
}

func addDecodeIssue(issues *[]decodeIssue, path, message string) {
	*issues = append(*issues, decodeIssue{path: path, message: message})
}

func joinDecodePath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func arrayDecodePath(parent string, index int) string {
	return parent + "[" + strconv.Itoa(index) + "]"
}
