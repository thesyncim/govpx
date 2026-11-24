//go:build goexperiment.simd && amd64 && !purego

package pixel

import "testing"

func TestGatherFastDisabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	var a, b [16]byte
	var out [256]int16
	if Gather8Fast(a[:], 0, b[:], 0, out[:]) || Gather16Fast(a[:], 0, b[:], 0, out[:]) {
		t.Fatal("disabled backend accepted")
	}
}
