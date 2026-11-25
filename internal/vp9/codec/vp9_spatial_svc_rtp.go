package codec

import (
	vp9rtp "github.com/thesyncim/govpx/internal/vp9/rtp"
)

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
// spatial-layer order. Each layer frame uses its own VP9 RTP start/end bits;
// the RTP marker bit is set only on the final fragment of the highest
// spatial-layer frame.
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

const (
	// VP9RTPPictureID15BitMask is the largest VP9 RTP 15-bit PictureID value.
	VP9RTPPictureID15BitMask uint16 = 0x7fff
)

// NextVP9RTPPictureID advances a VP9 RTP 15-bit PictureID with wraparound.
func NextVP9RTPPictureID(id uint16) uint16 {
	return (id + 1) & VP9RTPPictureID15BitMask
}

// VP9WebRTCSpatialLayerChangeNeedsKeyFrame reports whether a WebRTC VP9
// sender should force a key access unit before changing the number of
// transmitted spatial layers.
func VP9WebRTCSpatialLayerChangeNeedsKeyFrame(
	currentLayerCount int,
	nextLayerCount int,
) bool {
	return currentLayerCount != nextLayerCount
}

// LimitSpatialLayersForRTP returns a shallow copy of r that exposes only the
// first layerCount spatial layers for RTP transmission. It also rewrites
// active-layer metadata and clears hidden scalability-structure dimensions so
// receivers do not wait for non-transmitted enhancement layers.
//
// When a WebRTC sender changes the active layer count across access units, it
// should force a key access unit before packetizing the first result at the new
// count. [VP9WebRTCPacketizer] enforces that rule for this capped RTP view; the
// stateless packetizers cannot remember the previous access unit. Use
// [VP9WebRTCSpatialLayerChangeNeedsKeyFrame] to gate that control decision when
// managing the sequence yourself.
func (r VP9SpatialSVCEncodeResult) LimitSpatialLayersForRTP(
	layerCount int,
) (VP9SpatialSVCEncodeResult, error) {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return VP9SpatialSVCEncodeResult{}, err
	}
	if layerCount <= 0 || layerCount > count {
		return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
	}
	out := r
	out.LayerCount = uint8(layerCount)
	out.ScalabilityStructure = limitVP9RTPScalabilityStructure(
		r.ScalabilityStructure, layerCount)
	out.Data = nil
	out.SizeBytes = 0
	for i := range layerCount {
		layer := out.Layers[i]
		if layer.Dropped || len(layer.Data) == 0 ||
			layer.SpatialLayerID != uint8(i) {
			return VP9SpatialSVCEncodeResult{}, ErrInvalidConfig
		}
		layer.SpatialLayerCount = uint8(layerCount)
		layer.NotRefForUpperSpatialLayer = !out.InterLayerPrediction ||
			i == layerCount-1
		out.Layers[i] = layer
		out.SizeBytes += layer.SizeBytes
	}
	for i := layerCount; i < len(out.Layers); i++ {
		out.Layers[i] = VP9EncodeResult{}
	}
	return out, nil
}

