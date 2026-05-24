package ivf

import (
	"bytes"
	"encoding/binary"
)

const (
	FileHeaderSize  = 32
	FrameHeaderSize = 12
)

var (
	// FourCCVP8 is the IVF stream fourcc used by VP8 payloads.
	FourCCVP8 = [4]byte{'V', 'P', '8', '0'}
	// FourCCVP9 is the IVF stream fourcc used by VP9 payloads.
	FourCCVP9 = [4]byte{'V', 'P', '9', '0'}
)

var (
	ErrInvalid           = errString("govpx: invalid IVF data")
	ErrUnsupportedFourCC = errString("govpx: unsupported IVF fourcc")
)

type Header struct {
	FourCC [4]byte

	Width  int
	Height int

	TimebaseNumerator   uint32
	TimebaseDenominator uint32

	FrameCount uint32
}

type Frame struct {
	Index     int
	Timestamp uint64
	Data      []byte
}

func ParseHeader(data []byte) (Header, error) {
	if len(data) < FileHeaderSize {
		return Header{}, ErrInvalid
	}
	if data[0] != 'D' || data[1] != 'K' || data[2] != 'I' || data[3] != 'F' {
		return Header{}, ErrInvalid
	}
	headerSize := binary.LittleEndian.Uint16(data[6:8])
	if headerSize != FileHeaderSize {
		return Header{}, ErrInvalid
	}

	var fourcc [4]byte
	copy(fourcc[:], data[8:12])
	if fourcc != FourCCVP8 && fourcc != FourCCVP9 {
		return Header{}, ErrUnsupportedFourCC
	}

	width := int(binary.LittleEndian.Uint16(data[12:14]))
	height := int(binary.LittleEndian.Uint16(data[14:16]))
	if width <= 0 || height <= 0 {
		return Header{}, ErrInvalid
	}

	return Header{
		FourCC:              fourcc,
		Width:               width,
		Height:              height,
		TimebaseDenominator: binary.LittleEndian.Uint32(data[16:20]),
		TimebaseNumerator:   binary.LittleEndian.Uint32(data[20:24]),
		FrameCount:          binary.LittleEndian.Uint32(data[24:28]),
	}, nil
}

func FirstFrameOffset(data []byte) (int, error) {
	if _, err := ParseHeader(data); err != nil {
		return 0, err
	}
	return FileHeaderSize, nil
}

func NextFrame(data []byte, offset int, index int) (Frame, int, error) {
	if offset < FileHeaderSize || offset > len(data) {
		return Frame{}, offset, ErrInvalid
	}
	if offset == len(data) {
		return Frame{}, offset, ErrInvalid
	}
	if len(data)-offset < FrameHeaderSize {
		return Frame{}, offset, ErrInvalid
	}

	size := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	timestamp := binary.LittleEndian.Uint64(data[offset+4 : offset+12])
	payloadOff := offset + FrameHeaderSize
	next := payloadOff + size
	if size < 0 || next < payloadOff || next > len(data) {
		return Frame{}, offset, ErrInvalid
	}

	return Frame{
		Index:     index,
		Timestamp: timestamp,
		Data:      data[payloadOff:next],
	}, next, nil
}

func CountFrames(data []byte) (int, error) {
	offset, err := FirstFrameOffset(data)
	if err != nil {
		return 0, err
	}

	count := 0
	for offset < len(data) {
		_, next, err := NextFrame(data, offset, count)
		if err != nil {
			return 0, err
		}
		offset = next
		count++
	}
	return count, nil
}

// Frames returns every frame header and payload slice in data.
func Frames(data []byte) ([]Frame, error) {
	offset, err := FirstFrameOffset(data)
	if err != nil {
		return nil, err
	}
	var frames []Frame
	for i := 0; offset < len(data); i++ {
		frame, next, err := NextFrame(data, offset, i)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
		offset = next
	}
	return frames, nil
}

// FramePayloads returns copies of each frame payload in data.
func FramePayloads(data []byte) ([][]byte, error) {
	frames, err := Frames(data)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(frames))
	for i := range frames {
		out[i] = bytes.Clone(frames[i].Data)
	}
	return out, nil
}

