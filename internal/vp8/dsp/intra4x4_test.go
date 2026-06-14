package dsp

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestIntra4x4PredictModes(t *testing.T) {
	above := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	left := []byte{90, 100, 110, 120}
	const topLeft = 5

	cases := []struct {
		name string
		mode common.BPredictionMode
		want [16]byte
	}{
		{name: "dc", mode: common.BDCPred, want: [16]byte{
			65, 65, 65, 65,
			65, 65, 65, 65,
			65, 65, 65, 65,
			65, 65, 65, 65,
		}},
		{name: "tm", mode: common.BTMPred, want: [16]byte{
			95, 105, 115, 125,
			105, 115, 125, 135,
			115, 125, 135, 145,
			125, 135, 145, 155,
		}},
		{name: "ve", mode: common.BVEPred, want: [16]byte{
			11, 20, 30, 40,
			11, 20, 30, 40,
			11, 20, 30, 40,
			11, 20, 30, 40,
		}},
		{name: "he", mode: common.BHEPred, want: [16]byte{
			71, 71, 71, 71,
			100, 100, 100, 100,
			110, 110, 110, 110,
			118, 118, 118, 118,
		}},
		{name: "ld", mode: common.BLDPred, want: [16]byte{
			20, 30, 40, 50,
			30, 40, 50, 60,
			40, 50, 60, 70,
			50, 60, 70, 78,
		}},
		{name: "rd", mode: common.BRDPred, want: [16]byte{
			28, 11, 20, 30,
			71, 28, 11, 20,
			100, 71, 28, 11,
			110, 100, 71, 28,
		}},
		{name: "vr", mode: common.BVRPred, want: [16]byte{
			8, 15, 25, 35,
			28, 11, 20, 30,
			71, 8, 15, 25,
			100, 28, 11, 20,
		}},
		{name: "vl", mode: common.BVLPred, want: [16]byte{
			15, 25, 35, 45,
			20, 30, 40, 50,
			25, 35, 45, 60,
			30, 40, 50, 70,
		}},
		{name: "hd", mode: common.BHDPred, want: [16]byte{
			48, 28, 11, 20,
			95, 71, 48, 28,
			105, 100, 95, 71,
			115, 110, 105, 100,
		}},
		{name: "hu", mode: common.BHUPred, want: [16]byte{
			95, 100, 105, 110,
			105, 110, 115, 118,
			115, 118, 120, 120,
			120, 120, 120, 120,
		}},
	}

	for _, tc := range cases {
		dst := make([]byte, 4*8)
		for i := range dst {
			dst[i] = 222
		}

		if ok := Intra4x4Predict(dst, 8, tc.mode, above, left, topLeft); !ok {
			t.Fatalf("%s returned false", tc.name)
		}

		for y := range 4 {
			for x := range 4 {
				want := tc.want[y*4+x]
				if got := dst[y*8+x]; got != want {
					t.Fatalf("%s dst[%d,%d] = %d, want %d", tc.name, x, y, got, want)
				}
			}
			for x := 4; x < 8; x++ {
				if got := dst[y*8+x]; got != 222 {
					t.Fatalf("%s dst[%d,%d] = %d, want sentinel", tc.name, x, y, got)
				}
			}
		}
	}
}

func TestIntra4x4PredictInvalidMode(t *testing.T) {
	dst := make([]byte, 4*4)
	if ok := Intra4x4Predict(dst, 4, common.Above4x4, nil, nil, 0); ok {
		t.Fatalf("invalid intra mode returned true")
	}
}

