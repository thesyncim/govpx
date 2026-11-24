package dsp

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"slices"
	"testing"
)

var dsp09rPRSink int

func dsp09rPRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func dsp09rPRDirectionalRef(mode int, d []byte, ds int, a, l []byte, t byte) {
	switch mode {
	case 2:
		intra4x4VEPredictScalar(d, ds, a, t)
	case 3:
		intra4x4HEPredictScalar(d, ds, l, t)
	case 4:
		intra4x4LDPredictScalar(d, ds, a)
	case 5:
		intra4x4RDPredictScalar(d, ds, a, l, t)
	case 6:
		intra4x4VRPredictScalar(d, ds, a, l, t)
	case 7:
		intra4x4VLPredictScalar(d, ds, a)
	case 8:
		intra4x4HDPredictScalar(d, ds, a, l, t)
	case 9:
		intra4x4HUPredictScalar(d, ds, l)
	}
}
func dsp09rPRDirectionalFrozen(mode int, d []byte, ds int, a, l []byte, t byte) {
	if !pixel.Enabled {
		dsp09rPRDirectionalRef(mode, d, ds, a, l, t)
		return
	}
	var border [16]byte
	copy(border[:8], a)
	copy(border[8:12], l)
	border[12] = t
	pixel.Intra4(d, ds, &border, &intra4Selectors[mode-2])
}
func TestDSP09RPRDirectional(t *testing.T) {
	for k := 0; k < 100; k++ {
		a, l := dsp09rPRData(8), dsp09rPRData(4)
		for i := range a {
			a[i] += byte(k * 11)
		}
		for i := range l {
			l[i] += byte(k * 31)
		}
		for mode := 2; mode < 10; mode++ {
			x, z := dsp09rPRData(4*24), dsp09rPRData(4*24)
			Intra4x4Predict(x, 24, common.BPredictionMode(mode), a, l, byte(k))
			dsp09rPRDirectionalRef(mode, z, 24, a, l, byte(k))
			if !slices.Equal(x, z) {
				t.Fatalf("mode%d k%d", mode, k)
			}
		}
	}
}
func TestDSP09RPRDirectionalAlias(t *testing.T) {
	for mode := 2; mode < 10; mode++ {
		for _, ds := range []int{0, 1, 3, 4, 24} {
			for _, off := range []int{0, 1, 4, 8, 31} {
				x, z := dsp09rPRData(256), dsp09rPRData(256)
				a, l := x[off:off+8], x[off+1:off+5]
				c, e := z[off:off+8], z[off+1:off+5]
				Intra4x4Predict(x, ds, common.BPredictionMode(mode), a, l, 127)
				dsp09rPRDirectionalFrozen(mode, z, ds, c, e, 127)
				if !slices.Equal(x, z) {
					t.Fatalf("mode%d stride%d offset%d", mode, ds, off)
				}
			}
		}
	}
}

func BenchmarkDSP09RPRDirectional(b *testing.B) {
	for mode := 2; mode < 10; mode++ {
		b.Run(fmt.Sprintf("mode%d", mode), func(b *testing.B) {
			a, l, d := dsp09rPRData(8), dsp09rPRData(4), dsp09rPRData(4*24)
			b.ReportAllocs()
			for b.Loop() {
				Intra4x4Predict(d, 24, common.BPredictionMode(mode), a, l, 127)
			}
			dsp09rPRSink = int(d[0])
		})
	}
}
