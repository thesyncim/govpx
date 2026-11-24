package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestDecodeInterIntraMacroblockModeDC(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	payload := encodeInterIntraMacroblockMode(t, &probs, common.DCPred, common.HPred, common.BDCPred)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	DecodeInterIntraMacroblockMode(&br, &probs, &out)

	if out.RefFrame != common.IntraFrame || out.Mode != common.DCPred || out.UVMode != common.HPred || out.Is4x4 {
		t.Fatalf("mode = %+v, want intra DC/H non-4x4", out)
	}
}

func TestDecodeInterIntraMacroblockModeBPred(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	payload := encodeInterIntraMacroblockMode(t, &probs, common.BPred, common.VPred, common.BHEPred)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	DecodeInterIntraMacroblockMode(&br, &probs, &out)

	if out.Mode != common.BPred || out.UVMode != common.VPred || !out.Is4x4 {
		t.Fatalf("mode = %+v, want B_PRED/V 4x4", out)
	}
	for i, mode := range out.BModes {
		if mode != common.BHEPred {
			t.Fatalf("BMode[%d] = %d, want B_HE_PRED", i, mode)
		}
	}
}

func TestDecodeInterIntraMacroblockModeAllocatesZero(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	payload := encodeInterIntraMacroblockMode(t, &probs, common.BPred, common.VPred, common.BDCPred)
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		var out MacroblockMode
		DecodeInterIntraMacroblockMode(&br, &probs, &out)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterIntraMacroblockMode(t *testing.T, probs *ModeProbs, yMode common.MBPredictionMode, uvMode common.MBPredictionMode, bMode common.BPredictionMode) []byte {
	var w testBoolWriter
	w.init()
	writeTreeToken(t, &w, tables.YModeTree[:], probs.YMode[:], int(yMode))
	if yMode == common.BPred {
		for range 16 {
			writeTreeToken(t, &w, tables.BModeTree[:], probs.BMode[:], int(bMode))
		}
	}
	writeTreeToken(t, &w, tables.UVModeTree[:], probs.UVMode[:], int(uvMode))
	return w.finish()
}
