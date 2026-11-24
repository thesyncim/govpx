package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func assertFrameChecksumsEqual(t *testing.T, label string, got []testutil.FrameChecksum, want []testutil.FrameChecksum) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s frame count = %d, want %d from libvpx", label, len(got), len(want))
	}
	for i := range want {
		if !testutil.SameFrameChecksum(got[i], want[i]) {
			t.Fatalf("%s frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", label, i, formatChecksum(want[i]), formatChecksum(got[i]))
		}
	}
}

func decodeIVFChecksums(t *testing.T, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	return decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{})
}

func decodeIVFChecksumsWithOptions(t *testing.T, ivf []byte, opts DecoderOptions) []testutil.FrameChecksum {
	t.Helper()
	return decodeIVFChecksumsWithControlScript(t, ivf, opts, nil)
}

func decodeIVFChecksumsWithControlScript(t *testing.T, ivf []byte, opts DecoderOptions, apply map[int]func(*testing.T, *VP8Decoder)) []testutil.FrameChecksum {
	t.Helper()
	if _, err := testutil.ParseIVFHeader(ivf); err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	defer d.Close()

	var frames []testutil.FrameChecksum
	outputIndex := 0
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		if fn := apply[inputIndex]; fn != nil {
			fn(t, d)
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", inputIndex, err)
		}
		info := d.lastInfo
		img, ok := d.NextFrame()
		if info.ShowFrame {
			if !ok {
				t.Fatalf("NextFrame frame %d returned no frame", inputIndex)
			}
			frames = append(frames, checksumFrame(outputIndex, info.KeyFrame, info.ShowFrame, img))
			outputIndex++
		} else if ok {
			t.Fatalf("NextFrame frame %d returned an invisible frame", inputIndex)
		}
		offset = next
	}
	return frames
}

func decodeIVFExpectError(t *testing.T, ivf []byte, opts DecoderOptions) error {
	t.Helper()
	if _, err := testutil.ParseIVFHeader(ivf); err != nil {
		return err
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return err
	}
	d, err := NewVP8Decoder(opts)
	if err != nil {
		return err
	}
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		if _, err := PeekVP8StreamInfo(frame.Data); err != nil {
			return err
		}
		if err := d.Decode(frame.Data); err != nil {
			return err
		}
		_, _ = d.NextFrame()
		offset = next
	}
	return nil
}
