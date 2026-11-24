package encoder

import (
	"testing"

	common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestRDConstantsMatchSinglePassInitializeRDConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		rdMult int
		rdDiv  int
		errBit int
	}{
		{qIndex: 0, rdMult: 44, rdDiv: 100, errBit: 1},
		{qIndex: 4, rdMult: 179, rdDiv: 100, errBit: 1},
		{qIndex: 40, rdMult: 38, rdDiv: 1, errBit: 34},
		{qIndex: 56, rdMult: 72, rdDiv: 1, errBit: 66},
		{qIndex: 127, rdMult: 690, rdDiv: 1, errBit: 627},
	}
	for _, tt := range tests {
		rdMult, rdDiv := RDConstants(tt.qIndex)
		if rdMult != tt.rdMult || rdDiv != tt.rdDiv {
			t.Fatalf("q=%d rd = %d/%d, want %d/%d", tt.qIndex, rdMult, rdDiv, tt.rdMult, tt.rdDiv)
		}
		if got := ErrorPerBit(tt.qIndex); got != tt.errBit {
			t.Fatalf("q=%d errorperbit = %d, want %d", tt.qIndex, got, tt.errBit)
		}
	}

	if got := RDModeScore(4, 512, 10); got != 1358 {
		t.Fatalf("RDModeScore low q = %d, want libvpx RDCOST 1358", got)
	}
	if got := RDModeScore(40, 512, 100); got != 176 {
		t.Fatalf("RDModeScore mid q = %d, want libvpx RDCOST 176", got)
	}
}

func TestRDConstantsWithZbinAdjustMultiplier(t *testing.T) {
	baseMult, baseDiv := RDConstants(127)
	overMult, overDiv := RDConstantsWithZbin(127, 128)
	if overMult != 989 || overDiv != 1 {
		t.Fatalf("q127 zbin-over-quant rd = %d/%d, want 989/1", overMult, overDiv)
	}
	if overMult <= baseMult || overDiv != baseDiv {
		t.Fatalf("zbin-over-quant rd = %d/%d, base %d/%d, want larger multiplier with same divider", overMult, overDiv, baseMult, baseDiv)
	}
	if got := RDModeScoreWithZbin(127, 128, 512, 100); got != 2078 {
		t.Fatalf("zbin-over-quant RDModeScore = %d, want libvpx RDCOST 2078", got)
	}
	if got := ErrorPerBitWithZbin(127, 128); got != 899 {
		t.Fatalf("zbin-over-quant errorperbit = %d, want 899", got)
	}
}

