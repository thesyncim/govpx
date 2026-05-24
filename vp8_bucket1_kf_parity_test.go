//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8Bucket1KFParity pins byte-exact parity on the three bucket-1 fuzz
// seeds for the 1280x720 SSIM keyframe path. The parity scoreboard reported a
// "+33 first_part / +49 total" keyframe divergence for these seeds; refreshing
// the keyframe picker zbin_extra under segmentation_enabled closes the
// residual gap.
//
// Cohort:
//   - 1280×720, GoodQuality or BestQuality (NOT realtime), cpu_used=0
//   - threads=4, tune=SSIM, screen_content=1, token-parts=1
//   - arnr.maxframes=1, varied arnr.type/strength
//   - error_resilient on/off, RC = CBR/VBR
//
// The seeds replay the fuzz corpus entries
// testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_*.
// Resolving the option-grid bucket bytes (vp8OracleRuntimeControlFuzzBytes.pick):
//
//	"A"    -> 19981bff : deadline=Best, rc=VBR, er=true
//	"A120" -> 22f3d67c : deadline=Good, rc=VBR, er=false
//	"A1"   -> 788d442c : deadline=Good, rc=CBR, er=true
//
// All three share sc=1, tune=SSIM, threads=4, token-parts=1, arnr=1/*/*.
// Historic keyframe size triples before segmentation-enabled zbin refresh:
//
//	seed       govpx_total libvpx_total  first_part_Δ  total_Δ
//	19981bff   145546      145497        +33           +49
//	22f3d67c   145545      145496        +33           +49
//	788d442c   145546      145497        +33           +49
//
// Current state: all three full streams are byte-identical end-to-end
// (keyframe + inter).
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:427-438
//     (vp8cx_mb_init_quantizer call under xd->segmentation_enabled).
//   - libvpx v1.16.0 vp8/encoder/onyx_if.c:3779
//     (cyclic_background_refresh flips segmentation_enabled on every CBR KF).
//   - vp8_encoder_reconstruct.go buildReconstructingKeyFrameCoefficients
//     (govpx picker honors per-MB tunedZbinAdjustment when segmentation
//     is enabled — picked up by both CBR and VBR seeds in this cohort).
func TestVP8Bucket1KFParity(t *testing.T) {
	vp8test.RequireOracle(t, "the bucket-1 keyframe parity pin")
	vpxencOracle := vp8test.VpxencOracle(t)

	cases := []struct {
		label           string
		data            []byte
		wantKFLen       int
		wantKFFirstPart int
		wantInterLen    int
	}{
		{label: "19981bff", data: []byte("A"), wantKFLen: 145497, wantKFFirstPart: 20442, wantInterLen: 1828},
		{label: "22f3d67c", data: []byte("A120"), wantKFLen: 145496, wantKFFirstPart: 20441, wantInterLen: 6324},
		{label: "788d442c", data: []byte("A1"), wantKFLen: 145497, wantKFFirstPart: 20442, wantInterLen: 1781},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			cfg := newOptionGridFuzzCase(tc.data)
			opts, libvpxArgs := cfg.buildOpts()
			sources := cfg.buildSources()

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "bucket1-kf-"+tc.label, opts, cfg.targetKbps, sources, libvpxArgs)
			if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
				t.Fatalf("expected ≥2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			gKF, lKF := govpxFrames[0], libvpxFrames[0]
			gInt, lInt := govpxFrames[1], libvpxFrames[1]

			if got := len(gKF); got != tc.wantKFLen {
				t.Fatalf("govpx KF len drift: got=%d want=%d", got, tc.wantKFLen)
			}
			if got := len(lKF); got != tc.wantKFLen {
				t.Fatalf("libvpx KF len drift: got=%d want=%d", got, tc.wantKFLen)
			}
			if got, _ := parseVP8FramePartitionSizes(gKF); got != tc.wantKFFirstPart {
				t.Fatalf("govpx KF first_partition drift: got=%d want=%d", got, tc.wantKFFirstPart)
			}
			if got, _ := parseVP8FramePartitionSizes(lKF); got != tc.wantKFFirstPart {
				t.Fatalf("libvpx KF first_partition drift: got=%d want=%d", got, tc.wantKFFirstPart)
			}
			if !bytes.Equal(gKF, lKF) {
				t.Fatalf("KF byte mismatch on %s: first diff offset %d", tc.label, testutil.FirstByteDiff(gKF, lKF))
			}
			if got := len(gInt); got != tc.wantInterLen {
				t.Fatalf("govpx inter len drift: got=%d want=%d", got, tc.wantInterLen)
			}
			if got := len(lInt); got != tc.wantInterLen {
				t.Fatalf("libvpx inter len drift: got=%d want=%d", got, tc.wantInterLen)
			}
			if !bytes.Equal(gInt, lInt) {
				t.Fatalf("inter byte mismatch on %s: first diff offset %d", tc.label, testutil.FirstByteDiff(gInt, lInt))
			}

			kfSHA := sha256.Sum256(gKF)
			intSHA := sha256.Sum256(gInt)
			t.Logf("bucket-1 keyframe parity %s pinned: KF=%dB first_part=%d sha=%s inter=%dB sha=%s",
				tc.label, tc.wantKFLen, tc.wantKFFirstPart,
				hex.EncodeToString(kfSHA[:8]), tc.wantInterLen,
				hex.EncodeToString(intSHA[:8]))
		})
	}
}
