//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestOracleEncoderStreamByteParityExtended widens the strict byte-parity
// matrix beyond the static-knob axes already pinned by
// [TestOracleEncoderStreamByteParity]. It targets control surfaces that
// the original matrix never exercises — including AdaptiveKeyFrames,
// KeyFrameInterval=0 (libvpx --disable-kf), explicit TunePSNR with
// tuningSet, large/extreme kfInterval values, and several
// underrepresented cross-products (denoiser+threading,
// screen-content+noise, static-thresh+noise, sharpness+RC mode).
//
// Each subtest follows the same protocol as the base matrix: feed the
// same I420 fixture to govpx and to the patched vpxenc-oracle under
// matching options, then assert the encoded frame payloads byte-match.
// Cases that diverge are pinned with `limit:` so the gap is visible in
// the per-frame "byte mismatch (not asserted, ...)" log lines without
// regressing the strict gate.
func TestOracleEncoderStreamByteParityExtended(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented32 := fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}
	splitmv64 := fixture{name: "splitmv-64x64", w: 64, h: 64, source: encoderValidationSplitMVQuadrantFrame}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
		// limit caps how many leading frames must byte-match. 0 means
		// require the full `frames` budget; a positive value pins the
		// known-good prefix when later frames have a remaining
		// divergence still being investigated; -1 disables the strict
		// gate entirely (the per-frame status logs make any drift
		// visible).
		limit                    int
		rcMode                   RateControlMode
		rcModeSet                bool
		errorResilient           bool
		errorResilientPartitions bool
		sharpness                int
		noiseSensitivity         int
		tuning                   Tuning
		tuningSet                bool
		extraArgs                []string
		staticThreshold          int
		screenContentMode        int
		maxIntraBitratePct       int
		gfCBRBoostPct            int
		undershootPct            int
		overshootPct             int
		bufferSizeMs             int
		bufferInitialSizeMs      int
		bufferOptimalSizeMs      int
		dropFrameAllowed         bool
		dropFrameWaterMark       int
		tokenPartitions          int
		threads                  int
		targetKbpsOverride       int
		minQ                     int
		maxQ                     int
		minQSet                  bool
		maxQSet                  bool
		cqLevel                  int
		// kfInterval overrides KeyFrameInterval. 0 keeps the harness
		// default (999); the new `disableKf` field below is the way
		// to drive the explicit "no interval keys" path because the
		// base harness coalesces kfInterval==0 to the 999 default.
		kfInterval int
		// disableKf, when true, sets KeyFrameInterval=0 explicitly on
		// the govpx side and `--disable-kf` (VPX_KF_DISABLED) on the
		// libvpx side. This pins the parity of the no-periodic-keys
		// path: with `auto_key=0` the only keyframe is frame 0.
		disableKf bool
		// kfMaxOverride lets a case use a custom large
		// `--kf-min-dist/--kf-max-dist` value on the libvpx side. The
		// govpx side reads KeyFrameInterval = kfMaxOverride. Used to
		// pin the parity of the "kfInterval much larger than the
		// frame budget" path without the explicit disable-kf header
		// branch.
		kfMaxOverride int
		// adaptiveKeyFrames flips AdaptiveKeyFrames on. libvpx's
		// auto_key is enabled by default in vpxenc, so the matching
		// libvpx side requires no extra arg. The smooth panning
		// fixture should never trigger a scene-cut keyframe in either
		// implementation, so byte-parity must hold.
		adaptiveKeyFrames bool
		fpsOverride       int
		timebaseNum       int
		timebaseDen       int
		lookaheadFrames   int
		autoAltRef        bool
		arnrMaxFrames     int
		arnrStrength      int
		arnrType          int
	}{
		// AdaptiveKeyFrames + smooth panning across resolution × cpu_used.
		// The fixture is gradient-smooth so libvpx's scene-cut detector
		// must NOT fire — these probe whether enabling auto_key on the
		// govpx side stays a strict no-op when the source provides no
		// scene cut to detect. Any divergence here means the scene-cut
		// gate condition has drifted from libvpx.
		{name: "adaptive-kf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, adaptiveKeyFrames: true},
		{name: "adaptive-kf-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, adaptiveKeyFrames: true},
		{name: "adaptive-kf-realtime-cpu0-48x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, adaptiveKeyFrames: true},
		{name: "adaptive-kf-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, adaptiveKeyFrames: true},
		{name: "adaptive-kf-realtime-cpu4-16x16", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning16, adaptiveKeyFrames: true},
		{name: "adaptive-kf-realtime-cpu8-32x32", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning32, adaptiveKeyFrames: true},
		{name: "adaptive-kf-good-quality-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, adaptiveKeyFrames: true},
		{name: "adaptive-kf-best-quality-cpu0-16x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: panning16, adaptiveKeyFrames: true},
		// AdaptiveKeyFrames crossed with token partitions and tuning so
		// the per-frame scene-cut probe shares its frame path with the
		// other writer/picker axes the matrix already pins.
		{name: "adaptive-kf-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, adaptiveKeyFrames: true, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "adaptive-kf-realtime-cpu-3-64x64-tune-ssim", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, adaptiveKeyFrames: true, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		// AdaptiveKeyFrames + non-CBR rate-control modes (still
		// expected to byte-match because the panning fixture is too
		// smooth to trip the scene-cut gate).
		{name: "adaptive-kf-realtime-vbr-cpu-3-16x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, adaptiveKeyFrames: true, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "adaptive-kf-realtime-q-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, adaptiveKeyFrames: true, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},

		// KeyFrameInterval=0 + libvpx --disable-kf. This pins the
		// VPX_KF_DISABLED branch where auto_key is also cleared in
		// libvpx: the only keyframe in the stream is frame 0 (the
		// implicit initial key). Each base config gets a sweep so we
		// know the header path that writes kf_mode=DISABLED stays
		// byte-identical across CPU presets and frame sizes.
		{name: "disable-kf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, disableKf: true},
		{name: "disable-kf-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, disableKf: true},
		{name: "disable-kf-realtime-cpu0-48x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, disableKf: true},
		{name: "disable-kf-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, disableKf: true},
		{name: "disable-kf-realtime-cpu8-16x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning16, disableKf: true},
		{name: "disable-kf-good-quality-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, disableKf: true},
		{name: "disable-kf-best-quality-cpu0-16x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: panning16, disableKf: true},
		// disable-kf + non-CBR rate-control modes.
		{name: "disable-kf-realtime-vbr-cpu-3-16x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, disableKf: true, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "disable-kf-realtime-q-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, disableKf: true, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		// disable-kf crossed with token partitions / threading.
		{name: "disable-kf-realtime-cpu-3-64x64-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, disableKf: true, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "disable-kf-realtime-cpu0-48x48-threads2", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, disableKf: true, threads: 2, extraArgs: []string{"--threads=2"}},
		// disable-kf + error-resilient combinations.
		{name: "disable-kf-realtime-cpu0-32x32-error-resilient", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, disableKf: true, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},

		// Large kfInterval values. The base matrix only pins
		// kf={1,2,3,4,5,8,16,999}; this batch covers the cadence the
		// real-world WebRTC stack uses (kf=30/60/120/240/2000) so any
		// kfInterval-truncation arithmetic divergence is exposed.
		// Frame budget is 16 so the only keyframe is frame 0 in each
		// of these — they primarily exercise the kfInterval=N header
		// arithmetic, not the keyframe writer itself.
		{name: "kf30-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, kfInterval: 30, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=30"}},
		{name: "kf60-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, kfInterval: 60, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=60"}},
		{name: "kf120-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, kfInterval: 120, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=120"}},
		{name: "kf240-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, kfInterval: 240, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=240"}},
		{name: "kf2000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, kfInterval: 2000, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=2000"}},
		// kfMin == kfMax variants (pin the case where libvpx clamps
		// kf_min_dist == kf_max_dist, which is what real callers tend
		// to pass for fixed-cadence GOP).
		{name: "kf30-realtime-cpu0-32x32-min30", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, kfInterval: 30, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=30", "--kf-max-dist=30"}},
		{name: "kf60-realtime-cpu0-32x32-min60", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, kfInterval: 60, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=60", "--kf-max-dist=60"}},

		// Explicit tuningSet=true with TunePSNR. The base matrix
		// defaults TunePSNR implicitly (zero-value Tuning falls
		// through to TunePSNR), so the explicit-Set path is never
		// pinned in strict byte-parity. Both should produce the same
		// bitstream — confirms zero-vs-explicit equivalence.
		{name: "explicit-tune-psnr-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, tuning: TunePSNR, tuningSet: true, extraArgs: []string{"--tune=psnr"}},
		{name: "explicit-tune-psnr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tuning: TunePSNR, tuningSet: true, extraArgs: []string{"--tune=psnr"}},
		{name: "explicit-tune-psnr-good-quality-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, tuning: TunePSNR, tuningSet: true, extraArgs: []string{"--tune=psnr"}},
		{name: "explicit-tune-psnr-best-quality-cpu0-16x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: panning16, tuning: TunePSNR, tuningSet: true, extraArgs: []string{"--tune=psnr"}},

		// SSIM tuning crossed with sharpness / RC modes that the base
		// matrix never pins at SSIM. The picker uses SSE+SSIM rate
		// shaping at TuneSSIM, so these probe the SSIM rate-shaping
		// path against the writer combination.
		{name: "tune-ssim-realtime-cpu0-16x16-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, tuning: TuneSSIM, tuningSet: true, sharpness: 4, extraArgs: []string{"--tune=ssim", "--sharpness=4"}},
		{name: "tune-ssim-realtime-cpu-3-64x64-sharpness4", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tuning: TuneSSIM, tuningSet: true, sharpness: 4, extraArgs: []string{"--tune=ssim", "--sharpness=4"}},
		{name: "tune-ssim-realtime-vbr-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, tuning: TuneSSIM, tuningSet: true, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--tune=ssim", "--end-usage=vbr"}},
		{name: "tune-ssim-good-quality-cpu4-32x32-segmented", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: segmented32, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},

		// Underrepresented cross-products in the existing matrix:
		// denoiser + threading. Denoiser cases in the base matrix use
		// the serial (Threads=0/1) path; this batch confirms parity
		// holds when both the temporal denoiser and the row-worker
		// pool are active simultaneously.
		//
		// NEW GAP: All four currently diverge starting at the
		// keyframe (first_diff=273 on the 48x48 keyframe). The
		// row-thread fan-out reorders the denoiser's per-MB
		// sum_diff accumulation versus libvpx's serial reduction,
		// which propagates into the picker's chosen modes from
		// frame 0 onward. Pin with limit:-1 so the per-frame log
		// records the partition-size deltas without regressing
		// the strict gate.
		{name: "noise-sensitivity3-threads2-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, limit: -1, noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "noise-sensitivity6-threads2-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, limit: -1, noiseSensitivity: 6, threads: 2, extraArgs: []string{"--noise-sensitivity=6", "--threads=2"}},
		{name: "noise-sensitivity3-threads2-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, limit: -1, noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "noise-sensitivity6-threads2-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, limit: -1, noiseSensitivity: 6, threads: 2, extraArgs: []string{"--noise-sensitivity=6", "--threads=2"}},

		// Underrepresented cross-products: screen-content + denoiser.
		// Both controls flip in the per-MB encode pipeline; running
		// them together pins the combined path.
		{name: "screen-content1-noise3-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, screenContentMode: 1, noiseSensitivity: 3, extraArgs: []string{"--screen-content-mode=1", "--noise-sensitivity=3"}},
		{name: "screen-content2-noise6-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, screenContentMode: 2, noiseSensitivity: 6, extraArgs: []string{"--screen-content-mode=2", "--noise-sensitivity=6"}},

		// Underrepresented cross-products: static-thresh + denoiser.
		// static-thresh feeds into the encode_breakout gate that the
		// denoiser then re-evaluates; both interact in the inter
		// breakout decision.
		{name: "static-thresh100-noise3-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, staticThreshold: 100, noiseSensitivity: 3, extraArgs: []string{"--static-thresh=100", "--noise-sensitivity=3"}},
		{name: "static-thresh1000-noise6-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, staticThreshold: 1000, noiseSensitivity: 6, extraArgs: []string{"--static-thresh=1000", "--noise-sensitivity=6"}},

		// Underrepresented: sharpness + non-CBR RC mode.
		{name: "sharpness4-vbr-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, sharpness: 4, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--sharpness=4", "--end-usage=vbr"}},
		{name: "sharpness4-q-realtime-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, sharpness: 4, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--sharpness=4", "--end-usage=q", "--cq-level=20"}},
		{name: "sharpness7-q-realtime-cpu-3-16x16-q40", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, sharpness: 7, rcMode: RateControlQ, rcModeSet: true, cqLevel: 40, extraArgs: []string{"--sharpness=7", "--end-usage=q", "--cq-level=40"}},

		// Underrepresented: error-resilient + denoiser combinations.
		//
		// NEW GAP (limit=3): error-resilient + ns=3 diverges only at
		// frame 3 by a 1-byte first-partition-size delta
		// (govpx_first_part=171, libvpx_first_part=170). Frames
		// 0,1,2,4..15 byte-match. This is a single-frame entropy
		// transition gap that fires when the resilience reset and
		// the denoiser's per-MB filter overlay land on the same
		// frame. Pin frames 0..2 strict; frame 3 logs only.
		{name: "error-resilient-noise3-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, limit: 3, errorResilient: true, noiseSensitivity: 3, extraArgs: []string{"--error-resilient=1", "--noise-sensitivity=3"}},
		{name: "error-resilient-partitions-noise3-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilientPartitions: true, noiseSensitivity: 3, extraArgs: []string{"--error-resilient=2", "--noise-sensitivity=3"}},

		// Underrepresented: gf-cbr-boost + screen-content. Boost
		// interacts with the golden-frame refresh schedule that
		// screen-content also tweaks.
		{name: "gf-cbr-boost100-screen-content1-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 100, screenContentMode: 1, extraArgs: []string{"--gf-cbr-boost=100", "--screen-content-mode=1"}},
		{name: "gf-cbr-boost200-screen-content2-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 200, screenContentMode: 2, extraArgs: []string{"--gf-cbr-boost=200", "--screen-content-mode=2"}},

		// Underrepresented: drop-frame + non-CBR RC. The drop-frame
		// gate is normally only on CBR; vbr / cq paths still accept
		// the control but must remain a no-op for parity.
		{name: "drop-frame60-vbr-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, dropFrameAllowed: true, dropFrameWaterMark: 60, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--drop-frame=60", "--end-usage=vbr"}},

		// Splitmv-fixture parity probes at small/mid sizes the
		// existing matrix does not pin at splitmv-source. Splitmv MBs
		// stress the sub-MV picker that drives the BestQuality frame-14
		// gap; pinning the smooth-content baseline here makes any
		// regression near that path obvious.
		// NEW GAP (limit=2): denoiser-on + SPLITMV-source at
		// realtime+cpu0 — the existing splitmv-cpu0 parity case
		// matches the full sequence at NoiseSensitivity=0, and the
		// existing 48x48 panning + ns=3 case matches too. Crossing
		// the two (splitmv source AND ns>0) diverges from frame 2
		// onward (first inter frame with a populated sub-MV picker
		// state interacting with the denoiser's per-MB filter).
		{name: "splitmv-realtime-cpu0-64x64-noise3", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, limit: 2, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "splitmv-realtime-cpu-3-64x64-noise6", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "splitmv-realtime-cpu0-64x64-static-thresh100", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "splitmv-realtime-cpu-3-64x64-screen-content1", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},

		// Bitrate at the boundary of the libvpx clamp band so the
		// underlying rate-allocator-clamp parity is pinned. 50 kbps is
		// near the libvpx VP8 minimum; 5000 kbps approaches the upper
		// CBR allocator clip.
		{name: "low-bitrate50-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=50"}, targetKbpsOverride: 50},
		{name: "high-bitrate5000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=5000"}, targetKbpsOverride: 5000},

		// Asymmetric small frames with denoiser. The base matrix
		// covers 32x16 / 16x32 denoiser cases at ns=3/6 only; this
		// adds ns=1/2/4/5 to round out the per-level coverage on
		// asymmetric MB grids.
		{name: "noise1-realtime-cpu-3-32x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "noise2-realtime-cpu-3-32x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "noise4-realtime-cpu-3-32x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "noise5-realtime-cpu-3-32x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			caseTargetKbps := targetKbps
			if tc.targetKbpsOverride > 0 {
				caseTargetKbps = tc.targetKbpsOverride
			}
			minQ := 4
			if tc.minQSet || tc.minQ > 0 {
				minQ = tc.minQ
			}
			maxQ := 56
			if tc.maxQSet || tc.maxQ > 0 {
				maxQ = tc.maxQ
			}
			cqLevel := 0
			if tc.cqLevel > 0 {
				cqLevel = tc.cqLevel
			}
			// Resolve the keyframe interval the govpx encoder sees.
			//
			//   disableKf=true  -> KeyFrameInterval=0 (and the libvpx
			//                      side receives --disable-kf).
			//   kfMaxOverride>0 -> the libvpx-mandated cadence.
			//   kfInterval>0    -> explicit interval.
			//   otherwise       -> the test-wide default 999.
			var kfInterval int
			switch {
			case tc.disableKf:
				kfInterval = 0
			case tc.kfMaxOverride > 0:
				kfInterval = tc.kfMaxOverride
			case tc.kfInterval > 0:
				kfInterval = tc.kfInterval
			default:
				kfInterval = 999
			}
			caseFPS := fps
			if tc.fpsOverride > 0 {
				caseFPS = tc.fpsOverride
			}
			optsFPS := caseFPS
			if tc.timebaseNum > 0 {
				optsFPS = 0
			}
			tuning := TunePSNR
			if tc.tuningSet {
				tuning = tc.tuning
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      optsFPS,
				TimebaseNum:              tc.timebaseNum,
				TimebaseDen:              tc.timebaseDen,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        caseTargetKbps,
				MinQuantizer:             minQ,
				MaxQuantizer:             maxQ,
				QuantizerRangeSet:        tc.minQSet || tc.maxQSet,
				CQLevel:                  cqLevel,
				KeyFrameInterval:         kfInterval,
				AdaptiveKeyFrames:        tc.adaptiveKeyFrames,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   tuning,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
				Sharpness:                tc.sharpness,
				NoiseSensitivity:         tc.noiseSensitivity,
				StaticThreshold:          tc.staticThreshold,
				ScreenContentMode:        tc.screenContentMode,
				MaxIntraBitratePct:       tc.maxIntraBitratePct,
				GFCBRBoostPct:            tc.gfCBRBoostPct,
				UndershootPct:            tc.undershootPct,
				OvershootPct:             tc.overshootPct,
				BufferSizeMs:             tc.bufferSizeMs,
				BufferInitialSizeMs:      tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:      tc.bufferOptimalSizeMs,
				DropFrameAllowed:         tc.dropFrameAllowed,
				DropFrameWaterMark:       tc.dropFrameWaterMark,
				TokenPartitions:          tc.tokenPartitions,
				Threads:                  tc.threads,
				LookaheadFrames:          tc.lookaheadFrames,
				AutoAltRef:               tc.autoAltRef,
				ARNRMaxFrames:            tc.arnrMaxFrames,
				ARNRStrength:             tc.arnrStrength,
				ARNRType:                 tc.arnrType,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)

			// The base oracle helper hard-codes --kf-min-dist=999
			// --kf-max-dist=999. For disableKf we add --disable-kf
			// (which sets kf_mode = VPX_KF_DISABLED so kf_max_dist is
			// ignored); for explicit kfInterval cases the
			// extraArgs already carry overrides that override the
			// 999 default (libvpx's argument parser keeps the
			// last-seen value).
			extraArgs := tc.extraArgs
			if tc.disableKf {
				extraArgs = append([]string{"--disable-kf"}, extraArgs...)
			}
			extraArgs = libvpxEndUsageArgs(extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, caseTargetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}
