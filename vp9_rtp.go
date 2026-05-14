package govpx

const (
	vp9RTPFlagPictureID             = 0x80
	vp9RTPFlagInterPicturePredicted = 0x40
	vp9RTPFlagLayerIndices          = 0x20
	vp9RTPFlagFlexibleMode          = 0x10
	vp9RTPFlagStartOfFrame          = 0x08
	vp9RTPFlagEndOfFrame            = 0x04
	vp9RTPFlagScalabilityStructure  = 0x02
	vp9RTPFlagNotRefUpperSpatial    = 0x01
)

const (
	// VP9RTPMaxReferenceIndices is the maximum number of reference-index
	// entries in a VP9 RTP payload descriptor or scalability-structure entry.
	VP9RTPMaxReferenceIndices = 3
	// VP9RTPMaxSpatialLayers is the maximum number of spatial layers described
	// by a VP9 RTP scalability structure.
	VP9RTPMaxSpatialLayers = 8
)

// VP9RTPPayloadDescriptor describes the VP9 RTP payload descriptor from RFC
// 9628. It is the bytes after the RTP header and before the raw VP9 payload.
type VP9RTPPayloadDescriptor struct {
	PictureIDPresent bool
	PictureID        uint16
	PictureID15Bit   bool

	InterPicturePredicted       bool
	LayerIndicesPresent         bool
	FlexibleMode                bool
	StartOfFrame                bool
	EndOfFrame                  bool
	ScalabilityStructurePresent bool
	NotRefForUpperSpatialLayer  bool

	TemporalID           uint8
	SwitchingUpPoint     bool
	SpatialID            uint8
	InterLayerDependency bool
	TL0PICIDX            uint8
	ReferenceIndexCount  int
	ReferenceIndices     [VP9RTPMaxReferenceIndices]uint8
	ScalabilityStructure VP9RTPScalabilityStructure
}

// VP9RTPScalabilityStructure describes the optional VP9 RTP scalability
// structure. SpatialLayerCount defaults to one when marshaling.
type VP9RTPScalabilityStructure struct {
	SpatialLayerCount int
	ResolutionPresent bool
	Width             [VP9RTPMaxSpatialLayers]uint16
	Height            [VP9RTPMaxSpatialLayers]uint16

	PictureGroupPresent bool
	PictureGroups       []VP9RTPPictureGroup
}

// VP9RTPPictureGroup describes one picture-group entry in a VP9 RTP
// scalability structure.
type VP9RTPPictureGroup struct {
	TemporalID          uint8
	SwitchingUpPoint    bool
	ReferenceIndexCount int
	ReferenceIndices    [VP9RTPMaxReferenceIndices]uint8
}

// Size returns the number of bytes needed to marshal d, excluding the raw VP9
// payload bytes.
func (d VP9RTPPayloadDescriptor) Size() (int, error) {
	if err := d.validate(); err != nil {
		return 0, err
	}
	size := 1
	if d.PictureIDPresent {
		if d.PictureID15Bit {
			size += 2
		} else {
			size++
		}
	}
	if d.LayerIndicesPresent {
		size++
		if !d.FlexibleMode {
			size++
		}
	}
	if d.InterPicturePredicted && d.FlexibleMode {
		size += d.ReferenceIndexCount
	}
	if d.ScalabilityStructurePresent {
		ssSize, err := d.ScalabilityStructure.size()
		if err != nil {
			return 0, err
		}
		size += ssSize
	}
	return size, nil
}

