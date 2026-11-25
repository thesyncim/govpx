package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"math/rand"
	"testing"
)

// TestVP9NonrdReuseInterPredReconMatchesDecoder is the end-to-end gate for
// the reuse_inter_pred pick/commit handoff (libvpx vp9_pickmode.c:2668-2684
// winner retention + vp9_encodeframe.c:6073 encode-time rebuild skip): when
// the commit consumes the picker-retained luma predictor instead of
// re-convolving it, the encoder's committed reconstruction must still equal
// what a decoder reconstructs from the emitted bitstream, because the decoder
// always rebuilds the prediction from scratch. Any byte drift in the retained
// predictor would surface here as an encoder-recon/decoder-recon mismatch.
//
// The 144x80 geometry pins the clipped bottom SB row (80 luma rows = 10 mi
// rows, so the second SB row is a 16-pixel strip) whose >=8x8 walker leaves
// carry the nonrd_use_partition pred_pixel_ready seed without a matching
// var-part grid stamp — exactly the frame-edge reuse case; 128x128 pins the
// aligned interior. The panning source keeps the inter picker on non-zero
// motion so subpel predictors (not just zero-MV copies) cross the boundary.
func TestVP9NonrdReuseInterPredReconMatchesDecoder(t *testing.T) {
	for _, dims := range []struct{ w, h int }{
		{144, 80},  // clipped bottom SB strip (frame-edge reuse leaves)
		{128, 128}, // SB-aligned interior
	} {
		t.Run(fmt.Sprintf("%dx%d", dims.w, dims.h), func(t *testing.T) {
			testVP9NonrdReuseReconMatchesDecoder(t, dims.w, dims.h)
		})
	}
}

func testVP9NonrdReuseReconMatchesDecoder(t *testing.T, width, height int) {
	const frames = 12
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		FPS:               30,
		TargetBitrateKbps: 600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	uvW, uvH := (width+1)>>1, (height+1)>>1
	decoded := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvW*uvH),
		V:       make([]byte, uvW*uvH),
		YStride: width,
		UStride: uvW,
		VStride: uvW,
	}
	encodedInter := 0
	for i := range frames {
		src := vp9test.NewPanningYCbCr(width, height, i)
		data, err := e.Encode(src)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
		if len(data) == 0 {
			continue // rate-control drop: recon unchanged, nothing decoded
		}
		if _, err := d.DecodeInto(data, &decoded); err != nil {
			t.Fatalf("DecodeInto frame %d: %v", i, err)
		}
		if i > 0 {
			encodedInter++
		}
		comparePlane := func(plane int, name string, dec []byte,
			decStride, w, h int,
		) {
			recon, stride := e.vp9EncoderReconPlane(plane)
			if len(recon) == 0 || stride <= 0 {
				t.Fatalf("frame %d: encoder %s recon plane unavailable", i, name)
			}
			for y := range h {
				for x := range w {
					got := recon[y*stride+x]
					want := dec[y*decStride+x]
					if got != want {
						t.Fatalf("frame %d: %s recon mismatch at (%d,%d): encoder %d decoder %d",
							i, name, x, y, got, want)
					}
				}
			}
		}
		comparePlane(0, "Y", decoded.Y, decoded.YStride, width, height)
		comparePlane(1, "U", decoded.U, decoded.UStride, uvW, uvH)
		comparePlane(2, "V", decoded.V, decoded.VStride, uvW, uvH)
	}
	if encodedInter == 0 {
		t.Fatal("no inter frames were encoded; reuse boundary never exercised")
	}
}

func TestVP9NewmvDiffBiasNoiseInputs(t *testing.T) {
	cases := []struct {
		name        string
		ne          encoder.NoiseEstimateState
		wantEnabled bool
		wantMedium  bool
	}{
		{
			name: "disabled_high_value_stays_disabled",
			ne: encoder.NoiseEstimateState{
				Enabled: false,
				Thresh:  115,
				Value:   300,
			},
			wantEnabled: false,
			wantMedium:  false,
		},
		{
			name: "enabled_low_below_medium",
			ne: encoder.NoiseEstimateState{
				Enabled: true,
				Thresh:  115,
				Value:   90,
			},
			wantEnabled: true,
			wantMedium:  false,
		},
		{
			name: "enabled_medium_or_higher",
			ne: encoder.NoiseEstimateState{
				Enabled: true,
				Thresh:  115,
				Value:   116,
			},
			wantEnabled: true,
			wantMedium:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP9Encoder{noiseEstimate: tc.ne}
			gotEnabled, gotMedium := e.vp9NewmvDiffBiasNoiseInputs()
			if gotEnabled != tc.wantEnabled || gotMedium != tc.wantMedium {
				t.Fatalf("noise inputs = (%v,%v), want (%v,%v)",
					gotEnabled, gotMedium, tc.wantEnabled, tc.wantMedium)
			}
		})
	}
}

