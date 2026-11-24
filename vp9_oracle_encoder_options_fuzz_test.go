//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"crypto/sha256"
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// vp9OptionsParityGapSeeds lists VP9 options-fuzz seed payloads whose strict
// byte parity is gated behind libvpx VP9 features govpx has not yet ported.
// Each entry cites the libvpx file:line that drives the divergence so the
// corresponding port can remove one entry at a time.
//
// Deferred seeds: none currently.
//
// History:
//
//   - "\x00010" (bytes 0x00,0x30,0x31,0x30) resolves to width=16,
//     height=208, fps=50, cpu_used=1, Deadline=Realtime, RateControl=CBR,
//     and TargetBitrateKbps=50. The residual divergence was the tile
//     partition/mode token stream: libvpx's rd_pick_partition keeps
//     PARTITION_HORZ/PARTITION_VERT live at the image edges even after the
//     square-vs-split breakout clears do_rect, via the
//     `(do_rect || vp9_active_h_edge(...))` /
//     `(do_rect || vp9_active_v_edge(...))` gates
//     (vp9/encoder/vp9_encodeframe.c:4034 and :4084, with the active-edge
//     helpers at vp9/encoder/vp9_rdopt.c:3375 and :3403). govpx's RD
//     partition scorer was missing that active-edge term, so it split the
//     narrow 16x208 keyframe into 16x16 leaves instead of the 32x64
//     PARTITION_VERT columns libvpx emits. Porting vp9_active_h_edge /
//     vp9_active_v_edge into the keyframe RD partition gate closed the seed to
//     byte parity.
//
// Reverting any entry here must be paired with the corresponding verbatim
// libvpx port landing.
var vp9OptionsParityGapSeeds = [][]byte{}