// MarshalInto writes d into dst and returns the descriptor length. If dst is
// too small, it returns the required descriptor length and [ErrBufferTooSmall].
func (d VP9RTPPayloadDescriptor) MarshalInto(dst []byte) (int, error) {
	need, err := d.Size()
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, ErrBufferTooSmall
	}

	var first byte
	if d.PictureIDPresent {
		first |= vp9RTPFlagPictureID
	}
	if d.InterPicturePredicted {
		first |= vp9RTPFlagInterPicturePredicted
	}
	if d.LayerIndicesPresent {
		first |= vp9RTPFlagLayerIndices
	}
	if d.FlexibleMode {
		first |= vp9RTPFlagFlexibleMode
	}
	if d.StartOfFrame {
		first |= vp9RTPFlagStartOfFrame
	}
	if d.EndOfFrame {
		first |= vp9RTPFlagEndOfFrame
	}
	if d.ScalabilityStructurePresent {
		first |= vp9RTPFlagScalabilityStructure
	}
	if d.NotRefForUpperSpatialLayer {
		first |= vp9RTPFlagNotRefUpperSpatial
	}

	dst[0] = first
	off := 1
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
	if d.LayerIndicesPresent {
		layer := d.TemporalID<<5 | d.SpatialID<<1
		if d.SwitchingUpPoint {
			layer |= 0x10
		}
		if d.InterLayerDependency {
			layer |= 0x01
		}
		dst[off] = layer
		off++
		if !d.FlexibleMode {
			dst[off] = d.TL0PICIDX
			off++
		}
	}
	if d.InterPicturePredicted && d.FlexibleMode {
		for i := 0; i < d.ReferenceIndexCount; i++ {
			ref := d.ReferenceIndices[i] << 1
			if i+1 < d.ReferenceIndexCount {
				ref |= 0x01
			}
			dst[off] = ref
			off++
		}
	}
	if d.ScalabilityStructurePresent {
		off += d.ScalabilityStructure.marshalInto(dst[off:])
	}
	return need, nil
}

