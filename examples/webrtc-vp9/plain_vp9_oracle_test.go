//go:build govpx_oracle_trace

package main

import (
	"bytes"
	"hash/fnv"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestPlainVP9WebRTCLongNoLossStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	const frames = 48
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                defaultFPS,
		Deadline:           govpx.DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  900,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	packets := make([][]byte, 0, frames)
	pictureID := uint16(govpx.VP9RTPPictureID15BitMask - 2)
	for frame := 0; frame < frames; frame++ {
		if frame != 0 && frame%11 == 0 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*7), byte(224-frame*3),
			byte(96+frame*5), byte(192-frame*2)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		payloads, err := result.PacketizeWebRTCRTP(pictureID, 89)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTP frame %d: %v", frame, err)
		}
		wantSS := result.KeyFrame && !result.InterPicturePredicted &&
			result.TemporalLayerID == 0
		assertPlainVP9WebRTCPionPayloadBodiesForTest(t, result, payloads,
			pictureID, width, height, wantSS)
		refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame frame %d: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
	}

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := frames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
	assertPlainVP9VpxdecOutputVariesForTest(t, raw, frames, width, height)
}

func TestPlainVP9WebRTCPacketizerLongNoLossStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	const frames = 48
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                defaultFPS,
		Deadline:           govpx.DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  900,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	packets := make([][]byte, 0, frames)
	packetizer := govpx.NewVP9WebRTCPacketizer(
		govpx.VP9RTPPictureID15BitMask - 2)
	for frame := 0; frame < frames; frame++ {
		if frame != 0 && frame%11 == 0 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*7), byte(224-frame*3),
			byte(96+frame*5), byte(192-frame*2)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		pictureID := packetizer.PictureID()
		payloads, sent, err := packetizer.Packetize(result, 89)
		if err != nil {
			t.Fatalf("Packetize frame %d: %v", frame, err)
		}
		if !sent {
			t.Fatalf("Packetize frame %d reported unsent", frame)
		}
		wantSS := result.KeyFrame && !result.InterPicturePredicted &&
			result.TemporalLayerID == 0
		assertPlainVP9WebRTCPionPayloadBodiesForTest(t, result, payloads,
			pictureID, width, height, wantSS)
		refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame frame %d: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
	}

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := frames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
	assertPlainVP9VpxdecOutputVariesForTest(t, raw, frames, width, height)
}

func TestPlainVP9WebRTCPacketizerNoInterLayerPredictionLongNoLossStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	const frames = 48
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                defaultFPS,
		Deadline:           govpx.DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  900,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayersNoInterLayerPrediction,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	packets := make([][]byte, 0, frames)
	packetizer := govpx.NewVP9WebRTCPacketizer(
		govpx.VP9RTPPictureID15BitMask - 2)
	for frame := 0; frame < frames; frame++ {
		if frame != 0 && frame%11 == 0 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*7), byte(224-frame*3),
			byte(96+frame*5), byte(192-frame*2)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		pictureID := packetizer.PictureID()
		payloads, sent, err := packetizer.Packetize(result, 89)
		if err != nil {
			t.Fatalf("Packetize frame %d: %v", frame, err)
		}
		if !sent {
			t.Fatalf("Packetize frame %d reported unsent", frame)
		}
		wantSS := result.KeyFrame && !result.InterPicturePredicted &&
			result.TemporalLayerID == 0
		assertPlainVP9WebRTCPionPayloadBodiesForTest(t, result, payloads,
			pictureID, width, height, wantSS)
		refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame frame %d: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
	}

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := frames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
	assertPlainVP9VpxdecOutputVariesForTest(t, raw, frames, width, height)
}

func TestPlainVP9WebRTCNonFlexiblePacketizerLongNoLossStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	const frames = 48
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                defaultFPS,
		Deadline:           govpx.DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  900,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	packets := make([][]byte, 0, frames)
	packetizer := govpx.NewVP9WebRTCPacketizer(
		govpx.VP9RTPPictureID15BitMask - 2)
	for frame := 0; frame < frames; frame++ {
		if frame != 0 && frame%11 == 0 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*7), byte(224-frame*3),
			byte(96+frame*5), byte(192-frame*2)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		pictureID := packetizer.PictureID()
		payloads, sent, err := packetizer.PacketizeWebRTCNonFlexible(result, 89)
		if err != nil {
			t.Fatalf("PacketizeWebRTCNonFlexible frame %d: %v", frame, err)
		}
		if !sent {
			t.Fatalf("PacketizeWebRTCNonFlexible frame %d reported unsent", frame)
		}
		wantSS := result.KeyFrame && !result.InterPicturePredicted &&
			result.TemporalLayerID == 0
		assertPlainVP9WebRTCPionPayloadBodiesForTest(t, result, payloads,
			pictureID, width, height, wantSS)
		refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame frame %d: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d non-flexible WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
	}

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := frames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
	assertPlainVP9VpxdecOutputVariesForTest(t, raw, frames, width, height)
}

func TestPlainVP9WebRTCCBRDropStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	packets, sentFrames, droppedFrames := plainVP9WebRTCCBRDropStreamForTest(
		t, 48, 89, true)
	if droppedFrames == 0 {
		t.Fatal("test did not produce a VP9 CBR dropped frame")
	}
	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := sentFrames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
	assertPlainVP9VpxdecOutputVariesForTest(t, raw, sentFrames, width, height)
}

func assertPlainVP9VpxdecOutputVariesForTest(
	t *testing.T,
	raw []byte,
	frames int,
	width int,
	height int,
) {
	t.Helper()
	frameBytes := width * height * 3 / 2
	if len(raw) != frames*frameBytes {
		t.Fatalf("raw size = %d, want %d", len(raw), frames*frameBytes)
	}
	distinct := make(map[uint64]bool, frames)
	for frame := 0; frame < frames; frame++ {
		off := frame * frameBytes
		h := fnv.New64a()
		_, _ = h.Write(raw[off : off+frameBytes])
		distinct[h.Sum64()] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("vpxdec produced %d distinct frames over %d-frame WebRTC stream",
			len(distinct), frames)
	}
}
