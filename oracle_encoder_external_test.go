package gopvx

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/gopvx/internal/testutil"
)

type externalEncoderClip struct {
	name   string
	width  int
	height int
	fps    int
	frames []Image
}

func TestOracleExternalEncoderTestDataValidation(t *testing.T) {
	if os.Getenv("GOPVX_WITH_ORACLE") != "1" {
		t.Skip("set GOPVX_WITH_ORACLE=1 to run external encoder source tests")
	}
	root, ok := externalEncoderTestDataRoot(t)
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	vpxenc := findVpxenc(t)
	paths := findExternalEncoderTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no encoder source files found under %s", root)
	}
	assertExternalEncoderTestDataMinimum(t, paths)

	maxFrames := externalEncoderTestFrameLimit(t)
	for _, path := range paths {
		path := path
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			clip, ok := readExternalEncoderClip(t, path, maxFrames)
			if !ok {
				t.Skipf("%s is not a supported 8-bit 4:2:0 source clip", path)
			}
			tc := encoderValidationCase{
				name:       clip.name,
				width:      clip.width,
				height:     clip.height,
				frames:     len(clip.frames),
				fps:        clip.fps,
				targetKbps: externalEncoderTargetKbps(clip.width, clip.height, clip.fps),
				opts: encoderValidationOptions(clip.width, clip.height, clip.fps, externalEncoderTargetKbps(clip.width, clip.height, clip.fps), func(opts *EncoderOptions) {
					opts.KeyFrameInterval = 120
				}),
				minPSNR:          20.0,
				minSSIM:          0.75,
				minFramePSNR:     18.0,
				minFrameSSIM:     0.65,
				maxPSNRGap:       8.0,
				maxSSIMGap:       0.05,
				maxFramePSNRGap:  12.0,
				maxFrameSSIMGap:  0.08,
				maxRateHigh:      600.0,
				maxRateLow:       100.0,
				checkInterFrames: len(clip.frames) > 1,
			}

			got := encodeGopvxValidationCorpus(t, tc, clip.frames)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			wantChecksums := runLibvpxChecksumOracle(t, oracle, got.ivf)
			assertFrameChecksumsEqual(t, "external gopvx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertGopvxEncoderValidationFeatures(t, got.ivf, tc)
			assertEncoderValidationQuality(t, "external gopvx", got.quality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "external gopvx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, clip.frames)
			libvpxGotChecksums := decodeIVFChecksums(t, libvpxIVF)
			libvpxWantChecksums := runLibvpxChecksumOracle(t, oracle, libvpxIVF)
			assertFrameChecksumsEqual(t, "external libvpx encode decoded by gopvx", libvpxGotChecksums, libvpxWantChecksums)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, clip.frames)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(clip.frames)*testutil.IVFFrameHeaderSize, tc.fps, len(clip.frames))
			logEncoderValidationQuality(t, got.quality, got.outputKbps, libvpxQuality, libvpxOutputKbps)
			assertEncoderValidationQuality(t, "external libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "external libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQualityGap(t, got.quality, libvpxQuality, tc)
		})
	}
}

func TestReadExternalEncoderY4MClip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.y4m")
	frameSize := 4*2 + 2*(2*1)
	data := []byte("YUV4MPEG2 W4 H2 F30:1 Ip A0:0 C420jpeg\nFRAME\n")
	for i := 0; i < frameSize; i++ {
		data = append(data, byte(i))
	}
	data = append(data, []byte("FRAME\n")...)
	for i := 0; i < frameSize; i++ {
		data = append(data, byte(i+20))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	clip, ok := readExternalEncoderClip(t, path, 2)
	if !ok {
		t.Fatalf("readExternalEncoderClip ok=false, want true")
	}
	if clip.width != 4 || clip.height != 2 || clip.fps != 30 || len(clip.frames) != 2 {
		t.Fatalf("clip = %dx%d fps=%d frames=%d, want 4x2 fps=30 frames=2", clip.width, clip.height, clip.fps, len(clip.frames))
	}
	if clip.frames[1].Y[0] != 20 || clip.frames[1].U[0] != 28 || clip.frames[1].V[0] != 30 {
		t.Fatalf("frame 1 samples YUV=%d/%d/%d, want 20/28/30", clip.frames[1].Y[0], clip.frames[1].U[0], clip.frames[1].V[0])
	}
}

func externalEncoderTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOPVX_ENCODER_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOPVX_ENCODER_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOPVX_ENCODER_TEST_DATA_REQUIRED=1 but GOPVX_ENCODER_TEST_DATA_PATH is not set")
	}
	t.Skip("set GOPVX_ENCODER_TEST_DATA_PATH to a Y4M/YUV source file or directory")
	return "", false
}

func findExternalEncoderTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalEncoderTestDataLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if isExternalEncoderSourcePath(root) {
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
		if entry.IsDir() || !isExternalEncoderSourcePath(path) {
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

func isExternalEncoderSourcePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".y4m" || ext == ".yuv"
}

func externalEncoderTestDataLimit(t *testing.T) int {
	t.Helper()
	return nonNegativeEnvInt(t, "GOPVX_ENCODER_TEST_DATA_LIMIT", 0)
}

func externalEncoderTestFrameLimit(t *testing.T) int {
	t.Helper()
	limit := nonNegativeEnvInt(t, "GOPVX_ENCODER_TEST_DATA_FRAMES", 6)
	if limit == 0 {
		t.Fatalf("GOPVX_ENCODER_TEST_DATA_FRAMES must be positive")
	}
	return limit
}

func externalEncoderTestDataMinimum(t *testing.T) int {
	t.Helper()
	return nonNegativeEnvInt(t, "GOPVX_ENCODER_TEST_DATA_MIN", 0)
}

func assertExternalEncoderTestDataMinimum(t *testing.T, paths []string) {
	t.Helper()
	minimum := externalEncoderTestDataMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("encoder source test data count = %d, want at least %d from GOPVX_ENCODER_TEST_DATA_MIN", len(paths), minimum)
	}
}

func nonNegativeEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		t.Fatalf("%s = %q, want a non-negative integer", name, raw)
	}
	return value
}

func readExternalEncoderClip(t *testing.T, path string, maxFrames int) (externalEncoderClip, bool) {
	t.Helper()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".y4m":
		return readExternalEncoderY4MClip(t, path, maxFrames)
	case ".yuv":
		return readExternalEncoderRawI420Clip(t, path, maxFrames)
	default:
		return externalEncoderClip{}, false
	}
}

func readExternalEncoderY4MClip(t *testing.T, path string, maxFrames int) (externalEncoderClip, bool) {
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
		return externalEncoderClip{}, false
	}
	frames := make([]Image, 0, maxFrames)
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
		frame, err := readExternalEncoderI420Frame(reader, width, height)
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
	return externalEncoderClip{name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), width: width, height: height, fps: fps, frames: frames}, true
}

func parseY4MHeader(header string) (int, int, int, bool) {
	if !strings.HasPrefix(header, "YUV4MPEG2 ") {
		return 0, 0, 0, false
	}
	width := 0
	height := 0
	fps := 30
	chroma := "C420"
	for _, field := range strings.Fields(header) {
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

func readExternalEncoderRawI420Clip(t *testing.T, path string, maxFrames int) (externalEncoderClip, bool) {
	t.Helper()
	width, height, ok := inferRawI420Dimensions(filepath.Base(path))
	if !ok {
		return externalEncoderClip{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s returned error: %v", path, err)
	}
	defer file.Close()
	frames := make([]Image, 0, maxFrames)
	for len(frames) < maxFrames {
		frame, err := readExternalEncoderI420Frame(file, width, height)
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
	return externalEncoderClip{name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), width: width, height: height, fps: 30, frames: frames}, true
}

func readExternalEncoderI420Frame(reader io.Reader, width int, height int) (Image, error) {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	if _, err := io.ReadFull(reader, img.Y); err != nil {
		return Image{}, err
	}
	if _, err := io.ReadFull(reader, img.U); err != nil {
		return Image{}, err
	}
	if _, err := io.ReadFull(reader, img.V); err != nil {
		return Image{}, err
	}
	return img, nil
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

func externalEncoderTargetKbps(width int, height int, fps int) int {
	if fps <= 0 {
		fps = 30
	}
	target := (width * height * fps) / 1200
	if target < 100 {
		return 100
	}
	return target
}
