package dsp

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/pixel"
	"os"
	"testing"
	"unsafe"
)

func TestSADLimitX4ExactRows(t *testing.T) {
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !pixel.Enabled {
		t.Fatal("SIMD backend required")
	}
	for _, ss := range []int{0, 1, 15, 16, 19, 1312} {
		for mode := 0; mode < 3; mode++ {
			rs := ss + 3
			a := make([]byte, 3+15*ss+16)[3:]
			for i := range a {
				if mode == 0 {
					a[i] = byte(i*37 + 11)
				} else if mode == 1 {
					a[i] = 255
				}
			}
			var refs [4][]byte
			for n := range refs {
				refs[n] = make([]byte, 5+n+15*rs+16)[5+n:]
				for i := range refs[n] {
					if mode == 0 {
						refs[n][i] = byte(i*(19+n*2) + 53 + n*7)
					} else if mode == 2 {
						refs[n][i] = 255
					}
				}
			}
			saved := [][]byte{bytes.Clone(a), bytes.Clone(refs[0]), bytes.Clone(refs[1]), bytes.Clone(refs[2]), bytes.Clone(refs[3])}
			cutoffs := []int{-1, 0, 1024, 16384, 65279, 65280, 65281, 1 << 20, 1<<31 - 1, -1 << 31}
			for _, h := range []int{1, 2, 4, 8, 15} {
				for _, ref := range refs {
					v := sadLimitRowsOracle(a, ss, ref, rs, h, 1<<20)
					cutoffs = append(cutoffs, v-1, v, v+1)
				}
			}
			for _, cutoff := range cutoffs {
				limits := [4]int32{int32(cutoff), int32(cutoff + 1), int32(cutoff - 1), int32(cutoff)}
				var out, want [4]uint32
				for i := range want {
					want[i] = uint32(sadLimitRowsOracle(a, ss, refs[i], rs, 16, int(limits[i])))
				}
				SAD16x16x4LimitPtrFast(&a[0], ss, &refs[0][0], &refs[1][0], &refs[2][0], &refs[3][0], rs, &limits, &out)
				if out != want {
					t.Fatalf("stride%d mode%d cutoff%d got%v want%v", ss, mode, cutoff, out, want)
				}
			}
			for i, b := range append([][]byte{a}, refs[:]...) {
				if !bytes.Equal(b, saved[i]) {
					t.Fatal("input changed")
				}
			}
		}
	}
}

// Preserve each limit read, kernel evaluation and result store in that order.
// An earlier result can change later limits and source/reference input bytes.
func TestSADLimitX4AliasedStoreOrder(t *testing.T) {
	for _, initial := range [][4]int32{{-1, 0, 16384, 65280}, {65280, 65280, 65280, 65280}} {
		for _, offsets := range [][2]int{{150, 150}, {150, 149}, {150, 151}, {2, 150}, {260, 150}} {
			words := make([]uint32, 2048)
			for i := range words {
				words[i] = uint32(i*65537 + 1234567)
			}
			limits := (*[4]int32)(unsafe.Pointer(&words[offsets[1]]))
			*limits = initial
			expected := append([]uint32(nil), words...)
			data := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*4)
			wantData := unsafe.Slice((*byte)(unsafe.Pointer(&expected[0])), len(expected)*4)
			refs := [4]int{1024, 1026, 1031, 1040}
			wantLimits := (*[4]int32)(unsafe.Pointer(&expected[offsets[1]]))
			wantOut := (*[4]uint32)(unsafe.Pointer(&expected[offsets[0]]))
			for i, ref := range refs {
				wantOut[i] = uint32(sadLimitRowsOracle(wantData[3:], 32, wantData[ref:], 35, 16, int(wantLimits[i])))
			}
			out := (*[4]uint32)(unsafe.Pointer(&words[offsets[0]]))
			SAD16x16x4LimitPtrFast(&data[3], 32, &data[refs[0]], &data[refs[1]], &data[refs[2]], &data[refs[3]], 35, limits, out)
			if !bytes.Equal(data, wantData) {
				t.Fatalf("outword%d limitword%d changed ordered evaluation", offsets[0], offsets[1])
			}
		}
	}
}
