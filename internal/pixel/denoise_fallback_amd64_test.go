//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestDenoiseBytesRuntimeFallback(t *testing.T) {
	original := Enabled
	Enabled = false
	defer func() { Enabled = original }()
	for _, width := range []int{8, 16} {
		for inc := range 2 {
			for _, level := range []int{3, 4, 5} {
				mc, sig := make([]byte, width*width), make([]byte, width*width)
				for i := range mc {
					mc[i], sig[i] = byte(i*37+inc), byte(i*19+level)
				}
				got, want := bytes.Repeat([]byte{71}, width*width), bytes.Repeat([]byte{71}, width*width)
				expected := DenoiseFirstScalar(mc, width, want, width, sig, width, width, inc, level)
				actual := DenoiseFirst(mc, width, got, width, sig, width, width, inc, level)
				if actual != expected || !bytes.Equal(got, want) {
					t.Fatalf("width%d inc%d level%d sum%d/%d", width, inc, level, actual, expected)
				}
			}
		}
	}
}