// TestRDConstantsMatchLibvpxAcrossQuantizers pins the (rdmult, rddiv) pair
// libvpx v1.16.0 computes in vp8/encoder/rdopt.c vp8_initialize_rd_consts,
// single-pass / non-zbin-over-quant branch, across the entire QIndex range
// [0..127]. The reference was generated from a 1:1 reimplementation of the
// libvpx formula in C against vp8/common/quant_common.c dc_qlookup; any
// divergence between this table and RDConstantsWithZbin is a regression in
// the per-Q RDMULT/RDDIV derivation.
func TestRDConstantsMatchLibvpxAcrossQuantizers(t *testing.T) {
	// Each row is (rdMult, rdDiv) for qIndex=row_index produced by libvpx's
	// vp8_initialize_rd_consts when zbin_over_quant==0, pass==1 (or pass==2
	// with frame_type==KEY_FRAME so the rd_iifactor lift is skipped). The
	// >1000 split (RDDIV=1, RDMULT/=100) is applied verbatim. The table
	// holds the libvpx-side ground truth derived from
	// vp8_dc_quant(qIndex, 0) squared and scaled by rdconst=2.80; do not
	// regenerate from the govpx implementation.
	libvpxRD := [common.QIndexRange][2]int{
		{44, 100}, {70, 100}, {100, 100}, {137, 100}, {179, 100},
		{226, 100}, {280, 100}, {280, 100}, {338, 100}, {403, 100},
		{473, 100}, {548, 100}, {630, 100}, {716, 100}, {809, 100},
		{809, 100}, {907, 100}, {10, 1}, {11, 1}, {11, 1},
		{12, 1}, {12, 1}, {13, 1}, {13, 1}, {14, 1},
		{14, 1}, {16, 1}, {17, 1}, {17, 1}, {18, 1},
		{20, 1}, {21, 1}, {23, 1}, {25, 1}, {26, 1},
		{28, 1}, {30, 1}, {32, 1}, {34, 1}, {36, 1},
		{38, 1}, {38, 1}, {40, 1}, {42, 1}, {44, 1},
		{47, 1}, {49, 1}, {51, 1}, {54, 1}, {56, 1},
		{59, 1}, {59, 1}, {61, 1}, {64, 1}, {67, 1},
		{70, 1}, {72, 1}, {75, 1}, {78, 1}, {81, 1},
		{84, 1}, {87, 1}, {90, 1}, {94, 1}, {97, 1},
		{100, 1}, {104, 1}, {107, 1}, {111, 1}, {114, 1},
		{118, 1}, {121, 1}, {125, 1}, {129, 1}, {133, 1},
		{137, 1}, {141, 1}, {145, 1}, {149, 1}, {153, 1},
		{157, 1}, {161, 1}, {161, 1}, {166, 1}, {170, 1},
		{174, 1}, {179, 1}, {183, 1}, {188, 1}, {192, 1},
		{197, 1}, {202, 1}, {207, 1}, {211, 1}, {216, 1},
		{221, 1}, {231, 1}, {242, 1}, {252, 1}, {258, 1},
		{268, 1}, {280, 1}, {285, 1}, {291, 1}, {302, 1},
		{314, 1}, {326, 1}, {338, 1}, {351, 1}, {363, 1},
		{376, 1}, {389, 1}, {416, 1}, {430, 1}, {444, 1},
		{458, 1}, {473, 1}, {487, 1}, {502, 1}, {517, 1},
		{533, 1}, {548, 1}, {572, 1}, {588, 1}, {613, 1},
		{638, 1}, {664, 1}, {690, 1},
	}
	for q := range common.QIndexRange {
		rdMult, rdDiv := RDConstants(q)
		if rdMult != libvpxRD[q][0] || rdDiv != libvpxRD[q][1] {
			t.Fatalf("qIndex=%d RDConstants=%d/%d want libvpx=%d/%d", q, rdMult, rdDiv, libvpxRD[q][0], libvpxRD[q][1])
		}
	}

	// Spot-check the zbin_over_quant lift across the QIndex range. Each
	// reference value is derived from a 1:1 reimplementation of the libvpx
	// formula:
	//   oq_factor = 1.0 + 0.0015625 * zbin_over_quant
	//   modq      = (int)((double)capped_q * oq_factor)
	//   RDMULT    = (int)(2.80 * (modq*modq))
	//   {RDDIV=1, RDMULT/=100} when RDMULT > 1000
	// against vp8/encoder/rdopt.c vp8_initialize_rd_consts lines 177-187.
	zbinTests := []struct {
		qIndex        int
		zbinOverQuant int
		rdMult        int
		rdDiv         int
	}{
		{0, 0, 44, 100}, {0, 128, 44, 100},
		{4, 0, 179, 100}, {4, 64, 179, 100}, {4, 128, 226, 100},
		{11, 0, 548, 100}, {11, 64, 630, 100}, {11, 128, 716, 100},
		{40, 0, 38, 1}, {40, 32, 40, 1}, {40, 64, 44, 1}, {40, 128, 54, 1},
		{56, 0, 72, 1}, {56, 32, 78, 1}, {56, 64, 87, 1}, {56, 128, 104, 1},
		{80, 0, 157, 1}, {80, 32, 170, 1}, {80, 64, 188, 1}, {80, 128, 226, 1},
		{100, 0, 268, 1}, {100, 8, 274, 1}, {100, 32, 291, 1}, {100, 64, 320, 1}, {100, 128, 383, 1},
		{127, 0, 690, 1}, {127, 8, 698, 1}, {127, 32, 753, 1}, {127, 64, 828, 1}, {127, 128, 989, 1},
	}
	for _, tt := range zbinTests {
		rdMult, rdDiv := RDConstantsWithZbin(tt.qIndex, tt.zbinOverQuant)
		if rdMult != tt.rdMult || rdDiv != tt.rdDiv {
			t.Fatalf("qIndex=%d zbin=%d RDConstantsWithZbin=%d/%d want libvpx=%d/%d",
				tt.qIndex, tt.zbinOverQuant, rdMult, rdDiv, tt.rdMult, tt.rdDiv)
		}
	}
}

