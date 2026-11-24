//go:build goexperiment.simd && (amd64 || arm64) && !purego

package pixel

import (
	"simd/archsimd"
	"testing"
)

func TestFPClassIndexPacking(t *testing.T) {
	if !Enabled {
		t.Skip("native backend unavailable")
	}
	for base := 0; base < 12; base++ {
		for step := 0; step < 12; step++ {
			var lanes [8]int16
			for i := range lanes {
				lanes[i] = int16((base + i*step) % 12)
			}
			got := fpClassIndices(archsimd.LoadInt16x8Array(&lanes))
			var bytes [16]byte
			got.StoreArray(&bytes)
			for i, want := range lanes {
				if bytes[i] != byte(want) {
					t.Fatalf("base%d step%d lane%d got%d want%d", base, step, i, bytes[i], want)
				}
			}
		}
	}
}
