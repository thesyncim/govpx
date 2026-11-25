package govpx

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9KeyframeCoeffRateUsesIntraModeScan(t *testing.T) {
	var e VP9Encoder
	for tx := range e.fc.CoefProbs {
		for plane := range e.fc.CoefProbs[tx] {
			for ref := range e.fc.CoefProbs[tx][plane] {
				for band := range e.fc.CoefProbs[tx][plane][ref] {
					for ctx := range e.fc.CoefProbs[tx][plane][ref][band] {
						for node := range e.fc.CoefProbs[tx][plane][ref][band][ctx] {
							e.fc.CoefProbs[tx][plane][ref][band][ctx][node] = 128
						}
					}
				}
			}
		}
	}

	const tx = common.Tx4x4
	mode := common.VPred
	dequant := [2]int16{16, 17}
	var coeffs [16]int16

	wantScan := common.GetScan(tx, 0, 0, false, mode)
	defaultScan := common.DefaultScanOrders[tx]
	found := false
	for raster := range coeffs {
		if raster == 0 {
			continue
		}
		coeffs = [16]int16{}
		coeffs[raster] = dequant[1]
		want := e.vp9KeyframeCoeffBlockRateCostPlane(tx, 0, wantScan,
			dequant, coeffs[:], 0)
		defaultRate := e.vp9KeyframeCoeffBlockRateCostPlane(tx, 0, defaultScan,
			dequant, coeffs[:], 0)
		if want != defaultRate {
			found = true
			got := e.vp9KeyframeCoeffBlockRateCost(tx, mode, false,
				dequant, coeffs[:], 0)
			if got != want {
				t.Fatalf("rate = %d, want %d from intra-mode scan", got, want)
			}
			if got == defaultRate {
				t.Fatalf("rate = default scan rate %d, want intra-mode scan rate %d",
					defaultRate, want)
			}
			break
		}
	}
	if !found {
		t.Fatalf("test setup did not find a coefficient that distinguishes scan orders")
	}
}

func TestVP9LeafKeyframeDecisionCache(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafKeyframeDecisionCache(2, 3)

	decision := vp9KeyframeModeDecision{
		mode: common.HPred,
		bmi: [4]vp9dec.Bmi{
			{AsMode: common.VPred},
			{AsMode: common.DcPred},
			{AsMode: common.TmPred},
			{AsMode: common.HPred},
		},
		txSize: common.Tx8x8,
		uvMode: common.TmPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 2, common.Block8x8, decision)

	if got, ok := e.lookupVP9LeafKeyframeDecision(1, 2, common.Block8x8); !ok {
		t.Fatalf("lookup miss")
	} else if got != decision {
		t.Fatalf("lookup = %+v, want %+v", got, decision)
	}
	if _, ok := e.lookupVP9LeafKeyframeDecision(1, 2, common.Block16x16); ok {
		t.Fatalf("lookup hit for wrong block size")
	}

	e.ensureVP9LeafKeyframeDecisionCache(2, 3)
	if _, ok := e.lookupVP9LeafKeyframeDecision(1, 2, common.Block8x8); ok {
		t.Fatalf("lookup hit after frame invalidation")
	}
}

