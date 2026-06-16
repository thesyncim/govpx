package rtp

import (
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

// SpatialSVCLayerFrame is one coded VP9 spatial-SVC layer and the RTP
// descriptor state that should be used for that layer's first fragment.
type SpatialSVCLayerFrame struct {
	Descriptor PayloadDescriptor
	Frame      []byte
}

// SpatialSVCFramePacketizationSize returns the RTP payload count and
// payload-body bytes needed to packetize a VP9 spatial-SVC access unit.
//
// The access unit is written as one VP9 RTP frame per spatial layer, in the
// same order as layers. Each layer descriptor is owned by the caller so codec
// policy such as inter-layer dependency flags stays outside this package.
func SpatialSVCFramePacketizationSize(layers []SpatialSVCLayerFrame, mtu int) (int, int, error) {
	if len(layers) == 0 {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	packets := 0
	payloadBytes := 0
	for i := range layers {
		layer := layers[i]
		layerPackets, layerBytes, err := FramePacketizationSize(layer.Descriptor,
			layer.Frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		packets, err = vpxrtp.AddPayloadSize(packets, layerPackets)
		if err != nil {
			return 0, 0, err
		}
		payloadBytes, err = vpxrtp.AddPayloadSize(payloadBytes, layerBytes)
		if err != nil {
			return 0, 0, err
		}
	}
	return packets, payloadBytes, nil
}

// PacketizeSpatialSVCFrameInto packetizes a VP9 spatial-SVC access unit into
// caller-owned RTP payload storage. The returned payload bodies do not include
// RTP headers. Marker is true only on the final fragment of the highest
// spatial-layer frame.
func PacketizeSpatialSVCFrameInto(dst []vpxrtp.PayloadFragment, payloadBuf []byte,
	layers []SpatialSVCLayerFrame, mtu int,
) (int, int, error) {
	packets, payloadBytes, err := SpatialSVCFramePacketizationSize(layers, mtu)
	if err != nil {
		return 0, 0, err
	}
	if err := vpxrtp.CheckPacketizeBuffers(dst, payloadBuf, packets, payloadBytes); err != nil {
		return packets, payloadBytes, err
	}

	packetOff := 0
	byteOff := 0
	for i := range layers {
		layer := layers[i]
		layerPackets, layerBytes, err := FramePacketizationSize(layer.Descriptor,
			layer.Frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		writtenPackets, writtenBytes, err := PacketizeFrameInto(
			dst[packetOff:packetOff+layerPackets],
			payloadBuf[byteOff:byteOff+layerBytes],
			layer.Descriptor, layer.Frame, mtu)
		if err != nil {
			return 0, 0, err
		}
		for j := range writtenPackets {
			dst[packetOff+j].Marker = i == len(layers)-1 && j == writtenPackets-1
		}
		packetOff += writtenPackets
		byteOff += writtenBytes
	}
	return packets, payloadBytes, nil
}

// PacketizeSpatialSVCFrame returns RTP payload bodies for a VP9 spatial-SVC
// access unit. For caller-owned storage, use [PacketizeSpatialSVCFrameInto].
func PacketizeSpatialSVCFrame(layers []SpatialSVCLayerFrame, mtu int) ([]vpxrtp.PayloadFragment, error) {
	packets, payloadBytes, err := SpatialSVCFramePacketizationSize(layers, mtu)
	if err != nil {
		return nil, err
	}
	out := make([]vpxrtp.PayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, _, err := PacketizeSpatialSVCFrameInto(out, payloadBuf, layers, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}
