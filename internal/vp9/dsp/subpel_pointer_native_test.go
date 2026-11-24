package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var subpelPointerNativeSink uint32

// This oracle evaluates each bilinear pixel independently with integer scalar
// equations, retaining both rounding steps before the variance accumulation.
func subpelPointerOracle(src []byte, ss int, ref []byte, rs, w, h, x, y int) (uint32, uint32) {
	sum, sse := int64(0), uint32(0)
	fx, fy := 16*x, 16*y
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			a := (int(src[row*ss+col])*(128-fx) + int(src[row*ss+col+1])*fx + 64) >> 7
			b := (int(src[(row+1)*ss+col])*(128-fx) + int(src[(row+1)*ss+col+1])*fx + 64) >> 7
			d := ((a*(128-fy) + b*fy + 64) >> 7) - int(ref[row*rs+col])
			sum += int64(d)
			sse += uint32(d * d)
		}
	}
	return sse - uint32(sum*sum/int64(w*h)), sse
}

func BenchmarkVP9SubpelPointerNative(b *testing.B) {
	for _, tc := range []struct {
		w, h int
		fn   func([]byte, int, int, int, int, []byte, int, int, *uint32) uint32
	}{
		{16, 8, VpxSubPixelVariance16x8}, {16, 16, VpxSubPixelVariance16x16},
		{32, 16, VpxSubPixelVariance32x16}, {16, 32, VpxSubPixelVariance16x32},
	} {
		for _, phase := range [][2]int{{0, 0}, {3, 0}, {0, 5}, {4, 4}, {3, 5}} {
			for _, padded := range []bool{false, true} {
				b.Run(fmt.Sprintf("%dx%d/phase%d-%d/padded%v", tc.w, tc.h, phase[0], phase[1], padded), func(b *testing.B) {
					ss, rs := tc.w+1, tc.w
					if padded {
						ss, rs = tc.w+17, tc.w+25
					}
					sn, rn := tc.h*ss+tc.w+1, (tc.h-1)*rs+tc.w
					src, ref := make([]byte, 3+sn+13), make([]byte, 5+rn+13)
					for i := range src {
						src[i] = byte(i*37 + i/11 + 19)
					}
					for i := range ref {
						ref[i] = byte(i*53 + i/7 + 23)
					}
					beforeSrc, beforeRef := bytes.Clone(src), bytes.Clone(ref)
					s, r := src[3:3+sn], ref[5:5+rn]
					want, wantSSE := subpelPointerOracle(s, ss, r, rs, tc.w, tc.h, phase[0], phase[1])
					var got, gotSSE uint32
					fn := tc.fn
					b.ReportAllocs()
					for b.Loop() {
						got = fn(s, 0, ss, phase[0], phase[1], r, 0, rs, &gotSSE)
					}
					if got != want || gotSSE != wantSSE {
						b.Fatalf("got %d,%d want %d,%d", got, gotSSE, want, wantSSE)
					}
					if !bytes.Equal(src, beforeSrc) || !bytes.Equal(ref, beforeRef) {
						b.Fatal("read-only input changed")
					}
					subpelPointerNativeSink = got ^ gotSSE
				})
			}
		}
	}
}
