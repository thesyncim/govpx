package decoder

import (
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestDecodeKeyFrameMacroblockModeDC(t *testing.T) {
	payload := encodeKeyFrameMacroblockMode(t, common.DCPred, common.TMPred, common.BDCPred)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	DecodeKeyFrameMacroblockMode(&br, nil, nil, &out)

	if out.RefFrame != common.IntraFrame {
		t.Fatalf("RefFrame = %d, want intra", out.RefFrame)
	}
	if out.Mode != common.DCPred || out.UVMode != common.TMPred || out.Is4x4 {
		t.Fatalf("mode = %+v, want DC/TM non-4x4", out)
	}
}

func TestDecodeKeyFrameMacroblockModeBPred(t *testing.T) {
	payload := encodeKeyFrameMacroblockMode(t, common.BPred, common.VPred, common.BDCPred)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	DecodeKeyFrameMacroblockMode(&br, nil, nil, &out)

	if out.Mode != common.BPred || !out.Is4x4 || out.UVMode != common.VPred {
		t.Fatalf("mode = %+v, want B_PRED/V 4x4", out)
	}
	for i, mode := range out.BModes {
		if mode != common.BDCPred {
			t.Fatalf("BMode[%d] = %d, want B_DC_PRED", i, mode)
		}
	}
}

func TestKeyFrameBlockModeContexts(t *testing.T) {
	above := MacroblockMode{Mode: common.VPred}
	left := MacroblockMode{Mode: common.HPred}
	cur := MacroblockMode{}

	if got := keyFrameAboveBlockMode(&cur, &above, 0); got != common.BVEPred {
		t.Fatalf("above edge context = %d, want B_VE_PRED", got)
	}
	if got := keyFrameLeftBlockMode(&cur, &left, 0); got != common.BHEPred {
		t.Fatalf("left edge context = %d, want B_HE_PRED", got)
	}

	above = MacroblockMode{Mode: common.BPred}
	left = MacroblockMode{Mode: common.BPred}
	above.BModes[12] = common.BHUPred
	left.BModes[3] = common.BRDPred

	if got := keyFrameAboveBlockMode(&cur, &above, 0); got != common.BHUPred {
		t.Fatalf("above B_PRED context = %d, want B_HU_PRED", got)
	}
	if got := keyFrameLeftBlockMode(&cur, &left, 0); got != common.BRDPred {
		t.Fatalf("left B_PRED context = %d, want B_RD_PRED", got)
	}
}

func TestDecodeKeyFrameMacroblockReadsSegmentAndSkip(t *testing.T) {
	payload := encodeKeyFrameMacroblockWithFeatures(t)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	segmentation := SegmentationHeader{
		Enabled:   true,
		UpdateMap: true,
		TreeProbs: [common.MBFeatureTreeProbs]uint8{128, 128, 128},
	}
	modeHeader := ModeHeader{MBNoCoeffSkip: true, ProbSkipFalse: 128}
	var out MacroblockMode

	DecodeKeyFrameMacroblock(&br, &segmentation, modeHeader, nil, nil, &out)

	if out.SegmentID != 3 {
		t.Fatalf("SegmentID = %d, want 3", out.SegmentID)
	}
	if !out.MBSkipCoeff {
		t.Fatalf("MBSkipCoeff = false, want true")
	}
	if out.Mode != common.DCPred || out.UVMode != common.VPred {
		t.Fatalf("mode = %+v, want DC/V", out)
	}
}

func TestDecodeKeyFrameMacroblockModeAllocatesZero(t *testing.T) {
	payload := encodeKeyFrameMacroblockMode(t, common.BPred, common.VPred, common.BDCPred)
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		var out MacroblockMode
		DecodeKeyFrameMacroblockMode(&br, nil, nil, &out)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestDecodeKeyFrameMacroblockAllocatesZero(t *testing.T) {
	payload := encodeKeyFrameMacroblockWithFeatures(t)
	segmentation := SegmentationHeader{
		Enabled:   true,
		UpdateMap: true,
		TreeProbs: [common.MBFeatureTreeProbs]uint8{128, 128, 128},
	}
	modeHeader := ModeHeader{MBNoCoeffSkip: true, ProbSkipFalse: 128}
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		var out MacroblockMode
		DecodeKeyFrameMacroblock(&br, &segmentation, modeHeader, nil, nil, &out)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeKeyFrameMacroblockMode(t *testing.T, yMode common.MBPredictionMode, uvMode common.MBPredictionMode, bMode common.BPredictionMode) []byte {
	var w testBoolWriter
	w.init()
	writeKeyFrameMacroblockMode(t, &w, yMode, uvMode, bMode)
	return w.finish()
}

func encodeKeyFrameMacroblockWithFeatures(t *testing.T) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(1, 128)
	w.writeBool(1, 128)
	w.writeBool(1, 128)
	writeKeyFrameMacroblockMode(t, &w, common.DCPred, common.VPred, common.BDCPred)
	return w.finish()
}

func writeKeyFrameMacroblockMode(t *testing.T, w *testBoolWriter, yMode common.MBPredictionMode, uvMode common.MBPredictionMode, bMode common.BPredictionMode) {
	writeTreeToken(t, w, tables.KeyFrameYModeTree[:], tables.KeyFrameYModeProbs[:], int(yMode))
	if yMode == common.BPred {
		for i := 0; i < 16; i++ {
			writeTreeToken(t, w, tables.BModeTree[:], tables.KeyFrameBModeProbs[common.BDCPred][common.BDCPred][:], int(bMode))
		}
	}
	writeTreeToken(t, w, tables.UVModeTree[:], tables.KeyFrameUVModeProbs[:], int(uvMode))
}

func writeTreeToken(t *testing.T, w *testBoolWriter, tree []int16, probs []uint8, token int) {
	t.Helper()
	var nodes [16]int16
	var bits [16]uint8
	depth, ok := findTreeTokenPath(tree, 0, token, &nodes, &bits, 0)
	if !ok {
		t.Fatalf("token %d not found in tree", token)
	}
	for i := 0; i < depth; i++ {
		w.writeBool(bits[i], probs[nodes[i]>>1])
	}
}

func findTreeTokenPath(tree []int16, node int16, token int, nodes *[16]int16, bits *[16]uint8, depth int) (int, bool) {
	for bit := int16(0); bit < 2; bit++ {
		next := tree[node+bit]
		nodes[depth] = node
		bits[depth] = uint8(bit)
		if next <= 0 {
			if int(-next) == token {
				return depth + 1, true
			}
			continue
		}
		if foundDepth, ok := findTreeTokenPath(tree, next, token, nodes, bits, depth+1); ok {
			return foundDepth, true
		}
	}
	return 0, false
}
