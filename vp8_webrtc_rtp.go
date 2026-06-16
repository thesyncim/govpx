package govpx

const (
	// VP8RTPPictureID7BitMask is the largest VP8 RTP 7-bit PictureID value.
	VP8RTPPictureID7BitMask uint8 = 0x7f
	// VP8RTPPictureID15BitMask is the largest VP8 RTP 15-bit PictureID value.
	VP8RTPPictureID15BitMask uint16 = 0x7fff
	// VP8RTPKeyIndexMask is the largest VP8 RTP KEYIDX value.
	VP8RTPKeyIndexMask uint8 = 0x1f
)

// NextVP8RTPPictureID advances a VP8 RTP 15-bit PictureID with wraparound.
func NextVP8RTPPictureID(id uint16) uint16 {
	return NextVP8RTPPictureID15Bit(id)
}

// NextVP8RTPPictureID7Bit advances a VP8 RTP 7-bit PictureID with wraparound.
func NextVP8RTPPictureID7Bit(id uint8) uint8 {
	return (id + 1) & VP8RTPPictureID7BitMask
}

// NextVP8RTPPictureID15Bit advances a VP8 RTP 15-bit PictureID with wraparound.
func NextVP8RTPPictureID15Bit(id uint16) uint16 {
	return (id + 1) & VP8RTPPictureID15BitMask
}

// NextVP8RTPTL0PICIDX advances a VP8 RTP TL0PICIDX counter with 8-bit
// wraparound.
func NextVP8RTPTL0PICIDX(id uint8) uint8 {
	return id + 1
}

// NextVP8RTPKeyIndex advances a VP8 RTP KEYIDX counter with wraparound.
func NextVP8RTPKeyIndex(index uint8) uint8 {
	return (index + 1) & VP8RTPKeyIndexMask
}

// RTPPayloadDescriptor returns a VP8 RTP descriptor populated from the encoder
// result metadata. PictureID and KeyIndex are left unset so callers can apply
// their own RTP sequence policy. Use [EncodeResult.WebRTCRTPPayloadDescriptor]
// for the common WebRTC 15-bit PictureID shape.
func (r EncodeResult) RTPPayloadDescriptor() VP8RTPPayloadDescriptor {
	desc := VP8RTPPayloadDescriptor{
		NonReferenceFrame: r.Droppable,
	}
	if r.TemporalLayerCount > 1 {
		desc.TL0PICIDXPresent = true
		desc.TL0PICIDX = r.TL0PICIDX
		desc.TemporalIDPresent = true
		desc.TemporalID = uint8(r.TemporalLayerID)
		desc.LayerSync = r.TemporalLayerSync
	}
	return desc
}

// WebRTCRTPPayloadDescriptor returns a WebRTC-friendly VP8 RTP descriptor for
// r. It always carries a 15-bit PictureID and, when temporal layering is
// active, TL0PICIDX plus temporal-layer id/sync metadata. KeyIndex is left
// unset because WebRTC VP8 senders normally drive key-frame requests through
// RTCP PLI/FIR rather than the optional VP8 RTP key index.
func (r EncodeResult) WebRTCRTPPayloadDescriptor(pictureID uint16) VP8RTPPayloadDescriptor {
	desc := r.RTPPayloadDescriptor()
	desc.PictureIDPresent = true
	desc.PictureID15Bit = true
	desc.PictureID = pictureID & VP8RTPPictureID15BitMask
	return desc
}

// WebRTCRTPPacketizationSize returns the RTP payload count and payload-body
// bytes needed to packetize r using WebRTC-friendly VP8 descriptors.
func (r EncodeResult) WebRTCRTPPacketizationSize(
	pictureID uint16,
	mtu int,
) (int, int, error) {
	desc, frame, err := r.vp8WebRTCRTPDescriptorAndFrame(pictureID)
	if err != nil {
		return 0, 0, err
	}
	return VP8RTPFramePacketizationSize(desc, frame, mtu)
}

// PacketizeWebRTCRTPInto packetizes r into caller-owned RTP payload storage
// using WebRTC-friendly VP8 descriptors. Payload bodies do not include RTP
// headers; Marker is true only on the final packet of the frame.
func (r EncodeResult) PacketizeWebRTCRTPInto(
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	pictureID uint16,
	mtu int,
) (int, int, error) {
	desc, frame, err := r.vp8WebRTCRTPDescriptorAndFrame(pictureID)
	if err != nil {
		return 0, 0, err
	}
	return PacketizeVP8RTPFrameInto(dst, payloadBuf, desc, frame, mtu)
}

// PacketizeWebRTCRTP packetizes r into allocated RTP payload bodies using
// WebRTC-friendly VP8 descriptors. Payloads do not include RTP headers.
func (r EncodeResult) PacketizeWebRTCRTP(
	pictureID uint16,
	mtu int,
) ([]RTPPayloadFragment, error) {
	packets, payloadBytes, err := r.WebRTCRTPPacketizationSize(pictureID, mtu)
	if err != nil {
		return nil, err
	}
	out := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, _, err := r.PacketizeWebRTCRTPInto(out, payloadBuf, pictureID, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func (r EncodeResult) vp8WebRTCRTPDescriptorAndFrame(
	pictureID uint16,
) (VP8RTPPayloadDescriptor, []byte, error) {
	if r.Dropped || len(r.Data) == 0 {
		return VP8RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	if r.TemporalLayerCount < 0 ||
		r.TemporalLayerID < 0 ||
		r.TemporalLayerID >= r.TemporalLayerCount {
		return VP8RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	// RFC 7741 packs TID in two bits. Govpx can model libvpx's five-layer
	// VP8 schedules, but the WebRTC RTP descriptor can only signal 0..3.
	if r.TemporalLayerCount > 1 && r.TemporalLayerID > 3 {
		return VP8RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	return r.WebRTCRTPPayloadDescriptor(pictureID), r.Data, nil
}
