package govpx

const (
	vp8RTPFlagExtendedControl = 0x80
	vp8RTPFlagNonReference    = 0x20
	vp8RTPFlagStartPartition  = 0x10

	vp8RTPFlagPictureID = 0x80
	vp8RTPFlagTL0PICIDX = 0x40
	vp8RTPFlagTemporal  = 0x20
	vp8RTPFlagKeyIndex  = 0x10
)

// VP8RTPPayloadDescriptor describes the VP8 RTP payload descriptor from RFC
// 7741. It is the bytes after the RTP header and before the raw VP8 payload.
type VP8RTPPayloadDescriptor struct {
	NonReferenceFrame bool
	StartOfPartition  bool
	PartitionID       uint8

	PictureIDPresent bool
	PictureID        uint16
	PictureID15Bit   bool

	TL0PICIDXPresent bool
	TL0PICIDX        uint8

	TemporalIDPresent bool
	TemporalID        uint8
	LayerSync         bool

	KeyIndexPresent bool
	KeyIndex        uint8
}

// Size returns the number of bytes needed to marshal d, excluding the raw VP8
// payload bytes.
func (d VP8RTPPayloadDescriptor) Size() (int, error) {
	if err := d.validate(); err != nil {
		return 0, err
	}
	size := 1
	if d.hasExtensions() {
		size++
		if d.PictureIDPresent {
			if d.PictureID15Bit {
				size += 2
			} else {
				size++
			}
		}
		if d.TL0PICIDXPresent {
			size++
		}
		if d.TemporalIDPresent || d.KeyIndexPresent {
			size++
		}
	}
	return size, nil
}

// MarshalInto writes d into dst and returns the descriptor length. If dst is
// too small, it returns the required descriptor length and [ErrBufferTooSmall].
func (d VP8RTPPayloadDescriptor) MarshalInto(dst []byte) (int, error) {
	need, err := d.Size()
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, ErrBufferTooSmall
	}

	var first byte
	if d.hasExtensions() {
		first |= vp8RTPFlagExtendedControl
	}
	if d.NonReferenceFrame {
		first |= vp8RTPFlagNonReference
	}
	if d.StartOfPartition {
		first |= vp8RTPFlagStartPartition
	}
	first |= d.PartitionID
	dst[0] = first

	off := 1
	if d.hasExtensions() {
		var ext byte
		if d.PictureIDPresent {
			ext |= vp8RTPFlagPictureID
		}
		if d.TL0PICIDXPresent {
			ext |= vp8RTPFlagTL0PICIDX
		}
		if d.TemporalIDPresent {
			ext |= vp8RTPFlagTemporal
		}
		if d.KeyIndexPresent {
			ext |= vp8RTPFlagKeyIndex
		}
		dst[off] = ext
		off++

		if d.PictureIDPresent {
			if d.PictureID15Bit {
				dst[off] = 0x80 | byte(d.PictureID>>8)
				dst[off+1] = byte(d.PictureID)
				off += 2
			} else {
				dst[off] = byte(d.PictureID)
				off++
			}
		}
		if d.TL0PICIDXPresent {
			dst[off] = d.TL0PICIDX
			off++
		}
		if d.TemporalIDPresent || d.KeyIndexPresent {
			var tk byte
			if d.TemporalIDPresent {
				tk |= d.TemporalID << 6
			}
			if d.LayerSync {
				tk |= 0x20
			}
			if d.KeyIndexPresent {
				tk |= d.KeyIndex
			}
			dst[off] = tk
			off++
		}
	}
	return need, nil
}

