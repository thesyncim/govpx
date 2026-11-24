//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// FuzzVP8EncoderOptions decodes arbitrary fuzz bytes into EncoderOptions
// across the validator's full input domain. NewVP8Encoder either rejects with
// a documented sentinel error or accepts; accepted configs encode one keyframe
// and, when the libvpx CLI accepts the same shape, compare keyframe SHA-256
// against vpxenc-oracle.
//
// Asymmetries:
//
//   - Govpx rejects → documented sentinel error or contract bug.
//   - Govpx accepts, libvpx rejects this CLI shape → fuzz iteration
//     logs "libvpx CLI rejected (comparator inapplicable)" and
//     returns. Not yet a hard error because vpxenc CLI rejection
//     doesn't surface a single error class that maps cleanly to
//     govpx's sentinels; the data is tracked for a future
//     tightening pass.
//   - Both accept → keyframe bytes must SHA-256 match.
//
// Mirrors FuzzVP9EncoderOptions in shape and adds the libvpx
// keyframe-byte-parity comparator.
func FuzzVP8EncoderOptions(f *testing.F) {
	vp8test.RequireOracleF(f, "option-validation fuzz")
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		// Plausible 32×16 CBR config. The 17th byte is the errorRes
		// byte (bit 0 = ErrorResilient, bit 1 = ErrorResilientPartitions).
		// Short corpus seeds explicitly set it to 0x00 so cursor
		// wrap-around at byte[0] does not silently flip a different path
		// on top of the intended axis.
		{0x00, 0x20, 0x00, 0x10, 0x00, 0x1e, 0x02, 0xbc, 0x00, 0x00, 0x04, 0x38, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Out-of-range.
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		// All-zeros (default-construction).
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Large quantizer values.
		{0x00, 0x10, 0x00, 0x10, 0x00, 0x1e, 0x02, 0xbc, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Explicit CQ + Q seeds at production-shape resolutions
		// pin the libvpx-port CQ/Q paths (port of
		// vp8/encoder/ratectrl.c:849-852 active_worst CQ floor and
		// vp8/encoder/onyx_if.c:2847-2852 severe-undershoot recode
		// active_best_quality mutation) against vpxenc-oracle. Byte
		// layout: [w_idx, h_idx, rc_idx, dl_idx, cpu_idx, minQ, maxQ,
		// cq, kf_lo, kf_hi, br_lo, br_hi, sharp, tokenparts, threads,
		// noise, er]. rcPool = {CBR, VBR, CQ, Q}; dlPool = {RT, Good,
		// Best}; cpuPool = {0, -3, 4, 8}; widthPool = heightPool =
		// {16, 32, 48, 64}. er bit 0 = ErrorResilient, bit 1 =
		// ErrorResilientPartitions.
		// 64x64 CQ cq20 good cpu=0 1000kbps.
		{0x03, 0x03, 0x02, 0x01, 0x00, 0x04, 0x38, 0x14, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x00, 0x01, 0x00, 0x00},
		// 64x64 Q cq40 good cpu=0 1000kbps.
		{0x03, 0x03, 0x03, 0x01, 0x00, 0x04, 0x38, 0x28, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x00, 0x01, 0x00, 0x00},
		// 48x48 CQ cq56 best cpu=0 200kbps (high-CQ regime, exercises
		// active_worst CQ floor in libvpxActiveWorstQuantizerForFrame).
		{0x02, 0x02, 0x02, 0x02, 0x00, 0x04, 0x70, 0x38, 0x2c, 0x01, 0x96, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00},
		// 32x32 Q cq4 good cpu=0 1000kbps (low-CQ near minQ).
		{0x01, 0x01, 0x03, 0x01, 0x00, 0x04, 0x38, 0x04, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x00, 0x01, 0x00, 0x00},
		// 64x64 CQ cq30 realtime cpu=-3 1000kbps (exercises the
		// realtime recode-loop=0 CQ path).
		{0x03, 0x03, 0x02, 0x00, 0x01, 0x04, 0x38, 0x1e, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x00, 0x01, 0x00, 0x00},
		// 48x48 CQ cq20 with sharpness=4 good cpu=4.
		{0x02, 0x02, 0x02, 0x01, 0x02, 0x04, 0x38, 0x14, 0x2c, 0x01, 0xb6, 0x03, 0x04, 0x00, 0x01, 0x00, 0x00},
		// Explicit error-resilient × token-partitions keyframe parity
		// seeds. Each row toggles the er byte while keeping the rest
		// of the configuration shaped like an established passing seed,
		// so the (errorRes × tokens) cross gets a baseline keyframe
		// parity assertion every fuzz invocation.
		// 64x64 CBR good cpu=0 ErrorResilient=true tokens=1 (no split).
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x00, 0x01, 0x00, 0x01},
		// 64x64 CBR good cpu=0 ErrorResilient=true tokens=2.
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x01, 0x01, 0x00, 0x01},
		// 64x64 CBR good cpu=0 ErrorResilient=true tokens=4.
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x02, 0x01, 0x00, 0x01},
		// 64x64 CBR good cpu=0 ErrorResilient=true tokens=8.
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x03, 0x01, 0x00, 0x01},
		// 64x64 CBR good cpu=0 ErrorResilientPartitions tokens=4.
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x02, 0x01, 0x00, 0x02},
		// 64x64 CBR good cpu=0 ErrorResilient|Partitions tokens=8.
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x03, 0x01, 0x00, 0x03},
		// 64x64 CBR good cpu=0 ErrorResilient tokens=4 threads=2.
		{0x03, 0x03, 0x00, 0x01, 0x00, 0x04, 0x38, 0x00, 0x2c, 0x01, 0xb6, 0x03, 0x00, 0x02, 0x02, 0x00, 0x01},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewVP8Encoder panicked on %d-byte input: %v", len(data), r)
			}
		}()
		opts, ok := vp8EncoderOptionsFromFuzz(data)
		if !ok {
			return
		}
		e, err := NewVP8Encoder(opts)
		if err != nil {
			assertVP8FuzzEncoderConstructError(t, err)
			return
		}
		if e == nil {
			t.Fatal("NewVP8Encoder returned nil encoder without error")
		}
		src := encoderValidationPanningFrame(opts.Width, opts.Height, 0)
		dst := make([]byte, opts.Width*opts.Height*3+4096)
		result, err := e.EncodeInto(dst, src, 0, 1, 0)
		if err != nil {
			assertVP8FuzzEncoderRuntimeError(t, err)
			return
		}
		if result.Dropped || len(result.Data) == 0 {
			return
		}
		libvpxKey := tryLibvpxKeyFrameBytes(t, opts)
		if len(libvpxKey) == 0 {
			t.Logf("libvpx CLI rejected fuzzed config (comparator inapplicable, logged-only)")
			return
		}
		gHash := sha256.Sum256(result.Data)
		lHash := sha256.Sum256(libvpxKey)
		if gHash != lHash {
			// Strict: any config that BOTH sides accept must produce
			// identical keyframe bytes. Gap F is exactly the set of
			// configs where govpx accepts a normalisation that libvpx
			// quietly does differently; this fuzzer is the regression
			// gate.
			t.Errorf("keyframe byte mismatch under fuzzed options: govpx_len=%d libvpx_len=%d first_diff=%d",
				len(result.Data), len(libvpxKey), testutil.FirstByteDiff(result.Data, libvpxKey))
		}
	})
}

