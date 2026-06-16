package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

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
