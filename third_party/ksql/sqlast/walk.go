package sqlast

import "reflect"

// Walk visits the compact SQL graph once per pointer. Returning false skips
// descendants. It does not expand shared expression or query trees.
func Walk(root any, visit func(any) bool) {
	type key struct {
		typ     reflect.Type
		pointer uintptr
	}
	seen := map[key]bool{}
	var walk func(reflect.Value)
	walk = func(value reflect.Value) {
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Interface {
			if !value.IsNil() {
				walk(value.Elem())
			}
			return
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			k := key{value.Type(), value.Pointer()}
			if seen[k] {
				return
			}
			seen[k] = true
		}
		if value.CanInterface() && !visit(value.Interface()) {
			return
		}
		switch value.Kind() {
		case reflect.Pointer:
			walk(value.Elem())
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				walk(value.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				walk(value.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(root))
}
