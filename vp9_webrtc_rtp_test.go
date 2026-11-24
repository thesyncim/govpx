package govpx_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncodeResultPacketizeWebRTCRTP(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		Deadline:          govpx.DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 300,
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
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if key.TemporalLayeringMode != govpx.TemporalLayeringThreeLayers {
		t.Fatalf("key temporal mode = %v, want three-layer",
			key.TemporalLayeringMode)
	}

	const pictureID = uint16(0x8123)
	desc := key.WebRTCRTPPayloadDescriptor(pictureID)
	if !desc.PictureIDPresent || !desc.PictureID15Bit ||
		desc.PictureID != pictureID&govpx.VP9RTPPictureID15BitMask {
		t.Fatalf("key WebRTC PictureID = present:%t 15bit:%t id:%d",
			desc.PictureIDPresent, desc.PictureID15Bit, desc.PictureID)
	}
	if !desc.ScalabilityStructurePresent ||
		desc.ScalabilityStructure.SpatialLayerCount != 1 ||
		!desc.ScalabilityStructure.ResolutionPresent ||
		desc.ScalabilityStructure.Width[0] != width ||
		desc.ScalabilityStructure.Height[0] != height ||
		!desc.ScalabilityStructure.PictureGroupPresent ||
		len(desc.ScalabilityStructure.PictureGroups) != 4 {
		t.Fatalf("key WebRTC SS = %+v", desc.ScalabilityStructure)
	}
	wantGroups := []govpx.VP9RTPPictureGroup{
		{
			TemporalID:          0,
			ReferenceIndexCount: 1,
			ReferenceIndices:    [govpx.VP9RTPMaxReferenceIndices]uint8{4},
		},
		{
			TemporalID:          2,
			SwitchingUpPoint:    true,
			ReferenceIndexCount: 1,
			ReferenceIndices:    [govpx.VP9RTPMaxReferenceIndices]uint8{1},
		},
		{
			TemporalID:          1,
			SwitchingUpPoint:    true,
			ReferenceIndexCount: 1,
			ReferenceIndices:    [govpx.VP9RTPMaxReferenceIndices]uint8{2},
		},
		{
			TemporalID:          2,
			SwitchingUpPoint:    true,
			ReferenceIndexCount: 1,
			ReferenceIndices:    [govpx.VP9RTPMaxReferenceIndices]uint8{1},
		},
	}
	if !equalVP9PictureGroupsForTest(desc.ScalabilityStructure.PictureGroups,
		wantGroups) {
		t.Fatalf("key WebRTC GOF = %+v, want %+v",
			desc.ScalabilityStructure.PictureGroups, wantGroups)
	}

	const mtu = 80
	packets, payloadBytes, err := key.WebRTCRTPPacketizationSize(pictureID, mtu)
	if err != nil {
		t.Fatalf("WebRTCRTPPacketizationSize: %v", err)
	}
	if packets < 2 {
		t.Fatalf("packets = %d, want fragmented VP9 frame", packets)
	}
	short := make([]govpx.RTPPayloadFragment, packets-1)
	payloadBuf := make([]byte, payloadBytes)
	if needPackets, needBytes, err := key.PacketizeWebRTCRTPInto(short,
		payloadBuf, pictureID, mtu); !errors.Is(err, govpx.ErrBufferTooSmall) ||
		needPackets != packets || needBytes != payloadBytes {
		t.Fatalf("short PacketizeWebRTCRTPInto = packets:%d bytes:%d err:%v, want %d/%d ErrBufferTooSmall",
			needPackets, needBytes, err, packets, payloadBytes)
	}

	payloads := make([]govpx.RTPPayloadFragment, packets)
	n, used, err := key.PacketizeWebRTCRTPInto(payloads, payloadBuf,
		pictureID, mtu)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTPInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeWebRTCRTPInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	assertVP9WebRTCPayloadsForTest(t, payloads, key, pictureID,
		width, height, true)

	assembled, err := govpx.AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(assembled, key.Data) {
		t.Fatal("assembled WebRTC RTP frame differs from encoded keyframe")
	}

	inter, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		40, 208, 100, 180), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	interPictureID := govpx.NextVP9RTPPictureID(pictureID)
	interPayloads, err := inter.PacketizeWebRTCRTP(interPictureID, mtu)
	if err != nil {
		t.Fatalf("inter PacketizeWebRTCRTP: %v", err)
	}
	assertVP9WebRTCPayloadsForTest(t, interPayloads, inter,
		interPictureID, width, height, false)
	if got := govpx.NextVP9RTPPictureID(govpx.VP9RTPPictureID15BitMask); got != 0 {
		t.Fatalf("NextVP9RTPPictureID wrap = %d, want 0", got)
	}
}