func TestVP9KeyframeDecisionRegionSnapshotRestoresOnlyRegion(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafKeyframeDecisionCache(4, 4)
	e.ensureVP9KeyframePartitionDecisionCache(4, 4)

	inside := vp9KeyframeModeDecision{
		mode:   common.HPred,
		txSize: common.Tx8x8,
		uvMode: common.TmPred,
	}
	outside := vp9KeyframeModeDecision{
		mode:   common.VPred,
		txSize: common.Tx4x4,
		uvMode: common.DcPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 1, common.Block8x8, inside)
	e.storeVP9KeyframePartitionDecision(1, 1, common.Block16x16, common.Block8x8)
	e.storeVP9LeafKeyframeDecision(0, 0, common.Block8x8, outside)
	e.storeVP9KeyframePartitionDecision(0, 0, common.Block16x16, common.Block16x16)

	var snap vp9KeyframeDecisionRegionSnapshot
	if !e.snapshotVP9KeyframeDecisionRegion(4, 4, 1, 1, common.Block16x16, &snap) {
		t.Fatalf("snapshot failed")
	}

	mutatedInside := vp9KeyframeModeDecision{
		mode:   common.D45Pred,
		txSize: common.Tx16x16,
		uvMode: common.HPred,
	}
	mutatedOutside := vp9KeyframeModeDecision{
		mode:   common.D135Pred,
		txSize: common.Tx16x16,
		uvMode: common.VPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 1, common.Block16x16, mutatedInside)
	e.storeVP9KeyframePartitionDecision(1, 1, common.Block16x16, common.Block4x4)
	e.storeVP9LeafKeyframeDecision(0, 0, common.Block16x16, mutatedOutside)
	e.storeVP9KeyframePartitionDecision(0, 0, common.Block16x16, common.Block4x4)

	e.restoreVP9KeyframeDecisionRegion(&snap)

	if got, ok := e.lookupVP9LeafKeyframeDecision(1, 1, common.Block8x8); !ok {
		t.Fatalf("inside leaf lookup miss after restore")
	} else if got != inside {
		t.Fatalf("inside leaf = %+v, want %+v", got, inside)
	}
	if _, ok := e.lookupVP9LeafKeyframeDecision(1, 1, common.Block16x16); ok {
		t.Fatalf("inside mutated leaf survived restore")
	}
	if got, ok := e.lookupVP9KeyframePartitionDecision(1, 1, common.Block16x16); !ok {
		t.Fatalf("inside partition lookup miss after restore")
	} else if got != common.Block8x8 {
		t.Fatalf("inside partition = %v, want Block8x8", got)
	}

	if got, ok := e.lookupVP9LeafKeyframeDecision(0, 0, common.Block16x16); !ok {
		t.Fatalf("outside leaf lookup miss after restore")
	} else if got != mutatedOutside {
		t.Fatalf("outside leaf = %+v, want %+v", got, mutatedOutside)
	}
	if got, ok := e.lookupVP9KeyframePartitionDecision(0, 0, common.Block16x16); !ok {
		t.Fatalf("outside partition lookup miss after restore")
	} else if got != common.Block4x4 {
		t.Fatalf("outside partition = %v, want Block4x4", got)
	}
}

func TestVP9LeafDecisionTxSizeClamp(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafInterDecisionCache(2, 2)
	e.ensureVP9LeafKeyframeDecisionCache(2, 2)

	interDecision := vp9InterModeDecision{
		intra:  true,
		mode:   common.DcPred,
		txSize: common.Tx32x32,
	}
	e.storeVP9LeafInterDecision(0, 0, common.Block64x64, interDecision)
	keyDecision := vp9KeyframeModeDecision{
		mode:   common.HPred,
		txSize: common.Tx16x16,
		uvMode: common.DcPred,
	}
	e.storeVP9LeafKeyframeDecision(1, 1, common.Block16x16, keyDecision)

	e.clampVP9LeafDecisionTxSizes(common.Tx8x8)

	if got, ok := e.lookupVP9LeafInterDecision(0, 0, common.Block64x64); !ok {
		t.Fatalf("inter lookup miss after clamp")
	} else if got.txSize != common.Tx8x8 {
		t.Fatalf("inter tx size = %v, want %v", got.txSize, common.Tx8x8)
	}
	if got, ok := e.lookupVP9LeafKeyframeDecision(1, 1, common.Block16x16); !ok {
		t.Fatalf("keyframe lookup miss after clamp")
	} else if got.txSize != common.Tx8x8 {
		t.Fatalf("keyframe tx size = %v, want %v", got.txSize, common.Tx8x8)
	}
}

