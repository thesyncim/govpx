//go:build goexperiment.simd && amd64 && !purego

package pixel

import "testing"

func TestSSEPlane16Disabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	if _, ok := SSEPlane16(make([]byte, 16), 16, make([]byte, 16), 16, 16, 1); ok {
		t.Fatal("disabled backend accepted work")
	}
}
