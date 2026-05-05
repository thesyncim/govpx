package decoder

import (
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestReadTreeCoefficientToken(t *testing.T) {
	var br boolcoder.Decoder
	if err := br.Init(make([]byte, 16)); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	probs := [tables.EntropyNodes]uint8{128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128}
	token := ReadCoefToken(&br, probs[:])
	if token != tables.DCTEOBToken {
		t.Fatalf("token = %d, want DCT_EOB_TOKEN", token)
	}
}

func TestReadModeTrees(t *testing.T) {
	var br boolcoder.Decoder
	if err := br.Init([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	yProbs := [common.VP8YModes - 1]uint8{128, 128, 128, 128}
	uvProbs := [common.VP8UVModes - 1]uint8{128, 128, 128}
	bProbs := [common.VP8BIntraModes - 1]uint8{128, 128, 128, 128, 128, 128, 128, 128, 128}

	yMode := ReadYMode(&br, yProbs[:])
	uvMode := ReadUVMode(&br, uvProbs[:])
	bMode := ReadBMode(&br, bProbs[:])

	if yMode < 0 || yMode >= common.VP8YModes {
		t.Fatalf("yMode = %d out of range", yMode)
	}
	if uvMode < 0 || uvMode >= common.VP8UVModes {
		t.Fatalf("uvMode = %d out of range", uvMode)
	}
	if bMode < 0 || bMode >= common.VP8BIntraModes {
		t.Fatalf("bMode = %d out of range", bMode)
	}
}

func TestReadTreeAllocatesZero(t *testing.T) {
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	var br boolcoder.Decoder
	if err := br.Init(src); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	probs := [tables.EntropyNodes]uint8{128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128}

	allocs := testing.AllocsPerRun(1000, func() {
		if br.Err() != nil {
			_ = br.Init(src)
		}
		_ = ReadCoefToken(&br, probs[:])
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkReadCoefToken(b *testing.B) {
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	var br boolcoder.Decoder
	_ = br.Init(src)
	probs := [tables.EntropyNodes]uint8{128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if br.Err() != nil {
			_ = br.Init(src)
		}
		_ = ReadCoefToken(&br, probs[:])
	}
}
