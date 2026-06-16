package main

import (
	"bytes"
	"testing"

	"github.com/pion/rtp/codecs"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestPlainVP9WebRTCPacketizationParsesWithPion(t *testing.T) {
	const width, height = 64, 64
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
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
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	key, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width,
		height, 32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	assertPlainVP9WebRTCPionPayloadsForTest(t, key,
		govpx.VP9RTPPictureID15BitMask-1, 83, width, height, true)

	inter, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width,
		height, 40, 208, 100, 180), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	assertPlainVP9WebRTCPionPayloadsForTest(t, inter,
		govpx.VP9RTPPictureID15BitMask, 83, width, height, false)
}

func assertPlainVP9WebRTCPionPayloadsForTest(
	t *testing.T,
	result govpx.VP9EncodeResult,
	pictureID uint16,
	mtu int,
	width int,
	height int,
	wantSS bool,
) {
	t.Helper()
	payloads, err := result.PacketizeWebRTCRTP(pictureID, mtu)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTP: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatal("PacketizeWebRTCRTP returned no payloads")
	}

	var assembled []byte
	for i, payload := range payloads {
		var packet codecs.VP9Packet
		fragment, err := packet.Unmarshal(payload.Payload)
		if err != nil {
			t.Fatalf("pion VP9Packet.Unmarshal[%d]: %v", i, err)
		}
		if !packet.I || packet.PictureID != pictureID&govpx.VP9RTPPictureID15BitMask {
			t.Fatalf("payload %d PictureID = present:%t id:%d, want %d",
				i, packet.I, packet.PictureID,
				pictureID&govpx.VP9RTPPictureID15BitMask)
		}
		if packet.F {
			t.Fatalf("payload %d used flexible mode", i)
		}
		if got, want := packet.B, i == 0; got != want {
			t.Fatalf("payload %d B = %t, want %t", i, got, want)
		}
		if got, want := packet.E, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d E = %t, want %t", i, got, want)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		if packet.P != result.InterPicturePredicted {
			t.Fatalf("payload %d P = %t, want %t",
				i, packet.P, result.InterPicturePredicted)
		}
		if result.TemporalLayerCount > 1 {
			if !packet.L ||
				int(packet.TID) != result.TemporalLayerID ||
				packet.TL0PICIDX != result.TL0PICIDX ||
				packet.U != result.TemporalLayerSync {
				t.Fatalf("payload %d temporal = L:%t tid:%d tl0:%d u:%t, want %d/%d/%t",
					i, packet.L, packet.TID, packet.TL0PICIDX,
					packet.U, result.TemporalLayerID,
					result.TL0PICIDX, result.TemporalLayerSync)
			}
		}
		if i == 0 && wantSS {
			if !packet.V || !packet.Y || packet.NS != 0 ||
				len(packet.Width) != 1 || len(packet.Height) != 1 ||
				packet.Width[0] != uint16(width) ||
				packet.Height[0] != uint16(height) ||
				!packet.G || packet.NG != 4 {
				t.Fatalf("payload %d SS = V:%t Y:%t NS:%d %dx%d G:%t NG:%d",
					i, packet.V, packet.Y, packet.NS,
					firstUint16ForTest(packet.Width),
					firstUint16ForTest(packet.Height),
					packet.G, packet.NG)
			}
		} else if packet.V {
			t.Fatalf("payload %d unexpectedly repeated scalability structure", i)
		}
		assembled = append(assembled, fragment...)
	}
	if !bytes.Equal(assembled, result.Data) {
		t.Fatal("Pion VP9 payload fragments differ from encoded frame")
	}
}

func firstUint16ForTest(values []uint16) uint16 {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}