// Marshal returns d as a newly allocated VP8 RTP payload descriptor.
func (d VP8RTPPayloadDescriptor) Marshal() ([]byte, error) {
	need, err := d.Size()
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = d.MarshalInto(out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParseVP8RTPPayloadDescriptor parses the VP8 RTP payload descriptor at the
// front of packet and returns the descriptor plus the remaining raw VP8
// payload bytes.
func ParseVP8RTPPayloadDescriptor(packet []byte) (VP8RTPPayloadDescriptor, []byte, error) {
	if len(packet) == 0 {
		return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
	}
	first := packet[0]
	d := VP8RTPPayloadDescriptor{
		NonReferenceFrame: first&vp8RTPFlagNonReference != 0,
		StartOfPartition:  first&vp8RTPFlagStartPartition != 0,
		PartitionID:       first & 0x07,
	}
	off := 1
	if first&vp8RTPFlagExtendedControl == 0 {
		return d, packet[off:], nil
	}
	if off >= len(packet) {
		return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
	}
	ext := packet[off]
	off++
	d.PictureIDPresent = ext&vp8RTPFlagPictureID != 0
	d.TL0PICIDXPresent = ext&vp8RTPFlagTL0PICIDX != 0
	d.TemporalIDPresent = ext&vp8RTPFlagTemporal != 0
	d.KeyIndexPresent = ext&vp8RTPFlagKeyIndex != 0
	if d.TL0PICIDXPresent && !d.TemporalIDPresent {
		return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
	}
	if d.PictureIDPresent {
		if off >= len(packet) {
			return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
		}
		pid := packet[off]
		off++
		if pid&0x80 != 0 {
			if off >= len(packet) {
				return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
			}
			d.PictureID15Bit = true
			d.PictureID = uint16(pid&0x7f)<<8 | uint16(packet[off])
			off++
		} else {
			d.PictureID = uint16(pid)
		}
	}
	if d.TL0PICIDXPresent {
		if off >= len(packet) {
			return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
		}
		d.TL0PICIDX = packet[off]
		off++
	}
	if d.TemporalIDPresent || d.KeyIndexPresent {
		if off >= len(packet) {
			return VP8RTPPayloadDescriptor{}, nil, ErrInvalidData
		}
		tk := packet[off]
		off++
		if d.TemporalIDPresent {
			d.TemporalID = tk >> 6
		}
		d.LayerSync = tk&0x20 != 0
		if d.KeyIndexPresent {
			d.KeyIndex = tk & 0x1f
		}
	}
	return d, packet[off:], nil
}

// VP8RTPPayloadSize returns the number of bytes needed to pack desc and the
// raw VP8 payload into one RTP payload body.
func VP8RTPPayloadSize(desc VP8RTPPayloadDescriptor, payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, ErrInvalidConfig
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, err
	}
	maxInt := int(^uint(0) >> 1)
	if len(payload) > maxInt-descSize {
		return 0, ErrInvalidConfig
	}
	return descSize + len(payload), nil
}

// PackVP8RTPPayloadInto writes desc followed by payload into dst and returns
// the RTP payload length. It does not write an RTP header.
func PackVP8RTPPayloadInto(dst []byte, desc VP8RTPPayloadDescriptor, payload []byte) (int, error) {
	need, err := VP8RTPPayloadSize(desc, payload)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, ErrBufferTooSmall
	}
	n, err := desc.MarshalInto(dst)
	if err != nil {
		return 0, err
	}
	copy(dst[n:], payload)
	return need, nil
}

