//go:build (amd64 || arm64) && !purego

package dsp

import "testing"

func TestIntra4x4SIMDWrapperInvalidWindowPanicsInScalar(t *testing.T) {
	above := make([]byte, 8)
	left := make([]byte, 4)
	dst := make([]byte, 4*4)
	const topLeft = byte(128)

	cases := []struct {
		name string
		fn   func()
	}{
		{
			name: "dc-short-above",
			fn: func() {
				intra4x4DCPredict(dst, 4, make([]byte, 3), left)
			},
		},
		{
			name: "tm-short-left",
			fn: func() {
				intra4x4TMPredict(dst, 4, above, make([]byte, 3), topLeft)
			},
		},
		{
			name: "ve-short-above",
			fn: func() {
				intra4x4VEPredict(dst, 4, make([]byte, 4), topLeft)
			},
		},
		{
			name: "he-short-left",
			fn: func() {
				intra4x4HEPredict(dst, 4, make([]byte, 3), topLeft)
			},
		},
		{
			name: "ld-short-above",
			fn: func() {
				intra4x4LDPredict(dst, 4, make([]byte, 7))
			},
		},
		{
			name: "rd-short-dst",
			fn: func() {
				intra4x4RDPredict(make([]byte, 3), 4, above, left, topLeft)
			},
		},
		{
			name: "vr-short-left",
			fn: func() {
				intra4x4VRPredict(dst, 4, above, make([]byte, 2), topLeft)
			},
		},
		{
			name: "vl-short-above",
			fn: func() {
				intra4x4VLPredict(dst, 4, make([]byte, 7))
			},
		},
		{
			name: "hd-short-above",
			fn: func() {
				intra4x4HDPredict(dst, 4, make([]byte, 2), left, topLeft)
			},
		},
		{
			name: "hu-short-left",
			fn: func() {
				intra4x4HUPredict(dst, 4, make([]byte, 3))
			},
		},
		{
			name: "tm-negative-dst-stride",
			fn: func() {
				intra4x4TMPredict(dst, -1, above, left, topLeft)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected scalar bounds panic")
				}
			}()
			tc.fn()
		})
	}
}