func TestVP9EncodeResultPacketizeWebRTCRTPSingleLayerKeyCarriesGOF(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		Deadline:          govpx.DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 300,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if key.TemporalLayerCount != 1 || key.TemporalLayerID != 0 {
		t.Fatalf("key temporal metadata = count:%d id:%d, want single TL0",
			key.TemporalLayerCount, key.TemporalLayerID)
	}

	const pictureID = uint16(0x6123)
	payloads, err := key.PacketizeWebRTCRTP(pictureID, 1200)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTP: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatal("PacketizeWebRTCRTP returned no payloads")
	}
	desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	ss := desc.ScalabilityStructure
	if !desc.ScalabilityStructurePresent ||
		ss.SpatialLayerCount != 1 ||
		!ss.ResolutionPresent ||
		ss.Width[0] != width ||
		ss.Height[0] != height ||
		!ss.PictureGroupPresent ||
		len(ss.PictureGroups) != 1 {
		t.Fatalf("single-layer key SS = present:%t %+v",
			desc.ScalabilityStructurePresent, ss)
	}
	group := ss.PictureGroups[0]
	if group.TemporalID != 0 || group.ReferenceIndexCount != 0 ||
		group.SwitchingUpPoint {
		t.Fatalf("single-layer GOF = %+v, want TL0 without refs", group)
	}
}

func TestVP9EncodeResultPacketizeWebRTCRTPValidation(t *testing.T) {
	if _, _, err := (govpx.VP9EncodeResult{
		Data:               []byte{0x82, 0x49},
		KeyFrame:           true,
		TemporalLayerID:    0,
		TemporalLayerCount: 1,
	}).WebRTCRTPPacketizationSize(1, 1200); !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Fatalf("invalid keyframe WebRTCRTPPacketizationSize err = %v, want ErrInvalidVP9Data", err)
	}
	if _, _, err := (govpx.VP9EncodeResult{
		Dropped:            true,
		TemporalLayerID:    0,
		TemporalLayerCount: 1,
	}).WebRTCRTPPacketizationSize(1, 1200); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("dropped WebRTCRTPPacketizationSize err = %v, want ErrInvalidConfig", err)
	}
	if _, _, err := (govpx.VP9EncodeResult{
		Data:               []byte{0x01},
		TemporalLayerID:    2,
		TemporalLayerCount: 2,
	}).WebRTCRTPPacketizationSize(1, 1200); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("bad temporal WebRTCRTPPacketizationSize err = %v, want ErrInvalidConfig", err)
	}
}

func equalVP9PictureGroupsForTest(a, b []govpx.VP9RTPPictureGroup) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertVP9WebRTCPayloadsForTest(
	t *testing.T,
	payloads []govpx.RTPPayloadFragment,
	result govpx.VP9EncodeResult,
	pictureID uint16,
	width int,
	height int,
	wantSS bool,
) {
	t.Helper()
	for i, payload := range payloads {
		desc, fragment, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID&govpx.VP9RTPPictureID15BitMask {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID,
				pictureID&govpx.VP9RTPPictureID15BitMask)
		}
		if desc.FlexibleMode {
			t.Fatalf("payload %d used flexible mode", i)
		}
		if got, want := desc.StartOfFrame, i == 0; got != want {
			t.Fatalf("payload %d start = %t, want %t", i, got, want)
		}
		if got, want := desc.EndOfFrame, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d end = %t, want %t", i, got, want)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("RTP payload %d marker = %t, want %t", i, got, want)
		}
		if desc.InterPicturePredicted != result.InterPicturePredicted {
			t.Fatalf("payload %d P = %t, want %t",
				i, desc.InterPicturePredicted, result.InterPicturePredicted)
		}
		if result.TemporalLayerCount > 1 {
			if !desc.LayerIndicesPresent ||
				int(desc.TemporalID) != result.TemporalLayerID ||
				desc.TL0PICIDX != result.TL0PICIDX {
				t.Fatalf("payload %d temporal = L:%t tid:%d tl0:%d, want %d/%d",
					i, desc.LayerIndicesPresent, desc.TemporalID,
					desc.TL0PICIDX,
					result.TemporalLayerID, result.TL0PICIDX)
			}
		}
		if i == 0 && wantSS {
			ss := desc.ScalabilityStructure
			if !desc.ScalabilityStructurePresent ||
				ss.SpatialLayerCount != 1 ||
				!ss.ResolutionPresent ||
				ss.Width[0] != uint16(width) ||
				ss.Height[0] != uint16(height) ||
				!ss.PictureGroupPresent ||
				len(ss.PictureGroups) != 4 {
				t.Fatalf("payload %d SS = present:%t %+v",
					i, desc.ScalabilityStructurePresent, ss)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d unexpectedly repeated SS", i)
		}
		if len(fragment) == 0 {
			t.Fatalf("payload %d produced empty VP9 fragment", i)
		}
	}
}