func TestSADPerBitTablesMatchInitializeMEConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		want16 int
		want4  int
	}{
		{qIndex: 0, want16: 2, want4: 2},
		{qIndex: 6, want16: 2, want4: 3},
		{qIndex: 20, want16: 3, want4: 4},
		{qIndex: 30, want16: 4, want4: 5},
		{qIndex: 42, want16: 5, want4: 6},
		{qIndex: 54, want16: 6, want4: 7},
		{qIndex: 62, want16: 6, want4: 8},
		{qIndex: 78, want16: 8, want4: 10},
		{qIndex: 90, want16: 9, want4: 12},
		{qIndex: 102, want16: 10, want4: 13},
		{qIndex: 114, want16: 11, want4: 16},
		{qIndex: 126, want16: 14, want4: 20},
	}
	for _, tt := range tests {
		if got := SADPerBit16(tt.qIndex); got != tt.want16 {
			t.Fatalf("q=%d sad_per_bit16 = %d, want %d", tt.qIndex, got, tt.want16)
		}
		if got := SADPerBit4(tt.qIndex); got != tt.want4 {
			t.Fatalf("q=%d sad_per_bit4 = %d, want %d", tt.qIndex, got, tt.want4)
		}
	}
}

func TestFullPelMVSADCost16MatchesMotionVectorSADCost(t *testing.T) {
	tests := []struct {
		mv  MotionVector
		ref MotionVector
		q   int
	}{
		{mv: MotionVector{}, ref: MotionVector{}, q: 0},
		{mv: MotionVector{Row: 8, Col: -64}, ref: MotionVector{}, q: 30},
		{mv: MotionVector{Row: -96, Col: 72}, ref: MotionVector{Row: 16, Col: -8}, q: 56},
		{mv: MotionVector{Row: 4096, Col: -4096}, ref: MotionVector{}, q: 126},
	}
	for _, tt := range tests {
		got := FullPelMVSADCost16FromDeltas(int(tt.mv.Row)>>3, int(tt.mv.Col)>>3, int(tt.ref.Row)>>3, int(tt.ref.Col)>>3, tt.q)
		want := MotionVectorSADCost(tt.mv, tt.ref, SADPerBit16(tt.q))
		if got != want {
			t.Fatalf("mv=%+v ref=%+v q=%d full-pel SAD cost = %d, want %d", tt.mv, tt.ref, tt.q, got, want)
		}
	}
}

func TestFullPelMVSADCost4MatchesMotionVectorSADCost(t *testing.T) {
	tests := []struct {
		mv  MotionVector
		ref MotionVector
		q   int
	}{
		{mv: MotionVector{}, ref: MotionVector{}, q: 0},
		{mv: MotionVector{Row: 8, Col: -64}, ref: MotionVector{}, q: 30},
		{mv: MotionVector{Row: -96, Col: 72}, ref: MotionVector{Row: 16, Col: -8}, q: 56},
		{mv: MotionVector{Row: 4096, Col: -4096}, ref: MotionVector{}, q: 126},
	}
	for _, tt := range tests {
		got := FullPelMVSADCost4FromDeltas(int(tt.mv.Row)>>3, int(tt.mv.Col)>>3, int(tt.ref.Row)>>3, int(tt.ref.Col)>>3, tt.q)
		want := MotionVectorSADCost(tt.mv, tt.ref, SADPerBit4(tt.q))
		if got != want {
			t.Fatalf("mv=%+v ref=%+v q=%d split full-pel SAD cost = %d, want %d", tt.mv, tt.ref, tt.q, got, want)
		}
	}
}
