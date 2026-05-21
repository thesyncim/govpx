//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// FuzzEncoderResetLifecycle surfaces Reset state-leak bugs of the
// vp8_change_config family. The classic exemplar is
// vp8/encoder/onyx_if.c:1706 (libvpx v1.16.0) seeding
// cpi->Speed = oxcf.cpu_used: a Speed value that survived Reset()
// silently warps every subsequent encode. This harness drives a
// govpx encoder through a fuzz-generated op script that interleaves
// runtime control mutators and Reset() events, then asserts that
// each post-reset encode segment is byte-identical to a cold-start
// encoder fed only the same post-reset operations. Where the script
// contains no Reset between segment start and first encode the
// libvpx oracle is also driven in lockstep and a third byte-parity
// assertion runs against vpxenc-frameflags.
//
// Design summary:
//   - The script is a sequence of (op, value) tuples.
//   - Ops: Encode, Reset, SetBitrate, SetCpuUsed, SetSharpness,
//     SetNoiseSensitivity, SetMaxKeyframeInterval, SetTokenPartitions,
//     SetTuning, SetStaticThreshold, SetScreenContentMode,
//     SetMaxIntraBitratePct, SetCQLevel, SetGFCBRBoostPct, SetARNR,
//     SetFrameDropAllowed, SetReferenceFrame.
//   - After each Reset the script generator opens a new "segment".
//     For each segment the reset-path encoder is the encoder that
//     just observed Reset(); the cold-path encoder is a fresh
//     NewVP8Encoder(opts). Any state Reset() forgets to scrub will
//     immediately diverge them on the first encode after the boundary.
//
// The harness intentionally restricts ops to controls whose libvpx
// counterparts are wired into the vpxenc_frameflags --control-script
// driver, so the libvpx parity arm is exercisable when a segment
// happens to omit Reset() and runs from cold.
func FuzzEncoderResetLifecycle(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run reset-lifecycle fuzz parity")
	}
	seeds := [][]byte{
		// (dimBucket, cpuBucket, targetBucket) then op stream.
		// Op bytes pack op-kind in low nibble, value selector in high.
		{0, 0, 0, opEncode, opEncode, opEncode, opEncode},
		{0, 1, 1, opEncode, opReset, opEncode, opEncode, opEncode},
		{1, 0, 2, opEncode, opSetCpuUsed | (1 << 4), opEncode, opReset, opEncode, opEncode},
		{0, 2, 0, opEncode, opSetSharpness | (2 << 4), opEncode, opReset, opEncode},
		{1, 1, 1, opEncode, opSetBitrate | (2 << 4), opEncode, opReset, opEncode, opEncode},
		{0, 0, 0, opEncode, opSetNoiseSensitivity, opEncode, opReset, opEncode},
		{0, 1, 0, opEncode, opSetMaxKeyframeInterval | (1 << 4), opEncode, opReset, opEncode},
		{1, 0, 1, opEncode, opSetReferenceFrame | (1 << 4), opEncode, opReset, opEncode, opEncode},
		{0, 0, 0, opEncode, opSetTokenPartitions | (2 << 4), opSetTuning | (1 << 4), opEncode, opReset, opEncode, opEncode},
		{0, 2, 1, opEncode, opSetCpuUsed | (2 << 4), opEncode, opReset, opSetCpuUsed | (0 << 4), opEncode, opEncode},
		{1, 1, 2, opEncode, opEncode, opReset, opEncode, opEncode, opReset, opEncode},
		{0, 0, 0, opEncode, opSetStaticThreshold | (2 << 4), opSetScreenContentMode | (1 << 4), opEncode, opReset, opEncode},
		{0, 0, 0, opEncode, opSetARNR | (1 << 4), opEncode, opReset, opEncode},
		{0, 1, 0, opEncode, opSetMaxIntraBitratePct | (1 << 4), opSetGFCBRBoostPct | (1 << 4), opEncode, opReset, opEncode},
		{0, 0, 0, opEncode, opSetFrameDropAllowed | (1 << 4), opEncode, opReset, opEncode},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		segs, opts, targetKbps := resetLifecycleParseScript(data)
		if len(segs) == 0 {
			return
		}
		sum := sha256.Sum256(data)
		label := "fuzz-reset-lifecycle-" + hex.EncodeToString(sum[:4])
		t.Logf("%s width=%d height=%d cpu=%d kbps=%d segments=%d",
			label, opts.Width, opts.Height, opts.CpuUsed, targetKbps, len(segs))

		// Phase 1: reset-path encoder. Plays the full script: warm
		// pre-Reset segment, then for each segment-boundary Reset()
		// and replay the segment's ops. Per-segment frame outputs
		// are split out for parity. Also captures e.opts at the
		// segment boundary because Reset()'s contract retains
		// EncoderOptions (see VP8Encoder.Reset doc) — runtime
		// setters that mutate e.opts (SetTokenPartitions,
		// SetSharpness, SetTuning, SetCPUUsed, SetCQLevel,
		// SetStaticThreshold, SetScreenContentMode,
		// SetMaxIntraBitratePct, SetGFCBRBoostPct,
		// SetFrameDropAllowed, SetKeyFrameInterval) persist past
		// Reset, so the cold-path encoder must mirror those.
		resetSegFrames, segOpts := encodeResetLifecycleResetPath(t, opts, segs)

		// Phase 2: cold-path encoder. Per-segment fresh
		// NewVP8Encoder(segOpts[i]) replay. Each segment must
		// byte-match its reset-path twin or Reset is leaking
		// some non-opts state (the classic Speed-reset bug class).
		for i, seg := range segs {
			coldFrames := encodeResetLifecycleColdPath(t, segOpts[i], seg)
			label2 := label + "-seg" + strconv.Itoa(i) + "-cold-vs-reset"
			assertSegmentByteParity(t, label2, resetSegFrames[i], coldFrames, 0)
		}

		// Phase 3: libvpx oracle parity arm. For each segment, drive
		// vpxenc-frameflags with the segment ops. Skip when the
		// segment exercises ops the libvpx driver does not surface
		// 1:1, when there are zero encodes in the segment, or when
		// the cold-equivalent EncoderOptions diverge from the
		// initial constructor opts. The cmd-line surface of
		// vpxenc_frameflags (encodeFramesWithFrameFlagsDriver) only
		// forwards a fixed subset of opts: target-bitrate, min-q,
		// max-q, deadline, cpu-used, end-usage, token-parts,
		// cq-level. Reset's contract retains opts mutations from
		// runtime setters (SetSharpness, SetStaticThreshold,
		// SetScreenContentMode, SetMaxIntraBitratePct,
		// SetGFCBRBoostPct, SetFrameDropAllowed,
		// SetKeyFrameInterval, SetNoiseSensitivity); a libvpx
		// driver run at the constructor opts cannot mirror those
		// without round-tripping through control-script tokens,
		// which we cannot replay at frame 0 because libvpx applies
		// scripts BEFORE the matching input frame and the post-
		// Reset cold encoder receives them via NewVP8Encoder at
		// init time. Gate the libvpx arm to segments whose
		// effective opts equal opts.
		driver := coracletest.VpxencFrameFlags(t)
		for i, seg := range segs {
			if len(seg.frames) == 0 {
				continue
			}
			if !resetLifecycleSegmentLibvpxCompatible(seg) {
				continue
			}
			if !resetLifecycleSegmentOptsMatch(segOpts[i], opts) {
				continue
			}
			libvpxFrames := encodeResetLifecycleLibvpx(t, driver, label+"-seg"+strconv.Itoa(i), segOpts[i], targetKbps, seg)
			label3 := label + "-seg" + strconv.Itoa(i) + "-libvpx-vs-reset"
			assertSegmentByteParity(t, label3, resetSegFrames[i], libvpxFrames, 0)
		}
	})
}

