package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

// BenchmarkVP9EncodeCPUUsed measures govpx's per-frame encode time across
// representative cpu_used lanes: 0, 4, 6, 8.
// Frame size is 640x360 in line with the realtime CBR oracle target. The
// benchmark refreshes a reusable panning-YCbCr source while the timer is
// stopped so source-frame generation stays outside the timed encode loop.
//
// Invoke with:
//
//	go test -run none -bench BenchmarkVP9EncodeCPUUsed -benchmem -short .
func BenchmarkVP9EncodeCPUUsed(b *testing.B) {
	cpuUsedValues := []int{0, 4, 6, 8}
	const width, height = 640, 360
	for _, cpuUsed := range cpuUsedValues {
		var name string
		switch cpuUsed {
		case 0:
			name = "cpu0"
		case 4:
			name = "cpu4"
		case 6:
			name = "cpu6"
		case 8:
			name = "cpu8"
		}
		b.Run(name, func(b *testing.B) {
			deadline := DeadlineRealtime
			if cpuUsed == 0 {
				deadline = DeadlineBestQuality
			}
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:    width,
				Height:   height,
				Deadline: deadline,
				CpuUsed:  int8(cpuUsed),
			})
			if err != nil {
				b.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()
			src := vp9test.NewPanningYCbCr(width, height, 0)
			dst := make([]byte, width*height*2)
			for i := range 8 {
				vp9test.FillPanningYCbCr(src, i)
				if _, err := e.EncodeInto(src, dst); err != nil {
					b.Fatalf("warmup EncodeInto[%d]: %v", i, err)
				}
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				vp9test.FillPanningYCbCr(src, i+8)
				b.StartTimer()
				if _, err := e.EncodeInto(src, dst); err != nil {
					b.Fatalf("EncodeInto[%d]: %v", i, err)
				}
			}
		})
	}
}

func TestVP9InterReferenceFramesEnabledUseAltrefOnepass(t *testing.T) {
	e := &VP9Encoder{}
	got := e.vp9InterReferenceFramesEnabled()
	if len(got) != 3 || got[2] != vp9dec.AltrefFrame {
		t.Fatalf("default refs = %v, want LAST/GOLDEN/ALTREF", got)
	}

	e.sf.UseNonrdPickMode = 1
	e.sf.UseAltrefOnepass = 0
	got = e.vp9InterReferenceFramesEnabled()
	if len(got) != 2 ||
		got[0] != vp9dec.LastFrame ||
		got[1] != vp9dec.GoldenFrame {
		t.Fatalf("nonrd no-altref refs = %v, want LAST/GOLDEN", got)
	}

	e.sf.UseAltrefOnepass = 1
	got = e.vp9InterReferenceFramesEnabled()
	if len(got) != 3 || got[2] != vp9dec.AltrefFrame {
		t.Fatalf("onepass-altref refs = %v, want LAST/GOLDEN/ALTREF", got)
	}
}

func TestVP9InterCompoundEnabledNonrdGate(t *testing.T) {
	e := &VP9Encoder{}
	if !e.vp9InterCompoundEnabled() {
		t.Fatalf("full-RD compound gate = false, want true")
	}
	e.sf.UseNonrdPickMode = 1
	if e.vp9InterCompoundEnabled() {
		t.Fatalf("nonrd compound gate = true, want false")
	}
	e.sf.UseCompoundNonrdPickmode = 1
	if !e.vp9InterCompoundEnabled() {
		t.Fatalf("enabled nonrd compound gate = false, want true")
	}
}

