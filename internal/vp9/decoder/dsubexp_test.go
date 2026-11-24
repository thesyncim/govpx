package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// TestDiffUpdateProbNoUpdate writes a boolean-coded "no update" bit
// (0 against DIFF_UPDATE_PROB) and confirms the probability slot is
// preserved.
func TestDiffUpdateProbNoUpdate(t *testing.T) {
	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, DiffUpdateProb) // no update
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	p := uint8(128)
	VpxDiffUpdateProb(&r, &p)
	if p != 128 {
		t.Errorf("no-update path changed prob: got %d, want 128", p)
	}
}

// TestInvRemapTableMonotone covers the permutation table layout —
// every byte 0..254 maps to a unique uint8 in 1..254. Catches typos
// that would skew probability updates frame-wide.
func TestInvRemapTableMonotone(t *testing.T) {
	seen := make(map[uint8]bool)
	for _, v := range invMapTable {
		if v == 0 || v == 255 {
			t.Errorf("invMapTable contains illegal value %d", v)
		}
		// Allow exactly one duplicate at value 253 (libvpx documents it
		// — see vp9_dsubexp.c: the table maps 253 twice to keep all
		// 255 slots populated within the [1, 254] range).
		if seen[v] && v != 253 {
			t.Errorf("invMapTable has unexpected duplicate %d", v)
		}
		seen[v] = true
	}
}

// TestDecodeTermSubexpAllRanges round-trips representative values in
// each of the three sub-exp magnitude buckets through the inverse
// path. The encoder counterpart isn't yet ported; this exercises the
// pure-decoder side by constructing the bitstream by hand.
func TestDecodeTermSubexpAllRanges(t *testing.T) {
	cases := []struct {
		name string
		want int
		// bits[i] is the (bit, prob) pair for one boolean-coder write.
		// Probability 128 = half — encoder gets a fresh bit either way.
		bits [][2]uint32
	}{
		{
			name: "small_range",
			want: 11,
			bits: [][2]uint32{{0, 128}, {1, 128}, {0, 128}, {1, 128}, {1, 128}},
		},
		{
			name: "mid_range",
			want: 16 + 5,
			bits: [][2]uint32{{1, 128}, {0, 128}, {0, 128}, {1, 128}, {0, 128}, {1, 128}},
		},
		{
			name: "high_range",
			want: 32 + 9,
			bits: [][2]uint32{{1, 128}, {1, 128}, {0, 128}, {0, 128}, {1, 128}, {0, 128}, {0, 128}, {1, 128}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := make([]byte, 32)
			var w bitstream.Writer
			w.Start(buf)
			for _, b := range c.bits {
				w.Write(b[0], b[1])
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			var r bitstream.Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if got := decodeTermSubexp(&r); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}
