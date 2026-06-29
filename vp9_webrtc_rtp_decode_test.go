package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9WebRTCRTPLongTemporalNoLossStreamDecodes(t *testing.T) {
	const (
		width  = 64
		height = 64
		frames = 36
		mtu    = 41
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:                    width,
		Height:                   height,
		FPS:                      30,
		RateControlModeSet:       true,
		RateControlMode:          RateControlCBR,
		TargetBitrateKbps:        900,
		MinQuantizer:             4,
		MaxQuantizer:             56,
		DropFrameAllowed:         false,
		Deadline:                 DeadlineRealtime,
		CpuUsed:                  8,
		MaxKeyframeInterval:      120,
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		BufferSizeMs:             600,
		BufferInitialSizeMs:      400,
		BufferOptimalSizeMs:      500,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayersWithSync,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}
	defer enc.Close()

	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	defer dec.Close()

	packetizer := NewVP9WebRTCPacketizer(VP9RTPPictureID15BitMask - 1)
	packet := make([]byte, 1<<20)
	frameBuf := make([]byte, 1<<20)
	payloadBuf := make([]byte, 1<<20)
	fragments := make([]RTPPayloadFragment, 256)
	var wantTL0 uint8
	seenLayer := [3]bool{}
	fragmented := false

	for frame := range frames {
		result, err := enc.EncodeIntoWithResult(vp9test.NewPanningYCbCr(width, height, frame), packet)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult %d returned error: %v", frame, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult %d dropped; no-loss WebRTC regression needs emitted frames", frame)
		}
		if result.TemporalLayerCount != 3 {
			t.Fatalf("frame %d temporal layer count = %d, want 3",
				frame, result.TemporalLayerCount)
		}
		if result.TemporalLayerID == 0 {
			wantTL0 = result.TL0PICIDX
		}
		if result.TL0PICIDX != wantTL0 {
			t.Fatalf("frame %d TL0PICIDX = %d, want held at %d for layer %d",
				frame, result.TL0PICIDX, wantTL0, result.TemporalLayerID)
		}
		if result.TemporalLayerID >= 0 && result.TemporalLayerID < len(seenLayer) {
			seenLayer[result.TemporalLayerID] = true
		}

		packets, payloadBytes, sent, err := packetizer.WebRTCNonFlexiblePacketizationSize(result, mtu)
		if err != nil || !sent {
			t.Fatalf("WebRTCNonFlexiblePacketizationSize frame %d = packets:%d bytes:%d sent:%t err:%v",
				frame, packets, payloadBytes, sent, err)
		}
		if packets > len(fragments) || payloadBytes > len(payloadBuf) {
			t.Fatalf("frame %d test buffers too small: packets=%d payloadBytes=%d",
				frame, packets, payloadBytes)
		}
		pictureID := packetizer.PictureID()
		fragmentCount, usedBytes, sent, err := packetizer.PacketizeWebRTCNonFlexibleInto(
			result, fragments[:packets], payloadBuf[:payloadBytes], mtu)
		if err != nil || !sent {
			t.Fatalf("PacketizeWebRTCNonFlexibleInto frame %d = packets:%d/%d bytes:%d/%d sent:%t err:%v",
				frame, fragmentCount, packets, usedBytes, payloadBytes, sent, err)
		}
		if fragmentCount != packets || usedBytes != payloadBytes {
			t.Fatalf("frame %d packetization returned %d/%d, want %d/%d",
				frame, fragmentCount, usedBytes, packets, payloadBytes)
		}
		if fragmentCount > 1 {
			fragmented = true
		}
		payloads := fragments[:fragmentCount]
		for i, payload := range payloads {
			if len(payload.Payload) == 0 {
				t.Fatalf("frame %d fragment %d empty RTP payload", frame, i)
			}
			desc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor frame %d fragment %d: %v",
					frame, i, err)
			}
			if desc.PictureID != pictureID ||
				!desc.PictureIDPresent || !desc.PictureID15Bit {
				t.Fatalf("frame %d fragment %d descriptor picture id = %+v, want 15-bit %d",
					frame, i, desc, pictureID)
			}
			if !desc.LayerIndicesPresent ||
				int(desc.TemporalID) != result.TemporalLayerID ||
				desc.TL0PICIDX != result.TL0PICIDX ||
				desc.SwitchingUpPoint != result.TemporalLayerSync {
				t.Fatalf("frame %d fragment %d descriptor temporal = %+v, result layer=%d tl0=%d sync=%t",
					frame, i, desc, result.TemporalLayerID, result.TL0PICIDX,
					result.TemporalLayerSync)
			}
			if desc.StartOfFrame != (i == 0) || desc.EndOfFrame != (i == fragmentCount-1) {
				t.Fatalf("frame %d fragment %d start/end = %t/%t, want %t/%t",
					frame, i, desc.StartOfFrame, desc.EndOfFrame,
					i == 0, i == fragmentCount-1)
			}
			if payload.Marker != (i == fragmentCount-1) {
				t.Fatalf("frame %d fragment %d marker = %t, want %t",
					frame, i, payload.Marker, i == fragmentCount-1)
			}
		}
		n, err := dec.DecodeRTPIntoWithPTS(frameBuf, payloads, uint64(frame))
		if err != nil {
			t.Fatalf("DecodeRTPIntoWithPTS frame %d returned error: %v", frame, err)
		}
		if n != result.SizeBytes || payloadBytes == 0 {
			t.Fatalf("frame %d assembled bytes = %d, want %d; payload bytes=%d",
				frame, n, result.SizeBytes, payloadBytes)
		}
		info, ok := dec.LastFrameInfo()
		if !ok || info.PTS != uint64(frame) || info.Width != width || info.Height != height {
			t.Fatalf("frame %d LastFrameInfo = %+v ok=%t, want %dx%d PTS %d",
				frame, info, ok, width, height, frame)
		}
		img, ok := dec.NextFrame()
		if !ok {
			t.Fatalf("frame %d queued no visible decoded frame", frame)
		}
		if img.Width != width || img.Height != height || len(img.Y) == 0 {
			t.Fatalf("frame %d decoded image = %dx%d Y=%d",
				frame, img.Width, img.Height, len(img.Y))
		}
	}
	if !fragmented {
		t.Fatal("test did not exercise fragmented VP9 RTP access units")
	}
	for layer, seen := range seenLayer {
		if !seen {
			t.Fatalf("temporal layer %d was never emitted", layer)
		}
	}
}
