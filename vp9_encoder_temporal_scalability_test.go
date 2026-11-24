package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderTemporalTwoLayerResultSequence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		TargetBitrateKbps: 300,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringTwoLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	decoder, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	wantLayer := []int{0, 1, 0, 1}
	wantTL0 := []uint8{0, 0, 1, 1}
	wantRefresh := []uint8{0xff, 0x02, 0x01, 0x02}
	wantSync := []bool{false, true, false, false}
	var prevHeader *vp9dec.UncompressedHeader
	for i := range wantLayer {
		src := vp9test.NewYCbCr(width, height, byte(80+i*20), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", i, err)
		}
		packet := append([]byte(nil), result.Data...)
		if len(packet) == 0 || result.SizeBytes != len(packet) {
			t.Fatalf("result[%d] size = %d data=%d", i, result.SizeBytes, len(packet))
		}
		if got := result.TemporalLayerID; got != wantLayer[i] {
			t.Fatalf("frame %d temporal layer = %d, want %d", i, got, wantLayer[i])
		}
		if got := result.TemporalLayerCount; got != 2 {
			t.Fatalf("frame %d temporal layer count = %d, want 2", i, got)
		}
		if got := result.TL0PICIDX; got != wantTL0[i] {
			t.Fatalf("frame %d TL0PICIDX = %d, want %d", i, got, wantTL0[i])
		}
		if got, want := result.TemporalLayerSync, wantSync[i]; got != want {
			t.Fatalf("frame %d temporal sync = %t, want %t", i, got, want)
		}
		var br vp9dec.BitReader
		br.Init(packet)
		header, err := vp9dec.ReadUncompressedHeader(&br, prevHeader,
			func(uint8) (uint32, uint32) { return width, height })
		if err != nil {
			t.Fatalf("ReadUncompressedHeader[%d]: %v", i, err)
		}
		prevHeader = &header
		if got := result.RefreshFrameFlags; got != wantRefresh[i] {
			t.Fatalf("frame %d result refresh flags = %#x, want %#x", i, got, wantRefresh[i])
		}
		if got := header.RefreshFrameFlags; got != wantRefresh[i] {
			t.Fatalf("frame %d parsed header = %+v refresh flags = %#x, want %#x",
				i, header, got, wantRefresh[i])
		}
		if got, want := result.KeyFrame, i == 0; got != want {
			t.Fatalf("frame %d keyframe = %t, want %t", i, got, want)
		}
		if !result.ShowFrame || !header.ShowFrame {
			t.Fatalf("frame %d ShowFrame result=%t header=%t, want visible",
				i, result.ShowFrame, header.ShowFrame)
		}
		if err := decoder.Decode(packet); err != nil {
			t.Fatalf("Decode[%d]: %v", i, err)
		}
		if _, ok := decoder.NextFrame(); !ok {
			t.Fatalf("NextFrame[%d] returned !ok", i)
		}
		if i == 1 {
			desc := result.RTPPayloadDescriptor()
			payload, err := PackVP9RTPPayload(desc, packet)
			if err != nil {
				t.Fatalf("PackVP9RTPPayload: %v", err)
			}
			gotDesc, gotPacket, err := ParseVP9RTPPayloadDescriptor(payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
			}
			if !bytes.Equal(gotPacket, packet) {
				t.Fatalf("RTP payload packet changed")
			}
			if !gotDesc.LayerIndicesPresent || gotDesc.TemporalID != 1 ||
				gotDesc.TL0PICIDX != 0 || !gotDesc.SwitchingUpPoint ||
				!gotDesc.InterPicturePredicted {
				t.Fatalf("RTP descriptor = %+v, want temporal layer 1 sync", gotDesc)
			}
		}
	}
}

