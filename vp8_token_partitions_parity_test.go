//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"testing"
)

// TestVP8TokenPartitionsErrorResilientByteParity is the strict
// regression gate for task #251 — the wire-image audit of libvpx
// `vp8_pack_tokens_into_partitions` (libvpx v1.16.0
// vp8/encoder/bitstream.c:292-318) crossed with the error-resilient
// surface that flips per-partition entropy-context init at
// vp8/encoder/onyx_if.c:3944-3948 (libvpx error_resilient_mode →
// refresh_entropy_probs / VPX_ERROR_RESILIENT_PARTITIONS branch in
// vp8_update_coef_probs).
//
// The test pins the full 4-by-2 product (token_partitions ∈ {1,2,4,8} ×
// error_resilient ∈ {off, on}) at the small panning-32x32 fixture, plus
// one (threads=2 × 4-partitions × error-resilient=on) cross to exercise
// the multi-threaded partition writer dispatch (libvpx
// vp8/encoder/ethreading.c thread_encoding_proc, which still feeds
// `cpi->tplist[mb_row]` so `pack_tokens_into_partitions` reads the same
// per-MB-row token streams regardless of producer thread count).
//
// Each subtest does a full IVF-to-IVF byte comparison frame-by-frame so
// any per-partition arithmetic-coder state init divergence — or the
// `write_partition_size` 3-byte length-prefix splice at libvpx
// bitstream.c:281-290 — surfaces as a byte mismatch with the diff
// offset pointing at the first divergent partition.
func TestVP8TokenPartitionsErrorResilientByteParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run task #251 byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}

	// partitionsEncoding maps the libvpx CLI value (--token-parts=N which
	// encodes log2 of the partition count) to the EncoderOptions field.
	// 0=1 partition, 1=2, 2=4, 3=8 (libvpx vpx_codec_enc.h
	// VP8E_SET_TOKEN_PARTITIONS).
	cases := []struct {
		name            string
		tokenPartitions int
		errorResilient  bool
		threads         int
	}{
		{name: "1partition-no-er", tokenPartitions: 0, errorResilient: false},
		{name: "2partition-no-er", tokenPartitions: 1, errorResilient: false},
		{name: "4partition-no-er", tokenPartitions: 2, errorResilient: false},
		{name: "8partition-no-er", tokenPartitions: 3, errorResilient: false},
		{name: "1partition-er", tokenPartitions: 0, errorResilient: true},
		{name: "2partition-er", tokenPartitions: 1, errorResilient: true},
		{name: "4partition-er", tokenPartitions: 2, errorResilient: true},
		{name: "8partition-er", tokenPartitions: 3, errorResilient: true},
		// Threaded cross: 4 partitions × ER × threads=2. libvpx maps
		// num_part to N threads at vp8/encoder/onyx_if.c:1855 (CBR /
		// error_resilient enable thread-affinity); the produced bitstream
		// must remain byte-identical because the wire emit is driven by
		// the deterministic mod-N row scheduler in
		// vp8_pack_tokens_into_partitions (libvpx bitstream.c:307).
		{name: "4partition-er-threads2", tokenPartitions: 2, errorResilient: true, threads: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = panning32.source(panning32.w, panning32.h, i)
			}

			opts := EncoderOptions{
				Width:             panning32.w,
				Height:            panning32.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           strictByteParityCPUUsed(DeadlineRealtime, 0),
				Tuning:            TunePSNR,
				ErrorResilient:    tc.errorResilient,
				TokenPartitions:   tc.tokenPartitions,
				Threads:           tc.threads,
			}

			extraArgs := []string{"--end-usage=cbr"}
			if tc.tokenPartitions > 0 {
				extraArgs = append(extraArgs, "--token-parts="+strconv.Itoa(tc.tokenPartitions))
			}
			if tc.errorResilient {
				extraArgs = append(extraArgs, "--error-resilient=1")
			}
			if tc.threads > 0 {
				extraArgs = append(extraArgs, "--threads="+strconv.Itoa(tc.threads))
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
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
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}
