package govpx_test

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9SpatialSVCEncodeResultPacketizeWebRTCRTP(t *testing.T) {
	results := encodeVP9WebRTCSVCTestResults(t, 2)
	result := results[0]
	const pictureID = uint16(0x8042)
	payloads, err := result.PacketizeWebRTCRTP(pictureID, 96)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTP: %v", err)
	}
	if len(payloads) <= int(result.LayerCount) {
		t.Fatalf("payload count = %d, want fragmented spatial layers", len(payloads))
	}

	var byLayer [govpx.VP9RTPMaxSpatialLayers][]govpx.RTPPayloadFragment
	var sawBaseSS bool
	for i, payload := range payloads {
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID&govpx.VP9RTPPictureID15BitMask {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d",
				i, desc.PictureIDPresent, desc.PictureID15Bit, desc.PictureID)
		}
		if !desc.LayerIndicesPresent || desc.SpatialID >= result.LayerCount {
			t.Fatalf("payload %d descriptor = %+v, want valid spatial metadata",
				i, desc)
		}
		if desc.SpatialID == 0 && desc.StartOfFrame {
			sawBaseSS = desc.ScalabilityStructurePresent &&
				desc.ScalabilityStructure.SpatialLayerCount == int(result.LayerCount) &&
				desc.ScalabilityStructure.PictureGroupPresent &&
				len(desc.ScalabilityStructure.PictureGroups) == 2
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d repeated scalability structure", i)
		}
		byLayer[desc.SpatialID] = append(byLayer[desc.SpatialID], payload)
	}
	if !sawBaseSS {
		t.Fatal("base key payload did not carry WebRTC scalability structure")
	}
	for layerID := 0; layerID < int(result.LayerCount); layerID++ {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layerID])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layerID, err)
		}
		if !bytes.Equal(assembled, result.Layers[layerID].Data) {
			t.Fatalf("assembled layer %d does not match encoded layer", layerID)
		}
	}

	deltaPayloads, err := results[1].PacketizeWebRTCRTP(0x44, 96)
	if err != nil {
		t.Fatalf("delta PacketizeWebRTCRTP: %v", err)
	}
	for i, payload := range deltaPayloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor delta[%d]: %v", i, err)
		}
		if desc.ScalabilityStructurePresent {
			t.Fatalf("delta payload %d carried scalability structure", i)
		}
	}
	if got := govpx.NextVP9RTPPictureID(govpx.VP9RTPPictureID15BitMask); got != 0 {
		t.Fatalf("NextVP9RTPPictureID wrap = %d, want 0", got)
	}
}

func TestVP9WebRTCSpatialLayerChangeNeedsKeyFrame(t *testing.T) {
	tests := []struct {
		name    string
		current int
		next    int
		want    bool
	}{
		{name: "unchanged", current: 3, next: 3},
		{name: "cap down", current: 3, next: 1, want: true},
		{name: "cap up", current: 1, next: 3, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := govpx.VP9WebRTCSpatialLayerChangeNeedsKeyFrame(tt.current,
				tt.next); got != tt.want {
				t.Fatalf("VP9WebRTCSpatialLayerChangeNeedsKeyFrame(%d, %d) = %t, want %t",
					tt.current, tt.next, got, tt.want)
			}
		})
	}
}