func TestVP9EncoderTemporalForcedKeyFrameReportsBaseLayer(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		TargetBitrateKbps: 300,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	first, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, 80, 128, 128), dst)
	if err != nil {
		t.Fatalf("first EncodeIntoWithResult: %v", err)
	}
	if !first.KeyFrame || first.TemporalLayerID != 0 || first.TL0PICIDX != 0 {
		t.Fatalf("first temporal = key:%t id:%d tl0:%d, want key/0/0",
			first.KeyFrame, first.TemporalLayerID, first.TL0PICIDX)
	}

	e.ForceKeyFrame()
	forced, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, 100, 128, 128), dst)
	if err != nil {
		t.Fatalf("forced EncodeIntoWithResult: %v", err)
	}
	if !forced.KeyFrame || forced.TemporalLayerID != 0 ||
		forced.TemporalLayerCount != 3 || forced.TemporalLayerSync ||
		forced.TL0PICIDX != 1 {
		t.Fatalf("forced temporal = key:%t id:%d count:%d sync:%t tl0:%d, want key/0/3/false/1",
			forced.KeyFrame, forced.TemporalLayerID,
			forced.TemporalLayerCount, forced.TemporalLayerSync,
			forced.TL0PICIDX)
	}
	desc := forced.RTPPayloadDescriptor()
	payload, err := PackVP9RTPPayload(desc, forced.Data)
	if err != nil {
		t.Fatalf("PackVP9RTPPayload forced keyframe: %v", err)
	}
	gotDesc, _, err := ParseVP9RTPPayloadDescriptor(payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor forced keyframe: %v", err)
	}
	if gotDesc.InterPicturePredicted || int(gotDesc.TemporalID) != 0 ||
		gotDesc.TL0PICIDX != forced.TL0PICIDX {
		t.Fatalf("forced RTP descriptor = %+v, want keyframe T0/TL0 %d",
			gotDesc, forced.TL0PICIDX)
	}

	next, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, 120, 128, 128), dst)
	if err != nil {
		t.Fatalf("post-forced EncodeIntoWithResult: %v", err)
	}
	if next.TemporalLayerID != 2 || next.TL0PICIDX != forced.TL0PICIDX {
		t.Fatalf("post-forced temporal = id:%d tl0:%d, want 2/%d",
			next.TemporalLayerID, next.TL0PICIDX, forced.TL0PICIDX)
	}
}

func TestVP9EncoderSetTemporalScalabilityUpdatesResultSequence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		TargetBitrateKbps: 300,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringTwoLayers,
	}); err != nil {
		t.Fatalf("SetTemporalScalability: %v", err)
	}
	if got := e.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 180 || got[1] != 300 {
		t.Fatalf("derived VP9 temporal bitrates = %v, want [180 300 ...]", got)
	}

	dst := make([]byte, 65536)
	for i, wantLayer := range []int{0, 1} {
		result, err := e.EncodeIntoWithResult(
			vp9test.NewYCbCr(width, height, byte(90+i*20), 128, 128), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", i, err)
		}
		if result.TemporalLayerID != wantLayer || result.TemporalLayerCount != 2 {
			t.Fatalf("frame %d temporal = id:%d count:%d, want %d/2",
				i, result.TemporalLayerID, result.TemporalLayerCount, wantLayer)
		}
	}

	if err := e.SetTemporalLayerID(1); err != nil {
		t.Fatalf("SetTemporalLayerID: %v", err)
	}
	result, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 140, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult override: %v", err)
	}
	if result.TemporalLayerID != 1 {
		t.Fatalf("override temporal layer = %d, want 1", result.TemporalLayerID)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{}); err != nil {
		t.Fatalf("disable SetTemporalScalability: %v", err)
	}
	result, err = e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 160, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult disabled: %v", err)
	}
	if result.TemporalLayerID != 0 || result.TemporalLayerCount != 1 {
		t.Fatalf("disabled temporal = id:%d count:%d, want 0/1",
			result.TemporalLayerID, result.TemporalLayerCount)
	}
}
