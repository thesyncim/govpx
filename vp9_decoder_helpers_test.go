package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func miColsForSize(v uint32) int {
	miCols := (v + 7) >> 3
	return int(miCols)
}

const (
	vp9CompoundGoldenSlotForTest = 4
	vp9CompoundAltrefSlotForTest = 5
)

func vp9MiGridAtForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &miGrid[r*miCols+c]
}

func fillVP9MiGridForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// TestVP9DecoderMaxWidthRejectsLargerKeyframe: a header whose width
// exceeds the configured MaxWidth gets rejected before tile parsing or

func vp9SVCStyleSuperframeForTest(t *testing.T) []byte {
	t.Helper()
	return vp9test.SuperframePacket(t,
		vp9EncodedKeyframeForTest(t, 32, 32, 80),
		vp9EncodedKeyframeForTest(t, 64, 64, 160),
	)
}

func vp9EncodedKeyframeForTest(t *testing.T, width, height int, y byte) []byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 37,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize %dx%d: %v", width, height, err)
	}
	dst := make([]byte, dstSize)
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, y, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult %dx%d: %v", width, height, err)
	}
	if !result.KeyFrame || !result.ShowFrame || len(result.Data) == 0 {
		t.Fatalf("encoded test frame result = %+v, want visible keyframe", result)
	}
	return append([]byte(nil), result.Data...)
}

// TestVP9DecoderClose marks the decoder as closed; subsequent Decode
// returns ErrClosed.

func TestVP9DecoderInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter nearestmv steady state: got %v allocs/op, want 0", allocs)
	}
}
