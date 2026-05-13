package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteKeyframeBlockBlock16x16: emit segment(disabled) + skip +
// Y intra mode + UV mode for a 16x16 keyframe block, and re-parse
// via ReadIntraFrameModeInfo.
func TestWriteKeyframeBlockBlock16x16(t *testing.T) {
	var seg vp9dec.SegmentationParams
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 16),
		MiCols:             4,
	}

	mi := &vp9dec.NeighborMi{
		SbType: common.Block16x16,
		Mode:   common.HPred,
		TxSize: common.Tx4x4,
	}
	uvMode := common.D135Pred

	var fc vp9dec.FrameContext
	fc.SkipProbs[0] = 128

	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)

	WriteKeyframeBlock(&bw, WriteKeyframeBlockArgs{
		Seg:       &seg,
		Mi:        mi,
		TxMode:    common.Only4x4, // skips the tx-size cascade
		SkipProbs: fc.SkipProbs,
	})
	WriteKeyframeUvMode(&bw, uvMode, mi.Mode)
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := &vp9dec.NeighborMi{SbType: common.Block16x16}
	out := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
		Reader:   &r,
		Fc:       &fc,
		Seg:      &seg,
		Maps:     &maps,
		TxMode:   common.Only4x4,
		MiOffset: 0,
		XMis:     2, YMis: 2,
	}, got)

	if got.Skip != 0 {
		t.Errorf("Skip = %d, want 0", got.Skip)
	}
	if got.Mode != mi.Mode {
		t.Errorf("Y mode = %d, want %d", got.Mode, mi.Mode)
	}
	if out.UvMode != uvMode {
		t.Errorf("UV mode = %d, want %d", out.UvMode, uvMode)
	}
	if got.RefFrame[0] != vp9dec.IntraFrame || got.RefFrame[1] != vp9dec.NoRefFrame {
		t.Errorf("ref_frame = %v, want (Intra, NoRef)", got.RefFrame)
	}
}

// TestWriteKeyframeBlockSkipPath: a skip=1 block keeps the rest of
// the wire fragment (Y/UV modes) but the decoder's
// ReadIntraFrameModeInfo carries skip back through the output mi.
func TestWriteKeyframeBlockSkipPath(t *testing.T) {
	var seg vp9dec.SegmentationParams
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 16),
		MiCols:             4,
	}

	mi := &vp9dec.NeighborMi{
		SbType: common.Block16x16,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   1,
	}

	var fc vp9dec.FrameContext
	fc.SkipProbs[0] = 128

	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteKeyframeBlock(&bw, WriteKeyframeBlockArgs{
		Seg:       &seg,
		Mi:        mi,
		TxMode:    common.Only4x4,
		SkipProbs: fc.SkipProbs,
	})
	WriteKeyframeUvMode(&bw, common.VPred, mi.Mode)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	got := &vp9dec.NeighborMi{SbType: common.Block16x16}
	vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
		Reader:   &r,
		Fc:       &fc,
		Seg:      &seg,
		Maps:     &maps,
		TxMode:   common.Only4x4,
		MiOffset: 0,
		XMis:     2, YMis: 2,
	}, got)

	if got.Skip != 1 {
		t.Errorf("Skip = %d, want 1", got.Skip)
	}
}
