package dsp

import (
	"bytes"
	"testing"
)

// The scalar helpers are the independent libvpx v1.16.0 port. Comparing the
// complete shared allocation also checks aliased-plane ordering and sentinels.
func TestLoopFilterUVPairScalarParity(t *testing.T) {
	cases := []struct {
		name     string
		combined func([]byte, []byte, int, byte, byte, byte)
		scalar   func([]byte, int, byte, byte, byte, int)
	}{
		{"h", LoopFilterHorizontalEdgeUV, loopFilterHorizontalEdgeScalar},
		{"v", LoopFilterVerticalEdgeUV, loopFilterVerticalEdgeScalar},
		{"mh", MBLoopFilterHorizontalEdgeUV, mbLoopFilterHorizontalEdgeScalar},
		{"mv", MBLoopFilterVerticalEdgeUV, mbLoopFilterVerticalEdgeScalar},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, stride := range []int{8, 9, 17, 64} {
				n := 7*stride + 8
				for align := range 8 {
					for _, gap := range []int{0, 1, 8, 32, n, n + 3} {
						for trial := range 12 {
							original := make([]byte, align+gap+n+7)
							for i := range original {
								original[i] = byte(i*37 + trial*19)
								if trial%2 == 0 {
									original[i] = byte(112 + (i/stride+i%stride+trial)%17)
								}
							}
							for _, p := range [][3]byte{{0, 0, 0}, {64, 32, 0}, {128, 64, 8}, {254, 255, 255}, {255, 255, 0}} {
								got, want := bytes.Clone(original), bytes.Clone(original)
								tc.combined(got[align:align+n], got[align+gap:align+gap+n], stride, p[0], p[1], p[2])
								tc.scalar(want[align:align+n], stride, p[0], p[1], p[2], 1)
								tc.scalar(want[align+gap:align+gap+n], stride, p[0], p[1], p[2], 1)
								if !bytes.Equal(got, want) {
									t.Fatalf("stride%d align%d gap%d trial%d params%v", stride, align, gap, trial, p)
								}
							}
						}
					}
				}
			}
		})
	}
}