func TestVP9LeafInterDecisionCacheClearsTransientLumaPred(t *testing.T) {
	var e VP9Encoder
	e.ensureVP9LeafInterDecisionCache(2, 2)
	e.ensureVP9LeafInterRDDecisionCache(2, 2)

	decision := vp9InterModeDecision{
		mode:          common.NearestMv,
		refFrame:      vp9dec.LastFrame,
		refSlot:       0,
		interpFilter:  vp9dec.InterpEighttap,
		txSize:        common.Tx8x8,
		lumaPredReady: true,
	}
	e.storeVP9LeafInterDecision(0, 0, common.Block8x8, decision)
	if got, ok := e.lookupVP9LeafInterDecision(0, 0, common.Block8x8); !ok {
		t.Fatalf("inter lookup miss after store")
	} else if got.lumaPredReady {
		t.Fatalf("leaf cache preserved transient luma predictor readiness")
	}

	e.storeVP9LeafInterRDDecision(0, 1, common.Block8x8, decision)
	if got, ok := e.lookupVP9LeafInterRDDecision(0, 1, common.Block8x8); !ok {
		t.Fatalf("deep inter lookup miss after store")
	} else if got.lumaPredReady {
		t.Fatalf("deep leaf cache preserved transient luma predictor readiness")
	}
}

func TestVP9TileEncodeWorkerPreservesCountDecisionCaches(t *testing.T) {
	var src VP9Encoder
	vp9dec.SetupBlockPlanes(&src.planes, 1, 1)
	src.ensureVP9EncoderModeBuffers(2, 2)

	var worker VP9Encoder
	vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)
	worker.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, 4)
	worker.vp9LeafInterDecisionsRows = 2
	worker.vp9LeafInterDecisionsCols = 2
	worker.vp9LeafInterDecisionsVer = src.vp9LeafInterDecisionsVer
	interDecision := vp9InterModeDecision{
		mode:         common.NearestMv,
		interpFilter: vp9dec.InterpEighttap,
		rate:         17,
		distortion:   23,
		score:        41,
	}
	worker.storeVP9LeafInterDecision(0, 1, common.Block8x8, interDecision)

	worker.vp9LeafKeyframeDecisions = make([]vp9LeafKeyframeDecisionEntry, 4)
	worker.vp9LeafKeyframeDecisionsRows = 2
	worker.vp9LeafKeyframeDecisionsCols = 2
	worker.vp9LeafKeyframeDecisionsVer = src.vp9LeafKeyframeDecisionsVer
	keyDecision := vp9KeyframeModeDecision{
		mode: common.HPred,
		bmi: [4]vp9dec.Bmi{
			{AsMode: common.VPred},
			{AsMode: common.DcPred},
			{AsMode: common.TmPred},
			{AsMode: common.HPred},
		},
		txSize: common.Tx8x8,
		uvMode: common.TmPred,
	}
	worker.storeVP9LeafKeyframeDecision(1, 0, common.Block8x8, keyDecision)

	worker.prepareVP9TileEncodeWorker(&src, 2, 2)

	if got, ok := worker.lookupVP9LeafInterDecision(0, 1, common.Block8x8); !ok {
		t.Fatalf("inter lookup miss after tile encode worker prep")
	} else if got != interDecision {
		t.Fatalf("inter lookup = %+v, want %+v", got, interDecision)
	}
	if got, ok := worker.lookupVP9LeafKeyframeDecision(1, 0, common.Block8x8); !ok {
		t.Fatalf("keyframe lookup miss after tile encode worker prep")
	} else if got != keyDecision {
		t.Fatalf("keyframe lookup = %+v, want %+v", got, keyDecision)
	}
}

