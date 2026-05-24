package vp8corpus

import (
	"bufio"
	"errors"
	"image"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type SourceClip struct {
	Name   string
	Width  int
	Height int
	FPS    int
	Frames []*image.YCbCr
}

func SourceRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_ENCODER_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_ENCODER_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_ENCODER_TEST_DATA_REQUIRED=1 but GOVPX_ENCODER_TEST_DATA_PATH is not set")
	}
	t.Skip("set GOVPX_ENCODER_TEST_DATA_PATH to a Y4M/YUV source file or directory")
	return "", false
}

func FindSources(t testing.TB, root string) []string {
	t.Helper()
	limit := sourceLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if IsSourcePath(root) {
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
		if entry.IsDir() || !IsSourcePath(path) {
			return nil
		}
		paths = append(paths, path)
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

func IsSourcePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".y4m" || ext == ".yuv"
}

func SourceFrameLimit(t testing.TB) int {
	t.Helper()
	limit := envIntDefault(t, "GOVPX_ENCODER_TEST_DATA_FRAMES", 6)
	if limit == 0 {
		t.Fatalf("GOVPX_ENCODER_TEST_DATA_FRAMES must be positive")
	}
	return limit
}

func AssertSourceMinimum(t testing.TB, paths []string) {
	t.Helper()
	minimum := envIntDefault(t, "GOVPX_ENCODER_TEST_DATA_MIN", 0)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("encoder source test data count = %d, want at least %d from GOVPX_ENCODER_TEST_DATA_MIN", len(paths), minimum)
	}
}

func ReadSourceClip(t testing.TB, path string, maxFrames int) (SourceClip, bool) {
	t.Helper()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".y4m":
		return readY4MClip(t, path, maxFrames)
	case ".yuv":
		return readRawI420Clip(t, path, maxFrames)
	default:
		return SourceClip{}, false
	}
}

func SourceTargetKbps(width int, height int, fps int) int {
	if fps <= 0 {
		fps = 30
	}
	target := (width * height * fps) / 1200
	if target < 100 {
		return 100
	}
	return target
}

func sourceLimit(t testing.TB) int {
	t.Helper()
	return envIntDefault(t, "GOVPX_ENCODER_TEST_DATA_LIMIT", 0)
}

func envIntDefault(t testing.TB, name string, fallback int) int {
	t.Helper()
	value, set, err := envIntStatus(name)
	if err != nil {
		t.Fatal(err)
	}
	if !set {
		return fallback
	}
	return value
}

func envIntStatus(name string) (int, bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, true, errors.New(name + " = " + strconv.Quote(raw) + ", want a non-negative integer")
	}
	return value, true, nil
}

func readY4MClip(t testing.TB, path string, maxFrames int) (SourceClip, bool) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s returned error: %v", path, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	header, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString %s header returned error: %v", path, err)
	}
	width, height, fps, ok := parseY4MHeader(header)
	if !ok {
		return SourceClip{}, false
	}

	frames := make([]*image.YCbCr, 0, maxFrames)
	for len(frames) < maxFrames {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadString %s frame header returned error: %v", path, err)
		}
		if !strings.HasPrefix(line, "FRAME") {
			t.Fatalf("%s frame header = %q, want FRAME", path, strings.TrimSpace(line))
		}
		frame, err := readI420Frame(reader, width, height)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("ReadFull %s frame returned error: %v", path, err)
		}
		if err != nil {
			t.Fatalf("read frame %s returned error: %v", path, err)
		}
		frames = append(frames, frame)
	}
	if len(frames) == 0 {
		t.Fatalf("%s has no Y4M frames", path)
	}
	return SourceClip{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Width:  width,
		Height: height,
		FPS:    fps,
		Frames: frames,
	}, true
}

func parseY4MHeader(header string) (int, int, int, bool) {
	if !strings.HasPrefix(header, "YUV4MPEG2 ") {
		return 0, 0, 0, false
	}
	width := 0
	height := 0
	fps := 30
	chroma := "C420"
	for field := range strings.FieldsSeq(header) {
		switch {
		case strings.HasPrefix(field, "W"):
			width, _ = strconv.Atoi(strings.TrimPrefix(field, "W"))
		case strings.HasPrefix(field, "H"):
			height, _ = strconv.Atoi(strings.TrimPrefix(field, "H"))
		case strings.HasPrefix(field, "F"):
			num, den, ok := parseRatio(strings.TrimPrefix(field, "F"))
			if ok && den > 0 {
				fps = (num + den/2) / den
				if fps <= 0 {
					fps = 1
				}
			}
		case strings.HasPrefix(field, "C"):
			chroma = field
		}
	}
	if width <= 0 || height <= 0 || fps <= 0 {
		return 0, 0, 0, false
	}
	chroma = strings.ToLower(chroma)
	if !strings.HasPrefix(chroma, "c420") || strings.Contains(chroma, "p10") || strings.Contains(chroma, "p12") {
		return 0, 0, 0, false
	}
	return width, height, fps, true
}

func parseRatio(raw string) (int, int, bool) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	num, errNum := strconv.Atoi(parts[0])
	den, errDen := strconv.Atoi(parts[1])
	return num, den, errNum == nil && errDen == nil && den > 0
}

func readRawI420Clip(t testing.TB, path string, maxFrames int) (SourceClip, bool) {
	t.Helper()
	width, height, ok := inferRawI420Dimensions(filepath.Base(path))
	if !ok {
		return SourceClip{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s returned error: %v", path, err)
	}
	defer file.Close()

	frames := make([]*image.YCbCr, 0, maxFrames)
	for len(frames) < maxFrames {
		frame, err := readI420Frame(file, width, height)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			t.Fatalf("read frame %s returned error: %v", path, err)
		}
		frames = append(frames, frame)
	}
	if len(frames) == 0 {
		t.Fatalf("%s has no complete I420 frames", path)
	}
	return SourceClip{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Width:  width,
		Height: height,
		FPS:    30,
		Frames: frames,
	}, true
}

func readI420Frame(reader io.Reader, width int, height int) (*image.YCbCr, error) {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	if err := readPlane(reader, img.Y, img.YStride, width, height); err != nil {
		return nil, err
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	if err := readPlane(reader, img.Cb, img.CStride, uvWidth, uvHeight); err != nil {
		return nil, err
	}
	if err := readPlane(reader, img.Cr, img.CStride, uvWidth, uvHeight); err != nil {
		return nil, err
	}
	return img, nil
}

func readPlane(reader io.Reader, dst []byte, stride int, width int, height int) error {
	for row := range height {
		start := row * stride
		if _, err := io.ReadFull(reader, dst[start:start+width]); err != nil {
			return err
		}
	}
	return nil
}

var rawI420DimensionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(\d+)x(\d+)`),
	regexp.MustCompile(`(?i)w(\d+)h(\d+)`),
	regexp.MustCompile(`(?i)(\d+)_(\d+)`),
}

func inferRawI420Dimensions(name string) (int, int, bool) {
	for _, pattern := range rawI420DimensionPatterns {
		match := pattern.FindStringSubmatch(name)
		if len(match) != 3 {
			continue
		}
		width, errW := strconv.Atoi(match[1])
		height, errH := strconv.Atoi(match[2])
		if errW == nil && errH == nil && width > 0 && height > 0 {
			return width, height, true
		}
	}
	return 0, 0, false
}
