package testutil

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseIVFHeader(t *testing.T) {
	data := makeIVF(320, 240, 30, 1, [][]byte{{1, 2, 3}})

	header, err := ParseIVFHeader(data)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	if header.FourCC != [4]byte{'V', 'P', '8', '0'} {
		t.Fatalf("FourCC = %q, want VP80", header.FourCC)
	}
	if header.Width != 320 || header.Height != 240 {
		t.Fatalf("dimensions = %dx%d, want 320x240", header.Width, header.Height)
	}
	if header.TimebaseDenominator != 30 || header.TimebaseNumerator != 1 {
		t.Fatalf("timebase = %d/%d, want 30/1", header.TimebaseDenominator, header.TimebaseNumerator)
	}
	if header.FrameCount != 1 {
		t.Fatalf("FrameCount = %d, want 1", header.FrameCount)
	}
}

func TestNextIVFFrame(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2, 3}, {4, 5}})
	offset, err := FirstIVFFrameOffset(data)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}

	frame, next, err := NextIVFFrame(data, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame returned error: %v", err)
	}
	if frame.Index != 0 || frame.Timestamp != 0 {
		t.Fatalf("frame index/timestamp = %d/%d, want 0/0", frame.Index, frame.Timestamp)
	}
	if string(frame.Data) != string([]byte{1, 2, 3}) {
		t.Fatalf("frame data = %v, want [1 2 3]", frame.Data)
	}

	frame, _, err = NextIVFFrame(data, next, 1)
	if err != nil {
		t.Fatalf("second NextIVFFrame returned error: %v", err)
	}
	if frame.Index != 1 || frame.Timestamp != 1 {
		t.Fatalf("second frame index/timestamp = %d/%d, want 1/1", frame.Index, frame.Timestamp)
	}
	if string(frame.Data) != string([]byte{4, 5}) {
		t.Fatalf("second frame data = %v, want [4 5]", frame.Data)
	}
}

func TestCountIVFFrames(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}, {2}, {3}})
	count, err := CountIVFFrames(data)
	if err != nil {
		t.Fatalf("CountIVFFrames returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestParseIVFRejectsUnsupportedFourCC(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	copy(data[8:12], []byte("VP90"))

	_, err := ParseIVFHeader(data)
	if !errors.Is(err, ErrUnsupportedFourCC) {
		t.Fatalf("error = %v, want ErrUnsupportedFourCC", err)
	}
}

func TestMD5Plane(t *testing.T) {
	plane := []byte{
		1, 2, 3, 99,
		4, 5, 6, 99,
	}
	sum := MD5Plane(plane, 4, 3, 2)
	want := md5.Sum([]byte{1, 2, 3, 4, 5, 6})
	if sum != want {
		t.Fatalf("sum = %x, want %x", sum, want)
	}
	if MD5Hex(sum) != "6ac1e56bc78f031059be7be854522c4c" {
		t.Fatalf("MD5Hex = %s", MD5Hex(sum))
	}
}

func makeIVF(width int, height int, den uint32, num uint32, frames [][]byte) []byte {
	size := IVFFileHeaderSize
	for _, frame := range frames {
		size += IVFFrameHeaderSize + len(frame)
	}
	data := make([]byte, size)
	copy(data[0:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(data[4:6], 0)
	binary.LittleEndian.PutUint16(data[6:8], IVFFileHeaderSize)
	copy(data[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(data[12:14], uint16(width))
	binary.LittleEndian.PutUint16(data[14:16], uint16(height))
	binary.LittleEndian.PutUint32(data[16:20], den)
	binary.LittleEndian.PutUint32(data[20:24], num)
	binary.LittleEndian.PutUint32(data[24:28], uint32(len(frames)))

	offset := IVFFileHeaderSize
	for i, frame := range frames {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(len(frame)))
		binary.LittleEndian.PutUint64(data[offset+4:offset+12], uint64(i))
		copy(data[offset+IVFFrameHeaderSize:], frame)
		offset += IVFFrameHeaderSize + len(frame)
	}
	return data
}