// Marshal returns d as a newly allocated VP9 RTP payload descriptor.
func (d VP9RTPPayloadDescriptor) Marshal() ([]byte, error) {
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

// ParseVP9RTPPayloadDescriptor parses the VP9 RTP payload descriptor at the
// front of packet and returns the descriptor plus the remaining raw VP9
// payload bytes.
func ParseVP9RTPPayloadDescriptor(packet []byte) (VP9RTPPayloadDescriptor, []byte, error) {
	if len(packet) == 0 {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
	}
	first := packet[0]
	d := VP9RTPPayloadDescriptor{
		PictureIDPresent:            first&vp9RTPFlagPictureID != 0,
		InterPicturePredicted:       first&vp9RTPFlagInterPicturePredicted != 0,
		LayerIndicesPresent:         first&vp9RTPFlagLayerIndices != 0,
		FlexibleMode:                first&vp9RTPFlagFlexibleMode != 0,
		StartOfFrame:                first&vp9RTPFlagStartOfFrame != 0,
		EndOfFrame:                  first&vp9RTPFlagEndOfFrame != 0,
		ScalabilityStructurePresent: first&vp9RTPFlagScalabilityStructure != 0,
		NotRefForUpperSpatialLayer:  first&vp9RTPFlagNotRefUpperSpatial != 0,
	}
	if d.FlexibleMode && !d.PictureIDPresent {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
	}

	off := 1
	if d.PictureIDPresent {
		if off >= len(packet) {
			return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
		}
		pid := packet[off]
		off++
		if pid&0x80 != 0 {
			if off >= len(packet) {
				return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
			}
			d.PictureID15Bit = true
			d.PictureID = uint16(pid&0x7f)<<8 | uint16(packet[off])
			off++
		} else {
			d.PictureID = uint16(pid)
		}
	}
	if d.LayerIndicesPresent {
		if off >= len(packet) {
			return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
		}
		layer := packet[off]
		off++
		d.TemporalID = layer >> 5
		d.SwitchingUpPoint = layer&0x10 != 0
		d.SpatialID = (layer >> 1) & 0x07
		d.InterLayerDependency = layer&0x01 != 0
		if !d.InterPicturePredicted && d.TemporalID != 0 {
			return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
		}
		if d.SpatialID == 0 && d.InterLayerDependency {
			return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
		}
		if !d.FlexibleMode {
			if off >= len(packet) {
				return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
			}
			d.TL0PICIDX = packet[off]
			off++
		}
	}
	if d.InterPicturePredicted && d.FlexibleMode {
		for i := 0; ; i++ {
			if i == VP9RTPMaxReferenceIndices || off >= len(packet) {
				return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
			}
			ref := packet[off]
			off++
			pdiff := ref >> 1
			if pdiff == 0 {
				return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
			}
			d.ReferenceIndices[i] = pdiff
			d.ReferenceIndexCount = i + 1
			if ref&0x01 == 0 {
				break
			}
		}
	}
	if d.ScalabilityStructurePresent {
		ss, n, err := parseVP9RTPScalabilityStructure(packet[off:])
		if err != nil {
			return VP9RTPPayloadDescriptor{}, nil, err
		}
		d.ScalabilityStructure = ss
		off += n
	}
	return d, packet[off:], nil
}

// VP9RTPPayloadSize returns the number of bytes needed to pack desc and the
// raw VP9 payload into one RTP payload body.
func VP9RTPPayloadSize(desc VP9RTPPayloadDescriptor, payload []byte) (int, error) {
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

// PackVP9RTPPayloadInto writes desc followed by payload into dst and returns
// the RTP payload length. It does not write an RTP header.
func PackVP9RTPPayloadInto(dst []byte, desc VP9RTPPayloadDescriptor, payload []byte) (int, error) {
	need, err := VP9RTPPayloadSize(desc, payload)
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

// PackVP9RTPPayload returns desc followed by payload as one RTP payload body.
// It does not include an RTP header.
func PackVP9RTPPayload(desc VP9RTPPayloadDescriptor, payload []byte) ([]byte, error) {
	need, err := VP9RTPPayloadSize(desc, payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = PackVP9RTPPayloadInto(out, desc, payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VP9RTPFramePacketizationSize returns the number of RTP payload bodies and
// total payload-body bytes needed to packetize one raw VP9 frame at mtu bytes.
//
// mtu includes the VP9 RTP payload descriptor but excludes the RTP header.
// This helper packetizes one VP9 frame per call. Layer indices, flexible-mode
// references, and scalability structures are carried from desc into each
// emitted payload descriptor.
func VP9RTPFramePacketizationSize(desc VP9RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	if err := validateVP9RTPPacketizerDescriptor(desc); err != nil {
		return 0, 0, err
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, 0, err
	}
	return rtpFramePacketizationSize(len(frame), descSize, mtu)
}

// PacketizeVP9RTPFrameInto packetizes one raw VP9 frame into caller-owned
// RTP payload storage. dst receives packet metadata; payloadBuf receives the
// payload bodies. On [ErrBufferTooSmall], the returned packet and byte counts
// are the required capacities.
//
// The returned payload bodies do not include RTP headers. Marker is true only
// on the last payload body.
func PacketizeVP9RTPFrameInto(dst []RTPPayloadFragment, payloadBuf []byte,
	desc VP9RTPPayloadDescriptor, frame []byte, mtu int,
) (int, int, error) {
	packets, totalBytes, err := VP9RTPFramePacketizationSize(desc, frame, mtu)
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
	for i := 0; i < packets; i++ {
		chunk := min(maxPayload, len(frame)-frameOff)
		packetDesc := desc
		packetDesc.StartOfFrame = i == 0
		packetDesc.EndOfFrame = i == packets-1

		payload := frame[frameOff : frameOff+chunk]
		n, err := PackVP9RTPPayloadInto(payloadBuf[bufOff:bufOff+descSize+chunk],
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

// PacketizeVP9RTPFrame returns RTP payload bodies for one raw VP9 frame.
// Payloads do not include RTP headers; Marker is true only on the last body.
func PacketizeVP9RTPFrame(desc VP9RTPPayloadDescriptor, frame []byte, mtu int) ([]RTPPayloadFragment, error) {
	packets, totalBytes, err := VP9RTPFramePacketizationSize(desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	out := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, totalBytes)
	n, _, err := PacketizeVP9RTPFrameInto(out, payloadBuf, desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// VP9RTPFrameAssemblySize validates an ordered set of VP9 RTP payload bodies
// for one frame and returns the raw VP9 frame size.
//
// The caller owns RTP sequence-number validation, loss handling, and jitter
// buffering. Payloads must be in decode order and must include the marker bit
// value from each RTP header.
func VP9RTPFrameAssemblySize(payloads []RTPPayloadFragment) (int, error) {
	if len(payloads) == 0 {
		return 0, ErrInvalidVP9Data
	}
	total := 0
	var base VP9RTPPayloadDescriptor
	for i := range payloads {
		desc, fragment, err := ParseVP9RTPPayloadDescriptor(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		if len(fragment) == 0 {
			return 0, ErrInvalidVP9Data
		}
		if payloads[i].Marker != (i == len(payloads)-1) {
			return 0, ErrInvalidVP9Data
		}
		if desc.StartOfFrame != (i == 0) || desc.EndOfFrame != (i == len(payloads)-1) {
			return 0, ErrInvalidVP9Data
		}
		if err := validateVP9RTPPacketizerDescriptor(desc); err != nil {
			return 0, ErrInvalidVP9Data
		}
		if i == 0 {
			base = desc
			base.StartOfFrame = false
			base.EndOfFrame = false
		} else if !sameVP9RTPFrameDescriptor(base, desc) {
			return 0, ErrInvalidVP9Data
		}
		total, err = rtpAddPayloadSize(total, len(fragment))
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// AssembleVP9RTPFrameInto writes the raw VP9 frame carried by payloads into
// dst and returns the frame length. On [ErrBufferTooSmall], the returned
// length is the required capacity.
func AssembleVP9RTPFrameInto(dst []byte, payloads []RTPPayloadFragment) (int, error) {
	need, err := VP9RTPFrameAssemblySize(payloads)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, ErrBufferTooSmall
	}
	return assembleVP9RTPFrameIntoKnownSize(dst, payloads, need)
}

func assembleVP9RTPFrameIntoKnownSize(dst []byte, payloads []RTPPayloadFragment, size int) (int, error) {
	off := 0
	for i := range payloads {
		_, fragment, err := ParseVP9RTPPayloadDescriptor(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		copy(dst[off:], fragment)
		off += len(fragment)
	}
	return size, nil
}

// AssembleVP9RTPFrame returns the raw VP9 frame carried by an ordered set of
// RTP payload bodies.
func AssembleVP9RTPFrame(payloads []RTPPayloadFragment) ([]byte, error) {
	need, err := VP9RTPFrameAssemblySize(payloads)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = assembleVP9RTPFrameIntoKnownSize(out, payloads, need)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validateVP9RTPPacketizerDescriptor(desc VP9RTPPayloadDescriptor) error {
	return desc.validate()
}

func sameVP9RTPFrameDescriptor(base, desc VP9RTPPayloadDescriptor) bool {
	desc.StartOfFrame = false
	desc.EndOfFrame = false
	return base.PictureIDPresent == desc.PictureIDPresent &&
		base.PictureID == desc.PictureID &&
		base.PictureID15Bit == desc.PictureID15Bit &&
		base.InterPicturePredicted == desc.InterPicturePredicted &&
		base.LayerIndicesPresent == desc.LayerIndicesPresent &&
		base.FlexibleMode == desc.FlexibleMode &&
		base.ScalabilityStructurePresent == desc.ScalabilityStructurePresent &&
		base.NotRefForUpperSpatialLayer == desc.NotRefForUpperSpatialLayer &&
		base.TemporalID == desc.TemporalID &&
		base.SwitchingUpPoint == desc.SwitchingUpPoint &&
		base.SpatialID == desc.SpatialID &&
		base.InterLayerDependency == desc.InterLayerDependency &&
		base.TL0PICIDX == desc.TL0PICIDX &&
		base.ReferenceIndexCount == desc.ReferenceIndexCount &&
		base.ReferenceIndices == desc.ReferenceIndices &&
		sameVP9RTPScalabilityStructure(base.ScalabilityStructure, desc.ScalabilityStructure)
}

func sameVP9RTPScalabilityStructure(a, b VP9RTPScalabilityStructure) bool {
	if a.SpatialLayerCount != b.SpatialLayerCount ||
		a.ResolutionPresent != b.ResolutionPresent ||
		a.Width != b.Width ||
		a.Height != b.Height ||
		a.PictureGroupPresent != b.PictureGroupPresent ||
		len(a.PictureGroups) != len(b.PictureGroups) {
		return false
	}
	for i := range a.PictureGroups {
		if a.PictureGroups[i] != b.PictureGroups[i] {
			return false
		}
	}
	return true
}

func (d VP9RTPPayloadDescriptor) validate() error {
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
	if d.FlexibleMode && !d.PictureIDPresent {
		return ErrInvalidConfig
	}
	if d.LayerIndicesPresent {
		if d.TemporalID > 7 || d.SpatialID > 7 {
			return ErrInvalidConfig
		}
		if !d.InterPicturePredicted && d.TemporalID != 0 {
			return ErrInvalidConfig
		}
		if d.SpatialID == 0 && d.InterLayerDependency {
			return ErrInvalidConfig
		}
	} else if d.TemporalID != 0 || d.SwitchingUpPoint || d.SpatialID != 0 ||
		d.InterLayerDependency || d.TL0PICIDX != 0 {
		return ErrInvalidConfig
	}
	if d.InterPicturePredicted && d.FlexibleMode {
		if d.ReferenceIndexCount <= 0 || d.ReferenceIndexCount > VP9RTPMaxReferenceIndices {
			return ErrInvalidConfig
		}
		for i := 0; i < d.ReferenceIndexCount; i++ {
			if d.ReferenceIndices[i] == 0 || d.ReferenceIndices[i] > 0x7f {
				return ErrInvalidConfig
			}
		}
	} else if d.ReferenceIndexCount != 0 {
		return ErrInvalidConfig
	}
	if !d.ScalabilityStructurePresent && !d.ScalabilityStructure.isZero() {
		return ErrInvalidConfig
	}
	return nil
}

func (ss VP9RTPScalabilityStructure) size() (int, error) {
	layerCount, err := ss.normalizedSpatialLayerCount()
	if err != nil {
		return 0, err
	}
	size := 1
	if ss.ResolutionPresent {
		for i := 0; i < layerCount; i++ {
			if ss.Width[i] == 0 || ss.Height[i] == 0 {
				return 0, ErrInvalidConfig
			}
		}
		size += layerCount * 4
	}
	if ss.PictureGroupPresent {
		if len(ss.PictureGroups) > 255 {
			return 0, ErrInvalidConfig
		}
		size++
		for i := range ss.PictureGroups {
			group := ss.PictureGroups[i]
			if group.TemporalID > 7 ||
				group.ReferenceIndexCount < 0 ||
				group.ReferenceIndexCount > VP9RTPMaxReferenceIndices {
				return 0, ErrInvalidConfig
			}
			size++
			for j := 0; j < group.ReferenceIndexCount; j++ {
				if group.ReferenceIndices[j] == 0 {
					return 0, ErrInvalidConfig
				}
				size++
			}
		}
	} else if len(ss.PictureGroups) != 0 {
		return 0, ErrInvalidConfig
	}
	return size, nil
}

func (ss VP9RTPScalabilityStructure) marshalInto(dst []byte) int {
	layerCount, _ := ss.normalizedSpatialLayerCount()
	dst[0] = byte(layerCount-1) << 5
	if ss.ResolutionPresent {
		dst[0] |= 0x10
	}
	if ss.PictureGroupPresent {
		dst[0] |= 0x08
	}

	off := 1
	if ss.ResolutionPresent {
		for i := 0; i < layerCount; i++ {
			dst[off] = byte(ss.Width[i] >> 8)
			dst[off+1] = byte(ss.Width[i])
			dst[off+2] = byte(ss.Height[i] >> 8)
			dst[off+3] = byte(ss.Height[i])
			off += 4
		}
	}
	if ss.PictureGroupPresent {
		dst[off] = byte(len(ss.PictureGroups))
		off++
		for i := range ss.PictureGroups {
			group := ss.PictureGroups[i]
			dst[off] = group.TemporalID<<5 | byte(group.ReferenceIndexCount)<<2
			if group.SwitchingUpPoint {
				dst[off] |= 0x10
			}
			off++
			for j := 0; j < group.ReferenceIndexCount; j++ {
				dst[off] = group.ReferenceIndices[j]
				off++
			}
		}
	}
	return off
}

func (ss VP9RTPScalabilityStructure) normalizedSpatialLayerCount() (int, error) {
	if ss.SpatialLayerCount == 0 {
		return 1, nil
	}
	if ss.SpatialLayerCount < 0 || ss.SpatialLayerCount > VP9RTPMaxSpatialLayers {
		return 0, ErrInvalidConfig
	}
	return ss.SpatialLayerCount, nil
}

func (ss VP9RTPScalabilityStructure) isZero() bool {
	if ss.SpatialLayerCount != 0 || ss.ResolutionPresent || ss.PictureGroupPresent ||
		len(ss.PictureGroups) != 0 {
		return false
	}
	for i := 0; i < VP9RTPMaxSpatialLayers; i++ {
		if ss.Width[i] != 0 || ss.Height[i] != 0 {
			return false
		}
	}
	return true
}

func parseVP9RTPScalabilityStructure(packet []byte) (VP9RTPScalabilityStructure, int, error) {
	if len(packet) == 0 {
		return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
	}
	header := packet[0]
	off := 1
	ss := VP9RTPScalabilityStructure{
		SpatialLayerCount:   int(header>>5) + 1,
		ResolutionPresent:   header&0x10 != 0,
		PictureGroupPresent: header&0x08 != 0,
	}
	if ss.ResolutionPresent {
		need := ss.SpatialLayerCount * 4
		if len(packet)-off < need {
			return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
		}
		for i := 0; i < ss.SpatialLayerCount; i++ {
			ss.Width[i] = uint16(packet[off])<<8 | uint16(packet[off+1])
			ss.Height[i] = uint16(packet[off+2])<<8 | uint16(packet[off+3])
			if ss.Width[i] == 0 || ss.Height[i] == 0 {
				return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
			}
			off += 4
		}
	}
	if ss.PictureGroupPresent {
		if off >= len(packet) {
			return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
		}
		count := int(packet[off])
		off++
		if count != 0 {
			ss.PictureGroups = make([]VP9RTPPictureGroup, count)
		}
		for i := 0; i < count; i++ {
			if off >= len(packet) {
				return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
			}
			header := packet[off]
			off++
			group := VP9RTPPictureGroup{
				TemporalID:          header >> 5,
				SwitchingUpPoint:    header&0x10 != 0,
				ReferenceIndexCount: int((header >> 2) & 0x03),
			}
			if len(packet)-off < group.ReferenceIndexCount {
				return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
			}
			for j := 0; j < group.ReferenceIndexCount; j++ {
				ref := packet[off]
				off++
				if ref == 0 {
					return VP9RTPScalabilityStructure{}, 0, ErrInvalidVP9Data
				}
				group.ReferenceIndices[j] = ref
			}
			ss.PictureGroups[i] = group
		}
	}
	return ss, off, nil
}
