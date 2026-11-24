package ivf

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseHeader(t *testing.T) {
	data := makeIVF(320, 240, 30, 1, [][]byte{{1, 2, 3}})

	header, err := ParseHeader(data)
	if err != nil {
		t.Fatalf("ParseHeader returned error: %v", err)
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

func TestNextFrame(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2, 3}, {4, 5}})
	offset, err := FirstFrameOffset(data)
	if err != nil {
		t.Fatalf("FirstFrameOffset returned error: %v", err)
	}

	frame, next, err := NextFrame(data, offset, 0)
	if err != nil {
		t.Fatalf("NextFrame returned error: %v", err)
	}
	if frame.Index != 0 || frame.Timestamp != 0 {
		t.Fatalf("frame index/timestamp = %d/%d, want 0/0", frame.Index, frame.Timestamp)
	}
	if string(frame.Data) != string([]byte{1, 2, 3}) {
		t.Fatalf("frame data = %v, want [1 2 3]", frame.Data)
	}

	frame, _, err = NextFrame(data, next, 1)
	if err != nil {
		t.Fatalf("second NextFrame returned error: %v", err)
	}
	if frame.Index != 1 || frame.Timestamp != 1 {
		t.Fatalf("second frame index/timestamp = %d/%d, want 1/1", frame.Index, frame.Timestamp)
	}
	if string(frame.Data) != string([]byte{4, 5}) {
		t.Fatalf("second frame data = %v, want [4 5]", frame.Data)
	}
}

func TestCountFrames(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}, {2}, {3}})
	count, err := CountFrames(data)
	if err != nil {
		t.Fatalf("CountFrames returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestFramePayloadsReturnsCopies(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2}, {3, 4, 5}})
	payloads, err := FramePayloads(data)
	if err != nil {
		t.Fatalf("FramePayloads returned error: %v", err)
	}
	if len(payloads) != 2 || string(payloads[0]) != string([]byte{1, 2}) ||
		string(payloads[1]) != string([]byte{3, 4, 5}) {
		t.Fatalf("payloads = %v, want [[1 2] [3 4 5]]", payloads)
	}
	payloads[0][0] = 99
	if data[FileHeaderSize+FrameHeaderSize] == 99 {
		t.Fatalf("payload aliases input data")
	}
}

func TestFramePayloadViewsAliasInput(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2}, {3, 4, 5}})
	payloads, err := FramePayloadViews(data)
	if err != nil {
		t.Fatalf("FramePayloadViews returned error: %v", err)
	}
	if len(payloads) != 2 || string(payloads[0]) != string([]byte{1, 2}) ||
		string(payloads[1]) != string([]byte{3, 4, 5}) {
		t.Fatalf("payloads = %v, want [[1 2] [3 4 5]]", payloads)
	}
	payloads[0][0] = 99
	if data[FileHeaderSize+FrameHeaderSize] != 99 {
		t.Fatalf("payload view does not alias input data")
	}
}

func TestFramePayloadSizeSummary(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1, 2}, {3, 4, 5}})
	total, frames, err := FramePayloadSizeSummary(data)
	if err != nil {
		t.Fatalf("FramePayloadSizeSummary returned error: %v", err)
	}
	if total != 5 || frames != 2 {
		t.Fatalf("summary = %d/%d, want 5/2", total, frames)
	}
}

func TestBuildVP8(t *testing.T) {
	data := BuildVP8(16, 16, 30, 1, [][]byte{{1, 2}, {3}})
	header, err := ParseHeader(data)
	if err != nil {
		t.Fatalf("ParseHeader returned error: %v", err)
	}
	if header.FourCC != FourCCVP8 || header.Width != 16 || header.Height != 16 ||
		header.TimebaseDenominator != 30 || header.TimebaseNumerator != 1 ||
		header.FrameCount != 2 {
		t.Fatalf("header = %+v", header)
	}
	payloads, err := FramePayloads(data)
	if err != nil {
		t.Fatalf("FramePayloads returned error: %v", err)
	}
	if !bytes.Equal(payloads[0], []byte{1, 2}) || !bytes.Equal(payloads[1], []byte{3}) {
		t.Fatalf("payloads = %v", payloads)
	}
}

func TestBuildVP9(t *testing.T) {
	data := BuildVP9(16, 16, 30, 1, [][]byte{{1, 2}, {3}})
	header, err := ParseHeader(data)
	if err != nil {
		t.Fatalf("ParseHeader returned error: %v", err)
	}
	if header.FourCC != FourCCVP9 || header.Width != 16 || header.Height != 16 ||
		header.TimebaseDenominator != 30 || header.TimebaseNumerator != 1 ||
		header.FrameCount != 2 {
		t.Fatalf("header = %+v", header)
	}
	payloads, err := FramePayloads(data)
	if err != nil {
		t.Fatalf("FramePayloads returned error: %v", err)
	}
	if !bytes.Equal(payloads[0], []byte{1, 2}) || !bytes.Equal(payloads[1], []byte{3}) {
		t.Fatalf("payloads = %v", payloads)
	}
}

func TestParseIVFRejectsUnsupportedFourCC(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	// VP80 and VP90 are both accepted; pick an unrelated fourcc to
	// keep this test's rejection path biting.
	copy(data[8:12], []byte("AV01"))

	_, err := ParseHeader(data)
	if !errors.Is(err, ErrUnsupportedFourCC) {
		t.Fatalf("error = %v, want ErrUnsupportedFourCC", err)
	}
}

func TestParseIVFAcceptsVP90(t *testing.T) {
	data := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	copy(data[8:12], []byte("VP90"))

	hdr, err := ParseHeader(data)
	if err != nil {
		t.Fatalf("VP90 IVF: %v", err)
	}
	if hdr.FourCC != [4]byte{'V', 'P', '9', '0'} {
		t.Errorf("FourCC = %s, want VP90", hdr.FourCC[:])
	}
}

func makeIVF(width int, height int, den uint32, num uint32, frames [][]byte) []byte {
	return Build(Header{
		FourCC:              FourCCVP8,
		Width:               width,
		Height:              height,
		TimebaseDenominator: den,
		TimebaseNumerator:   num,
	}, frames)
}
