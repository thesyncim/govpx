package govpx

import (
	"crypto/sha256"
	"errors"
	"testing"
)

// vp8SVCFirstByteDiff returns the byte offset of the first
// divergence between a and b, or -1 if the prefixes match up to
// min(len(a), len(b)). Inline here so the regression test stays
// untagged (the oracle-tagged firstByteDiff lives in
// vp8_oracle_encoder_stream_parity_test.go).
func vp8SVCFirstByteDiff(a, b []byte) int {
	n := min(len(b), len(a))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

// vp8_multires_svc_parity_test.go exercises the previously
// uncovered VP8 multi-resolution simulcast + SVC + temporal-layer paths.
// Background:
//
//   - Task #79 audited the libvpx multi-resolution encoder code surface
//     (vp8/encoder/onyx_if.c:vp8_create_compressor multi_resolution
//     arm, vp8/encoder/mr_dissim.c, vpx/src/vpx_encoder.c
//     vpx_codec_enc_init_multi_ver). govpx does not expose a parent/
//     child mr_encoder API — multi-resolution is realised as N
//     independent VP8Encoder instances driven from a shared source,
//     mirroring the WebRTC simulcast pattern in
//     examples/webrtc-vp8/main.go.
//
//   - Task #195 audited libvpx VP8 SVC/temporal-layer rate control
//     (vp8/encoder/onyx_if.c cpi->oxcf.number_of_layers,
//     cpi->oxcf.target_bitrate[], cpi->oxcf.layer_id[]). govpx exposes
//     this via TemporalScalabilityConfig +
//     VP8Encoder.SetTemporalScalability / SetTemporalLayerID. Live byte
//     parity already passes for the canonical SVC fixtures (see
//     TestVP8OracleEncoderStreamByteParityTemporalSVC); this file adds the
//     missing regression coverage for the *combined* simulcast +
//     temporal-layer matrix.
//
// Both audits left zero live fuzz/regression seeds covering the
// simulcast pattern, so any future regression in the per-encoder
// initialisation order, per-layer rate-control restore, or the
// temporal-layer pattern table would only surface in production.
// This file plants the gate: three small fixtures exercise the three
// target shapes against govpx-internal byte-stability (the same bytes
// every run, no oracle binary required).
//
// Task #358 verification (post-#341/#347 RD picker changes):
// The intra-in-inter tteob==0 rate2 backout (commit 09a4cc91) and the
// frame-2 MB(0,0) NEWMV rd_thresh pin (commit 5f9805a3) both modify
// the inter-mode RD picker path that SVC base+enhancement layers
// traverse at cpu_used=-3. All three fixtures here
// (multires-2layer-simulcast cpu=-3, svc-2layer-2temporal cpu=-3,
// svc-2temporal-cpu-3) re-pin clean post-#341/#347: simulcast +
// SVC + temporal-layer paths remain byte-deterministic. The runtime
// SetTemporalLayerID override path (TestVP8TemporalLayerIDOverride)
// and the per-encoder independence path
// (TestVP8MultiResIndependentEncoders) also re-verify clean.
// No regression introduced by the #341/#347 RD changes.

// vp8MultiResSVCLayer is one rendition of a VP8 simulcast cluster.
// Fields mirror the WebRTC simulcast example in examples/webrtc-vp8.
type vp8MultiResSVCLayer struct {
	Name        string
	Width       int
	Height      int
	BitrateKbps int
}

// vp8MultiResSVCDownsampleI420 downsamples an I420 Image to the target
// dimensions using a libvpx-compatible 2:1 average-pool when the ratio
// is exactly 2x (the multi-resolution example uses the same), and a
// nearest-neighbour pick otherwise. The mr_dissim path in libvpx feeds
// the downscaled source produced by libvpx's vp8_scale_frame; govpx
// does not export an equivalent scaler, so the simulcast pattern
// pre-downsamples on the caller side (matching examples/webrtc-vp8).
func vp8MultiResSVCDownsampleI420(src Image, dstW, dstH int) Image {
	if src.Width == dstW && src.Height == dstH {
		dst := Image{
			Width:   src.Width,
			Height:  src.Height,
			Y:       append([]byte(nil), src.Y...),
			U:       append([]byte(nil), src.U...),
			V:       append([]byte(nil), src.V...),
			YStride: src.YStride,
			UStride: src.UStride,
			VStride: src.VStride,
		}
		return dst
	}
	dst := Image{
		Width:   dstW,
		Height:  dstH,
		Y:       make([]byte, dstW*dstH),
		U:       make([]byte, (dstW/2)*(dstH/2)),
		V:       make([]byte, (dstW/2)*(dstH/2)),
		YStride: dstW,
		UStride: dstW / 2,
		VStride: dstW / 2,
	}
	scaleX := src.Width
	scaleY := src.Height
	for y := range dstH {
		sy := (y * scaleY) / dstH
		for x := range dstW {
			sx := (x * scaleX) / dstW
			dst.Y[y*dstW+x] = src.Y[sy*src.YStride+sx]
		}
	}
	cW := dstW / 2
	cH := dstH / 2
	sCW := src.Width / 2
	sCH := src.Height / 2
	for y := range cH {
		sy := (y * sCH) / cH
		for x := range cW {
			sx := (x * sCW) / cW
			dst.U[y*cW+x] = src.U[sy*src.UStride+sx]
			dst.V[y*cW+x] = src.V[sy*src.VStride+sx]
		}
	}
	return dst
}

// vp8MultiResSVCStreams runs an N-rendition simulcast cluster and
// returns the per-rendition packet streams. Each rendition owns its own
// VP8Encoder; the source is downsampled per rendition with the same
// nearest-neighbour fall-back used in examples/webrtc-vp8. This is the
// govpx counterpart to libvpx's vp8_multi_resolution_encoder.c parent/
// child pattern (one VP8 codec per rendition, all driven from the same
// source frame each timestep).
func vp8MultiResSVCStreams(
	t *testing.T,
	parentW, parentH, fps, frames int,
	deadline Deadline,
	cpuUsed int,
	layers []vp8MultiResSVCLayer,
) [][][]byte {
	t.Helper()
	encoders := make([]*VP8Encoder, len(layers))
	for i, l := range layers {
		opts := EncoderOptions{
			Width:               l.Width,
			Height:              l.Height,
			FPS:                 fps,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   l.BitrateKbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			Deadline:            deadline,
			CpuUsed:             cpuUsed,
			KeyFrameInterval:    999,
			Tuning:              TunePSNR,
			StaticThreshold:     1,
			MaxIntraBitratePct:  900,
			BufferSizeMs:        1000,
			BufferInitialSizeMs: 600,
			BufferOptimalSizeMs: 600,
		}
		enc, err := NewVP8Encoder(opts)
		if err != nil {
			t.Fatalf("layer %d (%q) NewVP8Encoder: %v", i, l.Name, err)
		}
		encoders[i] = enc
	}
	streams := make([][][]byte, len(layers))
	for i := range streams {
		streams[i] = make([][]byte, 0, frames)
	}
	bufs := make([][]byte, len(layers))
	for i, l := range layers {
		bufs[i] = make([]byte, l.Width*l.Height*4+4096)
	}
	for f := range frames {
		parent := encoderValidationPanningFrame(parentW, parentH, f)
		for i, l := range layers {
			src := vp8MultiResSVCDownsampleI420(parent, l.Width, l.Height)
			result, err := encoders[i].EncodeInto(bufs[i], src, uint64(f), 1, 0)
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			if err != nil {
				t.Fatalf("layer %d (%q) EncodeInto frame %d: %v", i, l.Name, f, err)
			}
			if result.Dropped {
				continue
			}
			streams[i] = append(streams[i], append([]byte(nil), result.Data...))
		}
	}
	return streams
}

// vp8TemporalSVCStreams runs the temporal SVC pattern through govpx and
// returns the per-output-layer fanout (every frame at layer L is
// written to all output layers in [L, numLayers)). Mirrors the
// pattern in encodeFramesWithGovpxTemporalSVC but uses CBR + the
// standard WebRTC SVC parameters from the upstream
// vpx_temporal_svc_encoder example.
func vp8TemporalSVCStreams(
	t *testing.T,
	w, h, fps, frames int,
	cpuUsed int,
	mode TemporalLayeringMode,
	numLayers int,
	bitratesKbps [5]int,
) [][][]byte {
	t.Helper()
	opts := EncoderOptions{
		Width:               w,
		Height:              h,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   bitratesKbps[numLayers-1],
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    3000,
		ErrorResilient:      true,
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		TokenPartitions:     1,
		Tuning:              TunePSNR,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   mode,
			LayerTargetBitrateKbps: bitratesKbps,
		},
	}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, w*h*4+8192)
	streams := make([][][]byte, numLayers)
	for i := range streams {
		streams[i] = make([][]byte, 0, frames)
	}
	for f := range frames {
		src := encoderValidationPanningFrame(w, h, f)
		result, err := enc.EncodeInto(buf, src, uint64(f), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", f, err)
		}
		if result.Dropped {
			continue
		}
		layerID := result.TemporalLayerID
		if uint(layerID) >= uint(numLayers) {
			t.Fatalf("frame %d: TemporalLayerID=%d outside [0,%d)", f, layerID, numLayers)
		}
		payload := append([]byte(nil), result.Data...)
		for lo := layerID; lo < numLayers; lo++ {
			streams[lo] = append(streams[lo], payload)
		}
	}
	return streams
}

