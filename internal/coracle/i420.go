package coracle

import (
	"errors"
	"fmt"

	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func validateI420Raw(codec string, raw []byte, width int, height int, frames int) error {
	frameSize, err := i420FrameSize(codec, width, height)
	if err != nil {
		return err
	}
	if frames <= 0 {
		return fmt.Errorf("coracle: %s frame count %d must be positive", codec, frames)
	}
	want, err := checkedI420Mul(codec, frameSize, frames)
	if err != nil {
		return err
	}
	if len(raw) != want {
		return fmt.Errorf("coracle: %s raw I420 size = %d, want %d for %dx%d x %d frames",
			codec, len(raw), want, width, height, frames)
	}
	return nil
}

// validateI420RawFrameSizes validates that raw holds exactly one I420 frame for
// each width/height pair in frameSizes, concatenated in order. It is the
// variable-frame-size counterpart to validateI420Raw and is used for runtime
// resize parity runs where successive input frames change dimensions.
func validateI420RawFrameSizes(codec string, raw []byte, frameSizes [][2]int) error {
	if len(frameSizes) == 0 {
		return fmt.Errorf("coracle: %s variable frame-size run has no frames", codec)
	}
	want := 0
	for _, size := range frameSizes {
		frameSize, err := i420FrameSize(codec, size[0], size[1])
		if err != nil {
			return err
		}
		want, err = checkedI420Add(codec, want, frameSize)
		if err != nil {
			return err
		}
	}
	if len(raw) != want {
		return fmt.Errorf("coracle: %s variable frame-size raw I420 size = %d, want %d for %d frames",
			codec, len(raw), want, len(frameSizes))
	}
	return nil
}

func i420FrameSize(codec string, width int, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("coracle: invalid %s dimensions %dx%d", codec, width, height)
	}
	size, ok := buffers.I420FrameSize(width, height)
	if !ok {
		return 0, errors.New("coracle: " + codec + " I420 size overflows int")
	}
	return size, nil
}

func checkedI420Mul(codec string, a int, b int) (int, error) {
	if a != 0 && b > int(^uint(0)>>1)/a {
		return 0, errors.New("coracle: " + codec + " I420 size overflows int")
	}
	return a * b, nil
}

func checkedI420Add(codec string, a int, b int) (int, error) {
	if b > int(^uint(0)>>1)-a {
		return 0, errors.New("coracle: " + codec + " I420 size overflows int")
	}
	return a + b, nil
}