const (
	opEncode                 byte = 0x00
	opReset                  byte = 0x01
	opSetBitrate             byte = 0x02
	opSetCpuUsed             byte = 0x03
	opSetSharpness           byte = 0x04
	opSetNoiseSensitivity    byte = 0x05
	opSetMaxKeyframeInterval byte = 0x06
	opSetReferenceFrame      byte = 0x07
	opSetTokenPartitions     byte = 0x08
	opSetTuning              byte = 0x09
	opSetStaticThreshold     byte = 0x0a
	opSetScreenContentMode   byte = 0x0b
	opSetMaxIntraBitratePct  byte = 0x0c
	opSetCQLevel             byte = 0x0d
	opSetGFCBRBoostPct       byte = 0x0e
	opSetARNR                byte = 0x0f
	opSetFrameDropAllowed    byte = 0x10
	opCount                  byte = 0x11
)

type resetLifecycleSegment struct {
	// frames lists per-encoded-input ops to run BEFORE each Encode()
	// call. Index N's slice is replayed in order before the Nth
	// encode in this segment.
	frames [][]resetLifecycleAction
	// kindIndex is the source-frame seed offset so two segments at
	// the same dimensions still see distinct content (otherwise the
	// reset and cold paths trivially match because the same keyframe
	// reconstructs the same Q-decisions).
	kindIndex int
}