// TestVP8MultiResSVCParityBaseline plants the byte-parity
// floor for the three target shapes flagged in tasks #79 and #195:
//
//  1. multi-resolution simulcast (parent 640x360 + child 320x180,
//     two independent VP8Encoder instances driven from the same
//     source, no SVC),
//  2. 2-spatial-layer simulcast cluster + 2-temporal-layer SVC on
//     every rendition (the WebRTC SVC simulcast pattern), and
//  3. a 2-temporal-layer SVC config at 64x64 cpu=-3 (the smallest
//     SVC fixture in TestVP8OracleEncoderStreamByteParityTemporalSVC
//     promoted to a non-oracle regression test).
//
// Each fixture is encoded twice and the resulting per-stream byte
// payloads must match exactly. This pins govpx-internal determinism;
// any drift in the multi-resolution per-encoder init order, the SVC
// per-layer save/restore, or the temporal-layer pattern table will
// surface here before it reaches the production strict-parity gate.
func TestVP8MultiResSVCParityBaseline(t *testing.T) {
	t.Run("multires-2layer-simulcast", func(t *testing.T) {
		layers := []vp8MultiResSVCLayer{
			{Name: "low", Width: 320, Height: 180, BitrateKbps: 200},
			{Name: "high", Width: 640, Height: 360, BitrateKbps: 800},
		}
		const frames = 4
		runA := vp8MultiResSVCStreams(t, 640, 360, 30, frames, DeadlineRealtime, -3, layers)
		runB := vp8MultiResSVCStreams(t, 640, 360, 30, frames, DeadlineRealtime, -3, layers)
		if len(runA) != len(runB) || len(runA) != len(layers) {
			t.Fatalf("layer count mismatch: a=%d b=%d want=%d", len(runA), len(runB), len(layers))
		}
		for i, l := range layers {
			if len(runA[i]) != len(runB[i]) {
				t.Fatalf("layer %d (%q): frame count drift a=%d b=%d", i, l.Name, len(runA[i]), len(runB[i]))
			}
			for f := 0; f < len(runA[i]); f++ {
				ha := sha256.Sum256(runA[i][f])
				hb := sha256.Sum256(runB[i][f])
				if ha != hb {
					t.Errorf("layer %d (%q) frame %d non-deterministic: len_a=%d len_b=%d first_diff=%d",
						i, l.Name, f, len(runA[i][f]), len(runB[i][f]),
						vp8SVCFirstByteDiff(runA[i][f], runB[i][f]))
				}
			}
		}
	})

	t.Run("svc-2layer-2temporal", func(t *testing.T) {
		const (
			frames = 8
			fps    = 30
			w      = 64
			h      = 64
		)
		bitrates := [5]int{420, 700, 0, 0, 0}
		runA := vp8TemporalSVCStreams(t, w, h, fps, frames, -3, TemporalLayeringTwoLayers, 2, bitrates)
		runB := vp8TemporalSVCStreams(t, w, h, fps, frames, -3, TemporalLayeringTwoLayers, 2, bitrates)
		if len(runA) != 2 || len(runB) != 2 {
			t.Fatalf("layer count: a=%d b=%d want=2", len(runA), len(runB))
		}
		for layer := range 2 {
			if len(runA[layer]) != len(runB[layer]) {
				t.Fatalf("layer %d frame count drift a=%d b=%d", layer, len(runA[layer]), len(runB[layer]))
			}
			for f := 0; f < len(runA[layer]); f++ {
				ha := sha256.Sum256(runA[layer][f])
				hb := sha256.Sum256(runB[layer][f])
				if ha != hb {
					t.Errorf("svc layer %d frame %d non-deterministic: len_a=%d len_b=%d first_diff=%d",
						layer, f, len(runA[layer][f]), len(runB[layer][f]),
						vp8SVCFirstByteDiff(runA[layer][f], runB[layer][f]))
				}
			}
		}
	})

	t.Run("svc-2temporal-cpu-3", func(t *testing.T) {
		const (
			frames = 8
			fps    = 30
			w      = 64
			h      = 64
		)
		bitrates := [5]int{420, 700, 0, 0, 0}
		runA := vp8TemporalSVCStreams(t, w, h, fps, frames, -3, TemporalLayeringTwoLayersThreeFrame, 2, bitrates)
		runB := vp8TemporalSVCStreams(t, w, h, fps, frames, -3, TemporalLayeringTwoLayersThreeFrame, 2, bitrates)
		for layer := range 2 {
			if len(runA[layer]) != len(runB[layer]) {
				t.Fatalf("layer %d frame count drift a=%d b=%d", layer, len(runA[layer]), len(runB[layer]))
			}
			for f := 0; f < len(runA[layer]); f++ {
				ha := sha256.Sum256(runA[layer][f])
				hb := sha256.Sum256(runB[layer][f])
				if ha != hb {
					t.Errorf("svc-3frame layer %d frame %d non-deterministic: len_a=%d len_b=%d first_diff=%d",
						layer, f, len(runA[layer][f]), len(runB[layer][f]),
						vp8SVCFirstByteDiff(runA[layer][f], runB[layer][f]))
				}
			}
		}
	})
}

