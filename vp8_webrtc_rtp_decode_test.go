package govpx

import "testing"

func TestVP8WebRTCRTPLongTemporalNoLossStreamDecodes(t *testing.T) {
	const (
		width  = 64
		height = 64
		frames = 36
		mtu    = 37
	)
	enc, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    false,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayersWithSync,
		},
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer enc.Close()

	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	defer dec.Close()

	packet := make([]byte, 1<<20)
	frameBuf := make([]byte, 1<<20)
	payloadBuf := make([]byte, 1<<20)
	fragments := make([]RTPPayloadFragment, 256)
	pictureID := uint16(VP8RTPPictureID15BitMask - 1)
	var wantTL0 uint8
	seenLayer := [3]bool{}
	fragmented := false

	for frame := range frames {
		result, err := enc.EncodeInto(packet, rateControlTestFrame(width, height, frame),
			uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", frame, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto %d dropped; no-loss WebRTC regression needs emitted frames", frame)
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

		fragmentCount, payloadBytes, err := result.PacketizeWebRTCRTPInto(
			fragments, payloadBuf, pictureID, mtu)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTPInto frame %d returned error: %v", frame, err)
		}
		if fragmentCount > 1 {
			fragmented = true
		}
		payloads := fragments[:fragmentCount]
		for i, payload := range payloads {
			if len(payload.Payload) == 0 {
				t.Fatalf("frame %d fragment %d empty RTP payload", frame, i)
			}
			desc, _, err := ParseVP8RTPPayloadDescriptor(payload.Payload)
			if err != nil {
				t.Fatalf("ParseVP8RTPPayloadDescriptor frame %d fragment %d: %v",
					frame, i, err)
			}
			if desc.PictureID != pictureID ||
				!desc.PictureIDPresent || !desc.PictureID15Bit {
				t.Fatalf("frame %d fragment %d descriptor picture id = %+v, want 15-bit %d",
					frame, i, desc, pictureID)
			}
			if !desc.TL0PICIDXPresent || desc.TL0PICIDX != result.TL0PICIDX ||
				!desc.TemporalIDPresent || int(desc.TemporalID) != result.TemporalLayerID ||
				desc.LayerSync != result.TemporalLayerSync ||
				desc.NonReferenceFrame != result.Droppable {
				t.Fatalf("frame %d fragment %d descriptor temporal = %+v, result layer=%d tl0=%d sync=%t droppable=%t",
					frame, i, desc, result.TemporalLayerID, result.TL0PICIDX,
					result.TemporalLayerSync, result.Droppable)
			}
			if desc.StartOfPartition != (i == 0) {
				t.Fatalf("frame %d fragment %d start = %t, want %t",
					frame, i, desc.StartOfPartition, i == 0)
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
		pictureID = NextVP8RTPPictureID(pictureID)
	}
	if !fragmented {
		t.Fatal("test did not exercise fragmented VP8 RTP access units")
	}
	for layer, seen := range seenLayer {
		if !seen {
			t.Fatalf("temporal layer %d was never emitted", layer)
		}
	}
}