// TestVP9FrameIsKfGfArfMatchesLibvpx pins the libvpx
// frame_is_kf_gf_arf() condition govpx ports verbatim:
//
//	return frame_is_intra_only(&cpi->common) || cpi->refresh_alt_ref_frame ||
//	       (cpi->refresh_golden_frame && !cpi->rc.is_src_frame_alt_ref);
//
// libvpx: vp9_encoder.h:1013-1016 frame_is_kf_gf_arf,
// vp9_speed_features.c:38-40 frame_is_boosted.
func TestVP9FrameIsKfGfArfMatchesLibvpx(t *testing.T) {
	cases := []struct {
		name string
		ctx  vp9SpeedFrameContext
		want bool
	}{
		{
			name: "key_frame",
			ctx:  vp9SpeedFrameContext{frameType: common.KeyFrame, intraOnly: true},
			want: true,
		},
		{
			name: "intra_only_inter",
			ctx:  vp9SpeedFrameContext{frameType: common.InterFrame, intraOnly: true},
			want: true,
		},
		{
			name: "plain_inter_no_refresh",
			ctx:  vp9SpeedFrameContext{frameType: common.InterFrame},
			want: false,
		},
		{
			name: "refresh_alt_ref_only",
			ctx:  vp9SpeedFrameContext{frameType: common.InterFrame, refreshAltRefFrame: true},
			want: true,
		},
		{
			name: "refresh_golden_only",
			ctx:  vp9SpeedFrameContext{frameType: common.InterFrame, refreshGoldenFrame: true},
			want: true,
		},
		{
			name: "refresh_golden_but_src_is_alt_ref",
			ctx: vp9SpeedFrameContext{
				frameType:          common.InterFrame,
				refreshGoldenFrame: true,
				isSrcFrameAltRef:   true,
			},
			want: false,
		},
		{
			name: "refresh_alt_and_golden_src_alt_ref",
			ctx: vp9SpeedFrameContext{
				frameType:          common.InterFrame,
				refreshAltRefFrame: true,
				refreshGoldenFrame: true,
				isSrcFrameAltRef:   true,
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := vp9FrameIsKfGfArf(tc.ctx); got != tc.want {
				t.Fatalf("vp9FrameIsKfGfArf = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVP9GoodSpeedFeatureFramesizeIndependentBoostedOnGFFrame asserts the
// good-mode cpu_used=0 configurator picks the boosted-frame branch of
// UseSquarePartitionOnly when refresh_golden_frame is set on a non-key,
// non-altref-source frame on a small frame. Before the GF/ARF plumbing fix
// govpx incorrectly returned !boosted for every non-key frame, so the
// configurator picked UseSquarePartitionOnly=1 on GF refresh — diverging
// from libvpx which leaves UseSquarePartitionOnly=0 for boosted frames.
//
// libvpx: vp9_speed_features.c:238 — UseSquarePartitionOnly = !boosted at speed 0.
// libvpx: vp9_encoder.h:1013-1016 — frame_is_kf_gf_arf condition.
func TestVP9GoodSpeedFeatureFramesizeIndependentBoostedOnGFFrame(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    32,
		Height:   32,
		Deadline: DeadlineGoodQuality,
		CpuUsed:  0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	e.opts.CpuUsed = 0

	gfCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   false,
	})
	if !vp9FrameIsKfGfArf(gfCtx) {
		t.Fatal("GF refresh on non-altref-source frame should classify as boosted (KF/GF/ARF)")
	}
	var gfSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &gfSF, e.vp9SpeedFeatureCPUUsed(), gfCtx)
	if gfSF.UseSquarePartitionOnly != 0 {
		t.Fatalf("GF boosted frame UseSquarePartitionOnly = %d, want 0 (libvpx vp9_speed_features.c:238)",
			gfSF.UseSquarePartitionOnly)
	}
	// At speed 0 the trellis_opt_tx_rd.thresh is gated on boosted directly:
	// libvpx vp9_speed_features.c:243-244 — boosted ? 4.0 : 3.0.
	if gfSF.TrellisOptTxRd.Thresh != 4.0 {
		t.Fatalf("GF boosted frame TrellisOptTxRd.Thresh = %v, want 4.0 (libvpx vp9_speed_features.c:243-244)",
			gfSF.TrellisOptTxRd.Thresh)
	}

	plainCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: false,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   false,
	})
	if vp9FrameIsKfGfArf(plainCtx) {
		t.Fatal("plain inter frame (no refresh) should not classify as boosted")
	}
	var plainSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &plainSF, e.vp9SpeedFeatureCPUUsed(), plainCtx)
	if plainSF.UseSquarePartitionOnly != 1 {
		t.Fatalf("plain inter UseSquarePartitionOnly = %d, want 1 (libvpx vp9_speed_features.c:238)",
			plainSF.UseSquarePartitionOnly)
	}
	if plainSF.TrellisOptTxRd.Thresh != 3.0 {
		t.Fatalf("plain inter TrellisOptTxRd.Thresh = %v, want 3.0 (libvpx vp9_speed_features.c:243-244)",
			plainSF.TrellisOptTxRd.Thresh)
	}

	// ARF refresh frame (hidden, src-is-alt-ref true on the deferred show
	// frame; on the encode-time ARF the refresh_alt_ref_frame flag is set
	// and is_src_frame_alt_ref is false).
	arfCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          false,
		RefreshGoldenFrame: false,
		RefreshAltRefFrame: true,
		IsSrcFrameAltRef:   false,
	})
	if !vp9FrameIsKfGfArf(arfCtx) {
		t.Fatal("ARF refresh frame should classify as boosted (refresh_alt_ref_frame=true)")
	}
	var arfSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &arfSF, e.vp9SpeedFeatureCPUUsed(), arfCtx)
	if arfSF.UseSquarePartitionOnly != 0 {
		t.Fatalf("ARF boosted frame UseSquarePartitionOnly = %d, want 0", arfSF.UseSquarePartitionOnly)
	}

	// is_src_frame_alt_ref + refresh_golden suppresses the boost
	// classification (libvpx: !cpi->rc.is_src_frame_alt_ref gate).
	altSrcCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   true,
	})
	if vp9FrameIsKfGfArf(altSrcCtx) {
		t.Fatal("is_src_frame_alt_ref + refresh_golden should suppress the boost classification")
	}
}

