package testutil

import (
	"crypto/md5"
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

func TestIVFFramePayloadsReturnsCopies(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2}, {3, 4, 5}})
	payloads, err := IVFFramePayloads(data)
	if err != nil {
		t.Fatalf("IVFFramePayloads returned error: %v", err)
	}
	if len(payloads) != 2 || string(payloads[0]) != string([]byte{1, 2}) ||
		string(payloads[1]) != string([]byte{3, 4, 5}) {
		t.Fatalf("payloads = %v, want [[1 2] [3 4 5]]", payloads)
	}
	payloads[0][0] = 99
	if data[IVFFileHeaderSize+IVFFrameHeaderSize] == 99 {
		t.Fatalf("payload aliases input data")
	}
}

func TestIVFFramePayloadViewsAliasInput(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2}, {3, 4, 5}})
	payloads, err := IVFFramePayloadViews(data)
	if err != nil {
		t.Fatalf("IVFFramePayloadViews returned error: %v", err)
	}
	if len(payloads) != 2 || string(payloads[0]) != string([]byte{1, 2}) ||
		string(payloads[1]) != string([]byte{3, 4, 5}) {
		t.Fatalf("payloads = %v, want [[1 2] [3 4 5]]", payloads)
	}
	payloads[0][0] = 99
	if data[IVFFileHeaderSize+IVFFrameHeaderSize] != 99 {
		t.Fatalf("payload view does not alias input data")
	}
}

func TestIVFFramePayloadSizeSummary(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2}, {3, 4, 5}})
	total, frames, err := IVFFramePayloadSizeSummary(data)
	if err != nil {
		t.Fatalf("IVFFramePayloadSizeSummary returned error: %v", err)
	}
	if total != 5 || frames != 2 {
		t.Fatalf("summary = %d/%d, want 5/2", total, frames)
	}
}

func TestParseIVFRejectsUnsupportedFourCC(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	// VP80 and VP90 are both accepted; pick an unrelated fourcc to
	// keep this test's rejection path biting.
	copy(data[8:12], []byte("AV01"))

	_, err := ParseIVFHeader(data)
	if !errors.Is(err, ErrUnsupportedFourCC) {
		t.Fatalf("error = %v, want ErrUnsupportedFourCC", err)
	}
}

func TestParseIVFAcceptsVP90(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	copy(data[8:12], []byte("VP90"))

	hdr, err := ParseIVFHeader(data)
	if err != nil {
		t.Fatalf("VP90 IVF: %v", err)
	}
	if hdr.FourCC != [4]byte{'V', 'P', '9', '0'} {
		t.Errorf("FourCC = %s, want VP90", hdr.FourCC[:])
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
	return BuildIVF(IVFHeader{
		FourCC:              IVFFourCCVP8,
		Width:               width,
		Height:              height,
		TimebaseDenominator: den,
		TimebaseNumerator:   num,
	}, frames)
}