func TestVP9FrameParallelWorkerUsesPrivateDecisionCaches(t *testing.T) {
	var src VP9Encoder
	vp9dec.SetupBlockPlanes(&src.planes, 1, 1)
	src.ensureVP9EncoderModeBuffers(2, 2)

	var worker VP9Encoder
	vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)
	worker.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, 4)
	worker.vp9LeafKeyframeDecisions = make([]vp9LeafKeyframeDecisionEntry, 4)

	worker.prepareVP9FrameParallelWorker(&src, 2, 2, 16, 16)

	if len(worker.vp9LeafInterDecisions) != 4 {
		t.Fatalf("worker inter decision cache len = %d, want 4",
			len(worker.vp9LeafInterDecisions))
	}
	if len(worker.vp9LeafKeyframeDecisions) != 4 {
		t.Fatalf("worker keyframe decision cache len = %d, want 4",
			len(worker.vp9LeafKeyframeDecisions))
	}
	if &worker.vp9LeafInterDecisions[0] == &src.vp9LeafInterDecisions[0] {
		t.Fatalf("frame-parallel worker aliases parent inter decisions")
	}
	if &worker.vp9LeafKeyframeDecisions[0] == &src.vp9LeafKeyframeDecisions[0] {
		t.Fatalf("frame-parallel worker aliases parent keyframe decisions")
	}
}

// TestVP9KeyFrameFilteringChangesKeyframeBytes pins the libvpx wiring of
// VP9E_SET_KEY_FRAME_FILTERING (vp9/vp9_cx_iface.c:974) → ctx->extra_cfg.
// enable_keyframe_filtering → cpi->oxcf.enable_keyframe_filtering →
// vp9/encoder/vp9_encoder.c:6347-6364 vp9_temporal_filter(cpi, -1).
//
// When the gates pass, the encoder runs a forward-only temporal filter
// over the keyframe source against the lookahead window before encoding;
// the keyframe bytes diverge from a raw-source encode under the same
// configuration.  This test compares EnableKeyFrameFiltering=true vs
// false on a mixed-motion 8-frame lookahead and asserts the keyframe
// byte payload differs.
func TestVP9KeyFrameFilteringChangesKeyframeBytes(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableKF bool) []byte {
		opts := VP9EncoderOptions{
			Width:                   w,
			Height:                  h,
			FPS:                     30,
			LookaheadFrames:         8,
			AutoAltRef:              true,
			ARNRMaxFrames:           7,
			ARNRStrength:            3,
			ARNRType:                3,
			Deadline:                DeadlineGoodQuality,
			CpuUsed:                 1,
			RateControlModeSet:      true,
			RateControlMode:         RateControlQ,
			TargetBitrateKbps:       1000,
			CQLevel:                 32,
			MaxQuantizer:            63,
			EnableKeyFrameFiltering: enableKF,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder(enableKF=%v): %v", enableKF, err)
		}
		seq := newVP9TPLPanningSequence(w, h, 16)
		buf := make([]byte, 64*1024)
		var keyframeBytes []byte
		drain := func(res VP9EncodeResult) {
			if res.KeyFrame && len(keyframeBytes) == 0 {
				keyframeBytes = append(keyframeBytes, res.Data...)
			}
		}
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				drain(res)
			case errors.Is(err, ErrFrameNotReady):
			default:
				t.Fatalf("encode %d: %v", i, err)
			}
		}
		for {
			res, err := enc.FlushIntoWithResult(buf)
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			drain(res)
		}
		return keyframeBytes
	}
	off := encode(false)
	on := encode(true)
	if len(off) == 0 || len(on) == 0 {
		t.Fatalf("missing keyframe bytes off=%d on=%d", len(off), len(on))
	}
	if bytes.Equal(off, on) {
		t.Fatalf("EnableKeyFrameFiltering had no effect on keyframe bytes (size=%d both)",
			len(off))
	}
}