func TestVP9RtSpeedFeaturesDisablePartitionCopyAfterDrop(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		Deadline:           DeadlineRealtime,
		CpuUsed:            7,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  300,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	ctx := func() vp9SpeedFrameContext {
		return e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
			ShowFrame:  true,
			BaseQIndex: 100,
		})
	}

	e.lastFrameDropped = false
	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx())
	if sf.CopyPartitionFlag != 1 {
		t.Fatalf("CopyPartitionFlag without prior drop = %d, want 1 (libvpx vp9_speed_features.c:722-728)",
			sf.CopyPartitionFlag)
	}
	if e.maxCopiedFrame != 2 {
		t.Fatalf("maxCopiedFrame without prior drop = %d, want 2 (libvpx vp9_speed_features.c:728)",
			e.maxCopiedFrame)
	}

	e.lastFrameDropped = true
	sf = SpeedFeatures{}
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx())
	if sf.CopyPartitionFlag != 0 {
		t.Fatalf("CopyPartitionFlag after prior drop = %d, want 0 (libvpx vp9_speed_features.c:722)",
			sf.CopyPartitionFlag)
	}
	if e.maxCopiedFrame != 0 {
		t.Fatalf("maxCopiedFrame after prior drop = %d, want 0", e.maxCopiedFrame)
	}
}