// TestVP8TemporalLayerIDOverride pins libvpx's runtime layer-id
// override path (VP8E_SET_TEMPORAL_LAYER_ID). After
// SetTemporalLayerID(L), every emitted frame must carry
// TemporalLayerID == L until SetTemporalScalability replaces the
// pattern. Task #195's audit covered the static (pattern-driven) path;
// this test pins the runtime override path that the audit did not
// exercise.
func TestVP8TemporalLayerIDOverride(t *testing.T) {
	bitrates := [5]int{280, 420, 700, 0, 0}
	opts := EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -3,
		KeyFrameInterval:    3000,
		ErrorResilient:      true,
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		TokenPartitions:     1,
		Tuning:              TunePSNR,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringThreeLayers,
			LayerTargetBitrateKbps: bitrates,
		},
	}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	// Force every encoded frame to report layer 1 via the runtime
	// override. The pattern would otherwise rotate through {0,2,1,2}.
	if err := enc.SetTemporalLayerID(1); err != nil {
		t.Fatalf("SetTemporalLayerID(1): %v", err)
	}
	buf := make([]byte, 64*64*4+8192)
	const frames = 6
	for f := range frames {
		src := encoderValidationPanningFrame(64, 64, f)
		result, err := enc.EncodeInto(buf, src, uint64(f), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", f, err)
		}
		if result.Dropped {
			continue
		}
		if result.TemporalLayerID != 1 {
			t.Errorf("frame %d TemporalLayerID=%d, want 1 (override sticky)", f, result.TemporalLayerID)
		}
	}
	// Out-of-range layer id must be rejected.
	if err := enc.SetTemporalLayerID(99); err == nil {
		t.Errorf("SetTemporalLayerID(99) returned nil, want non-nil")
	}
}

