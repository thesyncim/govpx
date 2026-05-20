package govpx

import (
	"fmt"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func findVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP8IVFTestData(root, externalIVFTestLimit(t), false)
	if err != nil {
		t.Fatalf("FindVP8IVFTestData(%q): %v", root, err)
	}
	return paths
}

func findInvalidVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP8IVFTestData(root, externalInvalidIVFTestLimit(t), true)
	if err != nil {
		t.Fatalf("FindVP8IVFTestData(%q, invalid): %v", root, err)
	}
	return paths
}

func externalIVFTestDataRoot(t *testing.T, skipMessage string) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_TEST_DATA_REQUIRED=1 but GOVPX_TEST_DATA_PATH is not set")
	}
	t.Skip(skipMessage)
	return "", false
}

func externalInvalidIVFTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_INVALID_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_INVALID_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_REQUIRED=1 but neither GOVPX_INVALID_TEST_DATA_PATH nor GOVPX_TEST_DATA_PATH is set")
	}
	t.Skip("set GOVPX_INVALID_TEST_DATA_PATH to invalid VP8 IVF data or point GOVPX_TEST_DATA_PATH at a full libvpx test-data directory")
	return "", false
}

func externalIVFTestLimit(t *testing.T) int {
	t.Helper()
	return mustNonNegativeEnvInt(t, "GOVPX_TEST_DATA_LIMIT")
}

func externalInvalidIVFTestLimit(t *testing.T) int {
	t.Helper()
	return mustNonNegativeEnvInt(t, "GOVPX_INVALID_TEST_DATA_LIMIT")
}

func externalIVFTestMinimum(t *testing.T) int {
	t.Helper()
	return mustNonNegativeEnvInt(t, "GOVPX_TEST_DATA_MIN")
}

func externalInvalidIVFTestMinimum(t *testing.T) int {
	t.Helper()
	return mustNonNegativeEnvInt(t, "GOVPX_INVALID_TEST_DATA_MIN")
}

func assertExternalIVFTestDataMinimum(t *testing.T, paths []string) {
	t.Helper()
	minimum := externalIVFTestMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("VP8 IVF test data count = %d, want at least %d from GOVPX_TEST_DATA_MIN", len(paths), minimum)
	}
}

func assertExternalInvalidIVFTestDataMinimum(t *testing.T, paths []string) {
	t.Helper()
	minimum := externalInvalidIVFTestMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("invalid VP8 IVF test data count = %d, want at least %d from GOVPX_INVALID_TEST_DATA_MIN", len(paths), minimum)
	}
}

func mustNonNegativeEnvInt(t *testing.T, name string) int {
	t.Helper()
	value, _, err := testutil.NonNegativeEnvInt(name)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func decodeFrameSequence(t *testing.T, packets ...[]byte) []Image {
	t.Helper()
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	frames := make([]Image, 0, len(packets))
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d returned error: %v", i, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("NextFrame packet %d returned no frame", i)
		}
		frames = append(frames, cloneImage(frame))
	}
	return frames
}

func cloneImage(src Image) Image {
	dst := testImage(src.Width, src.Height)
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
	return dst
}

func checksumFrame(index int, keyFrame bool, showFrame bool, img Image) testutil.FrameChecksum {
	return testutil.FrameChecksum{
		Index:     index,
		Width:     img.Width,
		Height:    img.Height,
		KeyFrame:  keyFrame,
		ShowFrame: showFrame,
		MD5:       testutil.MD5Planes(img.Y, img.YStride, img.U, img.UStride, img.V, img.VStride, img.Width, img.Height),
	}
}

func formatChecksum(frame testutil.FrameChecksum) string {
	return fmt.Sprintf("frame=%d %dx%d key=%t show=%t y=%s u=%s v=%s full=%s",
		frame.Index,
		frame.Width,
		frame.Height,
		frame.KeyFrame,
		frame.ShowFrame,
		testutil.MD5Hex(frame.MD5.Y),
		testutil.MD5Hex(frame.MD5.U),
		testutil.MD5Hex(frame.MD5.V),
		testutil.MD5Hex(frame.MD5.Full),
	)
}