// TestVP9ApplySpeedFeaturesPerFrameRefreshesTxSizeSearchMethod pins the
// libvpx per-frame speed-feature refresh that
// encodeVP9FrameIntoWithFlagsResultInternal now invokes before the
// pickers fire. Mirrors libvpx vp9/encoder/vp9_encoder.c:3754 / 3765
// set_speed_features_framesize_{in,}dependent calls inside
// set_size_{in,}dependent_vars (vp9_encoder.c:4169-4170, 4377-4392):
// the SF dispatcher reads cm->frame_type via frame_is_kf_gf_arf, and
// at RT speed >= 5 (cpu_used >= 5, the govpx default) the keyframe
// branch picks USE_LARGESTALL while the non-keyframe branch picks
// USE_TX_8X8 (vp9_speed_features.c:1538-1542 / 1595-1597).
//
// Before this commit govpx pinned e.sf at compressor-create time and
// never re-applied, so non-key frames carried the keyframe-context
// USE_LARGESTALL value forever. This test verifies that
// vp9ApplySpeedFeatures applied with the live per-frame context
// produces the correct libvpx truth table.
func TestVP9ApplySpeedFeaturesPerFrameRefreshesTxSizeSearchMethod(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deadline  Deadline
		cpuUsed   int8
		isKey     bool
		intraOnly bool
		want      TxSizeSearchMethod
	}{
		{
			// libvpx vp9_speed_features.c:1539 — RT speed>=5 keyframe
			// path: USE_LARGESTALL.
			name:     "rt-cpu8-keyframe-uses-largestall",
			deadline: DeadlineRealtime, cpuUsed: 8, isKey: true,
			want: UseLargestAll,
		},
		{
			// libvpx vp9_speed_features.c:1541 — RT speed>=5 non-key
			// path: USE_TX_8X8.
			name:     "rt-cpu8-inter-uses-tx8x8",
			deadline: DeadlineRealtime, cpuUsed: 8,
			want: UseTx8x8,
		},
		{
			// libvpx vp9_speed_features.c:1541 — intra-only frames
			// have cm->frame_type == INTER_FRAME so the `is_keyframe`
			// branch falls through and USE_TX_8X8 applies. The non-
			// keyframe TxSize is the load-bearing assertion for the
			// per-frame refresh: prior to this commit govpx pinned
			// e.sf at create time so intra-only inherited whatever
			// the create-time keyframe context picked (USE_LARGESTALL).
			name:     "rt-cpu8-intra-only-uses-tx8x8",
			deadline: DeadlineRealtime, cpuUsed: 8, intraOnly: true,
			want: UseTx8x8,
		},
		{
			// libvpx vp9_speed_features.c:387 GOOD speed>=4 sets
			// tx_size_search_method = USE_LARGESTALL unconditionally
			// (speed 5+ does not override). govpx mirrors this.
			name:     "good-cpu5-inter-uses-largestall",
			deadline: DeadlineGoodQuality, cpuUsed: 5,
			want: UseLargestAll,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:    64,
				Height:   64,
				Deadline: tc.deadline,
				CpuUsed:  tc.cpuUsed,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			ctx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
				IsKey:     tc.isKey,
				IntraOnly: tc.intraOnly,
				ShowFrame: true,
			})
			e.vp9ApplySpeedFeatures(ctx)
			if got := e.sf.TxSizeSearchMethod; got != tc.want {
				t.Fatalf("TxSizeSearchMethod = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestVP9ApplySpeedFeaturesPerFrameTransitionsKeyToInter mirrors the
// libvpx per-frame refresh sequence: keyframe -> inter -> intra-only
// must each see the correct TxSizeSearchMethod, not a stale value
// from the previous frame. A stale create-time e.sf value would leave the
// keyframe USE_LARGESTALL setting on every inter frame.
func TestVP9ApplySpeedFeaturesPerFrameTransitionsKeyToInter(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    64,
		Height:   64,
		Deadline: DeadlineRealtime,
		CpuUsed:  8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// Frame 1: keyframe. libvpx vp9_speed_features.c:1539 -> USE_LARGESTALL.
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:     true,
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseLargestAll {
		t.Fatalf("keyframe TxSizeSearchMethod = %d, want UseLargestAll", got)
	}
	// Frame 2: inter. libvpx vp9_speed_features.c:1541 -> USE_TX_8X8.
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseTx8x8 {
		t.Fatalf("inter TxSizeSearchMethod = %d, want UseTx8x8 "+
			"(stale-from-keyframe bug)", got)
	}
	// Frame 3: intra-only. libvpx's is_keyframe predicate checks
	// cm->frame_type == KEY_FRAME literally and intra-only frames carry
	// INTER_FRAME — so the non-keyframe leg fires and USE_TX_8X8
	// applies (vp9_speed_features.c:1541).
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IntraOnly: true,
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseTx8x8 {
		t.Fatalf("intra-only TxSizeSearchMethod = %d, want UseTx8x8", got)
	}
	// Frame 4: back to inter -> USE_TX_8X8 (unchanged from frame 3).
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseTx8x8 {
		t.Fatalf("post-intra-only inter TxSizeSearchMethod = %d, want "+
			"UseTx8x8", got)
	}
	// Frame 5: another keyframe. SF refresh must flip back to
	// USE_LARGESTALL — the load-bearing inter->key transition.
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:     true,
		ShowFrame: true,
	}))
	if got := e.sf.TxSizeSearchMethod; got != UseLargestAll {
		t.Fatalf("second keyframe TxSizeSearchMethod = %d, want UseLargestAll "+
			"(stale-from-inter bug)", got)
	}
	// Sanity: common.KeyFrame import is used.
	_ = common.KeyFrame
}

