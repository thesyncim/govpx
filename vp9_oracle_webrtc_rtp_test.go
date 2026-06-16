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
