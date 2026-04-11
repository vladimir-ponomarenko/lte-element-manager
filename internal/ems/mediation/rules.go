package mediation

import (
	"reflect"
	"sync"

	"lte-element-manager/internal/ems/domain/canonical"
)

type FieldRule struct {
	JSONTag string
	Key     string
	Type    canonical.MetricType
	Unit    string
}

var fieldIndexCache sync.Map // map[reflect.Type]map[string]int

func ApplyFieldRules(dst map[string]canonical.Metric, src any, rules []FieldRule) {
	v := reflect.ValueOf(src)
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()
	idx := indexByJSONTag(t)

	for _, r := range rules {
		i, ok := idx[r.JSONTag]
		if !ok {
			continue
		}
		f := v.Field(i)
		val, ok := toFloat64(f)
		if !ok {
			continue
		}
		dst[r.Key] = canonical.Metric{Name: r.Key, Type: r.Type, Value: val, Unit: r.Unit}
	}
}

func indexByJSONTag(t reflect.Type) map[string]int {
	if v, ok := fieldIndexCache.Load(t); ok {
		return v.(map[string]int)
	}
	out := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		for j := 0; j < len(tag); j++ {
			if tag[j] == ',' {
				name = tag[:j]
				break
			}
		}
		if name == "" || name == "-" {
			continue
		}
		out[name] = i
	}
	fieldIndexCache.Store(t, out)
	return out
}

func toFloat64(v reflect.Value) (float64, bool) {
	if !v.IsValid() {
		return 0, false
	}
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(v.Uint()), true
	case reflect.Bool:
		if v.Bool() {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}
