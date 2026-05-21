package testutil

import (
	"bytes"
	"encoding/binary"
)

const (
	IVFFileHeaderSize  = 32
	IVFFrameHeaderSize = 12
)

var (
	// IVFFourCCVP8 is the IVF stream fourcc used by VP8 payloads.
	IVFFourCCVP8 = [4]byte{'V', 'P', '8', '0'}
	// IVFFourCCVP9 is the IVF stream fourcc used by VP9 payloads.
	IVFFourCCVP9 = [4]byte{'V', 'P', '9', '0'}
)

var (
	ErrInvalidIVF        = errString("govpx: invalid IVF data")
	ErrUnsupportedFourCC = errString("govpx: unsupported IVF fourcc")
)

type IVFHeader struct {
	FourCC [4]byte

	Width  int
	Height int

	TimebaseNumerator   uint32
	TimebaseDenominator uint32

	FrameCount uint32
}

type IVFFrame struct {
	Index     int
	Timestamp uint64
	Data      []byte
}

func ParseIVFHeader(data []byte) (IVFHeader, error) {
	if len(data) < IVFFileHeaderSize {
		return IVFHeader{}, ErrInvalidIVF
	}
	if data[0] != 'D' || data[1] != 'K' || data[2] != 'I' || data[3] != 'F' {
		return IVFHeader{}, ErrInvalidIVF
	}
	headerSize := binary.LittleEndian.Uint16(data[6:8])
	if headerSize != IVFFileHeaderSize {
		return IVFHeader{}, ErrInvalidIVF
	}

	var fourcc [4]byte
	copy(fourcc[:], data[8:12])
	if fourcc != IVFFourCCVP8 && fourcc != IVFFourCCVP9 {
		return IVFHeader{}, ErrUnsupportedFourCC
	}

	width := int(binary.LittleEndian.Uint16(data[12:14]))
	height := int(binary.LittleEndian.Uint16(data[14:16]))
	if width <= 0 || height <= 0 {
		return IVFHeader{}, ErrInvalidIVF
	}

	return IVFHeader{
		FourCC:              fourcc,
		Width:               width,
		Height:              height,
		TimebaseDenominator: binary.LittleEndian.Uint32(data[16:20]),
		TimebaseNumerator:   binary.LittleEndian.Uint32(data[20:24]),
		FrameCount:          binary.LittleEndian.Uint32(data[24:28]),
	}, nil
}

func FirstIVFFrameOffset(data []byte) (int, error) {
	if _, err := ParseIVFHeader(data); err != nil {
		return 0, err
	}
	return IVFFileHeaderSize, nil
}

func NextIVFFrame(data []byte, offset int, index int) (IVFFrame, int, error) {
	if offset < IVFFileHeaderSize || offset > len(data) {
		return IVFFrame{}, offset, ErrInvalidIVF
	}
	if offset == len(data) {
		return IVFFrame{}, offset, ErrInvalidIVF
	}
	if len(data)-offset < IVFFrameHeaderSize {
		return IVFFrame{}, offset, ErrInvalidIVF
	}

	size := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	timestamp := binary.LittleEndian.Uint64(data[offset+4 : offset+12])
	payloadOff := offset + IVFFrameHeaderSize
	next := payloadOff + size
	if size < 0 || next < payloadOff || next > len(data) {
		return IVFFrame{}, offset, ErrInvalidIVF
	}

	return IVFFrame{
		Index:     index,
		Timestamp: timestamp,
		Data:      data[payloadOff:next],
	}, next, nil
}

func CountIVFFrames(data []byte) (int, error) {
	offset, err := FirstIVFFrameOffset(data)
	if err != nil {
		return 0, err
	}

	count := 0
	for offset < len(data) {
		_, next, err := NextIVFFrame(data, offset, count)
		if err != nil {
			return 0, err
		}
		offset = next
		count++
	}
	return count, nil
}

// IVFFrames returns every frame header and payload slice in data.
func IVFFrames(data []byte) ([]IVFFrame, error) {
	offset, err := FirstIVFFrameOffset(data)
	if err != nil {
		return nil, err
	}
	var frames []IVFFrame
	for i := 0; offset < len(data); i++ {
		frame, next, err := NextIVFFrame(data, offset, i)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
		offset = next
	}
	return frames, nil
}

