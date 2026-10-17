//go:build goexperiment.simd && amd64 && !purego

package pixel

import "testing"

func TestGatherPointerDisabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	var a, b [16]byte
	var out [256]int16
	if Gather8PtrFast(&a[0], 0, &b[0], 0, &out[0]) || Gather16PtrFast(&a[0], 0, &b[0], 0, &out[0]) {
		t.Fatal("disabled backend accepted")
	}
}
