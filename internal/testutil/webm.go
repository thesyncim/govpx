package testutil

import (
	"errors"
	"fmt"
	"io"
)

const (
	webmIDSegment     = 0x18538067
	webmIDTracks      = 0x1654AE6B
	webmIDTrackEntry  = 0xAE
	webmIDTrackNumber = 0xD7
	webmIDTrackType   = 0x83
	webmIDCodecID     = 0x86
	webmIDCluster     = 0x1F43B675
	webmIDSimpleBlock = 0xA3
	webmIDBlockGroup  = 0xA0
	webmIDBlock       = 0xA1
)

type webmElement struct {
	id        uint64
	dataStart int
	dataEnd   int
}

type webmTrackEntry struct {
	number uint64
	video  bool
	codec  string
}

func ExtractVP9WebMPackets(data []byte) ([][]byte, error) {
	tracks := make(map[uint64]bool)
	if err := walkWebMElements(data, 0, len(data), func(elem webmElement) error {
		if elem.id != webmIDTrackEntry {
			return nil
		}
		track, err := parseWebMTrackEntry(data[elem.dataStart:elem.dataEnd])
		if err != nil {
			return err
		}
		if track.number != 0 && track.video && track.codec == "V_VP9" {
			tracks[track.number] = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, errors.New("no V_VP9 video track found")
	}

	var packets [][]byte
	if err := walkWebMElements(data, 0, len(data), func(elem webmElement) error {
		if elem.id != webmIDSimpleBlock && elem.id != webmIDBlock {
			return nil
		}
		track, frames, err := parseWebMBlock(data[elem.dataStart:elem.dataEnd])
		if err != nil {
			return err
		}
		if !tracks[track] {
			return nil
		}
		packets = append(packets, frames...)
		return nil
	}); err != nil {
		return nil, err
	}
	return packets, nil
}

func walkWebMElements(data []byte, start, end int, visit func(webmElement) error) error {
	for pos := start; pos < end; {
		elem, next, err := readWebMElement(data, pos, end)
		if err != nil {
			return err
		}
		if err := visit(elem); err != nil {
			return err
		}
		if isWebMMasterElement(elem.id) {
			if err := walkWebMElements(data, elem.dataStart, elem.dataEnd, visit); err != nil {
				return err
			}
		}
		pos = next
	}
	return nil
}

func isWebMMasterElement(id uint64) bool {
	switch id {
	case webmIDSegment, webmIDTracks, webmIDTrackEntry, webmIDCluster, webmIDBlockGroup:
		return true
	default:
		return false
	}
}

func readWebMElement(data []byte, pos, limit int) (webmElement, int, error) {
	id, idLen, err := readWebMID(data, pos, limit)
	if err != nil {
		return webmElement{}, 0, err
	}
	size, sizeLen, unknown, err := readWebMSize(data, pos+idLen, limit)
	if err != nil {
		return webmElement{}, 0, err
	}
	dataStart := pos + idLen + sizeLen
	dataEnd := limit
	if !unknown {
		if size > uint64(limit-dataStart) {
			return webmElement{}, 0, fmt.Errorf("WebM element 0x%x size exceeds input", id)
		}
		dataEnd = dataStart + int(size)
	}
	return webmElement{id: id, dataStart: dataStart, dataEnd: dataEnd}, dataEnd, nil
}

func readWebMID(data []byte, pos, limit int) (uint64, int, error) {
	if pos >= limit {
		return 0, 0, io.ErrUnexpectedEOF
	}
	first := data[pos]
	mask := byte(0x80)
	length := 1
	for length <= 4 && first&mask == 0 {
		mask >>= 1
		length++
	}
	if length > 4 || pos+length > limit {
		return 0, 0, errors.New("invalid WebM element id")
	}
	var id uint64
	for i := 0; i < length; i++ {
		id = (id << 8) | uint64(data[pos+i])
	}
	return id, length, nil
}

func readWebMSize(data []byte, pos, limit int) (uint64, int, bool, error) {
	value, length, err := readWebMVint(data, pos, limit)
	if err != nil {
		return 0, 0, false, err
	}
	unknown := value == (uint64(1)<<(7*length))-1
	return value, length, unknown, nil
}

func readWebMVint(data []byte, pos, limit int) (uint64, int, error) {
	if pos >= limit {
		return 0, 0, io.ErrUnexpectedEOF
	}
	first := data[pos]
	mask := byte(0x80)
	length := 1
	for length <= 8 && first&mask == 0 {
		mask >>= 1
		length++
	}
	if length > 8 || pos+length > limit {
		return 0, 0, errors.New("invalid WebM vint")
	}
	value := uint64(first & ^mask)
	for i := 1; i < length; i++ {
		value = (value << 8) | uint64(data[pos+i])
	}
	return value, length, nil
}

func readWebMSignedVint(data []byte, pos, limit int) (int64, int, error) {
	value, length, err := readWebMVint(data, pos, limit)
	if err != nil {
		return 0, 0, err
	}
	bias := (int64(1) << (7*length - 1)) - 1
	return int64(value) - bias, length, nil
}

func parseWebMTrackEntry(data []byte) (webmTrackEntry, error) {
	var track webmTrackEntry
	if err := walkWebMTrackEntryFields(data, func(elem webmElement) error {
		switch elem.id {
		case webmIDTrackNumber:
			track.number = readWebMUnsigned(data[elem.dataStart:elem.dataEnd])
		case webmIDTrackType:
			track.video = readWebMUnsigned(data[elem.dataStart:elem.dataEnd]) == 1
		case webmIDCodecID:
			track.codec = string(data[elem.dataStart:elem.dataEnd])
		}
		return nil
	}); err != nil {
		return webmTrackEntry{}, err
	}
	return track, nil
}

func walkWebMTrackEntryFields(data []byte, visit func(webmElement) error) error {
	for pos := 0; pos < len(data); {
		elem, next, err := readWebMElement(data, pos, len(data))
		if err != nil {
			return err
		}
		if err := visit(elem); err != nil {
			return err
		}
		pos = next
	}
	return nil
}

func readWebMUnsigned(data []byte) uint64 {
	var value uint64
	for _, b := range data {
		value = (value << 8) | uint64(b)
	}
	return value
}

func parseWebMBlock(data []byte) (uint64, [][]byte, error) {
	track, n, err := readWebMVint(data, 0, len(data))
	if err != nil {
		return 0, nil, err
	}
	if n+3 > len(data) {
		return 0, nil, io.ErrUnexpectedEOF
	}
	flags := data[n+2]
	frames, err := splitWebMBlockFrames(data[n+3:], int((flags&0x06)>>1))
	if err != nil {
		return 0, nil, err
	}
	return track, frames, nil
}

func splitWebMBlockFrames(data []byte, lacing int) ([][]byte, error) {
	switch lacing {
	case 0:
		return [][]byte{data}, nil
	case 1:
		return splitWebMXiphLacedFrames(data)
	case 2:
		return splitWebMFixedLacedFrames(data)
	case 3:
		return splitWebMEBMLLacedFrames(data)
	default:
		return nil, errors.New("invalid WebM lacing mode")
	}
}

func splitWebMXiphLacedFrames(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	frameCount := int(data[0]) + 1
	pos := 1
	sizes := make([]int, frameCount)
	for i := 0; i < frameCount-1; i++ {
		for {
			if pos >= len(data) {
				return nil, io.ErrUnexpectedEOF
			}
			b := int(data[pos])
			pos++
			sizes[i] += b
			if b != 255 {
				break
			}
		}
	}
	return sliceWebMLacedFrames(data[pos:], sizes)
}

func splitWebMFixedLacedFrames(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	frameCount := int(data[0]) + 1
	if (len(data)-1)%frameCount != 0 {
		return nil, errors.New("invalid fixed-size WebM lacing")
	}
	size := (len(data) - 1) / frameCount
	sizes := make([]int, frameCount)
	for i := range sizes {
		sizes[i] = size
	}
	return sliceWebMLacedFrames(data[1:], sizes)
}

func splitWebMEBMLLacedFrames(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	frameCount := int(data[0]) + 1
	pos := 1
	sizes := make([]int, frameCount)
	first, n, err := readWebMVint(data, pos, len(data))
	if err != nil {
		return nil, err
	}
	pos += n
	sizes[0] = int(first)
	prev := int64(first)
	for i := 1; i < frameCount-1; i++ {
		delta, n, err := readWebMSignedVint(data, pos, len(data))
		if err != nil {
			return nil, err
		}
		pos += n
		prev += delta
		if prev < 0 {
			return nil, errors.New("invalid negative WebM lace size")
		}
		sizes[i] = int(prev)
	}
	return sliceWebMLacedFrames(data[pos:], sizes)
}

func sliceWebMLacedFrames(data []byte, sizes []int) ([][]byte, error) {
	if len(sizes) == 0 {
		return nil, errors.New("missing WebM lace sizes")
	}
	total := 0
	for _, size := range sizes[:len(sizes)-1] {
		if size < 0 || size > len(data)-total {
			return nil, errors.New("invalid WebM lace size")
		}
		total += size
	}
	sizes[len(sizes)-1] = len(data) - total
	frames := make([][]byte, len(sizes))
	pos := 0
	for i, size := range sizes {
		if size < 0 || pos+size > len(data) {
			return nil, errors.New("invalid WebM lace frame bounds")
		}
		frames[i] = data[pos : pos+size]
		pos += size
	}
	return frames, nil
}
