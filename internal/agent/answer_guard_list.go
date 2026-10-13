package agent

import "reflect"

// typedSliceAsList counts the elements of a typed slice such as
// []AppliedFormatFix (tool Data is built in Go, not decoded from JSON).
func typedSliceAsList(v interface{}) []interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return nil
	}
	out := make([]interface{}, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}
