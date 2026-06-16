//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9WebRTCPreEncodeDropPacketizedStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		Deadline:           DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  120,
		DropFrameAllowed:   true,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	packetizer := NewVP9WebRTCPacketizer(VP9RTPPictureID15BitMask - 2)
	fragments := make([]RTPPayloadFragment, 0, 16)
	payloadBuf := make([]byte, 0, 4096)
	packets := make([][]byte, 0, 8)
	droppedFrames := 0
	for frame := 0; frame < 8; frame++ {
		if frame == 1 {
			e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
		}
		result, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(32+frame*11), byte(224-frame*7),
			byte(96+frame*3), byte(192-frame*5)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		packetsNeeded, payloadBytes, sent, err := packetizer.PacketizationSize(
			result, 89)
		if err != nil {
			t.Fatalf("PacketizationSize[%d]: %v", frame, err)
		}
		if result.Dropped {
			droppedFrames++
			if sent || packetsNeeded != 0 || payloadBytes != 0 {
				t.Fatalf("dropped frame %d size = packets:%d bytes:%d sent:%t",
					frame, packetsNeeded, payloadBytes, sent)
			}
			if _, _, sent, err := packetizer.PacketizeInto(result, nil, nil,
				89); err != nil || sent {
				t.Fatalf("dropped PacketizeInto[%d] = sent:%t err:%v",
					frame, sent, err)
			}
			if err := e.SetFrameDropAllowed(false); err != nil {
				t.Fatalf("SetFrameDropAllowed(false): %v", err)
			}
			continue
		}
		if !sent {
			t.Fatalf("non-dropped frame %d reported unsent size", frame)
		}
		if cap(fragments) < packetsNeeded {
			fragments = make([]RTPPayloadFragment, packetsNeeded)
		}
		fragments = fragments[:packetsNeeded]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		n, used, sent, err := packetizer.PacketizeInto(result, fragments,
			payloadBuf, 89)
		if err != nil || !sent {
			t.Fatalf("PacketizeInto[%d] = packets:%d bytes:%d sent:%t err:%v",
				frame, n, used, sent, err)
		}
		if n != packetsNeeded || used != payloadBytes {
			t.Fatalf("PacketizeInto[%d] returned %d/%d, want %d/%d",
				frame, n, used, packetsNeeded, payloadBytes)
		}
		assembled, err := AssembleVP9RTPFrame(fragments[:n])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame[%d]: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
	}
	if droppedFrames != 1 {
		t.Fatalf("pre-encode dropped frames = %d, want 1", droppedFrames)
	}

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	raw := vp9test.VpxdecI420(t, ivf)
	want := len(packets) * width * height * 3 / 2
	if len(raw) != want {
		t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
	}
}
