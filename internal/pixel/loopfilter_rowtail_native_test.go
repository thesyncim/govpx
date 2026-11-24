//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"fmt"
	"simd/archsimd"
	"testing"
)

func rowTailFixture(pitch, align, pattern int) ([]byte, []byte, [17]archsimd.Uint8x16) {
	span := 15*pitch + 20
	got := bytes.Repeat([]byte{0xa7}, align+span+19)
	want := bytes.Clone(got)
	var cols [17]archsimd.Uint8x16
	for x := range 17 {
		var lanes [16]byte
		for y := range lanes {
			v := byte(16*x + y)
			if pattern == 1 {
				v = 128
			} else if pattern > 1 {
				v = byte(x*71 + y*37 + pattern*13)
			}
			lanes[y] = v
			want[align+y*pitch+x+1] = v
		}
		cols[x] = archsimd.LoadUint8x16Array(&lanes)
	}
	return got, want, cols
}

func rowTailStore(view []byte, pitch int, c *[17]archsimd.Uint8x16) {
	storeLFRowValues(view, pitch, c[0], c[1], c[2], c[3], c[4], c[5], c[6], c[7], c[8], c[9], c[10], c[11], c[12], c[13], c[14], c[15], c[16])
}

func TestLFRowTailFullBacking(t *testing.T) {
	for _, pitch := range []int{20, 21, 32, 65, 1312} {
		for align := range 16 {
			for pattern := range 12 {
				got, want, c := rowTailFixture(pitch, align, pattern)
				span := 15*pitch + 20
				rowTailStore(got[align:align+span:align+span], pitch, &c)
				if !bytes.Equal(got, want) {
					t.Fatalf("pitch%d align%d pattern%d", pitch, align, pattern)
				}
			}
		}
	}
}

func BenchmarkLFRowTailNative(b *testing.B) {
	for _, pitch := range []int{32, 1312} {
		for _, align := range []int{0, 7} {
			for pattern := range 3 {
				b.Run(fmt.Sprintf("pitch%d/align%d/pattern%d", pitch, align, pattern), func(b *testing.B) {
					got, want, c := rowTailFixture(pitch, align, pattern)
					span := 15*pitch + 20
					view := got[align : align+span : align+span]
					rowTailStore(view, pitch, &c)
					if !bytes.Equal(got, want) {
						b.Fatal("pre backing oracle")
					}
					b.ReportAllocs()
					for b.Loop() {
						rowTailStore(view, pitch, &c)
					}
					if !bytes.Equal(got, want) {
						b.Fatal("post backing oracle")
					}
				})
			}
		}
	}
}
