package govpx

// RTPPayloadFragment is one RTP payload body plus the RTP marker-bit value
// the caller should put in the RTP header for that body.
//
// Payload contains codec-specific payload-descriptor bytes followed by the
// codec payload fragment. It does not include an RTP header.
type RTPPayloadFragment struct {
	Payload []byte
	Marker  bool
}

func rtpFramePacketizationSize(frameLen, descriptorSize, mtu int) (int, int, error) {
	if frameLen == 0 || descriptorSize <= 0 || mtu <= descriptorSize {
		return 0, 0, ErrInvalidConfig
	}
	payloadBytesPerPacket := mtu - descriptorSize
	packets := (frameLen + payloadBytesPerPacket - 1) / payloadBytesPerPacket

	maxInt := int(^uint(0) >> 1)
	if packets > (maxInt-frameLen)/descriptorSize {
		return 0, 0, ErrInvalidConfig
	}
	return packets, frameLen + packets*descriptorSize, nil
}