// IVFFramePayloads returns copies of each frame payload in data.
func IVFFramePayloads(data []byte) ([][]byte, error) {
	frames, err := IVFFrames(data)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(frames))
	for i := range frames {
		out[i] = bytes.Clone(frames[i].Data)
	}
	return out, nil
}

// IVFFramePayloadViews returns each frame payload as a slice backed by data.
func IVFFramePayloadViews(data []byte) ([][]byte, error) {
	frames, err := IVFFrames(data)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(frames))
	for i := range frames {
		out[i] = frames[i].Data
	}
	return out, nil
}

// IVFFramePayloadSizeSummary counts payload bytes and frames without copying.
func IVFFramePayloadSizeSummary(data []byte) (total int, frames int, err error) {
	offset, err := FirstIVFFrameOffset(data)
	if err != nil {
		return 0, 0, err
	}
	for offset < len(data) {
		frame, next, err := NextIVFFrame(data, offset, frames)
		if err != nil {
			return 0, 0, err
		}
		total += len(frame.Data)
		frames++
		offset = next
	}
	return total, frames, nil
}

type errString string

func (e errString) Error() string {
	return string(e)
}

// WriteIVFHeader writes the 32-byte IVF file header for one of the
// supported codecs ("VP80" or "VP90"). Mirrors the layout libvpx's
// IVF writer emits — required for vpxdec --codec=vp9 to recognize
// the stream.
func WriteIVFHeader(h IVFHeader) []byte {
	var buf [IVFFileHeaderSize]byte
	buf[0], buf[1], buf[2], buf[3] = 'D', 'K', 'I', 'F'
	binary.LittleEndian.PutUint16(buf[4:6], 0)                  // version
	binary.LittleEndian.PutUint16(buf[6:8], IVFFileHeaderSize)  // header_size
	copy(buf[8:12], h.FourCC[:])                                // fourcc
	binary.LittleEndian.PutUint16(buf[12:14], uint16(h.Width))  // width
	binary.LittleEndian.PutUint16(buf[14:16], uint16(h.Height)) // height
	binary.LittleEndian.PutUint32(buf[16:20], h.TimebaseDenominator)
	binary.LittleEndian.PutUint32(buf[20:24], h.TimebaseNumerator)
	binary.LittleEndian.PutUint32(buf[24:28], h.FrameCount)
	// buf[28:32] reserved, leave zeroed.
	return buf[:]
}

// WriteIVFFrame writes the 12-byte per-frame header followed by
// the frame payload. Mirrors libvpx's vpxenc IVF emit shape:
//
//	[ size_uint32_le ][ pts_uint64_le ][ payload[size] ]
func WriteIVFFrame(payload []byte, pts uint64) []byte {
	out := make([]byte, IVFFrameHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint64(out[4:12], pts)
	copy(out[12:], payload)
	return out
}

// BuildIVF writes one IVF stream with monotonically increasing frame
// timestamps starting at zero.
func BuildIVF(h IVFHeader, payloads [][]byte) []byte {
	h.FrameCount = uint32(len(payloads))
	size := IVFFileHeaderSize
	for _, payload := range payloads {
		size += IVFFrameHeaderSize + len(payload)
	}
	out := make([]byte, 0, size)
	out = append(out, WriteIVFHeader(h)...)
	for i, payload := range payloads {
		out = append(out, WriteIVFFrame(payload, uint64(i))...)
	}
	return out
}

// BuildVP8IVF writes a VP8 IVF stream for tests that already have
// compressed frame payloads.
func BuildVP8IVF(width int, height int, den uint32, num uint32, payloads [][]byte) []byte {
	header := IVFHeader{
		FourCC:              IVFFourCCVP8,
		Width:               width,
		Height:              height,
		TimebaseDenominator: den,
		TimebaseNumerator:   num,
	}
	return BuildIVF(header, payloads)
}

// BuildSingleFrameVP8IVF writes a VP8 IVF stream with one compressed payload.
func BuildSingleFrameVP8IVF(width int, height int, den uint32, num uint32, payload []byte) []byte {
	payloads := [][]byte{payload}
	return BuildVP8IVF(width, height, den, num, payloads)
}
