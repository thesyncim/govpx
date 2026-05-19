// Package rtp implements VP8 RTP payload descriptors and frame fragment
// assembly. libvpx v1.16.0 does not provide these RTP packetization helpers;
// the wire format follows RFC 7741.
package rtp

import (
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

const (
	vp8RTPFlagExtendedControl = 0x80
	vp8RTPFlagNonReference    = 0x20
	vp8RTPFlagStartPartition  = 0x10

	vp8RTPFlagPictureID = 0x80
	vp8RTPFlagTL0PICIDX = 0x40
	vp8RTPFlagTemporal  = 0x20
	vp8RTPFlagKeyIndex  = 0x10
)

// PayloadDescriptor describes the VP8 RTP payload descriptor from RFC
// 7741. It is the bytes after the RTP header and before the raw VP8 payload.
type PayloadDescriptor struct {
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
func (d PayloadDescriptor) Size() (int, error) {
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
// too small, it returns the required descriptor length and [vpxerrors.ErrBufferTooSmall].
func (d PayloadDescriptor) MarshalInto(dst []byte) (int, error) {
	need, err := d.Size()
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, vpxerrors.ErrBufferTooSmall
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
func (d PayloadDescriptor) Marshal() ([]byte, error) {
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

// ParsePayloadDescriptor parses the VP8 RTP payload descriptor at the
// front of packet and returns the descriptor plus the remaining raw VP8
// payload bytes.
func ParsePayloadDescriptor(packet []byte) (PayloadDescriptor, []byte, error) {
	if len(packet) == 0 {
		return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
	}
	first := packet[0]
	d := PayloadDescriptor{
		NonReferenceFrame: first&vp8RTPFlagNonReference != 0,
		StartOfPartition:  first&vp8RTPFlagStartPartition != 0,
		PartitionID:       first & 0x07,
	}
	off := 1
	if first&vp8RTPFlagExtendedControl == 0 {
		return d, packet[off:], nil
	}
	if off >= len(packet) {
		return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
	}
	ext := packet[off]
	off++
	d.PictureIDPresent = ext&vp8RTPFlagPictureID != 0
	d.TL0PICIDXPresent = ext&vp8RTPFlagTL0PICIDX != 0
	d.TemporalIDPresent = ext&vp8RTPFlagTemporal != 0
	d.KeyIndexPresent = ext&vp8RTPFlagKeyIndex != 0
	if d.TL0PICIDXPresent && !d.TemporalIDPresent {
		return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
	}
	if d.PictureIDPresent {
		if off >= len(packet) {
			return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
		}
		pid := packet[off]
		off++
		if pid&0x80 != 0 {
			if off >= len(packet) {
				return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
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
			return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
		}
		d.TL0PICIDX = packet[off]
		off++
	}
	if d.TemporalIDPresent || d.KeyIndexPresent {
		if off >= len(packet) {
			return PayloadDescriptor{}, nil, vpxerrors.ErrInvalidData
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

// PayloadSize returns the number of bytes needed to pack desc and the
// raw VP8 payload into one RTP payload body.
func PayloadSize(desc PayloadDescriptor, payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, err
	}
	return vpxrtp.PayloadBodySize(descSize, len(payload))
}

// PackPayloadInto writes desc followed by payload into dst and returns
// the RTP payload length. It does not write an RTP header.
func PackPayloadInto(dst []byte, desc PayloadDescriptor, payload []byte) (int, error) {
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

// PackPayload returns desc followed by payload as one RTP payload body.
// It does not include an RTP header.
func PackPayload(desc PayloadDescriptor, payload []byte) ([]byte, error) {
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

// FramePacketizationSize returns the number of RTP payload bodies and
// total payload-body bytes needed to packetize one raw VP8 frame at mtu bytes.
//
// mtu includes the VP8 RTP payload descriptor but excludes the RTP header.
// The packetizer manages StartOfPartition and always emits partition 0.
func FramePacketizationSize(desc PayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	if desc.PartitionID != 0 {
		return 0, 0, vpxerrors.ErrInvalidConfig
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, 0, err
	}
	return vpxrtp.FramePacketizationSize(len(frame), descSize, mtu)
}

// PacketizeFrameInto packetizes one raw VP8 frame into caller-owned
// RTP payload storage. dst receives packet metadata; payloadBuf receives the
// payload bodies. On [vpxerrors.ErrBufferTooSmall], the returned packet and byte counts
// are the required capacities.
//
// The returned payload bodies do not include RTP headers. Marker is true only
// on the last payload body.
func PacketizeFrameInto(dst []vpxrtp.PayloadFragment, payloadBuf []byte,
	desc PayloadDescriptor, frame []byte, mtu int,
) (int, int, error) {
	packets, totalBytes, err := FramePacketizationSize(desc, frame, mtu)
	if err != nil {
		return 0, 0, err
	}
	if err := vpxrtp.CheckPacketizeBuffers(dst, payloadBuf, packets, totalBytes); err != nil {
		return packets, totalBytes, err
	}
	descSize, err := desc.Size()
	if err != nil {
		return 0, 0, err
	}
	frameOff := 0
	bufOff := 0
	for i := range packets {
		chunk, err := vpxrtp.FramePayloadChunkSize(mtu, descSize, len(frame)-frameOff)
		if err != nil {
			return 0, 0, err
		}
		packetDesc := desc
		packetDesc.StartOfPartition = i == 0
		packetDesc.PartitionID = 0
		last := vpxrtp.LastFragment(i, packets)

		payload := frame[frameOff : frameOff+chunk]
		n, err := PackPayloadInto(payloadBuf[bufOff:bufOff+descSize+chunk],
			packetDesc, payload)
		if err != nil {
			return 0, 0, err
		}
		dst[i] = vpxrtp.PayloadFragment{
			Payload: payloadBuf[bufOff : bufOff+n],
			Marker:  last,
		}
		frameOff += chunk
		bufOff += n
	}
	return packets, totalBytes, nil
}

// PacketizeFrame returns RTP payload bodies for one raw VP8 frame.
// Payloads do not include RTP headers; Marker is true only on the last body.
func PacketizeFrame(desc PayloadDescriptor, frame []byte, mtu int) ([]vpxrtp.PayloadFragment, error) {
	packets, totalBytes, err := FramePacketizationSize(desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	out := make([]vpxrtp.PayloadFragment, packets)
	payloadBuf := make([]byte, totalBytes)
	n, _, err := PacketizeFrameInto(out, payloadBuf, desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// FrameAssemblySize validates an ordered set of VP8 RTP payload bodies
// for one frame and returns the raw VP8 frame size.
//
// The caller owns RTP sequence-number validation, loss handling, and jitter
// buffering. Payloads must be in decode order and must include the marker bit
// value from each RTP header.
func FrameAssemblySize(payloads []vpxrtp.PayloadFragment) (int, error) {
	if len(payloads) == 0 {
		return 0, vpxerrors.ErrInvalidData
	}
	total := 0
	var base PayloadDescriptor
	for i := range payloads {
		desc, fragment, err := ParsePayloadDescriptor(payloads[i].Payload)
		if err != nil {
			return 0, err
		}
		if len(fragment) == 0 {
			return 0, vpxerrors.ErrInvalidData
		}
		if !vpxrtp.MarkerMatchesFragmentIndex(payloads, i) {
			return 0, vpxerrors.ErrInvalidData
		}
		if desc.StartOfPartition != (i == 0) || desc.PartitionID != 0 {
			return 0, vpxerrors.ErrInvalidData
		}
		normalized := desc
		normalized.StartOfPartition = false
		if i == 0 {
			base = normalized
		} else if normalized != base {
			return 0, vpxerrors.ErrInvalidData
		}
		total, err = vpxrtp.AddPayloadSize(total, len(fragment))
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// AssembleFrameInto writes the raw VP8 frame carried by payloads into
// dst and returns the frame length. On [vpxerrors.ErrBufferTooSmall], the returned
// length is the required capacity.
func AssembleFrameInto(dst []byte, payloads []vpxrtp.PayloadFragment) (int, error) {
	need, err := FrameAssemblySize(payloads)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, vpxerrors.ErrBufferTooSmall
	}
	return vpxrtp.AssemblePayloadFragmentsInto(dst, payloads, need, parsePayloadFragment)
}

// AssembleFrame returns the raw VP8 frame carried by an ordered set of
// RTP payload bodies.
func AssembleFrame(payloads []vpxrtp.PayloadFragment) ([]byte, error) {
	need, err := FrameAssemblySize(payloads)
	if err != nil {
		return nil, err
	}
	return vpxrtp.AssemblePayloadFragments(payloads, need, parsePayloadFragment)
}

func parsePayloadFragment(payload []byte) ([]byte, error) {
	_, fragment, err := ParsePayloadDescriptor(payload)
	if err != nil {
		return nil, err
	}
	return fragment, nil
}

func (d PayloadDescriptor) hasExtensions() bool {
	return d.PictureIDPresent || d.TL0PICIDXPresent ||
		d.TemporalIDPresent || d.KeyIndexPresent
}

func (d PayloadDescriptor) validate() error {
	if d.PartitionID > 7 {
		return vpxerrors.ErrInvalidConfig
	}
	if d.PictureID15Bit && !d.PictureIDPresent {
		return vpxerrors.ErrInvalidConfig
	}
	if d.PictureIDPresent {
		if d.PictureID15Bit {
			if d.PictureID > 0x7fff {
				return vpxerrors.ErrInvalidConfig
			}
		} else if d.PictureID > 0x7f {
			return vpxerrors.ErrInvalidConfig
		}
	} else if d.PictureID != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	if d.TL0PICIDXPresent && !d.TemporalIDPresent {
		return vpxerrors.ErrInvalidConfig
	}
	if !d.TL0PICIDXPresent && d.TL0PICIDX != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	if d.TemporalIDPresent {
		if d.TemporalID > 3 {
			return vpxerrors.ErrInvalidConfig
		}
	} else if d.TemporalID != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	if d.LayerSync && !d.TemporalIDPresent && !d.KeyIndexPresent {
		return vpxerrors.ErrInvalidConfig
	}
	if d.KeyIndexPresent {
		if d.KeyIndex > 31 {
			return vpxerrors.ErrInvalidConfig
		}
	} else if d.KeyIndex != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	return nil
}