// FramePayloadViews returns each frame payload as a slice backed by data.
func FramePayloadViews(data []byte) ([][]byte, error) {
	frames, err := Frames(data)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(frames))
	for i := range frames {
		out[i] = frames[i].Data
	}
	return out, nil
}

// FramePayloadSizeSummary counts payload bytes and frames without copying.
func FramePayloadSizeSummary(data []byte) (total int, frames int, err error) {
	offset, err := FirstFrameOffset(data)
	if err != nil {
		return 0, 0, err
	}
	for offset < len(data) {
		frame, next, err := NextFrame(data, offset, frames)
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

// WriteHeader writes the 32-byte IVF file header for one of the
// supported codecs ("VP80" or "VP90"). Mirrors the layout libvpx's
// IVF writer emits — required for vpxdec --codec=vp9 to recognize
// the stream.
func WriteHeader(h Header) []byte {
	var buf [FileHeaderSize]byte
	buf[0], buf[1], buf[2], buf[3] = 'D', 'K', 'I', 'F'
	binary.LittleEndian.PutUint16(buf[4:6], 0)                  // version
	binary.LittleEndian.PutUint16(buf[6:8], FileHeaderSize)     // header_size
	copy(buf[8:12], h.FourCC[:])                                // fourcc
	binary.LittleEndian.PutUint16(buf[12:14], uint16(h.Width))  // width
	binary.LittleEndian.PutUint16(buf[14:16], uint16(h.Height)) // height
	binary.LittleEndian.PutUint32(buf[16:20], h.TimebaseDenominator)
	binary.LittleEndian.PutUint32(buf[20:24], h.TimebaseNumerator)
	binary.LittleEndian.PutUint32(buf[24:28], h.FrameCount)
	// buf[28:32] reserved, leave zeroed.
	return buf[:]
}

// WriteFrame writes the 12-byte per-frame header followed by
// the frame payload. Mirrors libvpx's vpxenc IVF emit shape:
//
//	[ size_uint32_le ][ pts_uint64_le ][ payload[size] ]
func WriteFrame(payload []byte, pts uint64) []byte {
	out := make([]byte, FrameHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint64(out[4:12], pts)
	copy(out[12:], payload)
	return out
}

// Build writes one IVF stream with monotonically increasing frame
// timestamps starting at zero.
func Build(h Header, payloads [][]byte) []byte {
	h.FrameCount = uint32(len(payloads))
	size := FileHeaderSize
	for _, payload := range payloads {
		size += FrameHeaderSize + len(payload)
	}
	out := make([]byte, 0, size)
	out = append(out, WriteHeader(h)...)
	for i, payload := range payloads {
		out = append(out, WriteFrame(payload, uint64(i))...)
	}
	return out
}

// BuildVP8 writes a VP8 IVF stream for tests that already have
// compressed frame payloads.
func BuildVP8(width int, height int, den uint32, num uint32, payloads [][]byte) []byte {
	header := Header{
		FourCC:              FourCCVP8,
		Width:               width,
		Height:              height,
		TimebaseDenominator: den,
		TimebaseNumerator:   num,
	}
	return Build(header, payloads)
}

// BuildVP9 writes a VP9 IVF stream for tests that already have
// compressed frame payloads.
func BuildVP9(width int, height int, den uint32, num uint32, payloads [][]byte) []byte {
	header := Header{
		FourCC:              FourCCVP9,
		Width:               width,
		Height:              height,
		TimebaseDenominator: den,
		TimebaseNumerator:   num,
	}
	return Build(header, payloads)
}

// BuildSingleFrameVP8 writes a VP8 IVF stream with one compressed payload.
func BuildSingleFrameVP8(width int, height int, den uint32, num uint32, payload []byte) []byte {
	payloads := [][]byte{payload}
	return BuildVP8(width, height, den, num, payloads)
}

// BuildSingleFrameVP9 writes a VP9 IVF stream with one compressed payload.
func BuildSingleFrameVP9(width int, height int, den uint32, num uint32, payload []byte) []byte {
	payloads := [][]byte{payload}
	return BuildVP9(width, height, den, num, payloads)
}