// WebRTCRTPPacketizationSize returns the RTP payload count and payload-body
// bytes needed to packetize r using WebRTC-friendly VP9 descriptors.
//
// The resulting payloads use a 15-bit PictureID shared by all spatial layers
// in the access unit. Scalability structure is emitted only on the base
// spatial layer of a non-predicted temporal-layer-0 key access unit.
func (r VP9SpatialSVCEncodeResult) WebRTCRTPPacketizationSize(
	pictureID uint16,
	mtu int,
) (int, int, error) {
	count, err := r.vp9WebRTCLayerCount()
	if err != nil {
		return 0, 0, err
	}
	packets := 0
	payloadBytes := 0
	for i := range count {
		desc, frame, err := r.vp9WebRTCLayerDescriptor(i, pictureID)
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

// PacketizeWebRTCRTPInto packetizes r into caller-owned RTP payload storage
// using WebRTC-friendly VP9 descriptors. Payload bodies do not include RTP
// headers; Marker is true only on the final packet of the access unit.
//
// This is a stateless helper: callers that run a long-lived WebRTC sender must
// own PictureID gaps and recovery-key decisions after dropped, withheld, or
// layer-count-changing access units. Use [VP9WebRTCPacketizer] for that stateful
// sender path.
func (r VP9SpatialSVCEncodeResult) PacketizeWebRTCRTPInto(
	dst []RTPPayloadFragment,
	payloadBuf []byte,
	pictureID uint16,
	mtu int,
) (int, int, error) {
	count, err := r.vp9WebRTCLayerCount()
	if err != nil {
		return 0, 0, err
	}
	packets, payloadBytes, err := r.WebRTCRTPPacketizationSize(pictureID, mtu)
	if err != nil {
		return 0, 0, err
	}
	if len(dst) < packets || len(payloadBuf) < payloadBytes {
		return packets, payloadBytes, ErrBufferTooSmall
	}
	packetOff := 0
	byteOff := 0
	for i := range count {
		desc, frame, err := r.vp9WebRTCLayerDescriptor(i, pictureID)
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
		for j := range writtenPackets {
			dst[packetOff+j].Marker = i == count-1 && j == writtenPackets-1
		}
		packetOff += writtenPackets
		byteOff += writtenBytes
	}
	return packets, payloadBytes, nil
}

// PacketizeWebRTCRTP packetizes r into allocated RTP payload bodies using
// WebRTC-friendly VP9 descriptors. Payloads do not include RTP headers.
//
// This is a stateless helper. Use [VP9WebRTCPacketizer] for long-lived WebRTC
// sender streams that need PictureID sequencing and recovery-key enforcement.
func (r VP9SpatialSVCEncodeResult) PacketizeWebRTCRTP(
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

func (r VP9SpatialSVCEncodeResult) vp9WebRTCLayerCount() (int, error) {
	count, err := r.vp9SpatialSVCLayerCount()
	if err != nil {
		return 0, err
	}
	for i := range count {
		layer := r.Layers[i]
		if layer.Dropped || len(layer.Data) == 0 ||
			layer.SpatialLayerID != uint8(i) ||
			layer.SpatialLayerCount != r.LayerCount {
			return 0, ErrInvalidConfig
		}
	}
	return count, nil
}

func (r VP9SpatialSVCEncodeResult) vp9WebRTCLayerDescriptor(
	layerID int,
	pictureID uint16,
) (VP9RTPPayloadDescriptor, []byte, error) {
	count, err := r.vp9WebRTCLayerCount()
	if err != nil {
		return VP9RTPPayloadDescriptor{}, nil, err
	}
	if layerID < 0 || layerID >= count {
		return VP9RTPPayloadDescriptor{}, nil, ErrInvalidConfig
	}
	layer := r.Layers[layerID]
	desc := layer.RTPPayloadDescriptor()
	desc.ScalabilityStructurePresent = false
	desc.ScalabilityStructure = VP9RTPScalabilityStructure{}
	if layerID == 0 && vp9WebRTCShouldSignalScalabilityStructure(layer, r) {
		desc.ScalabilityStructurePresent = true
		desc.ScalabilityStructure = r.ScalabilityStructure
	}
	desc.PictureIDPresent = true
	desc.PictureID15Bit = true
	desc.PictureID = pictureID & VP9RTPPictureID15BitMask
	return desc, layer.Data, nil
}

func vp9WebRTCShouldSignalScalabilityStructure(
	layer VP9EncodeResult,
	result VP9SpatialSVCEncodeResult,
) bool {
	if !layer.KeyFrame || layer.InterPicturePredicted ||
		layer.TemporalLayerID != 0 {
		return false
	}
	return !vp9RTPScalabilityStructureIsZero(result.ScalabilityStructure)
}

func limitVP9RTPScalabilityStructure(
	ss VP9RTPScalabilityStructure,
	layerCount int,
) VP9RTPScalabilityStructure {
	out := ss
	if layerCount > 0 &&
		(out.SpatialLayerCount == 0 || layerCount < out.SpatialLayerCount) {
		out.SpatialLayerCount = layerCount
	}
	for i := layerCount; i < len(out.Width); i++ {
		out.Width[i] = 0
		out.Height[i] = 0
	}
	return out
}

func vp9RTPScalabilityStructureIsZero(ss VP9RTPScalabilityStructure) bool {
	if ss.SpatialLayerCount != 0 || ss.ResolutionPresent ||
		ss.PictureGroupPresent || len(ss.PictureGroups) != 0 {
		return false
	}
	for i := range ss.Width {
		if ss.Width[i] != 0 || ss.Height[i] != 0 {
			return false
		}
	}
	return true
}