func TestVP9SpatialSVCEncodeResultLimitSpatialLayersForRTP(t *testing.T) {
	result := encodeVP9WebRTCSVCTestResults(t, 1)[0]
	capped, err := result.LimitSpatialLayersForRTP(1)
	if err != nil {
		t.Fatalf("LimitSpatialLayersForRTP: %v", err)
	}
	if capped.LayerCount != 1 || capped.Data != nil ||
		capped.SizeBytes != result.Layers[0].SizeBytes {
		t.Fatalf("capped result = layers:%d data:%t size:%d, want base-only RTP view",
			capped.LayerCount, capped.Data != nil, capped.SizeBytes)
	}
	if capped.Layers[0].SpatialLayerCount != 1 ||
		!capped.Layers[0].NotRefForUpperSpatialLayer {
		t.Fatalf("capped base metadata = %+v", capped.Layers[0])
	}
	if capped.ScalabilityStructure.SpatialLayerCount != 1 ||
		capped.ScalabilityStructure.Width[1] != 0 ||
		capped.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("capped SS = %+v, want hidden layer dimensions cleared",
			capped.ScalabilityStructure)
	}
	payloads, err := capped.PacketizeWebRTCRTP(0x55, 80)
	if err != nil {
		t.Fatalf("capped PacketizeWebRTCRTP: %v", err)
	}
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor capped[%d]: %v", i, err)
		}
		if desc.SpatialID != 0 {
			t.Fatalf("capped payload %d spatial id = %d, want base only",
				i, desc.SpatialID)
		}
		if desc.StartOfFrame && (!desc.ScalabilityStructurePresent ||
			desc.ScalabilityStructure.SpatialLayerCount != 1) {
			t.Fatalf("capped base SS = %+v", desc.ScalabilityStructure)
		}
	}

	if _, err := result.LimitSpatialLayersForRTP(0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("LimitSpatialLayersForRTP(0) err = %v, want ErrInvalidConfig", err)
	}
	if _, err := result.LimitSpatialLayersForRTP(int(result.LayerCount) + 1); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("LimitSpatialLayersForRTP(over) err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9WebRTCPacketizerPacketizesActiveSpatialSVCTransitions(t *testing.T) {
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringThreeLayers,
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           3,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{
				Width:                    32,
				Height:                   32,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        120,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    64,
				Height:                   64,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        240,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    128,
				Height:                   128,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        480,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()

	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 90, 120, 136),
		vp9test.NewYCbCr(64, 64, 90, 120, 136),
		vp9test.NewYCbCr(128, 128, 90, 120, 136),
	}
	dst := make([]byte, 1<<20)
	packetizer := govpx.NewVP9WebRTCPacketizer(govpx.VP9RTPPictureID15BitMask - 1)
	caps := []int{3, 3, 1, 1, 2, 3}
	lastCap := caps[0]
	for frame, cap := range caps {
		if frame == 0 || cap != lastCap {
			svc.ForceKeyFrame()
		}
		for layer, src := range srcs {
			vp9test.FillYCbCr(src, uint8(80+frame*9+layer*7), 120, 136)
		}
		result, err := svc.EncodeActiveLayersIntoWithResult(srcs, dst, cap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, cap, err)
		}
		if int(result.LayerCount) != cap {
			t.Fatalf("frame %d active layer count = %d, want %d",
				frame, result.LayerCount, cap)
		}

		pictureID := packetizer.PictureID()
		packets, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
			result, 80)
		if err != nil {
			t.Fatalf("SpatialSVCWebRTCPacketizationSize frame %d: %v",
				frame, err)
		}
		if got := packetizer.PictureID(); got != pictureID {
			t.Fatalf("size query frame %d advanced PictureID to %d, want %d",
				frame, got, pictureID)
		}
		payloads := make([]govpx.RTPPayloadFragment, packets)
		payloadBuf := make([]byte, payloadBytes)
		n, used, err := packetizer.PacketizeSpatialSVCWebRTCInto(result,
			payloads, payloadBuf, 80)
		if err != nil {
			t.Fatalf("PacketizeSpatialSVCWebRTCInto frame %d: %v", frame, err)
		}
		if n != packets || used != payloadBytes {
			t.Fatalf("PacketizeSpatialSVCWebRTCInto frame %d returned %d/%d, want %d/%d",
				frame, n, used, packets, payloadBytes)
		}
		assertVP9ActiveSVCWebRTCPacketizationForTest(t, frame, result,
			payloads[:n], pictureID)
		if got, want := packetizer.PictureID(), govpx.NextVP9RTPPictureID(pictureID); got != want {
			t.Fatalf("PacketizeSpatialSVCWebRTCInto frame %d PictureID = %d, want %d",
				frame, got, want)
		}
		lastCap = cap
	}
}

