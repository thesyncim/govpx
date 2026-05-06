package testutil

import "encoding/binary"

const (
	IVFFileHeaderSize  = 32
	IVFFrameHeaderSize = 12
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
	if fourcc != [4]byte{'V', 'P', '8', '0'} {
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

type errString string

func (e errString) Error() string {
	return string(e)
}
