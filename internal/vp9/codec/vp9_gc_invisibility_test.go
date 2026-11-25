package codec

import (
	"reflect"
	"testing"
)

func TestVP9MLPartitionContextHasZeroPointers(t *testing.T) {
	tp := reflect.TypeFor[vp9MLPartitionContext]()
	for i := range tp.NumField() {
		f := tp.Field(i)
		switch f.Type.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.UnsafePointer:
			t.Fatalf("vp9MLPartitionContext.%s has pointer kind %s", f.Name, f.Type.Kind())
		}
	}
}