func TestVP9WebRTCPacketizerRequiresRecoveryKeyOnSVCLayerCountChange(t *testing.T) {
	svc, srcs := newVP9WebRTCLayerChangeSVCForTest(t)
	dst := make([]byte, 1<<20)
	packetizer := govpx.NewVP9WebRTCPacketizer(0x220)
	const mtu = 80

	result := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 0, 3)
	packetizeSpatialSVCWebRTCForTest(t, &packetizer, result, mtu)
	firstNextPictureID := packetizer.PictureID()

	result = encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 1, 1)
	if result.Layers[0].KeyFrame {
		t.Fatal("test setup produced keyframe for unforced layer-count shrink")
	}
	packets, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		result, mtu)
	if !errors.Is(err, govpx.ErrInvalidConfig) ||
		packets != 0 || payloadBytes != 0 {
		t.Fatalf("shrink without key size = %d/%d err:%v, want ErrInvalidConfig",
			packets, payloadBytes, err)
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("layer-count shrink without key did not require recovery key")
	}
	if got := packetizer.PictureID(); got != firstNextPictureID {
		t.Fatalf("PictureID after rejected shrink = %d, want %d",
			got, firstNextPictureID)
	}
	if n, used, err := packetizer.PacketizeSpatialSVCWebRTCInto(result, nil,
		nil, mtu); !errors.Is(err, govpx.ErrInvalidConfig) ||
		n != 0 || used != 0 {
		t.Fatalf("shrink without key packetize = %d/%d err:%v, want ErrInvalidConfig",
			n, used, err)
	}

	svc.ForceKeyFrame()
	result = encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 2, 1)
	if !result.Layers[0].KeyFrame {
		t.Fatal("forced shrink recovery did not produce keyframe")
	}
	packetizeSpatialSVCWebRTCForTest(t, &packetizer, result, mtu)
	if packetizer.NeedsKeyFrame() {
		t.Fatal("recovery key did not clear NeedsKeyFrame")
	}
	secondNextPictureID := packetizer.PictureID()

	result = encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 3, 3)
	if result.Layers[0].KeyFrame {
		t.Fatal("test setup produced keyframe for unforced layer-count grow")
	}
	packets, payloadBytes, err = packetizer.SpatialSVCWebRTCPacketizationSize(
		result, mtu)
	if !errors.Is(err, govpx.ErrInvalidConfig) ||
		packets != 0 || payloadBytes != 0 {
		t.Fatalf("grow without key size = %d/%d err:%v, want ErrInvalidConfig",
			packets, payloadBytes, err)
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("layer-count grow without key did not require recovery key")
	}
	if got := packetizer.PictureID(); got != secondNextPictureID {
		t.Fatalf("PictureID after rejected grow = %d, want %d",
			got, secondNextPictureID)
	}
}

func TestVP9WebRTCPacketizerRequiresRecoveryAfterMarkedUnsentSVCAccessUnit(t *testing.T) {
	svc, srcs := newVP9WebRTCLayerChangeSVCForTest(t)
	dst := make([]byte, 1<<20)
	packetizer := govpx.NewVP9WebRTCPacketizer(0x300)
	const mtu = 80

	key := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 0, 3)
	packetizeSpatialSVCWebRTCForTest(t, &packetizer, key, mtu)

	unsentPictureID := packetizer.PictureID()
	unsent := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 1, 3)
	if unsent.Layers[0].KeyFrame {
		t.Fatal("test setup produced keyframe for withheld access unit")
	}
	payloads := packetizeSpatialSVCWebRTCForTest(t, &packetizer, unsent, mtu)
	if len(payloads) == 0 {
		t.Fatal("test setup withheld an empty access unit")
	}
	if got, want := packetizer.PictureID(),
		govpx.NextVP9RTPPictureID(unsentPictureID); got != want {
		t.Fatalf("PictureID after withheld packetize = %d, want %d",
			got, want)
	}

	packetizer.MarkAccessUnitUnsent()
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("MarkAccessUnitUnsent did not require a recovery key")
	}

	inter := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 2, 3)
	if inter.Layers[0].KeyFrame {
		t.Fatal("test setup produced keyframe before recovery request")
	}
	if packets, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		inter, mtu); !errors.Is(err, govpx.ErrInvalidConfig) ||
		packets != 0 || payloadBytes != 0 {
		t.Fatalf("post-unsent inter size = %d/%d err:%v, want ErrInvalidConfig",
			packets, payloadBytes, err)
	}
	if got, want := packetizer.PictureID(),
		govpx.NextVP9RTPPictureID(unsentPictureID); got != want {
		t.Fatalf("PictureID after rejected post-unsent inter = %d, want %d",
			got, want)
	}

	svc.ForceKeyFrame()
	recovery := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 3, 3)
	if !recovery.Layers[0].KeyFrame ||
		recovery.Layers[0].InterPicturePredicted {
		t.Fatalf("recovery base = key:%t inter:%t, want key/non-predicted",
			recovery.Layers[0].KeyFrame,
			recovery.Layers[0].InterPicturePredicted)
	}
	packetizeSpatialSVCWebRTCForTest(t, &packetizer, recovery, mtu)
	if packetizer.NeedsKeyFrame() {
		t.Fatal("recovery key did not clear NeedsKeyFrame")
	}
}

