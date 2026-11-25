package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestBandCoefContexts(t *testing.T) {
	if got := BandCoefContexts(0); got != 3 {
		t.Errorf("band 0: got %d, want 3", got)
	}
	for b := 1; b < CoefBands; b++ {
		if got := BandCoefContexts(b); got != 6 {
			t.Errorf("band %d: got %d, want 6", b, got)
		}
	}
}

// TestReadCoefProbsCommonSkip writes a single "0" gate bit; the table
// must be untouched.
func TestReadCoefProbsCommonSkip(t *testing.T) {
	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128) // skip update
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var model CoefProbsModel
	for i := range model {
		for j := range model[i] {
			for k := range model[i][j] {
				for l := range model[i][j][k] {
					for m := range model[i][j][k][l] {
						model[i][j][k][l][m] = 77
					}
				}
			}
		}
	}
	readCoefProbsCommon(&r, &model)
	// Validate by sampling four corners of the 4D table.
	if model[0][0][0][0][0] != 77 || model[1][1][5][5][2] != 77 ||
		model[1][0][3][2][1] != 77 || model[0][1][2][4][0] != 77 {
		t.Error("skip path mutated table")
	}
}

// TestReadCoefProbsWalksUpToTxModeBudget writes one skip bit per tx
// level allowed by the supplied TxMode and confirms no extra bits get
// consumed past the cap.
func TestReadCoefProbsWalksUpToTxModeBudget(t *testing.T) {
	cases := []struct {
		mode       common.TxMode
		expectedTx int // how many leading skip bits we should emit
	}{
		{common.Only4x4, 1},  // tx 4x4 only
		{common.Allow8x8, 2}, // 4x4 + 8x8
		{common.Allow16x16, 3},
		{common.Allow32x32, 4},
		{common.TxModeSelect, 4}, // also caps at 32x32 in the table
	}
	for _, c := range cases {
		buf := make([]byte, 16)
		var w bitstream.Writer
		w.Start(buf)
		for range c.expectedTx {
			w.Write(0, 128) // skip update for this tx level
		}
		// Trailing sentinel bit so we can confirm the reader stopped at
		// the right spot.
		w.Write(1, 128)
		size, err := w.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}

		var fc FrameCoefProbs
		ReadCoefProbs(&r, &fc, c.mode)
		// The next bit should be the sentinel = 1.
		if got := r.Read(128); got != 1 {
			t.Errorf("tx_mode=%d: reader consumed extra bits past cap (sentinel got %d)", c.mode, got)
		}
	}
}

