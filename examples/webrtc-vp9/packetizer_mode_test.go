package main

import "github.com/thesyncim/govpx"

type vp9WebRTCTestPacketizerMode uint8

const (
	vp9WebRTCFlexiblePacketizerForTest vp9WebRTCTestPacketizerMode = iota
	vp9WebRTCNonFlexiblePacketizerForTest
)

func (m vp9WebRTCTestPacketizerMode) packetizationSize(
	packetizer *govpx.VP9WebRTCPacketizer,
	result govpx.VP9SpatialSVCEncodeResult,
	mtu int,
) (int, int, error) {
	if m == vp9WebRTCNonFlexiblePacketizerForTest {
		return packetizer.SpatialSVCWebRTCNonFlexiblePacketizationSize(
			result, mtu)
	}
	return packetizer.SpatialSVCWebRTCPacketizationSize(result, mtu)
}

func (m vp9WebRTCTestPacketizerMode) packetizeInto(
	packetizer *govpx.VP9WebRTCPacketizer,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
	payloadBuf []byte,
	mtu int,
) (int, int, error) {
	if m == vp9WebRTCNonFlexiblePacketizerForTest {
		return packetizer.PacketizeSpatialSVCWebRTCNonFlexibleInto(result,
			payloads, payloadBuf, mtu)
	}
	return packetizer.PacketizeSpatialSVCWebRTCInto(result, payloads,
		payloadBuf, mtu)
}

func (m vp9WebRTCTestPacketizerMode) nonFlexible() bool {
	return m == vp9WebRTCNonFlexiblePacketizerForTest
}

func (m vp9WebRTCTestPacketizerMode) packetizationSizeName() string {
	if m.nonFlexible() {
		return "SpatialSVCWebRTCNonFlexiblePacketizationSize"
	}
	return "SpatialSVCWebRTCPacketizationSize"
}

func (m vp9WebRTCTestPacketizerMode) packetizeIntoName() string {
	if m.nonFlexible() {
		return "PacketizeSpatialSVCWebRTCNonFlexibleInto"
	}
	return "PacketizeSpatialSVCWebRTCInto"
}