func TestVP9WebRTCPacketizerConsumesEncodedUnsentSVCAccessUnit(t *testing.T) {
	svc, srcs := newVP9WebRTCLayerChangeSVCForTest(t)
	dst := make([]byte, 1<<20)
	packetizer := govpx.NewVP9WebRTCPacketizer(
		govpx.VP9RTPPictureID15BitMask - 1)
	const mtu = 80

	key := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 0, 3)
	packetizeSpatialSVCWebRTCForTest(t, &packetizer, key, mtu)

	unsentPictureID := packetizer.PictureID()
	unsent := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 1, 3)
	if unsent.Layers[0].KeyFrame {
		t.Fatal("test setup produced keyframe for encoded unsent access unit")
	}
	packets, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		unsent, mtu)
	if err != nil {
		t.Fatalf("encoded unsent size: %v", err)
	}
	if packets == 0 || payloadBytes == 0 {
		t.Fatalf("encoded unsent size = %d/%d, want non-empty RTP shape",
			packets, payloadBytes)
	}
	if got := packetizer.PictureID(); got != unsentPictureID {
		t.Fatalf("encoded unsent size advanced PictureID to %d, want %d",
			got, unsentPictureID)
	}

	packetizer.MarkEncodedAccessUnitUnsent()
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("MarkEncodedAccessUnitUnsent did not require a recovery key")
	}
	if got, want := packetizer.PictureID(),
		govpx.NextVP9RTPPictureID(unsentPictureID); got != want {
		t.Fatalf("PictureID after encoded unsent access unit = %d, want %d",
			got, want)
	}

	inter := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 2, 3)
	if inter.Layers[0].KeyFrame {
		t.Fatal("test setup produced keyframe before recovery request")
	}
	if packets, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		inter, mtu); !errors.Is(err, govpx.ErrInvalidConfig) ||
		packets != 0 || payloadBytes != 0 {
		t.Fatalf("post-encoded-unsent inter size = %d/%d err:%v, want ErrInvalidConfig",
			packets, payloadBytes, err)
	}
	if got, want := packetizer.PictureID(),
		govpx.NextVP9RTPPictureID(unsentPictureID); got != want {
		t.Fatalf("PictureID after rejected post-unsent inter = %d, want %d",
			got, want)
	}

	svc.ForceKeyFrame()
	recovery := encodeVP9WebRTCLayerChangeResultForTest(t, svc, srcs, dst, 3, 3)
	if !recovery.Layers[0].KeyFrame ||
		recovery.Layers[0].InterPicturePredicted {
		t.Fatalf("recovery base = key:%t inter:%t, want key/non-predicted",
			recovery.Layers[0].KeyFrame,
			recovery.Layers[0].InterPicturePredicted)
	}
	packetizeSpatialSVCWebRTCForTest(t, &packetizer, recovery, mtu)
	if packetizer.NeedsKeyFrame() {
		t.Fatal("recovery key did not clear NeedsKeyFrame")
	}
}