type resetLifecycleAction struct {
	op    byte
	token string // libvpx control-script equivalent, "" if not driveable.
	apply func(*testing.T, *VP8Encoder)
}

func resetLifecycleParseScript(data []byte) ([]resetLifecycleSegment, EncoderOptions, int) {
	if len(data) < 3 {
		return nil, EncoderOptions{}, 0
	}
	dims := [...]struct{ w, h int }{
		{16, 16},
		{32, 32},
		{64, 64},
	}
	// Negative cpu_used only: libvpx's positive-cpu_used realtime
	// auto-select-speed path is wall-clock driven (vp8_encoder_reset_parity_test.go
	// TestEncoderResetCBRBytesMatchColdStart documents the same gating), so
	// two equal encoders on a loaded fuzzer can cross a Speed threshold on
	// different host loads and diverge. Pin to deterministic speeds.
	cpus := [...]int{-3, -8, -5}
	targets := [...]int{300, 700, 1200}
	dim := dims[int(data[0])%len(dims)]
	cpu := cpus[int(data[1])%len(cpus)]
	targetKbps := targets[int(data[2])%len(targets)]
	opts := EncoderOptions{
		Width:             dim.w,
		Height:            dim.h,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           cpu,
		KeyFrameInterval:  999,
		Tuning:            TunePSNR,
	}

	const maxEncodes = 12
	const maxSegments = 4
	segments := make([]resetLifecycleSegment, 1, maxSegments+1)
	current := &segments[0]
	current.kindIndex = 0
	pending := []resetLifecycleAction{}
	totalEncodes := 0
	for i := 3; i < len(data); i++ {
		b := data[i]
		op := b & 0x1f
		if op >= opCount {
			op = opEncode
		}
		valueSel := int(b >> 5)
		switch op {
		case opEncode:
			if totalEncodes >= maxEncodes {
				break
			}
			frame := make([]resetLifecycleAction, len(pending))
			copy(frame, pending)
			pending = pending[:0]
			current.frames = append(current.frames, frame)
			totalEncodes++
		case opReset:
			if len(segments) >= maxSegments+1 {
				break
			}
			pending = pending[:0]
			segments = append(segments, resetLifecycleSegment{kindIndex: len(segments)})
			current = &segments[len(segments)-1]
		default:
			act := resetLifecycleActionForOp(op, valueSel, dim.w, dim.h, targets[:])
			if act.op != 0 {
				pending = append(pending, act)
			}
		}
	}
	// Drop trailing empty segment so the harness does not assert on
	// a zero-frame segment.
	if len(segments[len(segments)-1].frames) == 0 {
		segments = segments[:len(segments)-1]
	}
	if len(segments) == 0 || totalEncodes == 0 {
		return nil, opts, targetKbps
	}
	return segments, opts, targetKbps
}

