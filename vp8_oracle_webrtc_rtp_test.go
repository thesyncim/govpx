//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8WebRTCLongTemporalStreamDecodesWithVpxdec(t *testing.T) {
	vp8test.RequireOracle(t, "VP8 WebRTC RTP vpxdec oracle")
	vpxdec := vp8test.Vpxdec(t)

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

	packet := make([]byte, 1<<20)
	packets := make([][]byte, 0, frames)
	pictureID := uint16(VP8RTPPictureID15BitMask - 1)
	seenLayer := [3]bool{}
	fragmented := false

	for frame := 0; frame < frames; frame++ {
		result, err := enc.EncodeInto(packet, rateControlTestFrame(width, height, frame),
			uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto[%d]: %v", frame, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto[%d] dropped; no-loss oracle stream needs emitted frames", frame)
		}
		if result.TemporalLayerID < 0 || result.TemporalLayerID >= len(seenLayer) {
			t.Fatalf("frame %d temporal layer = %d, want 0..2", frame, result.TemporalLayerID)
		}
		seenLayer[result.TemporalLayerID] = true

		payloads, err := result.PacketizeWebRTCRTP(pictureID, mtu)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTP[%d]: %v", frame, err)
		}
		if len(payloads) > 1 {
			fragmented = true
		}
		for i, payload := range payloads {
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
			if payload.Marker != (i == len(payloads)-1) {
				t.Fatalf("frame %d fragment %d marker = %t, want %t",
					frame, i, payload.Marker, i == len(payloads)-1)
			}
		}

		assembled, err := AssembleVP8RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP8RTPFrame[%d]: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
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

	ivf := testutil.BuildVP8IVF(width, height, 30, 1, packets)
	raw, diag, err := vp8test.VpxdecVP8DecodeI420(ivf,
		vp8test.VpxdecVP8Config{BinaryPath: vpxdec})
	if err != nil {
		t.Fatalf("vpxdec VP8 decode failed: %v\n%s", err, diag)
	}
	want := frames * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
}