// TestVP9KeyFrameFilteringSetterRoundTrip pins the runtime control wiring:
// SetEnableKeyFrameFiltering toggles e.opts.EnableKeyFrameFiltering and
// allocates the ARNR scratch when both ARNR config and the toggle are
// active.  Mirrors libvpx's ctrl_set_keyframe_filtering
// (vp9/vp9_cx_iface.c:974) which writes through update_extra_cfg.
func TestVP9KeyFrameFilteringSetterRoundTrip(t *testing.T) {
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           64,
		Height:          64,
		FPS:             30,
		LookaheadFrames: 8,
		AutoAltRef:      true,
		ARNRMaxFrames:   7,
		ARNRStrength:    3,
		ARNRType:        3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if enc.opts.EnableKeyFrameFiltering {
		t.Fatalf("EnableKeyFrameFiltering default = true, want false")
	}
	if err := enc.SetEnableKeyFrameFiltering(true); err != nil {
		t.Fatalf("SetEnableKeyFrameFiltering(true): %v", err)
	}
	if !enc.opts.EnableKeyFrameFiltering {
		t.Fatalf("SetEnableKeyFrameFiltering(true) did not flip the option")
	}
	if len(enc.vp9ARNRScratch.Y) == 0 {
		t.Fatalf("ARNR scratch not allocated after SetEnableKeyFrameFiltering(true)")
	}
	if err := enc.SetEnableKeyFrameFiltering(false); err != nil {
		t.Fatalf("SetEnableKeyFrameFiltering(false): %v", err)
	}
	if enc.opts.EnableKeyFrameFiltering {
		t.Fatalf("SetEnableKeyFrameFiltering(false) did not clear the option")
	}
}

// TestVP9KeyFrameFilteringGateRespectsLibvpxPreconditions verifies the
// gate helper trips on every libvpx-listed precondition
// (vp9_encoder.c:6347-6353).  Each subtest flips one precondition off and
// asserts the gate returns false.
func TestVP9KeyFrameFilteringGateRespectsLibvpxPreconditions(t *testing.T) {
	base := VP9EncoderOptions{
		Width:                   64,
		Height:                  64,
		FPS:                     30,
		LookaheadFrames:         8,
		AutoAltRef:              true,
		ARNRMaxFrames:           7,
		ARNRStrength:            3,
		ARNRType:                3,
		Deadline:                DeadlineGoodQuality,
		CpuUsed:                 1,
		EnableKeyFrameFiltering: true,
	}
	for _, tc := range []struct {
		name string
		mut  func(*VP9EncoderOptions)
	}{
		{
			name: "Realtime deadline disables",
			mut:  func(o *VP9EncoderOptions) { o.Deadline = DeadlineRealtime },
		},
		{
			name: "Lossless disables",
			mut:  func(o *VP9EncoderOptions) { o.Lossless = true },
		},
		{
			name: "ARNRMaxFrames<=1 disables",
			mut:  func(o *VP9EncoderOptions) { o.ARNRMaxFrames = 1 },
		},
		{
			name: "ARNRStrength==0 disables",
			mut:  func(o *VP9EncoderOptions) { o.ARNRStrength = 0 },
		},
		{
			name: "CpuUsed>=2 disables",
			mut:  func(o *VP9EncoderOptions) { o.CpuUsed = 2 },
		},
		{
			name: "EnableKeyFrameFiltering==false disables",
			mut:  func(o *VP9EncoderOptions) { o.EnableKeyFrameFiltering = false },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			tc.mut(&opts)
			enc, err := NewVP9Encoder(opts)
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if enc.vp9KeyFrameFilteringActive() {
				t.Fatalf("gate returned true with %s", tc.name)
			}
		})
	}
	// Sanity: with the base config (all preconditions satisfied) the gate
	// is active.
	enc, err := NewVP9Encoder(base)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !enc.vp9KeyFrameFilteringActive() {
		t.Fatalf("gate returned false with the base config (all preconditions hold)")
	}
}

