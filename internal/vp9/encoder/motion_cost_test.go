package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestMVSADComponentCostClampsAndMirrorsSign(t *testing.T) {
	if got := MVSADComponentCost(-7); got != MVSADComponentCost(7) {
		t.Fatalf("negative MV cost = %d, want sign-mirrored cost", got)
	}
	if got := MVSADComponentCost(mvSADMax + 100); got != MVSADComponentCost(mvSADMax) {
		t.Fatalf("clamped MV cost = %d, want max-table cost", got)
	}
	if got := MVSADComponentCost(0); got != 0 {
		t.Fatalf("zero MV cost = %d, want 0", got)
	}
}

func TestUseMvHPThreshold(t *testing.T) {
	if !UseMvHP(vp9dec.MV{Row: 63, Col: -63}) {
		t.Fatalf("UseMvHP rejected reference inside threshold")
	}
	if UseMvHP(vp9dec.MV{Row: 64, Col: 0}) {
		t.Fatalf("UseMvHP accepted row at threshold")
	}
	if UseMvHP(vp9dec.MV{Row: 0, Col: -64}) {
		t.Fatalf("UseMvHP accepted col at threshold")
	}
}

func TestSubpelMVErrorCostUsesEncoderHPTable(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	mv := vp9dec.MV{Row: 135, Col: 13}
	ref := vp9dec.MV{Row: 128, Col: 0}
	const errorPerBit = 97

	raw := MvCostWithHP(mv, ref, &fc.Nmvc, true)
	want := uint64((int64(raw)*errorPerBit + (1 << 13)) >> 14)
	if got := SubpelMVErrorCost(&fc, mv, ref, true, errorPerBit); got != want {
		t.Fatalf("subpel MV error cost = %d, want HP-table cost %d",
			got, want)
	}
	if writerRaw := MvCost(mv, ref, &fc.Nmvc, true); writerRaw == raw {
		t.Fatalf("test vector did not exercise HP-table-only cost: raw=%d",
			raw)
	}
}
