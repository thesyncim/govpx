package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func decodeIVFIntoChecksums(t *testing.T, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := testImage(header.Width, header.Height)

	var frames []testutil.FrameChecksum
	outputIndex := 0
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", inputIndex, err)
		}
		if info.KeyFrame && (dst.Width != info.Width || dst.Height != info.Height) {
			dst = testImage(info.Width, info.Height)
		}
		frameInfo, err := d.DecodeInto(frame.Data, &dst)
		if err != nil {
			t.Fatalf("DecodeInto frame %d returned error: %v", inputIndex, err)
		}
		if _, ok := d.NextFrame(); ok {
			t.Fatalf("DecodeInto frame %d queued a NextFrame output", inputIndex)
		}
		if frameInfo.ShowFrame {
			frames = append(frames, checksumFrame(outputIndex, frameInfo.KeyFrame, frameInfo.ShowFrame, dst))
			outputIndex++
		}
		offset = next
	}
	return frames
}

func decodeIVFIntoExpectError(t *testing.T, ivf []byte) error {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		return err
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return err
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		return err
	}
	dst := testImage(header.Width, header.Height)
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			return err
		}
		if info.KeyFrame && (dst.Width != info.Width || dst.Height != info.Height) {
			dst = testImage(info.Width, info.Height)
		}
		if _, err := d.DecodeInto(frame.Data, &dst); err != nil {
			return err
		}
		offset = next
	}
	return nil
}
