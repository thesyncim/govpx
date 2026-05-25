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
			pidSize, err := d.pictureID().Size()
			if err != nil {
				return 0, err
			}
			size += pidSize
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
			n, err := d.pictureID().MarshalInto(dst[off:])
			if err != nil {
				return 0, err
			}
			off += n
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
	return vpxrtp.MarshalDescriptor(d)
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
		pid, n, err := vpxrtp.ParsePictureID(packet[off:], vpxerrors.ErrInvalidData)
		if err != nil {
			return PayloadDescriptor{}, nil, err
		}
		d.setPictureID(pid)
		off += n
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
	descSize, err := desc.Size()
	if err != nil {
		return 0, 0, err
	}
	return vpxrtp.PacketizeFrameInto(dst, payloadBuf, frame, mtu,
		packets, totalBytes, vp8PacketDescriptorForFragment(desc, descSize))
}

// PacketizeFrame returns RTP payload bodies for one raw VP8 frame.
// Payloads do not include RTP headers; Marker is true only on the last body.
func PacketizeFrame(desc PayloadDescriptor, frame []byte, mtu int) ([]vpxrtp.PayloadFragment, error) {
	packets, totalBytes, err := FramePacketizationSize(desc, frame, mtu)
	if err != nil {
		return nil, err
	}
	descSize, err := desc.Size()
	if err != nil {
		return nil, err
	}
	return vpxrtp.PacketizeFrame(frame, mtu, packets, totalBytes,
		vp8PacketDescriptorForFragment(desc, descSize))
}

func vp8PacketDescriptorForFragment(desc PayloadDescriptor, descSize int) vpxrtp.PacketDescriptor[PayloadDescriptor] {
	return func(i, _ int) (PayloadDescriptor, int, error) {
		packetDesc := desc
		packetDesc.StartOfPartition = i == 0
		packetDesc.PartitionID = 0
		return packetDesc, descSize, nil
	}
}

// FrameAssemblySize validates an ordered set of VP8 RTP payload bodies
// for one frame and returns the raw VP8 frame size.
//
// The caller owns RTP sequence-number validation, loss handling, and jitter
// buffering. Payloads must be in decode order and must include the marker bit
// value from each RTP header.
func FrameAssemblySize(payloads []vpxrtp.PayloadFragment) (int, error) {
	return vpxrtp.FrameAssemblySize(payloads, vpxerrors.ErrInvalidData,
		ParsePayloadDescriptor, vp8FrameAssemblyValidator())
}

// AssembleFrameInto writes the raw VP8 frame carried by payloads into
// dst and returns the frame length. On [vpxerrors.ErrBufferTooSmall], the returned
// length is the required capacity.
func AssembleFrameInto(dst []byte, payloads []vpxrtp.PayloadFragment) (int, error) {
	return vpxrtp.AssembleFrameInto(dst, payloads, vpxerrors.ErrInvalidData,
		ParsePayloadDescriptor, vp8FrameAssemblyValidator())
}

// AssembleFrame returns the raw VP8 frame carried by an ordered set of
// RTP payload bodies.
func AssembleFrame(payloads []vpxrtp.PayloadFragment) ([]byte, error) {
	return vpxrtp.AssembleFrame(payloads, vpxerrors.ErrInvalidData,
		ParsePayloadDescriptor, vp8FrameAssemblyValidator())
}

func vp8FrameAssemblyValidator() vpxrtp.FragmentValidator[PayloadDescriptor] {
	var base PayloadDescriptor
	return func(i, _ int, desc PayloadDescriptor) error {
		if desc.StartOfPartition != (i == 0) || desc.PartitionID != 0 {
			return vpxerrors.ErrInvalidData
		}
		normalized := desc
		normalized.StartOfPartition = false
		if i == 0 {
			base = normalized
			return nil
		}
		if normalized != base {
			return vpxerrors.ErrInvalidData
		}
		return nil
	}
}

func (d PayloadDescriptor) hasExtensions() bool {
	return d.PictureIDPresent || d.TL0PICIDXPresent ||
		d.TemporalIDPresent || d.KeyIndexPresent
}

func (d PayloadDescriptor) pictureID() vpxrtp.PictureID {
	return vpxrtp.PictureID{
		Present:    d.PictureIDPresent,
		Value:      d.PictureID,
		FifteenBit: d.PictureID15Bit,
	}
}

func (d *PayloadDescriptor) setPictureID(pid vpxrtp.PictureID) {
	d.PictureIDPresent = pid.Present
	d.PictureID = pid.Value
	d.PictureID15Bit = pid.FifteenBit
}

func (d PayloadDescriptor) validate() error {
	if d.PartitionID > 7 {
		return vpxerrors.ErrInvalidConfig
	}
	if err := d.pictureID().Validate(); err != nil {
		return err
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