func TestVP9WebRTCPacketizerKeepsSVCRefsOnBufferTooSmall(t *testing.T) {
	results := encodeVP9WebRTCSVCTestResults(t, 2)
	packetizer := govpx.NewVP9WebRTCPacketizer(0x1234)
	const mtu = 80

	keyPackets, keyBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		results[0], mtu)
	if err != nil {
		t.Fatalf("key SpatialSVCWebRTCPacketizationSize: %v", err)
	}
	keyPayloads := make([]govpx.RTPPayloadFragment, keyPackets)
	keyPayloadBuf := make([]byte, keyBytes)
	if n, used, err := packetizer.PacketizeSpatialSVCWebRTCInto(results[0],
		keyPayloads, keyPayloadBuf, mtu); err != nil ||
		n != keyPackets || used != keyBytes {
		t.Fatalf("key PacketizeSpatialSVCWebRTCInto = %d/%d err:%v, want %d/%d nil",
			n, used, err, keyPackets, keyBytes)
	}
	if got := packetizer.PictureID(); got != 0x1235 {
		t.Fatalf("PictureID after key = %d, want 0x1235", got)
	}

	deltaPictureID := packetizer.PictureID()
	deltaPackets, deltaBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		results[1], mtu)
	if err != nil {
		t.Fatalf("delta SpatialSVCWebRTCPacketizationSize: %v", err)
	}
	shortPayloads := make([]govpx.RTPPayloadFragment, deltaPackets-1)
	deltaPayloadBuf := make([]byte, deltaBytes)
	if gotPackets, gotBytes, err := packetizer.PacketizeSpatialSVCWebRTCInto(
		results[1], shortPayloads, deltaPayloadBuf, mtu); !errors.Is(err,
		govpx.ErrBufferTooSmall) ||
		gotPackets != deltaPackets || gotBytes != deltaBytes {
		t.Fatalf("short PacketizeSpatialSVCWebRTCInto = %d/%d err:%v, want %d/%d ErrBufferTooSmall",
			gotPackets, gotBytes, err, deltaPackets, deltaBytes)
	}
	if got := packetizer.PictureID(); got != deltaPictureID {
		t.Fatalf("PictureID advanced after buffer error: got %d want %d",
			got, deltaPictureID)
	}

	deltaPayloads := make([]govpx.RTPPayloadFragment, deltaPackets)
	n, used, err := packetizer.PacketizeSpatialSVCWebRTCInto(results[1],
		deltaPayloads, deltaPayloadBuf, mtu)
	if err != nil || n != deltaPackets || used != deltaBytes {
		t.Fatalf("retry PacketizeSpatialSVCWebRTCInto = %d/%d err:%v, want %d/%d nil",
			n, used, err, deltaPackets, deltaBytes)
	}
	assertVP9ActiveSVCWebRTCPacketizationForTest(t, 1, results[1],
		deltaPayloads[:n], deltaPictureID)
	sawPredictedRef := false
	for i, payload := range deltaPayloads[:n] {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("delta ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.StartOfFrame && desc.InterPicturePredicted {
			if desc.ReferenceIndexCount == 0 {
				t.Fatalf("delta start payload %d has P=1 without flexible refs",
					i)
			}
			sawPredictedRef = true
		}
	}
	if !sawPredictedRef {
		t.Fatal("retry did not exercise predicted flexible references")
	}
	if got, want := packetizer.PictureID(),
		govpx.NextVP9RTPPictureID(deltaPictureID); got != want {
		t.Fatalf("PictureID after retry = %d, want %d", got, want)
	}
}

func TestVP9WebRTCPacketizerPacketizesNonFlexibleSpatialSVC(t *testing.T) {
	results := encodeVP9WebRTCSVCTestResults(t, 2)
	packetizer := govpx.NewVP9WebRTCPacketizer(govpx.VP9RTPPictureID15BitMask)
	pictureID := packetizer.PictureID()
	packets, payloadBytes, err := packetizer.
		SpatialSVCWebRTCNonFlexiblePacketizationSize(results[0], 96)
	if err != nil {
		t.Fatalf("SpatialSVCWebRTCNonFlexiblePacketizationSize: %v", err)
	}
	if got := packetizer.PictureID(); got != pictureID {
		t.Fatalf("non-flexible size advanced PictureID to %d, want %d",
			got, pictureID)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, used, err := packetizer.PacketizeSpatialSVCWebRTCNonFlexibleInto(
		results[0], payloads, payloadBuf, 96)
	if err != nil {
		t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	payloads = payloads[:n]
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("non-flexible PictureID after packetize = %d, want wrap to 0",
			got)
	}

	var byLayer [govpx.VP9MaxSpatialLayers][]govpx.RTPPayloadFragment
	sawBaseSS := false
	for i, payload := range payloads {
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.FlexibleMode {
			t.Fatalf("payload %d used flexible descriptor", i)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, pictureID)
		}
		if !desc.LayerIndicesPresent ||
			desc.SpatialID >= results[0].LayerCount {
			t.Fatalf("payload %d descriptor = %+v, want spatial metadata", i, desc)
		}
		if desc.StartOfFrame && desc.SpatialID == 0 {
			sawBaseSS = desc.ScalabilityStructurePresent &&
				desc.ScalabilityStructure.PictureGroupPresent
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d repeated scalability structure", i)
		}
		byLayer[desc.SpatialID] = append(byLayer[desc.SpatialID], payload)
	}
	if !sawBaseSS {
		t.Fatal("non-flexible base key did not carry GOF scalability structure")
	}
	for layer := 0; layer < int(results[0].LayerCount); layer++ {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layer])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layer, err)
		}
		if !bytes.Equal(assembled, results[0].Layers[layer].Data) {
			t.Fatalf("assembled layer %d does not match encoded data", layer)
		}
	}
}