// PackVP8RTPPayload returns desc followed by payload as one RTP payload body.
// It does not include an RTP header.
func PackVP8RTPPayload(desc VP8RTPPayloadDescriptor, payload []byte) ([]byte, error) {
	need, err := VP8RTPPayloadSize(desc, payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = PackVP8RTPPayloadInto(out, desc, payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VP8RTPFramePacketizationSize returns the number of RTP payload bodies and
// total payload-body bytes needed to packetize one raw VP8 frame at mtu bytes.
//
// mtu includes the VP8 RTP payload descriptor but excludes the RTP header.
// The packetizer manages StartOfPartition and always emits partition 0.
func VP8RTPFramePacketizationSize(desc VP8RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	if desc.PartitionID != 0 {
		return 0, 0, ErrInvalidConfig
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, 0, err
	}
	return rtpFramePacketizationSize(len(frame), descSize, mtu)
}

// PacketizeVP8RTPFrameInto packetizes one raw VP8 frame into caller-owned
// RTP payload storage. dst receives packet metadata; payloadBuf receives the
// payload bodies. On [ErrBufferTooSmall], the returned packet and byte counts
// are the required capacities.
//
// The returned payload bodies do not include RTP headers. Marker is true only
// on the last payload body.
func PacketizeVP8RTPFrameInto(dst []RTPPayloadFragment, payloadBuf []byte,
	desc VP8RTPPayloadDescriptor, frame []byte, mtu int,
) (int, int, error) {
	packets, totalBytes, err := VP8RTPFramePacketizationSize(desc, frame, mtu)
	if err != nil {
		return 0, 0, err
	}
	if len(dst) < packets || len(payloadBuf) < totalBytes {
		return packets, totalBytes, ErrBufferTooSmall
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, 0, err
	}
	maxPayload := mtu - descSize
	frameOff := 0
	bufOff := 0
	for i := range packets {
		chunk := min(maxPayload, len(frame)-frameOff)
		packetDesc := desc
		packetDesc.StartOfPartition = i == 0
		packetDesc.PartitionID = 0

		payload := frame[frameOff : frameOff+chunk]
		n, err := PackVP8RTPPayloadInto(payloadBuf[bufOff:bufOff+descSize+chunk],
			packetDesc, payload)
		if err != nil {
			return 0, 0, err
		}
		dst[i] = RTPPayloadFragment{
			Payload: payloadBuf[bufOff : bufOff+n],
			Marker:  i == packets-1,
		}
		frameOff += chunk
		bufOff += n
	}
	return packets, totalBytes, nil
}

// PacketizeVP8RTPFrame returns RTP payload bodies for one raw VP8 frame.
// Payloads do not include RTP headers; Marker is true only on the last body.
func PacketizeVP8RTPFrame(desc VP8RTPPayloadDescriptor, frame []byte, mtu int) ([]RTPPayloadFragment, error) {
	packets, totalBytes, err := VP8RTPFramePacketizationSize(desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	out := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, totalBytes)
	n, _, err := PacketizeVP8RTPFrameInto(out, payloadBuf, desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// VP8RTPFrameAssemblySize validates an ordered set of VP8 RTP payload bodies
// for one frame and returns the raw VP8 frame size.
//
// The caller owns RTP sequence-number validation, loss handling, and jitter
// buffering. Payloads must be in decode order and must include the marker bit
// value from each RTP header.
func VP8RTPFrameAssemblySize(payloads []RTPPayloadFragment) (int, error) {
	if len(payloads) == 0 {
		return 0, ErrInvalidData
	}
	total := 0
	var base VP8RTPPayloadDescriptor
	for i := range payloads {
		desc, fragment, err := ParseVP8RTPPayloadDescriptor(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		if len(fragment) == 0 {
			return 0, ErrInvalidData
		}
		if payloads[i].Marker != (i == len(payloads)-1) {
			return 0, ErrInvalidData
		}
		if desc.StartOfPartition != (i == 0) || desc.PartitionID != 0 {
			return 0, ErrInvalidData
		}
		normalized := desc
		normalized.StartOfPartition = false
		if i == 0 {
			base = normalized
		} else if normalized != base {
			return 0, ErrInvalidData
		}
		total, err = rtpAddPayloadSize(total, len(fragment))
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// AssembleVP8RTPFrameInto writes the raw VP8 frame carried by payloads into
// dst and returns the frame length. On [ErrBufferTooSmall], the returned
// length is the required capacity.
func AssembleVP8RTPFrameInto(dst []byte, payloads []RTPPayloadFragment) (int, error) {
	need, err := VP8RTPFrameAssemblySize(payloads)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, ErrBufferTooSmall
	}
	return assembleVP8RTPFrameIntoKnownSize(dst, payloads, need)
}

func assembleVP8RTPFrameIntoKnownSize(dst []byte, payloads []RTPPayloadFragment, size int) (int, error) {
	off := 0
	for i := range payloads {
		_, fragment, err := ParseVP8RTPPayloadDescriptor(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		copy(dst[off:], fragment)
		off += len(fragment)
	}
	return size, nil
}

// AssembleVP8RTPFrame returns the raw VP8 frame carried by an ordered set of
// RTP payload bodies.
func AssembleVP8RTPFrame(payloads []RTPPayloadFragment) ([]byte, error) {
	need, err := VP8RTPFrameAssemblySize(payloads)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = assembleVP8RTPFrameIntoKnownSize(out, payloads, need)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (d VP8RTPPayloadDescriptor) hasExtensions() bool {
	return d.PictureIDPresent || d.TL0PICIDXPresent ||
		d.TemporalIDPresent || d.KeyIndexPresent
}

func (d VP8RTPPayloadDescriptor) validate() error {
	if d.PartitionID > 7 {
		return ErrInvalidConfig
	}
	if d.PictureID15Bit && !d.PictureIDPresent {
		return ErrInvalidConfig
	}
	if d.PictureIDPresent {
		if d.PictureID15Bit {
			if d.PictureID > 0x7fff {
				return ErrInvalidConfig
			}
		} else if d.PictureID > 0x7f {
			return ErrInvalidConfig
		}
	} else if d.PictureID != 0 {
		return ErrInvalidConfig
	}
	if d.TL0PICIDXPresent && !d.TemporalIDPresent {
		return ErrInvalidConfig
	}
	if !d.TL0PICIDXPresent && d.TL0PICIDX != 0 {
		return ErrInvalidConfig
	}
	if d.TemporalIDPresent {
		if d.TemporalID > 3 {
			return ErrInvalidConfig
		}
	} else if d.TemporalID != 0 {
		return ErrInvalidConfig
	}
	if d.LayerSync && !d.TemporalIDPresent && !d.KeyIndexPresent {
		return ErrInvalidConfig
	}
	if d.KeyIndexPresent {
		if d.KeyIndex > 31 {
			return ErrInvalidConfig
		}
	} else if d.KeyIndex != 0 {
		return ErrInvalidConfig
	}
	return nil
}