func resetLifecycleActionForOp(op byte, valueSel, w, h int, targets []int) resetLifecycleAction {
	switch op {
	case opSetBitrate:
		v := targets[valueSel%len(targets)]
		return resetLifecycleAction{
			op:    op,
			token: "bitrate:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps", e.SetBitrateKbps(v))
			},
		}
	case opSetCpuUsed:
		// Negative cpu_used only (see resetLifecycleParseScript
		// comment) — the positive-cpu_used realtime path is
		// wall-clock driven and produces fuzzer-flakiness.
		pool := [...]int{-3, -8, -5}
		v := pool[valueSel%len(pool)]
		return resetLifecycleAction{
			op:    op,
			token: "deadline:rt+cpu:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(v))
				mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineRealtime))
			},
		}
	case opSetSharpness:
		pool := [...]int{0, 3, 7}
		v := pool[valueSel%len(pool)]
		return resetLifecycleAction{
			op:    op,
			token: "sharpness:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetSharpness", e.SetSharpness(v))
			},
		}
	case opSetNoiseSensitivity:
		// libvpx applies noise-sensitivity at vpx_codec_enc_init via
		// the cfg.rc_*_section / VP8E_SET_NOISE_SENSITIVITY pair;
		// runtime changes go through vp8_change_config. Keep at the
		// no-op-rejected value 0 to avoid a noise on/off pathway
		// that needs a denoiser realloc in libvpx but is rejected
		// by the runtime control surface in govpx.
		v := 0
		return resetLifecycleAction{
			op:    op,
			token: "noise:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(v))
			},
		}
	case opSetMaxKeyframeInterval:
		pool := [...]int{120, 240, 999}
		v := pool[valueSel%len(pool)]
		return resetLifecycleAction{
			op:    op,
			token: "kfmin:" + strconv.Itoa(v) + "+kfmax:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(v))
			},
		}
	case opSetReferenceFrame:
		refNames := [...]string{"last", "golden", "altref"}
		refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}
		idx := valueSel % len(refs)
		imageIndex := 8 + (valueSel % 4)
		return resetLifecycleAction{
			op:    op,
			token: "setref:" + refNames[idx] + ":panning:" + strconv.Itoa(imageIndex),
			apply: setReferencePanningApply(refs[idx], imageIndex, refNames[idx]),
		}
	case opSetTokenPartitions:
		pool := [...]int{0, 1, 2}
		v := pool[valueSel%len(pool)]
		return resetLifecycleAction{
			op:    op,
			token: "token:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(v))
			},
		}
	case opSetTuning:
		names := [...]string{"psnr", "ssim"}
		tunes := [...]Tuning{TunePSNR, TuneSSIM}
		idx := valueSel % len(tunes)
		return resetLifecycleAction{
			op:    op,
			token: "tune:" + names[idx],
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTuning", e.SetTuning(tunes[idx]))
			},
		}
	case opSetStaticThreshold:
		pool := [...]int{0, 1, 500}
		v := pool[valueSel%len(pool)]
		return resetLifecycleAction{
			op:    op,
			token: "static:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(v))
			},
		}
	case opSetScreenContentMode:
		pool := [...]int{0, 1, 2}
		v := pool[valueSel%len(pool)]
		return resetLifecycleAction{
			op:    op,
			token: "screen:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(v))
			},
		}
	case opSetMaxIntraBitratePct:
		// libvpx VP8E_SET_MAX_INTRA_BITRATE_PCT only takes effect on
		// the next keyframe; keep govpx call paired but pin the
		// libvpx token to zero so the runtime-controls driver does
		// not flip into a different intra-rate cap.
		v := 0
		return resetLifecycleAction{
			op:    op,
			token: "maxintra:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetMaxIntraBitratePct", e.SetMaxIntraBitratePct(v))
			},
		}
	case opSetCQLevel:
		// VP8E_SET_CQ_LEVEL is read by ratecontrol picker code under
		// CBR + VBR but only matters under CQ mode. Pin to a CQ-
		// neutral seed of 4 (also the default in
		// vp8OracleRuntimeFuzzActionForKind for the same reason).
		v := 4
		return resetLifecycleAction{
			op:    op,
			token: "cq:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCQLevel", e.SetCQLevel(v))
			},
		}
	case opSetGFCBRBoostPct:
		v := 0
		return resetLifecycleAction{
			op:    op,
			token: "gfboost:" + strconv.Itoa(v),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetGFCBRBoostPct", e.SetGFCBRBoostPct(v))
			},
		}
	case opSetARNR:
		// SetARNR is realtime-no-op under DeadlineRealtime; we pin
		// to 0/0/1 so libvpx and govpx agree on the no-op.
		return resetLifecycleAction{
			op:    op,
			token: "arnrmax:0+arnrstrength:0+arnrtype:1",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetARNR", e.SetARNR(0, 0, 1))
			},
		}
	case opSetFrameDropAllowed:
		enabled := valueSel&1 != 0
		drop := 0
		if enabled {
			drop = defaultDropFramesWaterMark
		}
		return resetLifecycleAction{
			op:    op,
			token: "drop:" + strconv.Itoa(drop),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed", e.SetFrameDropAllowed(enabled))
			},
		}
	}
	return resetLifecycleAction{}
}

// resetLifecycleSegmentLibvpxCompatible returns true when every
// frame's action set has a corresponding libvpx control-script
// token. Reserved as a future guard against new ops without a
// libvpx counterpart.
func resetLifecycleSegmentLibvpxCompatible(seg resetLifecycleSegment) bool {
	for _, frame := range seg.frames {
		for _, a := range frame {
			if a.token == "" {
				return false
			}
		}
	}
	return true
}