func vp8EncoderOptionsFromFuzz(data []byte) (EncoderOptions, bool) {
	r := testutil.NewByteCursor(data)
	if r.Remaining() == 0 {
		return EncoderOptions{}, false
	}
	widthPool := [...]int{16, 32, 48, 64}
	heightPool := [...]int{16, 32, 48, 64}
	rcPool := [...]RateControlMode{RateControlCBR, RateControlVBR, RateControlCQ, RateControlQ}
	deadlinePool := [...]Deadline{DeadlineRealtime, DeadlineGoodQuality, DeadlineBestQuality}
	cpuPool := [...]int{0, -3, 4, 8}

	w := widthPool[int(r.Next())%len(widthPool)]
	h := heightPool[int(r.Next())%len(heightPool)]
	rc := rcPool[int(r.Next())%len(rcPool)]
	deadline := deadlinePool[int(r.Next())%len(deadlinePool)]
	cpu := cpuPool[int(r.Next())%len(cpuPool)]
	minQ := int(r.Next() & 0x7f)
	maxQ := int(r.Next() & 0x7f)
	cq := int(r.Next() & 0x7f)
	kfRaw := r.U16LE()
	bitrate := int(r.U16LE()&0x1fff) + 50
	sharp := int(r.Next() & 0x07)
	tokenParts := int(r.Next() & 0x03)
	threads := int(r.Next() & 0x07)
	noise := int(r.Next() & 0x07)
	// Bit 0 of the errorRes byte toggles EncoderOptions.ErrorResilient
	// (libvpx VPX_ERROR_RESILIENT_DEFAULT)
	// independently of TokenPartitions so each fuzz iteration can land
	// at any (token_partitions ∈ {1,2,4,8}, error_resilient ∈ {0,1}) pair.
	// Bit 1 toggles ErrorResilientPartitions (VPX_ERROR_RESILIENT_PARTITIONS,
	// libvpx vp8/encoder/onyx_if.c:3946 — flips refresh_entropy_probs
	// and exercises the independent_coef_context_savings branch). On
	// 16-byte seeds the cursor wraps and this lands at byte 0, preserving
	// the existing corpus seed behavior (ErrorResilient=false).
	errorResByte := r.Next()
	errorRes := errorResByte&0x01 != 0
	errorResPart := errorResByte&0x02 != 0

	opts := EncoderOptions{
		Width:                    w,
		Height:                   h,
		FPS:                      30,
		RateControlMode:          rc,
		TargetBitrateKbps:        bitrate,
		MinQuantizer:             minQ,
		MaxQuantizer:             maxQ,
		QuantizerRangeSet:        minQ > 0 || maxQ > 0,
		CQLevel:                  cq,
		KeyFrameInterval:         int(kfRaw),
		Deadline:                 deadline,
		CpuUsed:                  strictByteParityCPUUsed(deadline, cpu),
		Tuning:                   TunePSNR,
		Sharpness:                sharp,
		TokenPartitions:          tokenParts,
		Threads:                  threads,
		NoiseSensitivity:         noise,
		ErrorResilient:           errorRes,
		ErrorResilientPartitions: errorResPart,
	}
	return opts, true
}

