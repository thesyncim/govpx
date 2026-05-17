package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVP9DecoderVpxencOracleProfile0StreamMatchesLibvpx(t *testing.T) {
	requireVP9VpxdecOracle(t)
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9MotionYCbCrForTest(width, height),
		newVP9CheckerYCbCrForTest(width, height, 48, 208, 96, 160),
		newVP9HorizontalBandsForTest(width, height, 112, 144),
		newVP9ChromaHorizontalBandsForTest(width, height),
	}
	raw := make([]byte, 0, len(frames)*(width*height+2*((width+1)>>1)*((height+1)>>1)))
	for _, frame := range frames {
		raw = appendVP9YCbCrI420(raw, frame)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, len(frames),
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	assertVpxencVP9StreamInfo(t, ivf)

	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}
	got, err := decodeVP9IVFVisibleI420(ivf)
	if err != nil {
		t.Fatalf("govpx Decode VP9 vpxenc IVF returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for vpxenc-vp9 Profile 0 stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9VpxencOracleDefaultCQKeyframeBaseQIndex(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	frame := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	raw := appendVP9YCbCrI420(nil, frame)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	first, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, first.Data)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("vpxenc-vp9 BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func requireVP9VpxencOracle(t *testing.T) {
	t.Helper()
	if _, err := coracle.VpxencVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxencVP9NotBuilt) {
			t.Skip("vpxenc-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxencVP9Path: %v", err)
	}
}

// skipVP9MLBasedPartitionInterByteParity defers oracle inter-frame byte-parity
// tests whose vpxenc reference path is libvpx realtime cpu_used=8 with
// width*height <= 352*288. At that speed/size libvpx forces
// SPEED_FEATURES.nonrd_use_ml_partition=1 in
// set_rt_speed_feature_framesize_independent (libvpx
// vp9/encoder/vp9_speed_features.c:751-768) and then promotes
// partition_search_type to ML_BASED_PARTITION (libvpx
// vp9/encoder/vp9_speed_features.c:825-826). encode_nonrd_sb_row then routes
// ML_BASED_PARTITION through get_estimated_pred + nonrd_pick_partition
// (libvpx vp9/encoder/vp9_encodeframe.c:5103 get_estimated_pred and
// vp9/encoder/vp9_encodeframe.c:5313-5321 case ML_BASED_PARTITION). With
// nonrd_pick_partition absent govpx returns the 64x64 / 32x32 root
// partition where libvpx's ML walker splits to 16x16, observable as the
// BLOCK_32X32 vs BLOCK_16X16 SbType drift in the lossless and checker
// inter packets and as the row-1 byte-length drift in the no-alt-ref
// lookahead fixture.
//
// Port status:
//
//   - Phase A (ml_predict_var_partitioning NN data tables + nn_predict
//     evaluator) is LANDED in vp9_partition_models.go (libvpx
//     vp9/encoder/vp9_partition_models.h:610-735 vp9_var_part_nn* +
//     vp9/encoder/vp9_encodeframe.c:2994-3038 nn_predict). The NN
//     constants are byte-identical to libvpx so a future caller can
//     consume the evaluator directly.
//   - Phase B (get_estimated_pred: vp9_int_pro_motion_estimation +
//     vp9_setup_pre_planes + vp9_build_inter_predictors_sb at
//     BLOCK_64X64, libvpx vp9/encoder/vp9_encodeframe.c:5103-5198)
//     is NOT yet ported. ml_predict_var_partitioning reads from
//     x->est_pred which is populated only by get_estimated_pred.
//   - Phase C (nonrd_pick_partition body + nonrd_pick_sb_modes +
//     vp9_pick_inter_mode dispatch, libvpx
//     vp9/encoder/vp9_encodeframe.c:4598-4855 and
//     vp9/encoder/vp9_pickmode.c) is NOT yet ported. The govpx
//     equivalent (pickVP9InterPartitionBlockSize) walks a different
//     RD search and does not implement the recursive PC_TREE-based
//     SPLIT/NONE/HORZ/VERT enumeration libvpx runs.
//
// Re-enable these tests once Phase B + Phase C land verbatim.
func skipVP9MLBasedPartitionInterByteParity(t *testing.T) {
	t.Helper()
	t.Skip("ML_BASED_PARTITION (cpu_used=8, w*h<=352*288, inter) not yet " +
		"ported: Phase A (NN tables + nn_predict) landed in " +
		"vp9_partition_models.go; Phase B (get_estimated_pred, libvpx " +
		"vp9/encoder/vp9_encodeframe.c:5103) and Phase C " +
		"(nonrd_pick_partition body + nonrd_pick_sb_modes + " +
		"vp9_pick_inter_mode, libvpx vp9/encoder/vp9_encodeframe.c:4598 + " +
		"vp9/encoder/vp9_pickmode.c) still pending. See " +
		"vp9/encoder/vp9_speed_features.c:751-826 for the speed-feature " +
		"gate and vp9/encoder/vp9_encodeframe.c:5313-5321 for the " +
		"dispatch.")
}

func appendVP9YCbCrI420(out []byte, img *image.YCbCr) []byte {
	width := img.Rect.Dx()
	height := img.Rect.Dy()
	for row := range height {
		start := row * img.YStride
		out = append(out, img.Y[start:start+width]...)
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		start := row * img.CStride
		out = append(out, img.Cb[start:start+uvWidth]...)
	}
	for row := range uvHeight {
		start := row * img.CStride
		out = append(out, img.Cr[start:start+uvWidth]...)
	}
	return out
}

func assertVpxencVP9StreamInfo(t *testing.T, ivf []byte) {
	t.Helper()
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	seenInter := false
	for index := 0; offset < len(ivf); index++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, index)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", index, err)
		}
		info, err := PeekVP9StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP9StreamInfo[%d]: %v", index, err)
		}
		if info.Profile != 0 {
			t.Fatalf("frame %d profile = %d, want 0", index, info.Profile)
		}
		if index == 0 && !info.KeyFrame {
			t.Fatalf("first vpxenc-vp9 frame was not a keyframe")
		}
		if index > 0 && !info.KeyFrame {
			seenInter = true
		}
		offset = next
	}
	if !seenInter {
		t.Fatalf("vpxenc-vp9 corpus did not produce an inter frame")
	}
}
