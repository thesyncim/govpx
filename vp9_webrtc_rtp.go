package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

// WebRTCRTPPayloadDescriptor returns a WebRTC-friendly VP9 RTP descriptor for
// r. It always carries a 15-bit PictureID, preserves temporal-layer metadata,
// and includes a single-layer scalability structure on keyframes when the
// encoded VP9 header can be parsed.
func (r VP9EncodeResult) WebRTCRTPPayloadDescriptor(
	pictureID uint16,
) VP9RTPPayloadDescriptor {
	desc := r.RTPPayloadDescriptor()
	desc.PictureIDPresent = true
	desc.PictureID15Bit = true
	desc.PictureID = pictureID & VP9RTPPictureID15BitMask
	if vp9WebRTCSingleFrameShouldSignalScalabilityStructure(r) {
		if ss, ok := r.vp9WebRTCSingleFrameScalabilityStructure(); ok {
			desc.ScalabilityStructurePresent = true
			desc.ScalabilityStructure = ss
		}
	}
	return desc
}

// WebRTCRTPPacketizationSize returns the RTP payload count and payload-body
// bytes needed to packetize r using WebRTC-friendly VP9 descriptors.
func (r VP9EncodeResult) WebRTCRTPPacketizationSize(
	pictureID uint16,
	mtu int,
) (int, int, error) {
	desc, frame, err := r.vp9WebRTCRTPDescriptorAndFrame(pictureID)
	if err != nil {
		return 0, 0, err
	}
	return VP9RTPFramePacketizationSize(desc, frame, mtu)
}

// PacketizeWebRTCRTPInto packetizes r into caller-owned RTP payload storage
// using WebRTC-friendly VP9 descriptors. Payload bodies do not include RTP
// headers; Marker is true only on the final packet of the frame.
//
// This is a stateless helper: callers that run a long-lived WebRTC sender must
// own PictureID gaps and recovery-key decisions after dropped or withheld
// frames. Use [VP9WebRTCPacketizer] for that stateful sender path.
func (r VP9EncodeResult) PacketizeWebRTCRTPInto(
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	pictureID uint16,
	mtu int,
) (int, int, error) {
	desc, frame, err := r.vp9WebRTCRTPDescriptorAndFrame(pictureID)
	if err != nil {
		return 0, 0, err
	}
	return PacketizeVP9RTPFrameInto(dst, payloadBuf, desc, frame, mtu)
}

// PacketizeWebRTCRTP packetizes r into allocated RTP payload bodies using
// WebRTC-friendly VP9 descriptors. Payloads do not include RTP headers.
//
// This is a stateless helper. Use [VP9WebRTCPacketizer] for long-lived WebRTC
// sender streams that need PictureID sequencing and recovery-key enforcement.
func (r VP9EncodeResult) PacketizeWebRTCRTP(
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

func (r VP9EncodeResult) vp9WebRTCRTPDescriptorAndFrame(
	pictureID uint16,
) (VP9RTPPayloadDescriptor, []byte, error) {
	if r.Dropped || len(r.Data) == 0 {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	if r.TemporalLayerCount <= 0 ||
		r.TemporalLayerID < 0 ||
		r.TemporalLayerID >= r.TemporalLayerCount ||
		r.TemporalLayerID > 7 {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	desc := r.WebRTCRTPPayloadDescriptor(pictureID)
	if vp9WebRTCSingleFrameShouldSignalScalabilityStructure(r) &&
		!desc.ScalabilityStructurePresent {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidVP9Data
	}
	return desc, r.Data, nil
}

func vp9WebRTCSingleFrameShouldSignalScalabilityStructure(
	r VP9EncodeResult,
) bool {
	return r.KeyFrame && !r.vp9RTPInterPicturePredicted() &&
		r.TemporalLayerID == 0
}

func (r VP9EncodeResult) vp9WebRTCSingleFrameScalabilityStructure() (
	VP9RTPScalabilityStructure,
	bool,
) {
	if len(r.Data) == 0 {
		return VP9RTPScalabilityStructure{}, false
	}
	var br vp9dec.BitReader
	br.Init(r.Data)
	header, err := vp9dec.ReadUncompressedHeader(&br, nil,
		func(uint8) (uint32, uint32) { return 0, 0 })
	if err != nil || header.Width == 0 || header.Height == 0 ||
		header.Width > uint32(^uint16(0)) ||
		header.Height > uint32(^uint16(0)) {
		return VP9RTPScalabilityStructure{}, false
	}
	ss := VP9RTPScalabilityStructure{
		SpatialLayerCount: 1,
		ResolutionPresent: true,
		Width: [VP9RTPMaxSpatialLayers]uint16{
			uint16(header.Width),
		},
		Height: [VP9RTPMaxSpatialLayers]uint16{
			uint16(header.Height),
		},
	}
	if r.TemporalLayerCount == 1 {
		ss.PictureGroupPresent = true
		ss.PictureGroups = []VP9RTPPictureGroup{{TemporalID: 0}}
	} else if r.TemporalLayerCount > 1 {
		groups, ok := vp9WebRTCTemporalScalabilityPictureGroups(
			r.TemporalLayeringMode)
		if !ok {
			mode, ok := vp9DefaultWebRTCTemporalModeForLayerCount(
				r.TemporalLayerCount)
			if !ok {
				return VP9RTPScalabilityStructure{}, false
			}
			groups, ok = vp9WebRTCTemporalScalabilityPictureGroups(mode)
			if !ok {
				groups = vp9GenericTemporalScalabilityPictureGroups(mode)
			}
		}
		if len(groups) == 0 {
			return VP9RTPScalabilityStructure{}, false
		}
		ss.PictureGroupPresent = true
		ss.PictureGroups = groups
	}
	return ss, true
}

func vp9DefaultWebRTCTemporalModeForLayerCount(
	layerCount int,
) (TemporalLayeringMode, bool) {
	switch layerCount {
	case 1:
		return TemporalLayeringOneLayer, true
	case 2:
		return TemporalLayeringTwoLayers, true
	case 3:
		return TemporalLayeringThreeLayers, true
	case 5:
		return TemporalLayeringFiveLayers, true
	default:
		return TemporalLayeringOneLayer, false
	}
}