func TestVP9RealtimeSpeed5KeyframeHybridNonRDDispatch(t *testing.T) {
	for _, cpuUsed := range []int8{5, 7} {
		t.Run(fmt.Sprintf("cpu%d", cpuUsed), func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:    320,
				Height:   240,
				Deadline: DeadlineRealtime,
				CpuUsed:  cpuUsed,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()

			ctx := e.vp9DefaultSpeedFrameContext()
			ctx.frameType = common.KeyFrame
			ctx.intraOnly = false
			ctx.showFrame = true

			var sf SpeedFeatures
			speed := e.vp9SpeedFeatureCPUUsed()
			vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, speed, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &sf, speed, ctx)

			if sf.UseNonrdPickMode != 1 {
				t.Fatalf("UseNonrdPickMode = %d, want 1 (libvpx vp9_speed_features.c:606)", sf.UseNonrdPickMode)
			}
			if sf.NonrdKeyframe != 0 {
				t.Fatalf("NonrdKeyframe = %d, want 0 before speed 8 (libvpx vp9_speed_features.c:751)", sf.NonrdKeyframe)
			}
			if sf.TxSizeSearchMethod != UseLargestAll {
				t.Fatalf("TxSizeSearchMethod = %d, want USE_LARGESTALL on keyframes (libvpx vp9_speed_features.c:622)", sf.TxSizeSearchMethod)
			}
			for tx := range common.TxSizes {
				if sf.IntraUvModeMask[tx] != sfIntraDC {
					t.Fatalf("IntraUvModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:565)",
						tx, sf.IntraUvModeMask[tx], sfIntraDC)
				}
			}
		})
	}
}

func TestVP9KeyframeHybridNonRDIntraModeGate(t *testing.T) {
	var e VP9Encoder
	e.sf.UseNonrdPickMode = 1
	e.sf.NonrdKeyframe = 0

	if e.useVP9KeyframeNonRDIntraMode(common.Block8x8) {
		t.Fatalf("Block8x8 used non-RD; want RD arm when nonrd_keyframe=0")
	}
	if !e.useVP9KeyframeNonRDIntraMode(common.Block16x16) {
		t.Fatalf("Block16x16 used RD; want non-RD arm when use_nonrd_pick_mode=1")
	}

	e.sf.NonrdKeyframe = 1
	if !e.useVP9KeyframeNonRDIntraMode(common.Block8x8) {
		t.Fatalf("Block8x8 used RD; want non-RD arm when nonrd_keyframe=1")
	}

	e.sf.UseNonrdPickMode = 0
	if e.useVP9KeyframeNonRDIntraMode(common.Block64x64) {
		t.Fatalf("Block64x64 used non-RD without use_nonrd_pick_mode")
	}
}

func TestVP9KeyframeVariancePartitionRequiresNonRDRow(t *testing.T) {
	key := &vp9KeyframeEncodeState{
		hdr: &vp9dec.UncompressedHeader{FrameType: common.KeyFrame},
		dq:  &vp9dec.DequantTables{},
	}
	var e VP9Encoder
	e.sf.PartitionSearchType = VarBasedPartition

	if e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("keyframe variance partition enabled without use_nonrd_pick_mode")
	}

	e.sf.UseNonrdPickMode = 1
	if !e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("keyframe variance partition disabled on non-RD var-based row")
	}

	e.sf.PartitionSearchType = ReferencePartition
	if !e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("keyframe variance partition disabled on non-RD reference row")
	}

	key.lossless = true
	if e.vp9KeyframeVariancePartitionEnabled(key) {
		t.Fatalf("lossless keyframe enabled variance partition")
	}
}

func TestVP9KeyframeRDPartitionDispatchMirrorsLibvpxRows(t *testing.T) {
	key := &vp9KeyframeEncodeState{
		hdr: &vp9dec.UncompressedHeader{FrameType: common.KeyFrame},
		dq:  &vp9dec.DequantTables{},
	}
	var e VP9Encoder
	e.sf.PartitionSearchType = VarBasedPartition

	if !e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition disabled on var-based keyframe row")
	}

	e.sf.UseNonrdPickMode = 1
	if e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition enabled on non-RD row")
	}

	e.sf.UseNonrdPickMode = 0
	e.sf.PartitionSearchType = SearchPartition
	if !e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition disabled for search partition")
	}

	e.sf.PartitionSearchType = ReferencePartition
	if !e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition disabled for reference partition")
	}

	e.sf.PartitionSearchType = MlBasedPartition
	if !e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition disabled for ML partition")
	}

	e.sf.PartitionSearchType = FixedPartition
	if e.vp9KeyframeRDPartitionEnabled(key) {
		t.Fatalf("keyframe RD partition enabled for fixed partition")
	}
}

