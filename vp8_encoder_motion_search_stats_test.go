package govpx

import (
	"reflect"
	"testing"
)

func TestInterFrameMotionSearchDefaultPathHasNoStatsField(t *testing.T) {
	cases := []reflect.Type{
		reflect.TypeOf(interFrameMotionVectorSearch{}),
		reflect.TypeOf(interFrameSubpixelSearch{}),
	}
	for _, typ := range cases {
		if _, ok := typ.FieldByName("stats"); ok {
			t.Fatalf("%s carries stats field in default search path", typ.Name())
		}
	}
}