// TestVP9SpeedFeaturesSVCDefaultsSingleLayer pins the SVC consumers in the
// speed-features dispatcher when the encoder is a single-layer encoder
// (cpi->use_svc = 0, svc->number_spatial_layers = 1,
// svc->number_temporal_layers = 1). Single-layer encoders see exactly the same
// configurator output libvpx produces for cpi->use_svc==0.
//
// libvpx: vp9_speed_features.c:517 (reference_masking), :640 (bias_golden),
// :644 (nonrd_keyframe), :647 (overshoot_detection RE_ENCODE), :659
// (use_source_sad), :689-693 (temporal_layer fork), :706-716 (svc spatial /
// temporal mv fork), :721-734 (copy_partition_flag / max_copied_frame),
// :738-741 (svc_use_lowres_part), :742-747 (use_gf_temporal_ref), :754-758
// (nonrd_keyframe + max_copied_frame), :771 (low_temp_var !use_svc), :845-848
// (previous_frame_is_intra_only), :850-857 (high_num_blocks_with_motion).
func TestVP9SpeedFeaturesSVCDefaultsSingleLayer(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           w,
		Height:          h,
		Deadline:        DeadlineRealtime,
		CpuUsed:         7,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// libvpx: vp9_svc_layercontext.h:80-208 — single-layer defaults.
	if e.svc.UseSvc {
		t.Errorf("e.svc.UseSvc = true, want false (cpi->use_svc default = 0)")
	}
	if got, want := e.svc.NumberSpatialLayers, 1; got != want {
		t.Errorf("e.svc.NumberSpatialLayers = %d, want %d", got, want)
	}
	if got, want := e.svc.NumberTemporalLayers, 1; got != want {
		t.Errorf("e.svc.NumberTemporalLayers = %d, want %d", got, want)
	}
	if e.svc.SpatialLayerID != 0 || e.svc.TemporalLayerID != 0 {
		t.Errorf("e.svc.SpatialLayerID/TemporalLayerID = (%d,%d), want (0,0)",
			e.svc.SpatialLayerID, e.svc.TemporalLayerID)
	}

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:517 — reference_masking = number_spatial_layers == 1.
	if sf.ReferenceMasking != 1 {
		t.Errorf("ReferenceMasking = %d, want 1 (libvpx vp9_speed_features.c:517 single layer)", sf.ReferenceMasking)
	}

	// libvpx: vp9_speed_features.c:640 — bias_golden = !cpi->use_svc (CBR non-screen, speed 5+).
	if sf.BiasGolden != 1 {
		t.Errorf("BiasGolden = %d, want 1 (libvpx vp9_speed_features.c:640 !use_svc)", sf.BiasGolden)
	}

	// libvpx: vp9_speed_features.c:644 — nonrd_keyframe stays 0 at speed 5+ when use_svc==0.
	// Note: speed-7 doesn't toggle this; speed-8 forces nonrd_keyframe = 1.
	if sf.NonrdKeyframe != 0 {
		t.Errorf("NonrdKeyframe = %d, want 0 at speed 7 (libvpx vp9_speed_features.c:644 !use_svc)", sf.NonrdKeyframe)
	}

	// libvpx: vp9_speed_features.c:659 — use_source_sad = !external_resize.
	if sf.UseSourceSad != 1 {
		t.Errorf("UseSourceSad = %d, want 1 (libvpx vp9_speed_features.c:659 !external_resize)", sf.UseSourceSad)
	}

	// libvpx: vp9_speed_features.c:721-734 — copy_partition_flag = 1 when not
	// SVC + not external_resize + not last_frame_dropped.
	if sf.CopyPartitionFlag != 1 {
		t.Errorf("CopyPartitionFlag = %d, want 1 (libvpx vp9_speed_features.c:727 single layer)", sf.CopyPartitionFlag)
	}

	// libvpx: vp9_speed_features.c:728 — single-layer max_copied_frame = 2.
	if e.maxCopiedFrame != 2 {
		t.Errorf("maxCopiedFrame = %d, want 2 (libvpx vp9_speed_features.c:728)", e.maxCopiedFrame)
	}

	// libvpx: vp9_speed_features.c:738-741 — svc_use_lowres_part requires SVC.
	if sf.SvcUseLowresPart != 0 {
		t.Errorf("SvcUseLowresPart = %d, want 0 (libvpx vp9_speed_features.c:738-741 !use_svc; also forced 0 at line 870)", sf.SvcUseLowresPart)
	}

	// libvpx: vp9_speed_features.c:747 — ref_frame_flags must keep VP9_GOLD_FLAG
	// when !use_svc.
	if e.refFrameFlags&encoder.GoldFlag == 0 {
		t.Errorf("refFrameFlags = %x, want VP9_GOLD_FLAG set (libvpx vp9_speed_features.c:747 !use_svc)", e.refFrameFlags)
	}

	// libvpx: vp9_speed_features.c:706-711 — base spatial / temporal NSTEP
	// switch requires number_temporal_layers > 2. Single layer keeps FAST_DIAMOND.
	if sf.Mv.SearchMethod != SearchMethodFastDiamond {
		t.Errorf("Mv.SearchMethod = %d, want FAST_DIAMOND (libvpx vp9_speed_features.c:702 default)", sf.Mv.SearchMethod)
	}
	if sf.Mv.FullpelSearchStepParam != 10 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 10 (libvpx vp9_speed_features.c:703 default)", sf.Mv.FullpelSearchStepParam)
	}

	// libvpx: vp9_speed_features.c:845-848 — previous_frame_is_intra_only=false.
	// Partition search type must NOT be forced to FIXED_PARTITION.
	if sf.PartitionSearchType == FixedPartition {
		t.Errorf("PartitionSearchType = FIXED, want non-FIXED (libvpx vp9_speed_features.c:845-848 previous_frame_is_intra_only=false)")
	}
}

