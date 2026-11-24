//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestGatherFastDeclines(t *testing.T) {
	var a, b [16]byte
	var out [256]int16
	for _, fn := range []func([]byte, int, []byte, int, []int16) bool{Gather8Fast, Gather16Fast} {
		for _, strides := range [][2]int{{-1, 0}, {0, -1}, {int(^uint(0) >> 1), 0}, {0, int(^uint(0) >> 1)}, {1 << 30, 0}, {0, 1 << 30}} {
			if fn(a[:], strides[0], b[:], strides[1], out[:]) {
				t.Fatal("invalid stride accepted")
			}
		}
		if fn(nil, 0, b[:], 0, out[:]) || fn(a[:], 0, nil, 0, out[:]) || fn(a[:], 0, b[:], 0, nil) {
			t.Fatal("nil accepted")
		}
	}
	for _, v := range out {
		if v != 0 {
			t.Fatal("decline wrote")
		}
	}
}

func TestGatherFastSelected(t *testing.T) {
	if !Enabled {
		t.Skip("SIMD unavailable")
	}
	for _, w := range []int{8, 16} {
		for _, alias := range []bool{false, true} {
			ss, ps := 32, w
			src := make([]byte, 3+(w-1)*ss+w+1)
			for i := range src {
				src[i] = byte(i*73 + 197)
			}
			pred := make([]byte, 4+(w-1)*ps+w)
			for i := range pred {
				pred[i] = byte(i*131 + 7)
			}
			if alias {
				pred = src
				ps = ss
			}
			beforeSrc, beforePred := bytes.Clone(src), bytes.Clone(pred)
			out := make([]int16, w*w+16)
			for i := range out {
				out[i] = 12345
			}
			fn := Gather16Fast
			if w == 8 {
				fn = Gather8Fast
			}
			if !fn(src[3:], ss, pred[4:], ps, out[8:]) {
				t.Fatal("eligible SIMD declined")
			}
			for y := range w {
				for x := range w {
					at := 8 + ((y/4)*(w/4)+x/4)*16 + (y%4)*4 + x%4
					want := int16(src[3+y*ss+x]) - int16(pred[4+y*ps+x])
					if out[at] != want {
						t.Fatal("selected output differs from scalar")
					}
				}
			}
			for i := range 8 {
				if out[i] != 12345 || out[len(out)-1-i] != 12345 {
					t.Fatal("output guard changed")
				}
			}
			if !bytes.Equal(src, beforeSrc) || !bytes.Equal(pred, beforePred) {
				t.Fatal("input changed")
			}
		}
	}
}
