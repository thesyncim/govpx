//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"bytes"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

func TestDenoiserCompleteDecisionsScalar(t *testing.T) {
	requireCompleteDenoiser(t)
	seen := map[int]bool{}
	decisions := map[int]bool{}
	for _, motion := range []uint32{0, 24, 25, 100} {
		for _, increase := range []bool{false, true} {
			inc, level, threshold := 0, 3, 512
			if increase {
				threshold = 600
			}
			if motion <= 24 {
				level = 4
				if increase {
					inc, level = 1, 5
				}
			}
			for _, difference := range []int{0, 1, 3, 4, 5, 6, 7, 8, 15, 16, 255} {
				for positiveColumns := 0; positiveColumns <= 16; positiveColumns++ {
					ms, as, ss := 17, 23, 31
					mc, sig := bytes.Repeat([]byte{71}, 15*ms+16), bytes.Repeat([]byte{79}, 15*ss+16)
					for y := range 16 {
						for x := range 16 {
							if x < positiveColumns {
								mc[y*ms+x], sig[y*ss+x] = byte(difference), 0
							} else {
								mc[y*ms+x], sig[y*ss+x] = byte(255-difference), 255
							}
						}
					}
					avg := bytes.Repeat([]byte{97}, 15*as+16+13)
					wantAvg, wantSig := bytes.Clone(avg), bytes.Clone(sig)
					want := DenoiserFilterYScalar(mc, ms, wantAvg[:15*as+16], as, wantSig, ss, motion, increase)
					first := pixel.DenoiseFirstScalar(mc, ms, make([]byte, 15*as+16), as, sig, ss, 16, inc, level)
					if abs := max(first, -first); abs > threshold {
						seen[((abs-threshold)>>8)+1] = true
					} else {
						seen[0] = true
					}
					got, ok := denoiserFilterYCompleteSIMD(mc, ms, avg[:15*as+16], as, sig, ss, motion, increase)
					if !ok || got != want || !bytes.Equal(avg, wantAvg) || !bytes.Equal(sig, wantSig) {
						t.Fatalf("motion%d increase%v diff%d positive%d decision%d/%d handled%v", motion, increase, difference, positiveColumns, got, want, ok)
					}
					decisions[got] = true
				}
			}
		}
	}
	for _, delta := range []int{0, 1, 2, 3, 4} {
		if !seen[delta] {
			t.Fatalf("correction delta%d was not exercised", delta)
		}
	}
	if !decisions[DenoiserCopyBlock] || !decisions[DenoiserFilterBlock] {
		t.Fatal("both decisions must be exercised")
	}
}

func TestDenoiserCompleteDeclinesBeforeWriting(t *testing.T) {
	requireCompleteDenoiser(t)
	for _, which := range []int{0, 1, 2} {
		for _, n := range []int{0, 15, 16, 254, 255} {
			bufs := [3][]byte{bytes.Repeat([]byte{71}, 256), bytes.Repeat([]byte{73}, 256), bytes.Repeat([]byte{79}, 256)}
			bufs[which] = bufs[which][:n]
			before := [3][]byte{bytes.Clone(bufs[0]), bytes.Clone(bufs[1]), bytes.Clone(bufs[2])}
			if _, ok := pixel.DenoiseLuma16(bufs[0], 16, bufs[1], 16, bufs[2], 16, 1, 5, 600); ok {
				t.Fatal("short buffer accepted")
			}
			for i := range bufs {
				if !bytes.Equal(bufs[i], before[i]) {
					t.Fatal("declined short buffer modified")
				}
			}
		}
	}
	for _, offsets := range [][3]int{{0, 0, 512}, {0, 1, 512}, {0, 8, 512}, {512, 0, 0}, {512, 8, 0}, {0, 512, 513}} {
		b := bytes.Repeat([]byte{71}, 1024)
		before := bytes.Clone(b)
		if _, ok := pixel.DenoiseLuma16(b[offsets[0]:], 16, b[offsets[1]:], 16, b[offsets[2]:], 16, 1, 5, 600); ok {
			t.Fatal("overlap accepted")
		}
		if !bytes.Equal(b, before) {
			t.Fatal("declined overlap modified")
		}
	}
}

func TestDenoiserCompleteAliasedInputsScalar(t *testing.T) {
	requireCompleteDenoiser(t)
	for _, offsets := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {0, 8}, {8, 0}, {0, 17}, {17, 0}} {
		for _, motion := range []uint32{0, 25} {
			for _, increase := range []bool{false, true} {
				data := make([]byte, 15*19+16+17)
				for i := range data {
					data[i] = byte(i*37 + 13)
				}
				wantData := bytes.Clone(data)
				avg, wantAvg := bytes.Repeat([]byte{71}, 15*23+16+13), bytes.Repeat([]byte{71}, 15*23+16+13)
				mc, sig := data[offsets[0]:offsets[0]+15*19+16], data[offsets[1]:offsets[1]+15*19+16]
				wm, ws := wantData[offsets[0]:offsets[0]+15*19+16], wantData[offsets[1]:offsets[1]+15*19+16]
				want := DenoiserFilterYScalar(wm, 19, wantAvg[:15*23+16], 23, ws, 19, motion, increase)
				got, ok := denoiserFilterYCompleteSIMD(mc, 19, avg[:15*23+16], 23, sig, 19, motion, increase)
				if !ok || got != want || !bytes.Equal(data, wantData) || !bytes.Equal(avg, wantAvg) {
					t.Fatalf("offsets%v motion%d increase%v decision%d/%d handled%v", offsets, motion, increase, got, want, ok)
				}
			}
		}
	}
}

func TestDenoiserCompleteInvalidParametersDecline(t *testing.T) {
	requireCompleteDenoiser(t)
	maxInt := int(^uint(0) >> 1)
	for _, args := range [][6]int{
		{-1, 16, 16, 1, 5, 600}, {16, -1, 16, 1, 5, 600}, {16, 16, -1, 1, 5, 600},
		{15, 16, 16, 1, 5, 600}, {16, 15, 16, 1, 5, 600}, {16, 16, 15, 1, 5, 600},
		{maxInt, 16, 16, 1, 5, 600}, {16, maxInt, 16, 1, 5, 600}, {16, 16, maxInt, 1, 5, 600},
		{16, 16, 16, -1, 5, 600}, {16, 16, 16, 2, 5, 600},
		{16, 16, 16, 1, 2, 600}, {16, 16, 16, 1, 6, 600},
		{16, 16, 16, 1, 5, 511}, {16, 16, 16, 1, 5, 601}, {16, 16, 16, 1, 5, maxInt},
	} {
		mc, avg, sig := bytes.Repeat([]byte{71}, 256), bytes.Repeat([]byte{73}, 256), bytes.Repeat([]byte{79}, 256)
		wm, wa, ws := bytes.Clone(mc), bytes.Clone(avg), bytes.Clone(sig)
		if _, ok := pixel.DenoiseLuma16(mc, args[0], avg, args[1], sig, args[2], args[3], args[4], args[5]); ok {
			t.Fatalf("accepted unsupported arguments %v", args)
		}
		if !bytes.Equal(mc, wm) || !bytes.Equal(avg, wa) || !bytes.Equal(sig, ws) {
			t.Fatalf("decline wrote with arguments %v", args)
		}
	}
}

func requireCompleteDenoiser(t *testing.T) {
	t.Helper()
	if !pixel.Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required complete SIMD denoiser unavailable")
		}
		t.Skip("SIMD unavailable")
	}
}