func TestVP9VarPartForceSkipLowTempVarOK(t *testing.T) {
	e := &VP9Encoder{}
	e.sf.ShortCircuitLowTempVar = 3
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); ok || force {
		t.Fatalf("missing cache force=%v ok=%v, want false/false", force, ok)
	}

	e.varPartSBVarLow = make([][25]uint8, 1)
	e.varPartSBComputed = make([]bool, 1)
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); ok || force {
		t.Fatalf("uncomputed cache force=%v ok=%v, want false/false", force, ok)
	}

	e.varPartSBComputed[0] = true
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); !ok || force {
		t.Fatalf("computed non-low cache force=%v ok=%v, want false/true", force, ok)
	}

	e.varPartSBVarLow[0][5] = 1
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); !ok || !force {
		t.Fatalf("computed low cache force=%v ok=%v, want true/true", force, ok)
	}
}

func BenchmarkVP9VarPartForceSkipLowTempVarOK(b *testing.B) {
	e := &VP9Encoder{}
	e.sf.ShortCircuitLowTempVar = 3
	const miCols = 160
	sbCount := ((90 + 7) >> 3) * ((miCols + 7) >> 3)
	e.varPartSBVarLow = make([][25]uint8, sbCount)
	e.varPartSBComputed = make([]bool, sbCount)
	for i := range e.varPartSBComputed {
		e.varPartSBComputed[i] = true
		e.varPartSBVarLow[i][5] = 1
		e.varPartSBVarLow[i][encoder.PosShift16x16[1][2]] = 1
	}
	cases := [...]struct {
		row, col int
		bsize    common.BlockSize
	}{
		{0, 0, common.Block64x64},
		{0, 8, common.Block32x64},
		{8, 0, common.Block64x32},
		{10, 12, common.Block16x16},
		{10, 12, common.Block32x16},
		{10, 12, common.Block16x32},
		{10, 12, common.Block8x8},
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		tc := cases[i%len(cases)]
		force, ok := e.vp9VarPartForceSkipLowTempVarOK(miCols, tc.row, tc.col, tc.bsize)
		if !ok && tc.bsize != common.Block8x8 {
			b.Fatalf("force-skip lookup returned !ok for %v", tc.bsize)
		}
		if force && tc.bsize == common.Block8x8 {
			b.Fatal("8x8 force-skip returned true")
		}
	}
}

func TestVP9UseModelYrdLargeBlockContentStateGate(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			RateControlMode:    RateControlCBR,
			RateControlModeSet: true,
			CpuUsed:            8,
		},
	}
	if !e.vp9UseModelYrdLargeBlock(common.Block32x32,
		encoder.ContentStateLowSadLowSumdiff) {
		t.Fatal("speed8 low-content Block32x32 = false, want true")
	}
	if e.vp9UseModelYrdLargeBlock(common.Block32x32,
		encoder.ContentStateVeryHighSad) {
		t.Fatal("speed8 very-high-SAD Block32x32 = true, want false")
	}
	if !e.vp9UseModelYrdLargeBlock(common.Block64x64,
		encoder.ContentStateVeryHighSad) {
		t.Fatal("speed8 very-high-SAD Block64x64 = false, want true")
	}

	e.opts.CpuUsed = 6
	if e.vp9UseModelYrdLargeBlock(common.Block32x32,
		encoder.ContentStateInvalid) {
		t.Fatal("speed6 Block32x32 = true, want false")
	}
	if !e.vp9UseModelYrdLargeBlock(common.Block64x64,
		encoder.ContentStateInvalid) {
		t.Fatal("speed6 Block64x64 = false, want true")
	}

	e.opts.RateControlModeSet = false
	if e.vp9UseModelYrdLargeBlock(common.Block64x64,
		encoder.ContentStateInvalid) {
		t.Fatal("rate-control-disabled Block64x64 = true, want false")
	}
}

