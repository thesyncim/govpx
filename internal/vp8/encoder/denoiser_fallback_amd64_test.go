//go:build goexperiment.simd && amd64 && !purego

package encoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

func TestDenoiserCompleteRuntimeFallback(t *testing.T) {
	original := pixel.Enabled
	pixel.Enabled = false
	defer func() { pixel.Enabled = original }()
	mc, sig, avg := bytes.Repeat([]byte{128}, 256), bytes.Repeat([]byte{124}, 256), bytes.Repeat([]byte{71}, 256)
	wantSig, wantAvg := bytes.Clone(sig), bytes.Clone(avg)
	if _, handled := pixel.DenoiseLuma16(mc, 16, avg, 16, sig, 16, 1, 5, 600); handled {
		t.Fatal("disabled SIMD path accepted")
	}
	if !bytes.Equal(sig, wantSig) || !bytes.Equal(avg, wantAvg) {
		t.Fatal("disabled SIMD path wrote")
	}
	want := DenoiserFilterYScalar(mc, 16, wantAvg, 16, wantSig, 16, 0, true)
	got := DenoiserFilterY(mc, 16, avg, 16, sig, 16, 0, true)
	if got != want || !bytes.Equal(sig, wantSig) || !bytes.Equal(avg, wantAvg) {
		t.Fatal("runtime scalar fallback differs")
	}
}
