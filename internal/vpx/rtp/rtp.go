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

// PayloadDescriptor is the shared mechanical contract for codec RTP payload
// descriptors. Codec packages own descriptor syntax and validation.
type PayloadDescriptor interface {
	Size() (int, error)
	MarshalInto([]byte) (int, error)
}

// MarshalDescriptor returns desc as a newly allocated RTP payload descriptor.
// Codec packages own descriptor syntax; this helper owns the repeated
// size-allocate-marshal shape.
func MarshalDescriptor[D PayloadDescriptor](desc D) ([]byte, error) {
	need, err := desc.Size()
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = desc.MarshalInto(out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PictureID is the shared RFC VPx RTP PictureID encoding. VP8 and VP9 expose
// codec-specific descriptor structs, but the PictureID wire format is the same:
// one byte for 7-bit IDs or two bytes with the high marker bit set for 15-bit
// IDs.
type PictureID struct {
	Present    bool
	Value      uint16
	FifteenBit bool
}

// Size returns the number of bytes needed for p, or zero when no PictureID is
// present.
func (p PictureID) Size() (int, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if !p.Present {
		return 0, nil
	}
	if p.FifteenBit {
		return 2, nil
	}
	return 1, nil
}

// MarshalInto writes p into dst and returns the number of bytes written.
func (p PictureID) MarshalInto(dst []byte) (int, error) {
	need, err := p.Size()
	if err != nil {
		return 0, err
	}
	if need == 0 {
		return 0, nil
	}
	if len(dst) < need {
		return need, vpxerrors.ErrBufferTooSmall
	}
	if p.FifteenBit {
		dst[0] = 0x80 | byte(p.Value>>8)
		dst[1] = byte(p.Value)
		return 2, nil
	}
	dst[0] = byte(p.Value)
	return 1, nil
}

// Validate rejects impossible PictureID field combinations.
func (p PictureID) Validate() error {
	if p.FifteenBit && !p.Present {
		return vpxerrors.ErrInvalidConfig
	}
	if p.Present {
		if p.FifteenBit {
			if p.Value > 0x7fff {
				return vpxerrors.ErrInvalidConfig
			}
		} else if p.Value > 0x7f {
			return vpxerrors.ErrInvalidConfig
		}
	} else if p.Value != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	return nil
}

// ParsePictureID parses a present PictureID field and returns the value and
// number of bytes consumed. invalidData preserves the codec-specific parse
// sentinel used by the caller.
func ParsePictureID(packet []byte, invalidData error) (PictureID, int, error) {
	if len(packet) == 0 {
		return PictureID{}, 0, invalidData
	}
	pid := packet[0]
	if pid&0x80 == 0 {
		return PictureID{Present: true, Value: uint16(pid)}, 1, nil
	}
	if len(packet) < 2 {
		return PictureID{}, 0, invalidData
	}
	return PictureID{
		Present:    true,
		Value:      uint16(pid&0x7f)<<8 | uint16(packet[1]),
		FifteenBit: true,
	}, 2, nil
}

// PacketDescriptor returns the codec RTP payload descriptor and its byte
// length for fragment index i of fragments. Codec packages own the descriptor
// syntax and per-fragment state such as VP8 start-of-partition or VP9
// start/end-of-frame bits.
type PacketDescriptor[D PayloadDescriptor] func(i, fragments int) (D, int, error)

// PayloadSize returns the number of bytes needed to pack desc and payload
// into one RTP payload body.
func PayloadSize[D PayloadDescriptor](desc D, payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, err
	}
	return PayloadBodySize(descSize, len(payload))
}

// PackPayloadInto writes desc followed by payload into dst and returns the
// RTP payload-body length. It does not write an RTP header.
func PackPayloadInto[D PayloadDescriptor](dst []byte, desc D, payload []byte) (int, error) {
	need, err := PayloadSize(desc, payload)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, vpxerrors.ErrBufferTooSmall
	}
	n, err := desc.MarshalInto(dst)
	if err != nil {
		return 0, err
	}
	copy(dst[n:], payload)
	return need, nil
}

