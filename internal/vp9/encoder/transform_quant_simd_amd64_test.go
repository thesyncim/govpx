//go:build amd64 && !purego

package encoder

import (
	"math/rand"
	"strconv"
	"testing"
)

func TestQuantizeFPACSSE2MatchesScalar(t *testing.T) {
	roundAC, quantAC, deqAC := 5, 3855, 17
	for _, count := range []int{8, 16, 64} {
		t.Run("n"+strconv.Itoa(count), func(t *testing.T) {
			coeff := make([]int16, count)
			iscan := make([]int16, count)
			rng := rand.New(rand.NewSource(int64(count) * 97))
			for i := range coeff {
				switch i % 11 {
				case 0:
					coeff[i] = -32768
				case 1:
					coeff[i] = 32767
				case 2:
					coeff[i] = 0
				default:
					coeff[i] = int16(rng.Intn(2049) - 1024)
				}
				iscan[i] = int16(count - i)
			}

			gotQ := make([]int16, count)
			gotDQ := make([]int16, count)
			gotEOB := int(quantizeFPACSSE2(&coeff[0], &iscan[0], &gotQ[0],
				&gotDQ[0], count, roundAC, quantAC, deqAC))

			wantQ := make([]int16, count)
			wantDQ := make([]int16, count)
			wantEOB := 0
			for i, c16 := range coeff {
				c := int(c16)
				absCoeff := c
				if absCoeff < 0 {
					absCoeff = -absCoeff
				}
				sum := absCoeff + roundAC
				if sum < deqAC {
					continue
				}
				tmp := clampInt16(sum)
				tmp = (tmp * quantAC) >> 16
				q := tmp
				if c < 0 {
					q = -q
				}
				wantQ[i] = int16(q)
				wantDQ[i] = int16(q * deqAC)
				if tmp != 0 && int(iscan[i]) > wantEOB {
					wantEOB = int(iscan[i])
				}
			}

			if gotEOB != wantEOB {
				t.Fatalf("eob = %d, want %d", gotEOB, wantEOB)
			}
			for i := range coeff {
				if gotQ[i] != wantQ[i] {
					t.Fatalf("qcoeff[%d] = %d, want %d", i, gotQ[i], wantQ[i])
				}
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff[%d] = %d, want %d", i, gotDQ[i], wantDQ[i])
				}
			}
		})
	}
}