// TestVP9SpeedFeaturesSVCTemporalLayerFork pins libvpx's speed-6/7 forks that
// engage when svc->temporal_layer_id > 0.
//
// libvpx: vp9_speed_features.c:689-693 (speed 6),
//
//	vp9_speed_features.c:712-716 (speed 7).
func TestVP9SpeedFeaturesSVCTemporalLayerFork(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           w,
		Height:          h,
		Deadline:        DeadlineRealtime,
		CpuUsed:         6,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// Force the SVC state directly to model libvpx cpi->svc.
	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 2
	e.svc.SpatialLayerID = 0
	e.svc.TemporalLayerID = 1

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 6, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 6, ctx)

	// libvpx: vp9_speed_features.c:690-692 — temporal_layer_id > 0 path.
	if sf.AdaptiveRdThresh != 4 {
		t.Errorf("AdaptiveRdThresh = %d, want 4 (libvpx vp9_speed_features.c:690 temporal_layer_id > 0)", sf.AdaptiveRdThresh)
	}
	if sf.LimitNewmvEarlyExit != 0 {
		t.Errorf("LimitNewmvEarlyExit = %d, want 0 (libvpx vp9_speed_features.c:691 temporal_layer_id > 0)", sf.LimitNewmvEarlyExit)
	}
	if sf.BaseMvAggressive != 1 {
		t.Errorf("BaseMvAggressive = %d, want 1 (libvpx vp9_speed_features.c:692 temporal_layer_id > 0)", sf.BaseMvAggressive)
	}

	// libvpx: vp9_speed_features.c:644 — non-base spatial layer forces nonrd_keyframe.
	// Base spatial layer (SpatialLayerID=0) does not engage this branch.
	if sf.NonrdKeyframe != 0 {
		t.Errorf("NonrdKeyframe = %d, want 0 (libvpx vp9_speed_features.c:644 spatial_layer_id == 0)", sf.NonrdKeyframe)
	}

	// Now bump SpatialLayerID = 1 and re-dispatch at speed 5+, expect
	// nonrd_keyframe=1. Rebuild the context so ctx.svc captures the updated
	// state.
	e.svc.SpatialLayerID = 1
	ctx = e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false
	sf = SpeedFeatures{}
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 5, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 5, ctx)
	if sf.NonrdKeyframe != 1 {
		t.Errorf("NonrdKeyframe = %d, want 1 (libvpx vp9_speed_features.c:644 spatial_layer_id > 0)", sf.NonrdKeyframe)
	}
}

