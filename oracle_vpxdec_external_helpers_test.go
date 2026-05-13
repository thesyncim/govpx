package govpx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func findVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalIVFTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if !isInvalidVP8IVFTestDataName(root) && isVP8IVFTestData(t, root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ivf") || isInvalidVP8IVFTestDataName(path) {
			return nil
		}
		if isVP8IVFTestData(t, path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func findInvalidVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalInvalidIVFTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if isInvalidVP8IVFTestDataName(root) && isVP8IVFTestData(t, root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ivf") || !isInvalidVP8IVFTestDataName(path) {
			return nil
		}
		if isVP8IVFTestData(t, path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func isInvalidVP8IVFTestDataName(path string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(path)), "invalid-")
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
	raw := os.Getenv("GOVPX_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("GOVPX_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
}

func externalInvalidIVFTestLimit(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_INVALID_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
}

func externalIVFTestMinimum(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_TEST_DATA_MIN")
	if raw == "" {
		return 0
	}
	minimum, err := strconv.Atoi(raw)
	if err != nil || minimum < 0 {
		t.Fatalf("GOVPX_TEST_DATA_MIN = %q, want a non-negative integer", raw)
	}
	return minimum
}

func externalInvalidIVFTestMinimum(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_INVALID_TEST_DATA_MIN")
	if raw == "" {
		return 0
	}
	minimum, err := strconv.Atoi(raw)
	if err != nil || minimum < 0 {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_MIN = %q, want a non-negative integer", raw)
	}
	return minimum
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

func isVP8IVFTestData(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s returned error: %v", path, err)
	}
	defer file.Close()
	header := make([]byte, testutil.IVFFileHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			t.Fatalf("%s is not valid IVF data: %v", path, testutil.ErrInvalidIVF)
		}
		t.Fatalf("ReadFull %s returned error: %v", path, err)
	}
	hdr, err := testutil.ParseIVFHeader(header)
	if err == nil {
		// ParseIVFHeader now accepts VP80 and VP90 fourcc tags. The
		// VP8 oracle path is byte-parity-tested against the libvpx
		// VP8 reference; VP90 streams have a different bitstream
		// shape and aren't compatible with VP8Decode/Encode flows.
		return hdr.FourCC == [4]byte{'V', 'P', '8', '0'}
	}
	if errors.Is(err, testutil.ErrUnsupportedFourCC) {
		return false
	}
	t.Fatalf("%s is not valid VP8 IVF data: %v", path, err)
	return false
}

func safeIVFTestName(root string, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil || name == "." {
		name = filepath.Base(path)
	}
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "" {
		return "ivf"
	}
	return name
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
