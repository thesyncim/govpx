package dsp

import (
	"bytes"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

func TestDCPairInPlacePublicState(t *testing.T) {
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !pixel.Enabled {
		t.Fatal("required SIMD not selected")
	}
	dcPairInPlaceRunStates(t)
}

func dcPairInPlaceRunStates(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	strides := []int{0, 1, 2, 3, 4, 7, 8, 9, 15, 16, 17, 37, 1312, -1, -8, 1 << 30, maxInt, -maxInt - 1}
	values := [][2]int32{{0, 0}, {87, -99}, {2040, -2040}, {-2147483648, 2147483647}, {2147483644, -2147483645}, {99999999, -9999999}}
	checks := 0
	for _, stride := range strides {
		span := 64
		if stride >= 0 && stride <= 1312 {
			span = 3*stride + 8
		}
		for align := range 16 {
			for _, n := range []int{0, 1, 3, 4, 7, span - 1, span, span + 5} {
				for _, clipped := range []bool{false, true} {
					off := 64 + align
					initial := make([]byte, off+span+32)
					for i := range initial {
						initial[i] = byte(i*37 + i/5 + 11)
					}
					capacity := span + 7
					if clipped {
						capacity = n
					}
					for _, dc := range values {
						want := bytes.Clone(initial)
						wp := dcPairPanic(func() {
							b := want[off : off+n : off+capacity]
							dcPairOriginalReference(dc[0], dc[1], b, stride, b, stride)
						})
						calls := []func(int32, int32, []byte, int){DCOnlyIDCT4x4AddPairInPlaceInt32}
						if pixel.DCPairNative {
							calls = append(calls, pixel.DCPairInPlaceInt32Native)
						}
						for entry, call := range calls {
							got := bytes.Clone(initial)
							gp := dcPairPanic(func() { call(dc[0], dc[1], got[off:off+n:off+capacity], stride) })
							checks++
							if gp != wp || !bytes.Equal(got, want) {
								t.Fatalf("Enabled%v entry%d stride%d align%d len%d cap%d dc%v panic%q/%q", pixel.Enabled, entry, stride, align, n, capacity, dc, gp, wp)
							}
						}
					}
				}
			}
		}
	}
	t.Logf("Enabled=%v native=%v full-backing/panic checks=%d", pixel.Enabled, pixel.DCPairNative, checks)
}
