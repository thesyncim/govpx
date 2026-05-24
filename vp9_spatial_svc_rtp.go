package govpx

import vp9rtp "github.com/thesyncim/govpx/internal/vp9/rtp"

// RTPPacketizationSize returns the RTP payload fragment count and payload-body
// bytes needed to packetize every coded spatial layer in r at mtu bytes.
//
// mtu includes each VP9 RTP payload descriptor but excludes the RTP header.
// The access unit is packetized as one VP9 RTP frame per spatial layer, in
// ascending spatial-layer order. The base layer carries the parent scalability
// structure; enhancement layers carry only their layer indices.
func (r VP9SpatialSVCEncodeResult) RTPPacketizationSize(mtu int) (int, int, error) {
	var layerBuf [VP9MaxSpatialLayers]vp9rtp.SpatialSVCLayerFrame
	layers, err := r.vp9SpatialSVCRTPLayers(&layerBuf)
	if err != nil {
		return 0, 0, err
	}
	return vp9rtp.SpatialSVCFramePacketizationSize(layers, mtu)
}

// PacketizeRTPInto packetizes every coded spatial layer in r into caller-owned
// RTP payload storage. dst receives fragment metadata and payloadBuf receives
// the payload bodies. On [ErrBufferTooSmall], the returned fragment and byte
// counts are the required capacities.
//
// Payload bodies do not include RTP headers. Fragments are written in
// spatial-layer order, and each layer frame uses its own VP9 RTP start/end and
// marker semantics.
func (r VP9SpatialSVCEncodeResult) PacketizeRTPInto(dst []RTPPayloadFragment,
	payloadBuf []byte, mtu int,
) (int, int, error) {
	var layerBuf [VP9MaxSpatialLayers]vp9rtp.SpatialSVCLayerFrame
	layers, err := r.vp9SpatialSVCRTPLayers(&layerBuf)
	if err != nil {
		return 0, 0, err
	}
	return vp9rtp.PacketizeSpatialSVCFrameInto(dst, payloadBuf, layers, mtu)
}

// PacketizeRTP returns RTP payload bodies for every coded spatial layer in r.
// For a zero-allocation packetization path, use [VP9SpatialSVCEncodeResult.PacketizeRTPInto].
func (r VP9SpatialSVCEncodeResult) PacketizeRTP(mtu int) ([]RTPPayloadFragment, error) {
	var layerBuf [VP9MaxSpatialLayers]vp9rtp.SpatialSVCLayerFrame
	layers, err := r.vp9SpatialSVCRTPLayers(&layerBuf)
	if err != nil {
		return nil, err
	}
	return vp9rtp.PacketizeSpatialSVCFrame(layers, mtu)
}

func (r VP9SpatialSVCEncodeResult) vp9SpatialSVCLayerCount() (int, error) {
	if r.LayerCount == 0 || r.LayerCount > VP9MaxSpatialLayers {
		return 0, ErrInvalidConfig
	}
	return int(r.LayerCount), nil
}

func (r VP9SpatialSVCEncodeResult) vp9SpatialSVCRTPLayers(
	dst *[VP9MaxSpatialLayers]vp9rtp.SpatialSVCLayerFrame,
) ([]vp9rtp.SpatialSVCLayerFrame, error) {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return nil, err
	}
	for layerID := range count {
		desc, frame, err := r.vp9SpatialSVCRTPFrame(layerID)
		if err != nil {
			return nil, err
		}
		dst[layerID] = vp9rtp.SpatialSVCLayerFrame{
			Descriptor: desc,
			Frame:      frame,
		}
	}
	return dst[:count], nil
}

func (r VP9SpatialSVCEncodeResult) vp9SpatialSVCRTPFrame(
	layerID int,
) (VP9RTPPayloadDescriptor, []byte, error) {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	if layerID < 0 || layerID >= count {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	layer := r.Layers[layerID]
	if layer.Dropped || len(layer.Data) == 0 ||
		layer.SpatialLayerID != uint8(layerID) ||
		layer.SpatialLayerCount != r.LayerCount {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	desc := layer.RTPPayloadDescriptor()
	if layerID == 0 {
		if !vp9rtp.ScalabilityStructureIsZero(r.ScalabilityStructure) {
			desc.ScalabilityStructurePresent = true
			desc.ScalabilityStructure = r.ScalabilityStructure
		}
	} else {
		desc.ScalabilityStructurePresent = false
		desc.ScalabilityStructure = VP9RTPScalabilityStructure{}
	}
	return desc, layer.Data, nil
}