func vp9OptionsParityGapSeed(data []byte) bool {
	for _, seed := range vp9OptionsParityGapSeeds {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

func TestVP9OracleRealtimeCPU1KeyframePartitionHeaderParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 realtime cpu=1 keyframe partition header parity")
	vp9test.RequireVpxenc(t)

	seed := []byte{0x00, 0x30, 0x31, 0x30}
	opts := vp9oracle.NormalizeFuzzOptionsForLibvpxCLI(
		vp9EncoderOptionsFromFuzz(seed))
	src := vp9test.NewYCbCr(opts.Width, opts.Height, 128, 128, 128)

	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	got, err := enc.Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := tryVP9LibvpxKeyFrameBytes(t, opts, src)
	if len(want) == 0 {
		t.Fatal("vpxenc-vp9 rejected realtime cpu=1 keyframe seed")
	}
	gotHeader, gotTileStart := vp9test.ParseHeader(t, got)
	wantHeader, wantTileStart := vp9test.ParseHeader(t, want)
	vp9oracle.AssertKeyframeHeaderParity(t, gotHeader, wantHeader)

	gotComp, gotFC, gotUncSize := vp9test.ReadCompressedHeader(t, got, gotHeader)
	wantComp, wantFC, wantUncSize := vp9test.ReadCompressedHeader(t, want,
		wantHeader)
	if gotComp != wantComp {
		t.Fatalf("compressed header = %+v, want vpxenc %+v", gotComp, wantComp)
	}
	if gotFC != wantFC {
		t.Fatal("frame context after compressed header diverged from vpxenc")
	}
	gotCompBytes := got[gotUncSize:gotTileStart]
	wantCompBytes := want[wantUncSize:wantTileStart]
	if !bytes.Equal(gotCompBytes, wantCompBytes) {
		t.Fatalf("compressed header bytes = % x, want vpxenc % x",
			gotCompBytes, wantCompBytes)
	}
	if !bytes.Equal(got, want) {
		t.Logf("realtime cpu=1 keyframe tile payload still diverges: govpx_len=%d vpxenc_len=%d first_diff=%d",
			len(got), len(want), testutil.FirstByteDiff(got, want))
	}
}

// FuzzVP9OracleEncoderOptions complements FuzzVP9EncoderOptions (which only
// asserts no-panic + sentinel-error contracts on NewVP9Encoder) by adding the
// libvpx keyframe-byte-parity comparator that the VP8 sibling
// FuzzVP8EncoderOptions already enforces. For each fuzz iteration:
//
//   - Govpx rejects → documented sentinel error or contract bug (logged via
//     assertVP9FuzzEncoderConstructError).
//   - Govpx accepts, vpxenc-vp9 CLI rejects this shape → comparator
//     inapplicable, logged-only (mirrors FuzzVP8EncoderOptions). The fuzz
//     iteration keeps going.
//   - Both accept → keyframe bytes must SHA-256 match. Mismatch is t.Errorf
//     so divergences land as seed regressions under testdata/fuzz/.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary. Without the
// binary the fuzzer t.Skips cleanly so plain `go test` runs are green.
func FuzzVP9OracleEncoderOptions(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 option-validation oracle fuzz")
	vp9test.RequireVpxenc(f)
	// Seeds mirror FuzzVP9EncoderOptions shape but biased toward configs
	// the libvpx CLI accepts AND that the govpx VP9 encoder can keyframe
	// byte-identically to the libvpx CLI under the comparator
	// normalisation above. Configurations that intentionally exercise the
	// govpx CBR rate-controller / cpu_used speed-feature divergences
	// described in vp9oracle.NormalizeFuzzOptionsForLibvpxCLI are NOT in the seed
	// corpus -- those red configurations live as separate encoder-side
	// parity-gap list so the seed corpus stays green and any new red seed
	// captures genuinely new option-validation surface fallout. Byte
	// layouts decode via vp9EncoderOptionsFromFuzz (see
	// vp9_encoder_fuzz_test.go).
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		// Plausible 64×64 VBR config: cpu_used=8 (realtime bracket where
		// govpx and libvpx speed-features agree), VBR @ 300 kbps, min/max-q
		// 4..56, cq 32. CBR is not exercised in the seed corpus because
		// govpx's one-pass CBR rate controller picks a different base
		// qindex than libvpx on small frames.
		{
			0x0c, 0x0c, 0x1d, 0x11, 0x01, 0xfa, 0x00, 0x20,
			0x04, 0x34, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
		// 32×32 lossless: cpu_used=8, default rate-control mode (CBR is the
		// zero-pool slot), Lossless bit set via byte 26 bit 0.
		{
			0x04, 0x04, 0x1d, 0x11, 0x00, 0xfa, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
		// 16×16 CBR @ 250 kbps, cpu_used=8 (realtime). Exercises the
		// libvpx vp9_calc_iframe_target_size_one_pass_cbr keyframe target
		// (kf_boost ramp) path that prior to the fix was hard-coded to
		// the per-frame bandwidth in govpx, producing a slightly higher
		// base qindex than the libvpx CLI on tiny frames. byte[4]=0x00
		// selects CBR, byte[5,6]=(0xfa,0x00)=250 kbps target.
		{
			0x00, 0x00, 0x1d, 0x11, 0x00, 0xfa, 0x00, 0x20,
			0x04, 0x34, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
		// 64×64 CBR @ 300 kbps, cpu_used=8 (realtime). The pair of
		// 16x16 and 64x64 CBR keyframes pins the fix in place across
		// the small-frame-size regimes that libvpx's
		// rc_pick_q_and_bounds_one_pass_cbr path treats specially via
		// the cm->width*cm->height <= 352*288 q_adj_factor branch.
		{
			0x0c, 0x0c, 0x1d, 0x11, 0x00, 0x2c, 0x01, 0x20,
			0x04, 0x34, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
		// Out-of-range/all-0xff to push validator.
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		// All-zeros default-construction.
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewVP9Encoder panicked on %d-byte input: %v", len(data), r)
			}
		}()
		if vp9OptionsParityGapSeed(data) {
			t.Skip("seed tracks a known VP9 parity gap; see vp9OptionsParityGapSeeds")
		}
		opts := vp9oracle.NormalizeFuzzOptionsForLibvpxCLI(vp9EncoderOptionsFromFuzz(data))
		e, err := govpx.NewVP9Encoder(opts)
		if err != nil {
			assertVP9FuzzEncoderConstructError(t, err)
			return
		}
		if e == nil {
			t.Fatal("NewVP9Encoder returned nil encoder without error")
		}
		src := vp9test.NewYCbCr(opts.Width, opts.Height, 128, 128, 128)
		got, err := e.Encode(src)
		if err != nil {
			assertVP9FuzzEncoderRuntimeError(t, err)
			return
		}
		if len(got) == 0 {
			return
		}
		libvpxKey := tryVP9LibvpxKeyFrameBytes(t, opts, src)
		if len(libvpxKey) == 0 {
			t.Logf("vpxenc-vp9 rejected fuzzed config (comparator inapplicable, logged-only)")
			return
		}
		gHash := sha256.Sum256(got)
		lHash := sha256.Sum256(libvpxKey)
		if gHash != lHash {
			t.Errorf("keyframe byte mismatch under fuzzed options: govpx_len=%d vpxenc_len=%d first_diff=%d",
				len(got), len(libvpxKey),
				testutil.FirstByteDiff(got, libvpxKey))
		}
		_ = bytes.Equal // keep import in case future tightening drops first_diff log.
	})
}

// tryVP9LibvpxKeyFrameBytes runs vpxenc-vp9 for one keyframe at the fuzzed
// options and returns the keyframe IVF payload, or nil if the CLI rejects the
// shape. Mirrors the VP8 sibling
// tryLibvpxKeyFrameBytes by threading every fuzzed knob with a vpxenc-vp9
// CLI mapping so libvpx sees the same effective config govpx does. The
// VpxencVP9EncodeI420 helper pins a deterministic baseline (--rt --cpu-used=8
// --end-usage=q --min-q=4 --max-q=56 --cq-level=32 --aq-mode=0
// --tile-columns=0 --tile-rows=0 --auto-alt-ref=0 --lag-in-frames=0 --row-mt=0
// --fps=30/1); duplicate args appended via extra are last-wins inside vpxenc,
// so the overrides below replace each pin.
func tryVP9LibvpxKeyFrameBytes(t *testing.T, opts govpx.VP9EncoderOptions, src *image.YCbCr) []byte {
	t.Helper()
	vp9test.RequireVpxenc(t)
	extra := vp9oracle.LibvpxArgsFromOptions(opts)
	packets, _, err := vp9test.VpxencPacketsResult([]*image.YCbCr{src}, extra...)
	if err != nil || len(packets) == 0 {
		return nil
	}
	return append([]byte(nil), packets[0]...)
}