// TestReadCompressedHeaderLosslessKeyframe writes the minimal
// compressed-header bitstream for a lossless keyframe: TxMode forced
// to Only4x4 (no tx-mode read), one coef-probs skip bit, then three
// skip-probs no-update bits. Confirms the parser consumes exactly
// that and leaves every FrameContext slot untouched.
func TestReadCompressedHeaderLosslessKeyframe(t *testing.T) {
	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	// Coef-probs: a single 0 bit per tx level. Lossless → Only4x4
	// → only one tx level walked.
	w.Write(0, 128)
	// Skip-probs: three "no update" bits.
	for range SkipContexts {
		w.Write(0, DiffUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var fc FrameContext
	// Seed two arbitrary slots so we can detect any unintended write.
	fc.SkipProbs = [SkipContexts]uint8{11, 22, 33}
	fc.IntraInterProb[0] = 55

	out := ReadCompressedHeader(&r, &fc, ReadCompressedHeaderArgs{
		Lossless: true,
		KeyFrame: true,
	})
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}
	if fc.SkipProbs != [SkipContexts]uint8{11, 22, 33} {
		t.Errorf("SkipProbs changed: %v", fc.SkipProbs)
	}
	if fc.IntraInterProb[0] != 55 {
		t.Error("IntraInterProb[0] changed despite KeyFrame branch")
	}
}

// TestReadCompressedHeaderInterFrameSelectMode writes a compressed
// header for an inter frame with TxMode = TxModeSelect, no probability
// updates anywhere along the long inter path. Confirms the parser
// walks every fragment without panicking.
func TestReadCompressedHeaderInterFrameSelectMode(t *testing.T) {
	buf := make([]byte, 512)
	var w bitstream.Writer
	w.Start(buf)
	// TxMode select: bits 11 then 1.
	w.Write(1, 128) // bit1
	w.Write(1, 128) // bit0  → Allow32x32 = 3
	w.Write(1, 128) // promote to TxModeSelect
	// TxModeProbs: 12 "no update" bits.
	for range 12 {
		w.Write(0, DiffUpdateProb)
	}
	// CoefProbs: 4 skip bits (Allow32x32 caps at 32x32 = all 4 levels).
	for range 4 {
		w.Write(0, 128)
	}
	// SkipProbs: 3 no-update bits.
	for range SkipContexts {
		w.Write(0, DiffUpdateProb)
	}
	// InterModeProbs: 7 contexts × 3 mode-prob slots = 21 no-updates.
	for range common.InterModeContexts * (common.InterModes - 1) {
		w.Write(0, DiffUpdateProb)
	}
	// SwitchableInterpProbs skipped (InterpFilter != Switchable).
	// IntraInterProbs: 4 no-update bits.
	for range common.IntraInterContexts {
		w.Write(0, DiffUpdateProb)
	}
	// ReferenceMode: compound disallowed → 0 bits read. We pass
	// CompoundRefAllowed=false to skip the 1-or-2-bit prefix and the
	// gating subtables.
	// SingleReference path runs: COMP_INTER skipped, single_ref runs
	// (RefContexts × 2 slots), comp_ref skipped.
	for range common.RefContexts * 2 {
		w.Write(0, DiffUpdateProb)
	}
	// YModeProbs: 4 × (10-1) = 36 no-update bits.
	for range BlockSizeGroups * (common.IntraModes - 1) {
		w.Write(0, DiffUpdateProb)
	}
	// PartitionProbs: 16 × 3 = 48 no-update bits.
	for range common.PartitionContexts * (common.PartitionTypes - 1) {
		w.Write(0, DiffUpdateProb)
	}
	// MvProbs (no_hp): 65 no-update bits.
	for range 65 {
		w.Write(0, MvUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var fc FrameContext
	out := ReadCompressedHeader(&r, &fc, ReadCompressedHeaderArgs{
		KeyFrame:             false,
		IntraOnly:            false,
		InterpFilter:         InterpEighttap,
		AllowHighPrecisionMv: false,
		CompoundRefAllowed:   false,
	})
	if out.TxMode != common.TxModeSelect {
		t.Errorf("TxMode = %d, want TxModeSelect", out.TxMode)
	}
	if out.ReferenceMode != SingleReference {
		t.Errorf("ReferenceMode = %d, want SingleReference", out.ReferenceMode)
	}
}

func TestReadFrameReferenceModeNotAllowed(t *testing.T) {
	// When compound is disallowed the function reads no bits.
	var r bitstream.Reader
	// Init needs at least one byte; a zero byte fails the marker check
	// but we never call Init when compoundAllowed=false. The function
	// signature lets us pass a zero-valued reader.
	if got := ReadFrameReferenceMode(&r, false); got != SingleReference {
		t.Errorf("got %d, want SingleReference", got)
	}
}

func TestReadFrameReferenceModeAllowed(t *testing.T) {
	cases := []struct {
		name string
		bits []uint32
		want ReferenceMode
	}{
		{"single", []uint32{0}, SingleReference},
		{"compound", []uint32{1, 0}, CompoundReference},
		{"select", []uint32{1, 1}, ReferenceModeSelect},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := make([]byte, 16)
			var w bitstream.Writer
			w.Start(buf)
			for _, b := range c.bits {
				w.Write(b, 128)
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			var r bitstream.Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if got := ReadFrameReferenceMode(&r, true); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestUpdateMvProbsNoUpdates(t *testing.T) {
	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	// Five "no update" bits.
	for range 5 {
		w.Write(0, MvUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	probs := []uint8{10, 20, 30, 40, 50}
	want := append([]uint8(nil), probs...)
	UpdateMvProbs(&r, probs)
	for i := range probs {
		if probs[i] != want[i] {
			t.Errorf("probs[%d] = %d, want %d", i, probs[i], want[i])
		}
	}
}

func TestUpdateMvProbsWithUpdate(t *testing.T) {
	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	// Slot 0: no update. Slot 1: update with literal 0x5A (= 90).
	w.Write(0, MvUpdateProb)
	w.Write(1, MvUpdateProb)
	const literal = 0x5A
	for b := 6; b >= 0; b-- {
		w.Write(uint32((literal>>uint(b))&1), 128)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	probs := []uint8{77, 88}
	UpdateMvProbs(&r, probs)
	if probs[0] != 77 {
		t.Errorf("probs[0] = %d, want 77 (preserved)", probs[0])
	}
	wantSlot1 := uint8((literal << 1) | 1)
	if probs[1] != wantSlot1 {
		t.Errorf("probs[1] = %d, want %d", probs[1], wantSlot1)
	}
}

// TestReadMvProbsNoUpdatesPreservesAll writes a bitstream of "no
// update" bits matching the exact number of probability slots
// read_mv_probs touches in (allow_hp=false / allow_hp=true) mode and
// confirms every slot in the NmvContext keeps its prior value.
func TestReadMvProbsNoUpdatesPreservesAll(t *testing.T) {
	cases := []struct {
		name    string
		allowHp bool
		slots   int
	}{
		// joints: 3
		// per comp (×2): sign(1) + classes(10) + class0(1) + bits(10) = 22
		// per comp (×2): class0_fp(2 ×3) + fp(3) = 9
		// allow_hp: per comp (×2): class0_hp(1) + hp(1) = 2 each
		{
			name:    "no_hp",
			allowHp: false,
			slots:   3 + 2*22 + 2*9, // = 65
		},
		{
			name:    "with_hp",
			allowHp: true,
			slots:   3 + 2*22 + 2*9 + 2*2, // = 69
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := make([]byte, 64)
			var w bitstream.Writer
			w.Start(buf)
			for range c.slots {
				w.Write(0, MvUpdateProb)
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			var r bitstream.Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}

			var ctx NmvContext
			// Seed every slot so we can detect any unintended write.
			seed := uint8(33)
			ctx.Joints[0] = seed
			ctx.Joints[1] = seed
			ctx.Joints[2] = seed
			for i := range 2 {
				cc := &ctx.Comps[i]
				cc.Sign = seed
				for j := range cc.Classes {
					cc.Classes[j] = seed
				}
				for j := range cc.Class0 {
					cc.Class0[j] = seed
				}
				for j := range cc.Bits {
					cc.Bits[j] = seed
				}
				for j := range cc.Class0Fp {
					for k := range cc.Class0Fp[j] {
						cc.Class0Fp[j][k] = seed
					}
				}
				for j := range cc.Fp {
					cc.Fp[j] = seed
				}
				cc.Class0Hp = seed
				cc.Hp = seed
			}

			ReadMvProbs(&r, &ctx, c.allowHp)

			check := func(name string, got uint8) {
				if got != seed {
					t.Errorf("%s changed: got %d, want %d", name, got, seed)
				}
			}
			for i, v := range ctx.Joints {
				check("Joints", v)
				_ = i
			}
			for i := range 2 {
				cc := &ctx.Comps[i]
				check("Sign", cc.Sign)
				for _, v := range cc.Classes {
					check("Classes", v)
				}
				for _, v := range cc.Class0 {
					check("Class0", v)
				}
				for _, v := range cc.Bits {
					check("Bits", v)
				}
				for _, row := range cc.Class0Fp {
					for _, v := range row {
						check("Class0Fp", v)
					}
				}
				for _, v := range cc.Fp {
					check("Fp", v)
				}
				check("Class0Hp", cc.Class0Hp)
				check("Hp", cc.Hp)
			}
		})
	}
}

func TestNmvComponentLayout(t *testing.T) {
	// Spot-check the constants — these are wire-stable and any drift
	// from libvpx would corrupt every MV update frame-wide.
	if MvJoints != 4 || MvClasses != 11 || Class0Size != 2 || MvOffsetBits != 10 || MvFpSize != 4 {
		t.Fatalf("MV constants wrong: joints=%d classes=%d class0=%d offset=%d fp=%d",
			MvJoints, MvClasses, Class0Size, MvOffsetBits, MvFpSize)
	}
}

// TestReadTxModeRoundTrip writes each of the five TxMode values via
// the boolean-coder encoding libvpx's read_tx_mode expects and
// confirms the decoder recovers the original.
func TestReadTxModeRoundTrip(t *testing.T) {
	cases := []common.TxMode{
		common.Only4x4,
		common.Allow8x8,
		common.Allow16x16,
		common.Allow32x32,
		common.TxModeSelect,
	}
	for _, want := range cases {
		buf := make([]byte, 32)
		var w bitstream.Writer
		w.Start(buf)
		// 2 bits: bit0 then bit1 (writeLiteral-style MSB-first is what
		// ReadLiteral expects).
		raw := uint32(want)
		if want > common.Allow32x32 {
			raw = uint32(common.Allow32x32)
		}
		w.Write((raw>>1)&1, 128)
		w.Write(raw&1, 128)
		if want >= common.Allow32x32 {
			if want == common.TxModeSelect {
				w.Write(1, 128)
			} else {
				w.Write(0, 128)
			}
		}
		size, err := w.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadTxMode(&r); got != want {
			t.Errorf("tx_mode %d: got %d", want, got)
		}
	}
}

// TestReadTxModeProbsNoUpdates confirms that when every update bit is
// 0 the probability table is preserved unchanged.
func TestReadTxModeProbsNoUpdates(t *testing.T) {
	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	// 18 update slots total (2*1 + 2*2 + 2*3 = 12... wait: 2*(1) + 2*(2)
	// + 2*(3) = 12). Each emits a "0" against DIFF_UPDATE_PROB.
	for range 12 {
		w.Write(0, DiffUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var tp TxProbs
	// Seed with sentinel values so we can detect any unintended write.
	for i := range tp.P8x8 {
		for j := range tp.P8x8[i] {
			tp.P8x8[i][j] = 33
		}
	}
	for i := range tp.P16x16 {
		for j := range tp.P16x16[i] {
			tp.P16x16[i][j] = 44
		}
	}
	for i := range tp.P32x32 {
		for j := range tp.P32x32[i] {
			tp.P32x32[i][j] = 55
		}
	}

	ReadTxModeProbs(&r, &tp)
	for i := range tp.P8x8 {
		for j := range tp.P8x8[i] {
			if tp.P8x8[i][j] != 33 {
				t.Errorf("P8x8[%d][%d] = %d, want 33 preserved", i, j, tp.P8x8[i][j])
			}
		}
	}
	for i := range tp.P16x16 {
		for j := range tp.P16x16[i] {
			if tp.P16x16[i][j] != 44 {
				t.Errorf("P16x16[%d][%d] = %d, want 44 preserved", i, j, tp.P16x16[i][j])
			}
		}
	}
	for i := range tp.P32x32 {
		for j := range tp.P32x32[i] {
			if tp.P32x32[i][j] != 55 {
				t.Errorf("P32x32[%d][%d] = %d, want 55 preserved", i, j, tp.P32x32[i][j])
			}
		}
	}
}

func TestReadSkipProbsNoUpdates(t *testing.T) {
	buf := make([]byte, 16)
	var w bitstream.Writer
	w.Start(buf)
	for range SkipContexts {
		w.Write(0, DiffUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	probs := [SkipContexts]uint8{42, 84, 168}
	ReadSkipProbs(&r, &probs)
	if probs != [SkipContexts]uint8{42, 84, 168} {
		t.Errorf("skip probs changed: %v", probs)
	}
}