// TestVP9SpatialSVCEncoderInheritsSVCState pins the layer-context wiring on
// the VP9SpatialSVCEncoder: every per-layer encoder must reflect the parent's
// number_spatial_layers and the layer's own spatial_layer_id before any
// speed-features dispatch.
//
// libvpx: vp9_svc_layercontext.c vp9_init_layer_context().
func TestVP9SpatialSVCEncoderInheritsSVCState(t *testing.T) {
	const (
		baseW, baseH = 320, 240
		topW, topH   = 640, 480
	)
	opts := VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
	}
	opts.Layers[0] = VP9EncoderOptions{
		Width:    baseW,
		Height:   baseH,
		Deadline: DeadlineRealtime,
		CpuUsed:  7,
	}
	opts.Layers[1] = VP9EncoderOptions{
		Width:    topW,
		Height:   topH,
		Deadline: DeadlineRealtime,
		CpuUsed:  7,
	}
	svc, err := NewVP9SpatialSVCEncoder(opts)
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()

	for i := range 2 {
		layer, err := svc.LayerEncoder(uint8(i))
		if err != nil {
			t.Fatalf("LayerEncoder(%d): %v", i, err)
		}
		if !layer.svc.UseSvc {
			t.Errorf("layer[%d].svc.UseSvc = false, want true", i)
		}
		if layer.svc.SpatialLayerID != i {
			t.Errorf("layer[%d].svc.SpatialLayerID = %d, want %d", i, layer.svc.SpatialLayerID, i)
		}
		if layer.svc.NumberSpatialLayers != 2 {
			t.Errorf("layer[%d].svc.NumberSpatialLayers = %d, want 2", i, layer.svc.NumberSpatialLayers)
		}
		if layer.svc.NumberTemporalLayers != 1 {
			t.Errorf("layer[%d].svc.NumberTemporalLayers = %d, want 1 (no temporal scalability configured)", i, layer.svc.NumberTemporalLayers)
		}
	}
}

// TestVP9SpeedFeaturesSVCSpeed8MaxCopiedFrame pins libvpx's speed-8
// max_copied_frame override that skips for SVC encoders.
//
// libvpx: vp9_speed_features.c:758.
func TestVP9SpeedFeaturesSVCSpeed8MaxCopiedFrame(t *testing.T) {
	t.Run("single-layer", func(t *testing.T) {
		const w, h = 320, 240
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:           w,
			Height:          h,
			Deadline:        DeadlineRealtime,
			CpuUsed:         8,
			RateControlMode: RateControlCBR,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()

		ctx := e.vp9DefaultSpeedFrameContext()
		ctx.frameType = common.InterFrame
		ctx.intraOnly = false

		var sf SpeedFeatures
		vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
		vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)

		// libvpx: vp9_speed_features.c:758 — !use_svc => max_copied_frame = 4.
		if e.maxCopiedFrame != 4 {
			t.Errorf("maxCopiedFrame = %d, want 4 (libvpx vp9_speed_features.c:758 !use_svc)", e.maxCopiedFrame)
		}
	})

	t.Run("svc-non-top-spatial", func(t *testing.T) {
		const w, h = 320, 240
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:           w,
			Height:          h,
			Deadline:        DeadlineRealtime,
			CpuUsed:         8,
			RateControlMode: RateControlCBR,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()

		e.svc.UseSvc = true
		e.svc.NumberSpatialLayers = 2
		e.svc.SpatialLayerID = 0

		ctx := e.vp9DefaultSpeedFrameContext()
		ctx.frameType = common.InterFrame
		ctx.intraOnly = false

		var sf SpeedFeatures
		vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
		vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)

		// libvpx: vp9_speed_features.c:758 — use_svc=true => skip setter, stays 0.
		if e.maxCopiedFrame != 0 {
			t.Errorf("maxCopiedFrame = %d, want 0 (libvpx vp9_speed_features.c:758 use_svc skip)", e.maxCopiedFrame)
		}
	})
}