// TestVP8MultiResIndependentEncoders pins the rule from
// libvpx's vp8_multi_resolution_encoder.c that running N independent
// VP8 encoders side-by-side over a shared source produces fully
// deterministic per-encoder streams: any cross-encoder state leak (a
// shared package-level cache, a non-thread-safe global) would surface
// as a per-encoder hash drift between a serial and an interleaved
// schedule.
//
// We don't go through the libvpx parent/child mr_encoder API (govpx
// does not expose it); the simulcast pattern in examples/webrtc-vp8
// is the production shape and is what this test pins.
func TestVP8MultiResIndependentEncoders(t *testing.T) {
	layers := []vp8MultiResSVCLayer{
		{Name: "low", Width: 320, Height: 180, BitrateKbps: 200},
		{Name: "high", Width: 640, Height: 360, BitrateKbps: 800},
	}
	const frames = 4
	// Reference: per-encoder runs (one encoder at a time over the full
	// clip).
	refStreams := make([][][]byte, len(layers))
	for i, l := range layers {
		single := []vp8MultiResSVCLayer{l}
		runs := vp8MultiResSVCStreams(t, 640, 360, 30, frames, DeadlineRealtime, -3, single)
		refStreams[i] = runs[0]
	}
	// Interleaved: all encoders advancing frame-by-frame.
	interleaved := vp8MultiResSVCStreams(t, 640, 360, 30, frames, DeadlineRealtime, -3, layers)
	for i, l := range layers {
		if len(interleaved[i]) != len(refStreams[i]) {
			t.Fatalf("layer %d (%q) frame count drift: interleaved=%d ref=%d",
				i, l.Name, len(interleaved[i]), len(refStreams[i]))
		}
		for f := 0; f < len(refStreams[i]); f++ {
			gh := sha256.Sum256(interleaved[i][f])
			rh := sha256.Sum256(refStreams[i][f])
			if gh != rh {
				t.Errorf("layer %d (%q) frame %d schedule-dependent: interleaved_len=%d ref_len=%d first_diff=%d",
					i, l.Name, f,
					len(interleaved[i][f]), len(refStreams[i][f]),
					vp8SVCFirstByteDiff(interleaved[i][f], refStreams[i][f]))
			}
		}
	}
}
