package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestReadInterReferenceFrame(t *testing.T) {
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	tests := []struct {
		name string
		bits []uint8
		want common.MVReferenceFrame
	}{
		{name: "intra", bits: []uint8{0}, want: common.IntraFrame},
		{name: "last", bits: []uint8{1, 0}, want: common.LastFrame},
		{name: "golden", bits: []uint8{1, 1, 0}, want: common.GoldenFrame},
		{name: "altref", bits: []uint8{1, 1, 1}, want: common.AltRefFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var br boolcoder.Decoder
			if err := br.Init(encodeInterReferenceBits(tt.bits)); err != nil {
				t.Fatalf("Init returned error: %v", err)
			}

			got := ReadInterReferenceFrame(&br, header)

			if got != tt.want {
				t.Fatalf("ref = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadInterReferenceFrameAllocatesZero(t *testing.T) {
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterReferenceBits([]uint8{1, 1, 1})
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = ReadInterReferenceFrame(&br, header)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterReferenceBits(bits []uint8) []byte {
	var w testBoolWriter
	w.init()
	for _, bit := range bits {
		w.writeBool(bit, 128)
	}
	return w.finish()
}