// PackPayload returns desc followed by payload as one RTP payload body. It
// does not include an RTP header.
func PackPayload[D PayloadDescriptor](desc D, payload []byte) ([]byte, error) {
	need, err := PayloadSize(desc, payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = PackPayloadInto(out, desc, payload)
	if err != nil {
		return nil, err
	}
	return out, nil
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

// PacketizeFrameInto packetizes frame into caller-owned RTP payload-body
// storage. descriptor supplies the codec-specific descriptor for each
// fragment; this helper only owns the shared mechanics of chunk sizing,
// marker-bit assignment, and descriptor+payload packing.
func PacketizeFrameInto[D PayloadDescriptor](dst []PayloadFragment, payloadBuf []byte,
	frame []byte, mtu int, packets int, totalBytes int, descriptor PacketDescriptor[D],
) (int, int, error) {
	if err := CheckPacketizeBuffers(dst, payloadBuf, packets, totalBytes); err != nil {
		return packets, totalBytes, err
	}
	frameOff := 0
	bufOff := 0
	for i := range packets {
		packetDesc, descSize, err := descriptor(i, packets)
		if err != nil {
			return 0, 0, err
		}
		chunk, err := FramePayloadChunkSize(mtu, descSize, len(frame)-frameOff)
		if err != nil {
			return 0, 0, err
		}
		payload := frame[frameOff : frameOff+chunk]
		n, err := PackPayloadInto(payloadBuf[bufOff:bufOff+descSize+chunk],
			packetDesc, payload)
		if err != nil {
			return 0, 0, err
		}
		dst[i] = PayloadFragment{
			Payload: payloadBuf[bufOff : bufOff+n],
			Marker:  LastFragment(i, packets),
		}
		frameOff += chunk
		bufOff += n
	}
	return packets, totalBytes, nil
}

// PacketizeFrame returns RTP payload bodies for frame using descriptor to
// provide codec-specific per-fragment descriptors.
func PacketizeFrame[D PayloadDescriptor](frame []byte, mtu int, packets int,
	totalBytes int, descriptor PacketDescriptor[D],
) ([]PayloadFragment, error) {
	out := make([]PayloadFragment, packets)
	payloadBuf := make([]byte, totalBytes)
	n, _, err := PacketizeFrameInto(out, payloadBuf, frame, mtu,
		packets, totalBytes, descriptor)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
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

// PayloadParser returns the codec descriptor and codec payload fragment from
// one RTP payload body.
type PayloadParser[D any] func([]byte) (D, []byte, error)

// FragmentValidator validates codec-specific descriptor sequence rules for
// payload fragment i of fragments.
type FragmentValidator[D any] func(i, fragments int, desc D) error

// FrameAssemblySize validates ordered RTP payload bodies and returns the
// concatenated codec payload size. Codec packages supply descriptor parsing
// and sequence validation; this helper owns marker-bit, empty-fragment, and
// overflow checks.
func FrameAssemblySize[D any](payloads []PayloadFragment, invalidData error,
	parse PayloadParser[D], validate FragmentValidator[D],
) (int, error) {
	if len(payloads) == 0 {
		return 0, invalidData
	}
	total := 0
	for i := range payloads {
		desc, fragment, err := parse(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		if len(fragment) == 0 {
			return 0, invalidData
		}
		if !MarkerMatchesFragmentIndex(payloads, i) {
			return 0, invalidData
		}
		if err := validate(i, len(payloads), desc); err != nil {
			return 0, err
		}
		total, err = AddPayloadSize(total, len(fragment))
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// AssembleFrameInto validates payloads, strips codec descriptors, and copies
// the concatenated codec payload into dst.
func AssembleFrameInto[D any](dst []byte, payloads []PayloadFragment,
	invalidData error, parse PayloadParser[D], validate FragmentValidator[D],
) (int, error) {
	need, err := FrameAssemblySize(payloads, invalidData, parse, validate)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, vpxerrors.ErrBufferTooSmall
	}
	return AssemblePayloadFragmentsInto(dst, payloads, need, payloadParser(parse))
}

// AssembleFrame validates payloads and returns the concatenated codec payload.
func AssembleFrame[D any](payloads []PayloadFragment, invalidData error,
	parse PayloadParser[D], validate FragmentValidator[D],
) ([]byte, error) {
	need, err := FrameAssemblySize(payloads, invalidData, parse, validate)
	if err != nil {
		return nil, err
	}
	return AssemblePayloadFragments(payloads, need, payloadParser(parse))
}

func payloadParser[D any](parse PayloadParser[D]) PayloadFragmentParser {
	return func(payload []byte) ([]byte, error) {
		_, fragment, err := parse(payload)
		if err != nil {
			return nil, err
		}
		return fragment, nil
	}
}

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
