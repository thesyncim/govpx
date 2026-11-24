//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestSSE8EntryDisabledScalar(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	for _, h := range []int{1, 2, 3, 4, 7, 8, 9, 16, 31, 63, 64, 65} {
		for _, strides := range [][2]int{{0, 0}, {1, 3}, {8, 8}, {9, 17}, {31, 37}} {
			ss, rs := strides[0], strides[1]
			sn, rn := (h-1)*ss+8, (h-1)*rs+8
			b := make([]byte, max(3+sn, 5+rn)+17)
			for i := range b {
				b[i] = byte(i*43 + i/11)
			}
			a, r := b[3:3+sn:3+sn], b[5:5+rn:5+rn]
			before := bytes.Clone(b)
			if got, want := SSE8(a, ss, r, rs, h), SSE8Scalar(a, ss, r, rs, h); got != want || !bytes.Equal(b, before) {
				t.Fatalf("h%d strides%v got%d want%d", h, strides, got, want)
			}
		}
	}
	for _, h := range []int{0, -1} {
		if SSE8(nil, -1, nil, -1, h) != 0 {
			t.Fatal("empty geometry changed")
		}
	}
	maxInt := int(^uint(0) >> 1)
	for _, stride := range []int{-maxInt - 1, -1, 1 << 30, maxInt} {
		a, b := bytes.Repeat([]byte{255}, 8), make([]byte, 8)
		if SSE8(a, stride, b, stride, 1) != 8*255*255 {
			t.Fatal("unused stride changed")
		}
	}
}