func TestIntra4x4PredictInvalidWindowPanicsInScalar(t *testing.T) {
	above := make([]byte, 8)
	left := make([]byte, 4)
	dst := make([]byte, 4*4)
	const topLeft = byte(128)

	cases := []struct {
		name string
		fn   func()
	}{
		{
			name: "dc-short-dst",
			fn: func() {
				Intra4x4DCPredict(make([]byte, 3), 4, above, left)
			},
		},
		{
			name: "dc-short-above",
			fn: func() {
				Intra4x4DCPredict(dst, 4, above[:3], left)
			},
		},
		{
			name: "dc-short-left",
			fn: func() {
				Intra4x4DCPredict(dst, 4, above, left[:3])
			},
		},
		{
			name: "dc-negative-dst-stride",
			fn: func() {
				Intra4x4DCPredict(dst, -1, above, left)
			},
		},
		{
			name: "tm-short-dst",
			fn: func() {
				Intra4x4TMPredict(make([]byte, 3), 4, above, left, topLeft)
			},
		},
		{
			name: "tm-short-above",
			fn: func() {
				Intra4x4TMPredict(dst, 4, above[:3], left, topLeft)
			},
		},
		{
			name: "tm-short-left",
			fn: func() {
				Intra4x4TMPredict(dst, 4, above, left[:3], topLeft)
			},
		},
		{
			name: "tm-negative-dst-stride",
			fn: func() {
				Intra4x4TMPredict(dst, -1, above, left, topLeft)
			},
		},
		{
			name: "ve-short-dst",
			fn: func() {
				Intra4x4VEPredict(make([]byte, 3), 4, above, topLeft)
			},
		},
		{
			name: "ve-short-above",
			fn: func() {
				Intra4x4VEPredict(dst, 4, above[:4], topLeft)
			},
		},
		{
			name: "ve-negative-dst-stride",
			fn: func() {
				Intra4x4VEPredict(dst, -1, above, topLeft)
			},
		},
		{
			name: "he-short-dst",
			fn: func() {
				Intra4x4HEPredict(make([]byte, 3), 4, left, topLeft)
			},
		},
		{
			name: "he-short-left",
			fn: func() {
				Intra4x4HEPredict(dst, 4, left[:3], topLeft)
			},
		},
		{
			name: "he-negative-dst-stride",
			fn: func() {
				Intra4x4HEPredict(dst, -1, left, topLeft)
			},
		},
		{
			name: "ld-short-dst",
			fn: func() {
				Intra4x4LDPredict(make([]byte, 3), 4, above)
			},
		},
		{
			name: "ld-short-above",
			fn: func() {
				Intra4x4LDPredict(dst, 4, above[:7])
			},
		},
		{
			name: "ld-negative-dst-stride",
			fn: func() {
				Intra4x4LDPredict(dst, -1, above)
			},
		},
		{
			name: "rd-short-dst",
			fn: func() {
				Intra4x4RDPredict(make([]byte, 3), 4, above, left, topLeft)
			},
		},
		{
			name: "rd-short-above",
			fn: func() {
				Intra4x4RDPredict(dst, 4, above[:3], left, topLeft)
			},
		},
		{
			name: "rd-short-left",
			fn: func() {
				Intra4x4RDPredict(dst, 4, above, left[:3], topLeft)
			},
		},
		{
			name: "rd-negative-dst-stride",
			fn: func() {
				Intra4x4RDPredict(dst, -1, above, left, topLeft)
			},
		},
		{
			name: "vr-short-dst",
			fn: func() {
				Intra4x4VRPredict(make([]byte, 3), 4, above, left, topLeft)
			},
		},
		{
			name: "vr-short-above",
			fn: func() {
				Intra4x4VRPredict(dst, 4, above[:3], left, topLeft)
			},
		},
		{
			name: "vr-short-left",
			fn: func() {
				Intra4x4VRPredict(dst, 4, above, left[:2], topLeft)
			},
		},
		{
			name: "vr-negative-dst-stride",
			fn: func() {
				Intra4x4VRPredict(dst, -1, above, left, topLeft)
			},
		},
		{
			name: "vl-short-dst",
			fn: func() {
				Intra4x4VLPredict(make([]byte, 3), 4, above)
			},
		},
		{
			name: "vl-short-above",
			fn: func() {
				Intra4x4VLPredict(dst, 4, above[:7])
			},
		},
		{
			name: "vl-negative-dst-stride",
			fn: func() {
				Intra4x4VLPredict(dst, -1, above)
			},
		},
		{
			name: "hd-short-dst",
			fn: func() {
				Intra4x4HDPredict(make([]byte, 3), 4, above, left, topLeft)
			},
		},
		{
			name: "hd-short-above",
			fn: func() {
				Intra4x4HDPredict(dst, 4, above[:2], left, topLeft)
			},
		},
		{
			name: "hd-short-left",
			fn: func() {
				Intra4x4HDPredict(dst, 4, above, left[:3], topLeft)
			},
		},
		{
			name: "hd-negative-dst-stride",
			fn: func() {
				Intra4x4HDPredict(dst, -1, above, left, topLeft)
			},
		},
		{
			name: "hu-short-dst",
			fn: func() {
				Intra4x4HUPredict(make([]byte, 3), 4, left)
			},
		},
		{
			name: "hu-short-left",
			fn: func() {
				Intra4x4HUPredict(dst, 4, left[:3])
			},
		},
		{
			name: "hu-negative-dst-stride",
			fn: func() {
				Intra4x4HUPredict(dst, -1, left)
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

func TestIntra4x4PredictAllocatesZero(t *testing.T) {
	above := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	left := []byte{90, 100, 110, 120}
	dst := make([]byte, 4*4)
	allocs := testing.AllocsPerRun(1000, func() {
		Intra4x4Predict(dst, 4, common.BRDPred, above, left, 5)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkIntra4x4Predict(b *testing.B) {
	above := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	left := []byte{90, 100, 110, 120}
	dst := make([]byte, 4*4)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Intra4x4Predict(dst, 4, common.BRDPred, above, left, 5)
	}
}
