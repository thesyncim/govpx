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

func TestNmvCostTableMatchesScalarCosts(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	// Make the two axes visibly different so row/col table indexing is covered.
	fc.Nmvc.Comps[0].Sign = 91
	fc.Nmvc.Comps[1].Sign = 173
	fc.Nmvc.Comps[0].Fp = [3]uint8{88, 101, 179}
	fc.Nmvc.Comps[1].Fp = [3]uint8{71, 139, 211}

	tests := []struct {
		name  string
		mv    vp9dec.MV
		ref   vp9dec.MV
		useHP bool
	}{
		{name: "zero", mv: vp9dec.MV{}, ref: vp9dec.MV{}, useHP: false},
		{name: "class0-nohp", mv: vp9dec.MV{Row: 7, Col: -13}, ref: vp9dec.MV{Row: 2, Col: -1}, useHP: false},
		{name: "class0-hp", mv: vp9dec.MV{Row: 8, Col: -11}, ref: vp9dec.MV{Row: 1, Col: -3}, useHP: true},
		{name: "large-positive", mv: vp9dec.MV{Row: 4097, Col: 8191}, ref: vp9dec.MV{Row: 0, Col: 3}, useHP: true},
		{name: "large-negative", mv: vp9dec.MV{Row: -7000, Col: -9000}, ref: vp9dec.MV{Row: 13, Col: -5}, useHP: false},
		{name: "range-edge", mv: vp9dec.MV{Row: nmvCostTableMax, Col: -nmvCostTableMax}, ref: vp9dec.MV{}, useHP: true},
	}
	const errorPerBit = 93
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var table NmvCostTable
			if !table.Build(&fc.Nmvc, tt.useHP) {
				t.Fatal("Build returned false")
			}
			gotRaw, ok := table.MvCost(tt.mv, tt.ref)
			if !ok {
				t.Fatal("MvCost returned !ok")
			}
			wantRaw := MvCostWithHP(tt.mv, tt.ref, &fc.Nmvc, tt.useHP)
			if gotRaw != wantRaw {
				t.Fatalf("raw table cost = %d, want scalar %d", gotRaw, wantRaw)
			}
			gotScaled, ok := table.SubpelMVErrorCost(tt.mv, tt.ref, errorPerBit)
			if !ok {
				t.Fatal("SubpelMVErrorCost returned !ok")
			}
			wantScaled := SubpelMVErrorCost(&fc, tt.mv, tt.ref, tt.useHP, errorPerBit)
			if gotScaled != wantScaled {
				t.Fatalf("scaled table cost = %d, want scalar %d", gotScaled, wantScaled)
			}
		})
	}
}

func TestNmvCostTableRejectsOutOfRangeDiff(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var table NmvCostTable
	table.Build(&fc.Nmvc, true)
	if _, ok := table.MvCost(
		vp9dec.MV{Row: nmvCostTableMax, Col: 0},
		vp9dec.MV{Row: -1, Col: 0},
	); ok {
		t.Fatal("MvCost accepted a diff above MV_MAX")
	}
}