func assertVP8FuzzEncoderConstructError(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, ErrInvalidConfig) ||
		errors.Is(err, ErrInvalidQuantizer) ||
		errors.Is(err, ErrInvalidBitrate) {
		return
	}
	t.Errorf("NewVP8Encoder returned undocumented error type: %v", err)
}

func assertVP8FuzzEncoderRuntimeError(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, ErrFrameNotReady) ||
		errors.Is(err, ErrInvalidConfig) ||
		errors.Is(err, ErrBufferTooSmall) ||
		errors.Is(err, ErrInvalidData) {
		return
	}
	t.Errorf("EncodeInto returned undocumented error type: %v", err)
}

// tryLibvpxKeyFrameBytes runs vpxenc-oracle for a single keyframe at
// the fuzzed options and returns the keyframe packet bytes, or nil
// on any oracle-side failure. Process-level rejection is treated as
// "comparator inapplicable for this config" rather than t.Fatal so
// the fuzzer can keep iterating on adjacent configs.
func tryLibvpxKeyFrameBytes(t *testing.T, opts EncoderOptions) []byte {
	t.Helper()
	sources := []Image{encoderValidationPanningFrame(opts.Width, opts.Height, 0)}

	endUsage := "--end-usage=cbr"
	switch opts.RateControlMode {
	case RateControlVBR:
		endUsage = "--end-usage=vbr"
	case RateControlCQ:
		endUsage = "--end-usage=cq"
	case RateControlQ:
		endUsage = "--end-usage=q"
	}
	// Resolve the effective min/max quantizer the way NewVP8Encoder
	// resolves them inside defaultRateControlConfig so the libvpx CLI
	// receives the same operating quantizer range govpx will actually
	// use. Without this normalization, the fuzz harness passes raw
	// EncoderOptions.MinQuantizer/MaxQuantizer (often 0/0) directly to
	// vpxenc-oracle, forcing libvpx to operate at Q=0 while govpx
	// silently defaults to 4..56 — a divergence that surfaces as a
	// first-partition-size mismatch at byte 0 of the keyframe tag.
	effMinQ := opts.MinQuantizer
	effMaxQ := opts.MaxQuantizer
	if effMinQ == 0 && effMaxQ == 0 && !opts.QuantizerRangeSet {
		effMinQ = 4
		effMaxQ = 56
	}
	extraArgs := []string{
		endUsage,
	}
	if opts.CQLevel > 0 && (opts.RateControlMode == RateControlCQ || opts.RateControlMode == RateControlQ) {
		extraArgs = append(extraArgs, "--cq-level="+strconv.Itoa(opts.CQLevel))
	}
	if opts.TokenPartitions > 0 {
		extraArgs = append(extraArgs, "--token-parts="+strconv.Itoa(opts.TokenPartitions))
	}
	if opts.Threads > 0 {
		extraArgs = append(extraArgs, "--threads="+strconv.Itoa(opts.Threads))
	}
	// Mirror error_resilient toggles to the libvpx CLI. The libvpx
	// control takes a bitmask: 1=VPX_ERROR_RESILIENT_DEFAULT,
	// 2=VPX_ERROR_RESILIENT_PARTITIONS, 3=both (libvpx vpx_encoder.h:1117
	// and vp8/encoder/bitstream.c:1334 multi_token_partition splice).
	if opts.ErrorResilient || opts.ErrorResilientPartitions {
		mask := 0
		if opts.ErrorResilient {
			mask |= 1
		}
		if opts.ErrorResilientPartitions {
			mask |= 2
		}
		extraArgs = append(extraArgs, "--error-resilient="+strconv.Itoa(mask))
	}
	if opts.Sharpness > 0 {
		extraArgs = append(extraArgs, "--sharpness="+strconv.Itoa(opts.Sharpness))
	}
	if opts.NoiseSensitivity > 0 {
		extraArgs = append(extraArgs, "--noise-sensitivity="+strconv.Itoa(opts.NoiseSensitivity))
	}
	frames, _, err := vp8test.VpxencVP8OracleFramePayloadsI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8Config{
			Width:                opts.Width,
			Height:               opts.Height,
			Frames:               1,
			Deadline:             libvpxOracleDeadline(opts.Deadline),
			DisableWarningPrompt: true,
			CPUUsed:              opts.CpuUsed,
			TargetBitrateKbps:    opts.TargetBitrateKbps,
			MinQ:                 effMinQ,
			MaxQ:                 effMaxQ,
			Timebase:             "1/" + strconv.Itoa(opts.FPS),
			FPS:                  strconv.Itoa(opts.FPS) + "/1",
			KeyFrameDistSet:      true,
			KeyFrameMinDist:      999,
			KeyFrameMaxDist:      999,
			ExtraArgs:            extraArgs,
		},
	)
	if err != nil {
		return nil
	}
	if len(frames) == 0 {
		return nil
	}
	return frames[0]
}