// TestVP9NonrdInterModeCostTableMatchesTreeWalk pins the per-block hoisted
// inter-mode cost table plus the NmvCostTable NEWMV bit read against the
// tree-walking vp9NonrdInterModeRateCost for every mode across randomized
// probability contexts, MVs, and HP settings — the two paths must be
// value-identical for the picker's candidate scoring to stay byte-exact.
func TestVP9NonrdInterModeCostTableMatchesTreeWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(0x9e3779b9))
	modes := [...]common.PredictionMode{
		common.ZeroMv, common.NearestMv, common.NearMv, common.NewMv,
	}
	for trial := range 64 {
		var fc vp9dec.FrameContext
		vp9dec.ResetFrameContext(&fc)
		for i := range fc.InterModeProbs {
			for j := range fc.InterModeProbs[i] {
				fc.InterModeProbs[i][j] = uint8(1 + rng.Intn(255))
			}
		}
		fc.Nmvc.Joints = [3]uint8{uint8(1 + rng.Intn(255)),
			uint8(1 + rng.Intn(255)), uint8(1 + rng.Intn(255))}
		for axis := range fc.Nmvc.Comps {
			c := &fc.Nmvc.Comps[axis]
			c.Sign = uint8(1 + rng.Intn(255))
			for i := range c.Classes {
				c.Classes[i] = uint8(1 + rng.Intn(255))
			}
			for i := range c.Fp {
				c.Fp[i] = uint8(1 + rng.Intn(255))
			}
			c.Class0Hp = uint8(1 + rng.Intn(255))
			c.Hp = uint8(1 + rng.Intn(255))
		}
		allowHP := trial&1 == 0
		inter := &vp9InterEncodeState{
			mvCostFc:      fc,
			mvCostFcBuilt: true,
			allowHP:       allowHP,
		}
		var nmvTbl encoder.NmvCostTable
		if !nmvTbl.Build(&fc.Nmvc, allowHP) {
			t.Fatal("NmvCostTable.Build returned false")
		}
		for ctx := -1; ctx <= len(fc.InterModeProbs); ctx++ {
			tbl, ok := vp9NonrdInterModeCostTable(inter, ctx)
			ctxValid := ctx >= 0 && ctx < len(fc.InterModeProbs)
			if ok != ctxValid {
				t.Fatalf("trial %d ctx %d: table ok = %v, want %v",
					trial, ctx, ok, ctxValid)
			}
			for _, mode := range modes {
				mv := vp9dec.MV{
					Row: int16(rng.Intn(2049) - 1024),
					Col: int16(rng.Intn(2049) - 1024),
				}
				refMv := vp9dec.MV{
					Row: int16(rng.Intn(129) - 64),
					Col: int16(rng.Intn(129) - 64),
				}
				want := vp9NonrdInterModeRateCost(inter, ctx, mode, mv, refMv)
				got := 0
				if ok {
					got = tbl[encoder.ModeOffsetInter(mode)]
					if mode == common.NewMv {
						if c, tok := nmvTbl.MvBitCost(mv, refMv); tok {
							got += c
						} else {
							got += encoder.MvBitCost(mv, refMv, &fc.Nmvc, allowHP)
						}
					}
				}
				if got != want {
					t.Fatalf("trial %d ctx %d mode %v mv %v ref %v hp %v: table %d, tree %d",
						trial, ctx, mode, mv, refMv, allowHP, got, want)
				}
			}
		}
	}
	// Unbuilt cost context: both paths must be exactly zero.
	inter := &vp9InterEncodeState{}
	if _, ok := vp9NonrdInterModeCostTable(inter, 0); ok {
		t.Fatal("unbuilt context: table ok = true, want false")
	}
	if got := vp9NonrdInterModeRateCost(inter, 0, common.NewMv,
		vp9dec.MV{Row: 4, Col: 4}, vp9dec.MV{}); got != 0 {
		t.Fatalf("unbuilt context tree walk = %d, want 0", got)
	}
}
