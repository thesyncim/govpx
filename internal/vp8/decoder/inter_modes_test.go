package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestReadInterPredictionMode(t *testing.T) {
	counts := InterModeCounts{Intra: 0, Nearest: 1, Near: 2, Split: 3}
	tests := []struct {
		name string
		bits []uint8
		want common.MBPredictionMode
	}{
		{name: "zero", bits: []uint8{0}, want: common.ZeroMV},
		{name: "nearest", bits: []uint8{1, 0}, want: common.NearestMV},
		{name: "near", bits: []uint8{1, 1, 0}, want: common.NearMV},
		{name: "new", bits: []uint8{1, 1, 1, 0}, want: common.NewMV},
		{name: "split", bits: []uint8{1, 1, 1, 1}, want: common.SplitMV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var br boolcoder.Decoder
			if err := br.Init(encodeInterModeBits(counts, tt.bits)); err != nil {
				t.Fatalf("Init returned error: %v", err)
			}

			got := ReadInterPredictionMode(&br, counts)

			if got != tt.want {
				t.Fatalf("mode = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadInterPredictionModeAllocatesZero(t *testing.T) {
	counts := InterModeCounts{Intra: 0, Nearest: 1, Near: 2, Split: 3}
	payload := encodeInterModeBits(counts, []uint8{1, 1, 1, 0})
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = ReadInterPredictionMode(&br, counts)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterModeBits(counts InterModeCounts, bits []uint8) []byte {
	var w testBoolWriter
	w.init()
	probs := [4]uint8{
		tables.InterModeContexts[counts.Intra][0],
		tables.InterModeContexts[counts.Nearest][1],
		tables.InterModeContexts[counts.Near][2],
		tables.InterModeContexts[counts.Split][3],
	}
	for i, bit := range bits {
		w.writeBool(bit, probs[i])
	}
	return w.finish()
}