func newVP9WebRTCLayerChangeSVCForTest(
	t *testing.T,
) (*govpx.VP9SpatialSVCEncoder, []*image.YCbCr) {
	t.Helper()
	dims := []struct {
		width, height int
		kbps          int
	}{
		{32, 32, 120},
		{64, 64, 240},
		{128, 128, 480},
	}
	var layers [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions
	srcs := make([]*image.YCbCr, len(dims))
	for i, dim := range dims {
		layers[i] = govpx.VP9EncoderOptions{
			Width:                    dim.width,
			Height:                   dim.height,
			FPS:                      30,
			Deadline:                 govpx.DeadlineRealtime,
			CpuUsed:                  8,
			RateControlModeSet:       true,
			RateControlMode:          govpx.RateControlCBR,
			TargetBitrateKbps:        dim.kbps,
			ErrorResilient:           true,
			FrameParallelDecodingSet: true,
			FrameParallelDecoding:    true,
		}
		srcs[i] = image.NewYCbCr(image.Rect(0, 0, dim.width, dim.height),
			image.YCbCrSubsampleRatio420)
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           uint8(len(dims)),
		InterLayerPrediction: true,
		Layers:               layers,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc, srcs
}

func encodeVP9WebRTCLayerChangeResultForTest(
	t *testing.T,
	svc *govpx.VP9SpatialSVCEncoder,
	srcs []*image.YCbCr,
	dst []byte,
	frame int,
	activeLayers int,
) govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	for layer, src := range srcs {
		vp9test.FillYCbCr(src, uint8(72+frame*11+layer*17), 120, 136)
	}
	result, err := svc.EncodeActiveLayersIntoWithResult(srcs, dst, activeLayers)
	if err != nil {
		t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
			frame, activeLayers, err)
	}
	if int(result.LayerCount) != activeLayers {
		t.Fatalf("frame %d active layer count = %d, want %d",
			frame, result.LayerCount, activeLayers)
	}
	return result
}

