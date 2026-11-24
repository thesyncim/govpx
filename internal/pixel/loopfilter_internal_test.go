//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestInternalLoopFilterMinimalWindows(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	for _, stride := range []int{8, 9, 15, 16, 17, 32} {
		for _, vertical := range []bool{false, true} {
			if stride < 16 && !vertical {
				continue
			}
			step, outer := stride, 1
			if vertical {
				step, outer = 1, stride
			}
			for align := range 8 {
				for trial := range 12 {
					original := make([]byte, align+15*stride+16)
					for i := range original {
						original[i] = byte(i*37 + trial*19)
						if trial%2 == 0 {
							original[i] = byte(112 + (i/stride+i%stride+trial)%17)
						}
					}
					for _, blimit := range []int{0, 64, 254, 255} {
						got, want := bytes.Clone(original), bytes.Clone(original)
						filtered := LoopFilterInternal16(got[align:], step, outer, blimit, 32, 0)
						if blimit == 255 {
							if filtered || !bytes.Equal(got, original) {
								t.Fatal("255 boundary must fall back without writes")
							}
							continue
						}
						if !filtered {
							t.Fatal("native dispatch not selected")
						}
						for edge := 1; edge <= 3; edge++ {
							LoopFilter(want[align:], 4*edge*step, step, outer, 16, blimit, 32, 0, 4)
						}
						if !bytes.Equal(got, want) {
							t.Fatalf("stride%d vertical%v align%d trial%d blimit%d", stride, vertical, align, trial, blimit)
						}
					}
				}
			}
		}
	}
}

func TestInternalLoopFilterRejectInvalidSpans(t *testing.T) {
	p := bytes.Repeat([]byte{128}, 256)
	saved := bytes.Clone(p)
	maxInt := int(^uint(0) >> 1)
	for _, shape := range [][2]int{{maxInt, 1}, {1, maxInt}, {16, 1}, {1, 16}} {
		source := p
		if shape[0] == 16 || shape[1] == 16 {
			source = p[:255]
		}
		if LoopFilterInternal16(source, shape[0], shape[1], 64, 32, 0) || !bytes.Equal(p, saved) {
			t.Fatalf("invalid span accepted or changed: %v", shape)
		}
	}
	if LoopFilterInternal16(nil, 16, 1, 64, 32, 0) {
		t.Fatal("nil tile accepted")
	}
}
