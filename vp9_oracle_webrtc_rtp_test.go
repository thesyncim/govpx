//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9WebRTCSingleLayerPacketizedStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 96, 96
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		Deadline:          govpx.DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 500,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	const frames = 8
	packets := make([][]byte, 0, frames)
	pictureID := uint16(govpx.VP9RTPPictureID15BitMask - 2)
	for frame := 0; frame < frames; frame++ {
		if frame == 4 {
			e.ForceKeyFrame()
		}
		result, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width,
			height, byte(32+frame*11), byte(224-frame*7),
			byte(96+frame*3), byte(192-frame*5)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		payloads, err := result.PacketizeWebRTCRTP(pictureID, 97)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTP[%d]: %v", frame, err)
		}
		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame[%d]: %v", frame, err)
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
}

func TestVP9WebRTCNonFlexibleLongTemporalStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const (
		width  = 64
		height = 64
		frames = 36
		mtu    = 41
	)
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:                    width,
		Height:                   height,
		FPS:                      30,
		RateControlModeSet:       true,
		RateControlMode:          govpx.RateControlCBR,
		TargetBitrateKbps:        900,
		MinQuantizer:             4,
		MaxQuantizer:             56,
		DropFrameAllowed:         false,
		Deadline:                 govpx.DeadlineRealtime,
		CpuUsed:                  8,
		MaxKeyframeInterval:      120,
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		BufferSizeMs:             600,
		BufferInitialSizeMs:      400,
		BufferOptimalSizeMs:      500,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayersWithSync,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	packetizer := govpx.NewVP9WebRTCPacketizer(govpx.VP9RTPPictureID15BitMask - 1)
	dst := make([]byte, 1<<20)
	packets := make([][]byte, 0, frames)
	seenLayer := [3]bool{}
	fragmented := false

	for frame := 0; frame < frames; frame++ {
		result, err := e.EncodeIntoWithResult(vp9test.NewPanningYCbCr(width, height, frame), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult[%d] dropped; no-loss oracle stream needs emitted frames", frame)
		}
		if result.TemporalLayerID < 0 || result.TemporalLayerID >= len(seenLayer) {
			t.Fatalf("frame %d temporal layer = %d, want 0..2", frame, result.TemporalLayerID)
		}
		seenLayer[result.TemporalLayerID] = true

		payloads, sent, err := packetizer.PacketizeWebRTCNonFlexible(result, mtu)
		if err != nil || !sent {
			t.Fatalf("PacketizeWebRTCNonFlexible[%d]: sent=%t err=%v", frame, sent, err)
		}
		if len(payloads) > 1 {
			fragmented = true
		}
		for i, payload := range payloads {
			desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor frame %d fragment %d: %v",
					frame, i, err)
			}
			if desc.FlexibleMode {
				t.Fatalf("frame %d fragment %d used flexible mode descriptor", frame, i)
			}
			if !desc.LayerIndicesPresent ||
				int(desc.TemporalID) != result.TemporalLayerID ||
				desc.TL0PICIDX != result.TL0PICIDX ||
				desc.SwitchingUpPoint != result.TemporalLayerSync {
				t.Fatalf("frame %d fragment %d descriptor temporal = %+v, result layer=%d tl0=%d sync=%t",
					frame, i, desc, result.TemporalLayerID, result.TL0PICIDX,
					result.TemporalLayerSync)
			}
			if desc.StartOfFrame != (i == 0) || desc.EndOfFrame != (i == len(payloads)-1) {
				t.Fatalf("frame %d fragment %d start/end = %t/%t, want %t/%t",
					frame, i, desc.StartOfFrame, desc.EndOfFrame,
					i == 0, i == len(payloads)-1)
			}
			if payload.Marker != (i == len(payloads)-1) {
				t.Fatalf("frame %d fragment %d marker = %t, want %t",
					frame, i, payload.Marker, i == len(payloads)-1)
			}
		}

		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame[%d]: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
	}
	if !fragmented {
		t.Fatal("test did not exercise fragmented VP9 RTP access units")
	}
	for layer, seen := range seenLayer {
		if !seen {
			t.Fatalf("temporal layer %d was never emitted", layer)
		}
	}

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := frames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
}
