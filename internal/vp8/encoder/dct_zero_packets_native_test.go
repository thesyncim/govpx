package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func TestFDCTZeroLiteral(t *testing.T) {
	var c [32]int16
	var want [32]int16
	forwardDCT4x4Scalar(c[:16], 4, (*[16]int16)(want[:16]))
	forwardDCT4x4Scalar(c[16:], 4, (*[16]int16)(want[16:]))
	if want != ([32]int16{1: 1, 17: 1}) {
		t.Fatalf("unexpected scalar zero transform: %v", want)
	}
	for _, alias := range []bool{false, true} {
		got := make([]int16, 32)
		in := c[:]
		if alias {
			in = got
		}
		ForwardDCT4x4Batch(in, got, 2)
		for i, x := range got {
			if x != want[i] {
				t.Fatalf("alias%v lane%d got%d want%d", alias, i, x, want[i])
			}
		}
	}
}
func BenchmarkFDCTZeroScreen(b *testing.B) {
	for _, pattern := range []string{"zero", "sparse", "dense"} {
		b.Run(pattern, func(b *testing.B) {
			var in, out [16 * 16]int16
			for i := range in {
				if pattern == "dense" {
					in[i] = int16((i*37+11)%511 - 255)
				} else if pattern == "sparse" && i%32 == 0 {
					in[i] = 17
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				ForwardDCT4x4Batch(in[:], out[:], 16)
			}
		})
	}
}

// Preserve the original two-block load-before-store semantics on overlapping windows.
func TestFDCTZeroPacketAliasOrder(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("two-block SIMD packet ordering")
	}
	for _, count := range []int{1, 2, 3, 8, 16, 25} {
		for delta := -17; delta <= 17; delta++ {
			const off = 64
			got := make([]int16, off+25*16+80)
			for i := range got {
				got[i] = int16(0x1357)
			}
			for i := 0; i < count*16; i++ {
				got[off+i] = 0
			}
			want := append([]int16(nil), got...)
			in, out := want[off:off+count*16], want[off+delta:off+delta+count*16]
			i := 0
			for ; i+2 <= count; i += 2 {
				var stage, result [32]int16
				copy(stage[:], in[i*16:i*16+32])
				peak := 0
				for _, v := range stage {
					x := int(v)
					if x < 0 {
						x = -x
					}
					if x > peak {
						peak = x
					}
				}
				if peak > 255 {
					forwardDCT4x4Scalar(in[i*16:], 4, (*[16]int16)(out[i*16:]))
					forwardDCT4x4Scalar(in[i*16+16:], 4, (*[16]int16)(out[i*16+16:]))
				} else {
					forwardDCT4x4Scalar(stage[:16], 4, (*[16]int16)(result[:16]))
					forwardDCT4x4Scalar(stage[16:], 4, (*[16]int16)(result[16:]))
					copy(out[i*16:i*16+32], result[:])
				}
			}
			for ; i < count; i++ {
				forwardDCT4x4Scalar(in[i*16:], 4, (*[16]int16)(out[i*16:]))
			}
			ForwardDCT4x4Batch(got[off:off+count*16], got[off+delta:off+delta+count*16], count)
			for lane, x := range got {
				if x != want[lane] {
					t.Fatalf("count%d delta%d lane%d got%d want%d", count, delta, lane, x, want[lane])
				}
			}
		}
	}
}
