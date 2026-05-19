// Package rtp holds shared mechanical helpers for RTP payload assembly.
package rtp

import vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"

// PayloadFragment is one RTP payload body plus the RTP marker-bit value
// the caller should put in the RTP header for that body.
//
// Payload contains codec-specific payload-descriptor bytes followed by the
// codec payload fragment. It does not include an RTP header.
type PayloadFragment struct {
	Payload []byte
	Marker  bool
}

// PayloadBodySize returns the byte length of one RTP payload body containing
// descriptor bytes followed by codec payload bytes.
func PayloadBodySize(descriptorSize, payloadLen int) (int, error) {
	if descriptorSize <= 0 || payloadLen == 0 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	return AddPayloadSize(descriptorSize, payloadLen)
}

// FramePacketizationSize returns the packet count and total payload-body bytes
// for a frame split into RTP bodies with a fixed descriptor size.
func FramePacketizationSize(frameLen, descriptorSize, mtu int) (int, int, error) {
	if frameLen == 0 || descriptorSize <= 0 || mtu <= descriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	payloadBytesPerPacket := mtu - descriptorSize
	packets := (frameLen + payloadBytesPerPacket - 1) / payloadBytesPerPacket

	maxInt := int(^uint(0) >> 1)
	if packets > (maxInt-frameLen)/descriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	return packets, frameLen + packets*descriptorSize, nil
}

// VariableFramePacketizationSize returns the packet count and total
// payload-body bytes for a frame whose first RTP payload body uses a different
// descriptor size from the remaining bodies.
func VariableFramePacketizationSize(frameLen, firstDescriptorSize, restDescriptorSize, mtu int) (int, int, error) {
	if frameLen == 0 || firstDescriptorSize <= 0 || restDescriptorSize <= 0 ||
		mtu <= firstDescriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}

	firstPayload := mtu - firstDescriptorSize
	if frameLen <= firstPayload {
		total, err := PayloadBodySize(firstDescriptorSize, frameLen)
		if err != nil {
			return 0, 0, err
		}
		return 1, total, nil
	}
	if mtu <= restDescriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}

	restPayload := mtu - restDescriptorSize
	remaining := frameLen - firstPayload
	restPackets := (remaining + restPayload - 1) / restPayload

	total, err := AddPayloadSize(firstDescriptorSize, firstPayload)
	if err != nil {
		return 0, 0, err
	}
	total, err = AddPayloadSize(total, remaining)
	if err != nil {
		return 0, 0, err
	}
	maxInt := int(^uint(0) >> 1)
	if restPackets > maxInt/restDescriptorSize {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	total, err = AddPayloadSize(total, restPackets*restDescriptorSize)
	if err != nil {
		return 0, 0, err
	}
	return 1 + restPackets, total, nil
}

// CheckPacketizeBuffers verifies the caller-provided packet metadata and
// payload body buffers can hold a packetized frame.
func CheckPacketizeBuffers(dst []PayloadFragment, payloadBuf []byte, packets, totalBytes int) error {
	if len(dst) < packets || len(payloadBuf) < totalBytes {
		return vpxerrors.ErrBufferTooSmall
	}
	return nil
}

// FramePayloadChunkSize returns the next codec-payload fragment size for a
// descriptor of descriptorSize bytes in an RTP payload body capped by mtu.
func FramePayloadChunkSize(mtu, descriptorSize, remaining int) (int, error) {
	if remaining <= 0 || descriptorSize <= 0 || mtu <= descriptorSize {
		return 0, vpxerrors.ErrInvalidConfig
	}
	n := mtu - descriptorSize
	if n > remaining {
		n = remaining
	}
	return n, nil
}

// LastFragment reports whether i is the final fragment index in a packetized
// frame of n fragments.
func LastFragment(i, n int) bool {
	return i == n-1
}

// MarkerMatchesFragmentIndex reports whether the RTP marker bit is set only
// on the final fragment of the frame.
func MarkerMatchesFragmentIndex(payloads []PayloadFragment, i int) bool {
	return payloads[i].Marker == LastFragment(i, len(payloads))
}

// PayloadFragmentParser returns the codec payload bytes from one RTP payload
// body after validating and stripping the codec-specific descriptor.
type PayloadFragmentParser func([]byte) ([]byte, error)

// AssemblePayloadFragmentsInto copies codec payload fragments into dst. The
// caller remains responsible for codec-specific descriptor and sequence
// validation before calling this helper.
func AssemblePayloadFragmentsInto(dst []byte, payloads []PayloadFragment,
	size int, parse PayloadFragmentParser,
) (int, error) {
	off := 0
	for i := range payloads {
		fragment, err := parse(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		if len(fragment) > len(dst)-off {
			return size, vpxerrors.ErrBufferTooSmall
		}
		copy(dst[off:], fragment)
		off += len(fragment)
	}
	return size, nil
}

// AssemblePayloadFragments returns the concatenated codec payload carried by
// payloads. The caller supplies the already validated frame size.
func AssemblePayloadFragments(payloads []PayloadFragment, size int,
	parse PayloadFragmentParser,
) ([]byte, error) {
	out := make([]byte, size)
	_, err := AssemblePayloadFragmentsInto(out, payloads, size, parse)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AddPayloadSize adds n to total while rejecting negative sizes and int
// overflow.
func AddPayloadSize(total, n int) (int, error) {
	maxInt := int(^uint(0) >> 1)
	if n < 0 || total > maxInt-n {
		return 0, vpxerrors.ErrInvalidConfig
	}
	return total + n, nil
}