func TestVP9KeyframeRDPartitionBreakoutThresholds(t *testing.T) {
	var e VP9Encoder
	e.sf.PartitionSearchBreakoutThr.Dist = 1 << 19
	e.sf.PartitionSearchBreakoutThr.Rate = 80

	dist, rate := e.vp9KeyframeRDPartitionBreakoutThresholds(common.Block8x8)
	if dist != 1<<13 {
		t.Fatalf("Block8x8 dist threshold = %d, want %d", dist, 1<<13)
	}
	if rate != 80*int(common.NumPelsLog2Lookup[common.Block8x8]) {
		t.Fatalf("Block8x8 rate threshold = %d, want %d",
			rate, 80*int(common.NumPelsLog2Lookup[common.Block8x8]))
	}

	dist, rate = e.vp9KeyframeRDPartitionBreakoutThresholds(common.Block64x64)
	if dist != 1<<19 {
		t.Fatalf("Block64x64 dist threshold = %d, want %d", dist, 1<<19)
	}
	if rate != 80*int(common.NumPelsLog2Lookup[common.Block64x64]) {
		t.Fatalf("Block64x64 rate threshold = %d, want %d",
			rate, 80*int(common.NumPelsLog2Lookup[common.Block64x64]))
	}
}

func TestVP9KeyframeRDPartitionNoneBreakout(t *testing.T) {
	var e VP9Encoder
	e.sf.PartitionSearchBreakoutThr.Dist = 1 << 19
	e.sf.PartitionSearchBreakoutThr.Rate = 80

	dist, rate := e.vp9KeyframeRDPartitionBreakoutThresholds(common.Block64x64)
	rd := vp9KeyframePartitionRD{
		rate:       rate - 1,
		distortion: dist - 1,
		skippable:  true,
	}
	if !e.vp9KeyframeRDPartitionNoneBreakout(common.Block64x64, rd, false) {
		t.Fatalf("skippable low-rate PARTITION_NONE did not break out")
	}

	rd.rate = rate + 1
	rd.distortion = (dist >> 2) - 1
	if !e.vp9KeyframeRDPartitionNoneBreakout(common.Block64x64, rd, false) {
		t.Fatalf("very low distortion PARTITION_NONE did not break out")
	}

	rd.rate = rate - 1
	rd.distortion = dist - 1
	rd.skippable = false
	if e.vp9KeyframeRDPartitionNoneBreakout(common.Block64x64, rd, false) {
		t.Fatalf("non-skippable PARTITION_NONE broke out")
	}

	rd.skippable = true
	if e.vp9KeyframeRDPartitionNoneBreakout(common.Block64x64, rd, true) {
		t.Fatalf("lossless PARTITION_NONE broke out")
	}

	rd.rate = rate + 1
	rd.distortion = dist
	if e.vp9KeyframeRDPartitionNoneBreakout(common.Block64x64, rd, false) {
		t.Fatalf("high-rate high-distortion PARTITION_NONE broke out")
	}
}

func TestVP9KeyframeRDRectAllowedAfterSplitMiss(t *testing.T) {
	var e VP9Encoder

	if !e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block8x8, true, true) {
		t.Fatalf("Block8x8 split miss suppressed sub-8 rectangular candidates")
	}
	if e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block16x16, true, true) {
		t.Fatalf("Block16x16 split miss kept rectangular candidates with NONE allowed")
	}
	if !e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block16x16, false, true) {
		t.Fatalf("edge split miss suppressed rectangular candidates")
	}
	if e.vp9KeyframeRDRectAllowedAfterSplitMiss(common.Block8x8, true, false) {
		t.Fatalf("disabled rectangular search was re-enabled")
	}
}