// TestVP9SpeedFeaturesSVCSpeed8NonrdKeyframeFork pins libvpx's speed-8
// nonrd_keyframe fork that engages when SVC has multiple spatial layers and
// simulcast_mode is off.
//
// libvpx: vp9_speed_features.c:754-757.
func TestVP9SpeedFeaturesSVCSpeed8NonrdKeyframeFork(t *testing.T) {
	cases := []struct {
		name              string
		numSpatial        int
		simulcastMode     bool
		wantNonrdKeyframe int
	}{
		{"single-layer", 1, false, 1},
		{"svc-simulcast", 2, true, 1},
		{"svc-non-simulcast", 2, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const w, h = 320, 240
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:           w,
				Height:          h,
				Deadline:        DeadlineRealtime,
				CpuUsed:         8,
				RateControlMode: RateControlCBR,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()
			if tc.numSpatial > 1 {
				e.svc.UseSvc = true
				e.svc.NumberSpatialLayers = tc.numSpatial
			}
			e.svc.SimulcastMode = tc.simulcastMode

			ctx := e.vp9DefaultSpeedFrameContext()
			ctx.frameType = common.InterFrame
			ctx.intraOnly = false

			var sf SpeedFeatures
			vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)

			if sf.NonrdKeyframe != tc.wantNonrdKeyframe {
				t.Errorf("NonrdKeyframe = %d, want %d (libvpx vp9_speed_features.c:754-757)", sf.NonrdKeyframe, tc.wantNonrdKeyframe)
			}
		})
	}
}

// TestVP9SpeedFeaturesSVCPreviousFrameIntraOnly pins the post-cascade fixup
// that forces FIXED_PARTITION + BLOCK_64X64 when the previous frame was an
// intra-only SVC frame.
//
// libvpx: vp9_speed_features.c:845-848.
func TestVP9SpeedFeaturesSVCPreviousFrameIntraOnly(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           w,
		Height:          h,
		Deadline:        DeadlineRealtime,
		CpuUsed:         5,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 1
	e.svc.SpatialLayerID = 0
	e.svc.PreviousFrameIsIntraOnly = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 5, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 5, ctx)

	// libvpx: vp9_speed_features.c:846-847.
	if sf.PartitionSearchType != FixedPartition {
		t.Errorf("PartitionSearchType = %d, want FIXED (libvpx vp9_speed_features.c:846 previous_frame_is_intra_only)", sf.PartitionSearchType)
	}
	if sf.AlwaysThisBlockSize != common.Block64x64 {
		t.Errorf("AlwaysThisBlockSize = %d, want BLOCK_64X64 (libvpx vp9_speed_features.c:847)", sf.AlwaysThisBlockSize)
	}
}

// TestVP9SpeedFeaturesSVCScreenContentHighMotion pins the screen-content
// fixup that switches base-spatial motion search to NSTEP step_param=2 when
// high_num_blocks_with_motion or last_layer_dropped[0] is set.
//
// libvpx: vp9_speed_features.c:849-857.
func TestVP9SpeedFeaturesSVCScreenContentHighMotion(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             w,
		Height:            h,
		Deadline:          DeadlineRealtime,
		CpuUsed:           7,
		ScreenContentMode: int8(VP9ScreenContentScreen),
		RateControlMode:   RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 1
	e.svc.SpatialLayerID = 0
	e.svc.HighNumBlocksWithMotion = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:853-856.
	if sf.Mv.SearchMethod != SearchMethodNStep {
		t.Errorf("Mv.SearchMethod = %d, want NSTEP (libvpx vp9_speed_features.c:853 screen content high motion)", sf.Mv.SearchMethod)
	}
	if sf.Mv.FullpelSearchStepParam != 2 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 2 (libvpx vp9_speed_features.c:856)", sf.Mv.FullpelSearchStepParam)
	}
}