// resetLifecycleSegmentOptsMatch returns true when the cold-
// equivalent opts for this segment are identical to the
// constructor opts. The libvpx parity arm is only meaningful
// under this gate because encodeFramesWithFrameFlagsDriver only
// forwards a fixed subset of opts through the vpxenc-frameflags
// cmd line; runtime-setter mutations to e.opts (Sharpness,
// StaticThreshold, ScreenContentMode, MaxIntraBitratePct,
// GFCBRBoostPct, DropFrameAllowed, KeyFrameInterval,
// NoiseSensitivity, etc.) cannot be replicated on the libvpx
// init path through cmd-line args alone.
func resetLifecycleSegmentOptsMatch(a, b EncoderOptions) bool {
	// EncoderOptions contains a slice (TwoPassStats), so it is not
	// `==`-comparable. The harness never mutates TwoPassStats, so
	// nil the slice on both sides and compare via reflect.DeepEqual.
	aClean := a
	bClean := b
	aClean.TwoPassStats = nil
	bClean.TwoPassStats = nil
	if len(a.TwoPassStats) != len(b.TwoPassStats) {
		return false
	}
	return reflect.DeepEqual(aClean, bClean)
}

func encodeResetLifecycleResetPath(t *testing.T, opts EncoderOptions, segs []resetLifecycleSegment) ([][][]byte, []EncoderOptions) {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][][]byte, len(segs))
	segOpts := make([]EncoderOptions, len(segs))
	pts := uint64(0)
	for si, seg := range segs {
		if si > 0 {
			enc.Reset()
		}
		segOpts[si] = enc.opts
		out[si] = make([][]byte, 0, len(seg.frames))
		for fi, frameActions := range seg.frames {
			for _, action := range frameActions {
				action.apply(t, enc)
			}
			src := resetLifecycleSourceFrame(opts.Width, opts.Height, seg.kindIndex, fi)
			result, err := enc.EncodeInto(buf, src, pts, 1, 0)
			pts++
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			if err != nil {
				t.Fatalf("EncodeInto seg=%d frame=%d: %v", si, fi, err)
			}
			if !result.Dropped {
				out[si] = append(out[si], append([]byte(nil), result.Data...))
			}
		}
	}
	return out, segOpts
}

func encodeResetLifecycleColdPath(t *testing.T, opts EncoderOptions, seg resetLifecycleSegment) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(seg.frames))
	for fi, frameActions := range seg.frames {
		for _, action := range frameActions {
			action.apply(t, enc)
		}
		src := resetLifecycleSourceFrame(opts.Width, opts.Height, seg.kindIndex, fi)
		// Cold path uses pts = fi (segment-local timeline) since a
		// freshly constructed encoder starts at frameCount=0, which
		// is also where the reset-path encoder sits after Reset().
		result, err := enc.EncodeInto(buf, src, uint64(fi), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("cold EncodeInto frame=%d: %v", fi, err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	return out
}

func encodeResetLifecycleLibvpx(t *testing.T, driver, label string, opts EncoderOptions, targetKbps int, seg resetLifecycleSegment) [][]byte {
	t.Helper()
	sources := make([]Image, len(seg.frames))
	for fi := range seg.frames {
		sources[fi] = resetLifecycleSourceFrame(opts.Width, opts.Height, seg.kindIndex, fi)
	}
	flags := make([]EncodeFlags, len(seg.frames))
	script := make([]string, len(seg.frames))
	for fi, frameActions := range seg.frames {
		if len(frameActions) == 0 {
			script[fi] = "-"
			continue
		}
		tokens := make([]string, 0, len(frameActions))
		for _, a := range frameActions {
			if a.token == "" {
				continue
			}
			tokens = append(tokens, a.token)
		}
		if len(tokens) == 0 {
			script[fi] = "-"
		} else {
			script[fi] = strings.Join(tokens, "+")
		}
	}
	extraArgs := []string{"--control-script=" + strings.Join(script, ",")}
	return encodeFramesWithFrameFlagsDriver(t, driver, label, opts, targetKbps, sources, flags, extraArgs)
}

func resetLifecycleSourceFrame(width, height, kind, idx int) Image {
	// Mix kind into idx so two segments with kindIndex=0 vs 1 never
	// see the same source frame; this guarantees that "Reset
	// produces cold-equivalent output" is a non-trivial claim about
	// state cleanliness rather than a triviality about identical
	// input streams. Use encoderValidationPanningFrame for parity
	// with the existing oracle infrastructure (libvpx via
	// vpxenc_frameflags reads the same i420 source dump).
	return encoderValidationPanningFrame(width, height, kind*16+idx)
}