func packetizeSpatialSVCWebRTCForTest(
	t *testing.T,
	packetizer *govpx.VP9WebRTCPacketizer,
	result govpx.VP9SpatialSVCEncodeResult,
	mtu int,
) []govpx.RTPPayloadFragment {
	t.Helper()
	pictureID := packetizer.PictureID()
	packets, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
		result, mtu)
	if err != nil {
		t.Fatalf("SpatialSVCWebRTCPacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, used, err := packetizer.PacketizeSpatialSVCWebRTCInto(result, payloads,
		payloadBuf, mtu)
	if err != nil {
		t.Fatalf("PacketizeSpatialSVCWebRTCInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeSpatialSVCWebRTCInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	payloads = payloads[:n]
	assertVP9ActiveSVCWebRTCPacketizationForTest(t, 0, result, payloads,
		pictureID)
	if got, want := packetizer.PictureID(),
		govpx.NextVP9RTPPictureID(pictureID); got != want {
		t.Fatalf("PictureID after packetize = %d, want %d", got, want)
	}
	return payloads
}

func assertVP9ActiveSVCWebRTCPacketizationForTest(
	t *testing.T,
	frame int,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
	pictureID uint16,
) {
	t.Helper()
	count := int(result.LayerCount)
	var byLayer [govpx.VP9MaxSpatialLayers][]govpx.RTPPayloadFragment
	starts := 0
	sawBaseSS := false
	for i, payload := range payloads {
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("frame %d payload %d marker = %t, want %t",
				frame, i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
				frame, i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID {
			t.Fatalf("frame %d payload %d PictureID = present:%t 15bit:%t id:%d, want %d",
				frame, i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, pictureID)
		}
		if !desc.FlexibleMode {
			t.Fatalf("frame %d payload %d used non-flexible descriptor",
				frame, i)
		}
		layerID := int(desc.SpatialID)
		if count > 1 {
			if !desc.LayerIndicesPresent || layerID >= count {
				t.Fatalf("frame %d payload %d descriptor = %+v, want spatial id < %d",
					frame, i, desc, count)
			}
		} else if desc.LayerIndicesPresent && desc.SpatialID != 0 {
			t.Fatalf("frame %d payload %d single-layer descriptor = %+v, want spatial id 0",
				frame, i, desc)
		} else {
			layerID = 0
		}
		if desc.StartOfFrame {
			starts++
			if layerID == 0 {
				sawBaseSS = desc.ScalabilityStructurePresent
				if desc.ScalabilityStructurePresent &&
					desc.ScalabilityStructure.SpatialLayerCount != count {
					t.Fatalf("frame %d base SS layers = %d, want %d",
						frame, desc.ScalabilityStructure.SpatialLayerCount,
						count)
				}
				if desc.ScalabilityStructurePresent &&
					desc.ScalabilityStructure.PictureGroupPresent {
					t.Fatalf("frame %d flexible base SS unexpectedly carried GOF",
						frame)
				}
			} else if desc.ScalabilityStructurePresent {
				t.Fatalf("frame %d layer %d unexpectedly carried SS",
					frame, layerID)
			}
		}
		byLayer[layerID] = append(byLayer[layerID], payload)
	}
	if starts != count {
		t.Fatalf("frame %d start payloads = %d, want %d", frame, starts, count)
	}
	wantSS := result.Layers[0].KeyFrame &&
		!result.Layers[0].InterPicturePredicted &&
		result.Layers[0].TemporalLayerID == 0
	if sawBaseSS != wantSS {
		t.Fatalf("frame %d base SS present = %t, want %t",
			frame, sawBaseSS, wantSS)
	}

	var frames [govpx.VP9MaxSpatialLayers][]byte
	for layer := 0; layer < count; layer++ {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layer])
		if err != nil {
			t.Fatalf("frame %d AssembleVP9RTPFrame layer %d: %v",
				frame, layer, err)
		}
		if !bytes.Equal(assembled, result.Layers[layer].Data) {
			t.Fatalf("frame %d layer %d RTP reassembly changed payload",
				frame, layer)
		}
		frames[layer] = assembled
	}
	need, err := govpx.VP9SuperframeSize(frames[:count]...)
	if err != nil {
		t.Fatalf("frame %d VP9SuperframeSize: %v", frame, err)
	}
	packet := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(packet, frames[:count]...)
	if err != nil {
		t.Fatalf("frame %d PackVP9SuperframeInto: %v", frame, err)
	}
	if !bytes.Equal(packet[:n], result.Data) {
		t.Fatalf("frame %d RTP-reassembled active access unit changed payload",
			frame)
	}
}

func TestVP9SpatialSVCEncodeResultPacketizeWebRTCRTPIntoSteadyStateNoAlloc(t *testing.T) {
	result := encodeVP9WebRTCSVCTestResults(t, 1)[0]
	const mtu = 80
	packets, payloadBytes, err := result.WebRTCRTPPacketizationSize(0x77, mtu)
	if err != nil {
		t.Fatalf("WebRTCRTPPacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	if _, _, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf, 0x77, mtu); err != nil {
		t.Fatalf("warmup PacketizeWebRTCRTPInto: %v", err)
	}

	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		n, used, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf,
			0x77, mtu)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTPInto: %v", err)
		}
		if n != packets || used != payloadBytes {
			t.Fatalf("PacketizeWebRTCRTPInto returned %d/%d, want %d/%d",
				n, used, packets, payloadBytes)
		}
	})
	if allocs != 0 {
		t.Fatalf("PacketizeWebRTCRTPInto allocs = %f, want 0", allocs)
	}

	if _, _, err := result.PacketizeWebRTCRTPInto(payloads[:packets-1],
		payloadBuf, 0x77, mtu); !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("short PacketizeWebRTCRTPInto err = %v, want ErrBufferTooSmall", err)
	}
}

func encodeVP9WebRTCSVCTestResults(
	t *testing.T,
	frames int,
) []govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringTwoLayers,
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{
				Width:                    32,
				Height:                   32,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        120,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    64,
				Height:                   64,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        240,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()
	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 90, 120, 136),
		vp9test.NewYCbCr(64, 64, 90, 120, 136),
	}
	dst := make([]byte, 1<<20)
	results := make([]govpx.VP9SpatialSVCEncodeResult, frames)
	for frame := 0; frame < frames; frame++ {
		vp9test.FillYCbCr(srcs[0], uint8(90+frame*7), 120, 136)
		vp9test.FillYCbCr(srcs[1], uint8(90+frame*7), 120, 136)
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results[frame] = result
	}
	return results
}
