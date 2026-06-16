package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

type vp9WebRTCReferenceTracker struct {
	pictureID     [common.RefFrames]uint16
	valid         [common.RefFrames]bool
	lastPictureID uint16
	haveLast      bool
}

func (p *VP9WebRTCPacketizer) vp9WebRTCPacketizationSize(
	r VP9EncodeResult,
	mtu int,
) (int, int, error) {
	desc, frame, err := p.vp9WebRTCFlexibleRTPDescriptorAndFrame(r,
		p.pictureID)
	if err != nil {
		return 0, 0, err
	}
	return VP9RTPFramePacketizationSize(desc, frame, mtu)
}

func (p *VP9WebRTCPacketizer) vp9PacketizeWebRTCInto(
	r VP9EncodeResult,
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	mtu int,
) (int, int, error) {
	desc, frame, err := p.vp9WebRTCFlexibleRTPDescriptorAndFrame(r,
		p.pictureID)
	if err != nil {
		return 0, 0, err
	}
	return PacketizeVP9RTPFrameInto(dst, payloadBuf, desc, frame, mtu)
}

func (p *VP9WebRTCPacketizer) vp9PacketizeWebRTC(
	r VP9EncodeResult,
	mtu int,
) ([]RTPPayloadFragment, error) {
	packets, payloadBytes, err := p.vp9WebRTCPacketizationSize(r, mtu)
	if err != nil {
		return nil, err
	}
	out := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, _, err := p.vp9PacketizeWebRTCInto(r, out, payloadBuf, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func (p *VP9WebRTCPacketizer) vp9SpatialSVCWebRTCPacketizationSize(
	r VP9SpatialSVCEncodeResult,
	mtu int,
) (int, int, error) {
	count, err := r.vp9WebRTCLayerCount()
	if err != nil {
		return 0, 0, err
	}
	packets := 0
	payloadBytes := 0
	for i := 0; i < count; i++ {
		desc, frame, err := p.vp9SpatialSVCWebRTCLayerDescriptor(r, i,
			p.pictureID)
		if err != nil {
			return 0, 0, err
		}
		layerPackets, layerBytes, err := VP9RTPFramePacketizationSize(desc,
			frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		packets += layerPackets
		payloadBytes += layerBytes
	}
	return packets, payloadBytes, nil
}

func (p *VP9WebRTCPacketizer) vp9PacketizeSpatialSVCWebRTCInto(
	r VP9SpatialSVCEncodeResult,
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	mtu int,
) (int, int, error) {
	count, err := r.vp9WebRTCLayerCount()
	if err != nil {
		return 0, 0, err
	}
	packets, payloadBytes, err := p.vp9SpatialSVCWebRTCPacketizationSize(r,
		mtu)
	if err != nil {
		return 0, 0, err
	}
	if len(dst) < packets || len(payloadBuf) < payloadBytes {
		return packets, payloadBytes, ErrBufferTooSmall
	}
	packetOff := 0
	byteOff := 0
	for i := 0; i < count; i++ {
		desc, frame, err := p.vp9SpatialSVCWebRTCLayerDescriptor(r, i,
			p.pictureID)
		if err != nil {
			return 0, 0, err
		}
		layerPackets, layerBytes, err := VP9RTPFramePacketizationSize(desc,
			frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		writtenPackets, writtenBytes, err := PacketizeVP9RTPFrameInto(
			dst[packetOff:packetOff+layerPackets],
			payloadBuf[byteOff:byteOff+layerBytes],
			desc, frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		for j := 0; j < writtenPackets; j++ {
			dst[packetOff+j].Marker = i == count-1 && j == writtenPackets-1
		}
		packetOff += writtenPackets
		byteOff += writtenBytes
	}
	return packets, payloadBytes, nil
}

func (p *VP9WebRTCPacketizer) vp9PacketizeSpatialSVCWebRTC(
	r VP9SpatialSVCEncodeResult,
	mtu int,
) ([]RTPPayloadFragment, error) {
	packets, payloadBytes, err := p.vp9SpatialSVCWebRTCPacketizationSize(r,
		mtu)
	if err != nil {
		return nil, err
	}
	out := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, _, err := p.vp9PacketizeSpatialSVCWebRTCInto(r, out, payloadBuf, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func (p *VP9WebRTCPacketizer) vp9SpatialSVCWebRTCLayerDescriptor(
	r VP9SpatialSVCEncodeResult,
	layerID int,
	pictureID uint16,
) (VP9RTPPayloadDescriptor, []byte, error) {
	desc, frame, err := r.vp9WebRTCLayerDescriptor(layerID, pictureID)
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	desc, err = p.vp9WebRTCFlexibleDescriptorForResult(r.Layers[layerID],
		desc, frame, pictureID)
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	return desc, frame, nil
}

func (p *VP9WebRTCPacketizer) vp9WebRTCFlexibleRTPDescriptorAndFrame(
	r VP9EncodeResult,
	pictureID uint16,
) (VP9RTPPayloadDescriptor, []byte, error) {
	desc, frame, err := r.vp9WebRTCRTPDescriptorAndFrame(pictureID)
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	desc, err = p.vp9WebRTCFlexibleDescriptorForResult(r, desc, frame,
		pictureID)
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	return desc, frame, nil
}

func (p *VP9WebRTCPacketizer) vp9WebRTCFlexibleDescriptorForResult(
	r VP9EncodeResult,
	desc VP9RTPPayloadDescriptor,
	frame []byte,
	pictureID uint16,
) (VP9RTPPayloadDescriptor, error) {
	desc.FlexibleMode = true
	desc.TL0PICIDX = 0
	if desc.ScalabilityStructurePresent {
		desc.ScalabilityStructure = vp9WebRTCFlexibleScalabilityStructure(
			desc.ScalabilityStructure)
	}
	if !desc.InterPicturePredicted {
		return desc, nil
	}
	refs, count, err := p.references.flexibleReferenceDiffs(r, frame,
		pictureID)
	if err != nil {
		return VP9RTPPayloadDescriptor{}, err
	}
	desc.ReferenceIndices = refs
	desc.ReferenceIndexCount = count
	return desc, nil
}

func vp9WebRTCFlexibleScalabilityStructure(
	ss VP9RTPScalabilityStructure,
) VP9RTPScalabilityStructure {
	ss.PictureGroupPresent = false
	ss.PictureGroups = nil
	return ss
}

func (t *vp9WebRTCReferenceTracker) flexibleReferenceDiffs(
	r VP9EncodeResult,
	frame []byte,
	pictureID uint16,
) ([VP9RTPMaxReferenceIndices]uint8, int, error) {
	var refs [VP9RTPMaxReferenceIndices]uint8
	slots, slotCount, err := vp9WebRTCReferenceSlotsForFrame(frame)
	if err != nil {
		return refs, 0, err
	}
	if slotCount == 0 {
		return refs, 0, vp9WebRTCRecoveryKeyRequiredError()
	}
	count := 0
	for i := 0; i < slotCount; i++ {
		if !t.addReferenceSlotDiff(&refs, &count, pictureID, slots[i]) {
			return refs, 0, vp9WebRTCRecoveryKeyRequiredError()
		}
	}
	if count == 0 {
		return refs, 0, vp9WebRTCRecoveryKeyRequiredError()
	}
	return refs, count, nil
}

func (t *vp9WebRTCReferenceTracker) addReferenceSlotDiff(
	refs *[VP9RTPMaxReferenceIndices]uint8,
	count *int,
	pictureID uint16,
	slot uint8,
) bool {
	if int(slot) >= len(t.valid) || !t.valid[slot] {
		return false
	}
	return addVP9WebRTCReferenceDiff(refs, count, pictureID,
		t.pictureID[slot])
}

func addVP9WebRTCReferenceDiff(
	refs *[VP9RTPMaxReferenceIndices]uint8,
	count *int,
	pictureID uint16,
	refPictureID uint16,
) bool {
	if refs == nil || count == nil ||
		*count < 0 || *count > VP9RTPMaxReferenceIndices {
		return false
	}
	diff := (pictureID - refPictureID) & VP9RTPPictureID15BitMask
	if diff == 0 || diff > 0x7f {
		return false
	}
	refDiff := uint8(diff)
	for i := 0; i < *count; i++ {
		if refs[i] == refDiff {
			return true
		}
	}
	if *count == VP9RTPMaxReferenceIndices {
		return false
	}
	refs[*count] = refDiff
	*count = *count + 1
	return true
}

func vp9WebRTCReferenceSlotsForFrame(
	frame []byte,
) ([VP9RTPMaxReferenceIndices]uint8, int, error) {
	var slots [VP9RTPMaxReferenceIndices]uint8
	var r vp9dec.BitReader
	r.Init(frame)
	if err := vp9dec.ReadFrameMarker(&r); err != nil {
		return slots, 0, ErrInvalidVP9Data
	}
	profile := vp9dec.ReadProfile(&r)
	if profile >= common.MaxProfiles {
		return slots, 0, ErrInvalidVP9Data
	}
	if r.ReadBit() != 0 {
		slots[0] = uint8(r.ReadLiteral(3))
		return slots, 1, nil
	}
	frameType := common.FrameType(r.ReadBit())
	showFrame := r.ReadBit() != 0
	errorResilient := r.ReadBit() != 0
	if frameType == common.KeyFrame {
		return slots, 0, nil
	}
	intraOnly := false
	if !showFrame {
		intraOnly = r.ReadBit() != 0
	}
	if !errorResilient {
		_ = r.ReadLiteral(2)
	}
	if intraOnly {
		return slots, 0, nil
	}
	_ = r.ReadLiteral(common.RefFrames)
	interRefs := vp9dec.ReadInterRefBlock(&r)
	return interRefs.RefIndex, len(interRefs.RefIndex), nil
}

func (p *VP9WebRTCPacketizer) commitVP9WebRTCReferences(
	r VP9EncodeResult,
	pictureID uint16,
) {
	p.references.commitResult(r, pictureID)
}

func (p *VP9WebRTCPacketizer) commitVP9SpatialSVCWebRTCReferences(
	r VP9SpatialSVCEncodeResult,
	pictureID uint16,
) {
	count, err := r.vp9WebRTCLayerCount()
	if err != nil {
		return
	}
	for i := 0; i < count; i++ {
		p.references.commitResult(r.Layers[i], pictureID)
	}
}

func (t *vp9WebRTCReferenceTracker) commitResult(
	r VP9EncodeResult,
	pictureID uint16,
) {
	t.lastPictureID = pictureID & VP9RTPPictureID15BitMask
	t.haveLast = true
	refreshFlags := r.RefreshFrameFlags
	if r.KeyFrame {
		refreshFlags = 0xff
	}
	for slot := range t.valid {
		if refreshFlags&(1<<uint(slot)) == 0 {
			continue
		}
		t.valid[slot] = true
		t.pictureID[slot] = t.lastPictureID
	}
}
