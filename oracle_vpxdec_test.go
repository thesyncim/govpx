package libgopx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thesyncim/libgopx/internal/testutil"
)

func TestOracleVpxdecDecodesEncodeIntoKeyFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle smoke tests")
	}
	vpxdec := os.Getenv("LIBGOPX_VPXDEC")
	if vpxdec == "" {
		path, err := exec.LookPath("vpxdec")
		if err != nil {
			t.Skip("vpxdec not found; set LIBGOPX_VPXDEC to a libvpx v1.16.0 vpxdec binary")
		}
		vpxdec = path
	}

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	packet := make([]byte, 4096)
	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	ivf := makeSingleFrameIVF(16, 16, 30, 1, result.Data)
	path := filepath.Join(t.TempDir(), "libgopx-keyframe.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cmd := exec.Command(vpxdec, "--codec=vp8", "--noblit", "--summary", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxdec failed: %v\n%s", err, out)
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoKeyFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}
	packet := make([]byte, 8192)
	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	ivf := makeSingleFrameIVF(32, 16, 30, 1, result.Data)
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want 1", len(oracleFrames))
	}

	decoded := decodeSingleFrame(t, result.Data)
	libgopxFrame := checksumFrame(0, true, true, decoded)
	if !testutil.SameFrameChecksum(oracleFrames[0], libgopxFrame) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\nlibgopx: %s", formatChecksum(oracleFrames[0]), formatChecksum(libgopxFrame))
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoInterFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 2 {
		t.Fatalf("oracle frame count = %d, want 2", len(oracleFrames))
	}
	want := []testutil.FrameChecksum{
		checksumFrame(0, true, true, reconstructed),
		checksumFrame(1, false, true, reconstructed),
	}
	for i := range want {
		if !testutil.SameFrameChecksum(oracleFrames[i], want[i]) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want[i]))
		}
	}
}

func makeSingleFrameIVF(width int, height int, den uint32, num uint32, frame []byte) []byte {
	return makeIVF(width, height, den, num, [][]byte{frame})
}

func makeIVF(width int, height int, den uint32, num uint32, frames [][]byte) []byte {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for _, frame := range frames {
		size += frameHeaderSize + len(frame)
	}
	out := make([]byte, size)
	copy(out[0:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(out[4:6], 0)
	binary.LittleEndian.PutUint16(out[6:8], fileHeaderSize)
	copy(out[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(out[12:14], uint16(width))
	binary.LittleEndian.PutUint16(out[14:16], uint16(height))
	binary.LittleEndian.PutUint32(out[16:20], den)
	binary.LittleEndian.PutUint32(out[20:24], num)
	binary.LittleEndian.PutUint32(out[24:28], uint32(len(frames)))
	offset := fileHeaderSize
	for i, frame := range frames {
		binary.LittleEndian.PutUint32(out[offset:offset+4], uint32(len(frame)))
		binary.LittleEndian.PutUint64(out[offset+4:offset+12], uint64(i))
		copy(out[offset+frameHeaderSize:], frame)
		offset += frameHeaderSize + len(frame)
	}
	return out
}

func findChecksumOracle(t *testing.T) string {
	t.Helper()
	oracle := os.Getenv("LIBGOPX_ORACLE")
	if oracle != "" {
		return oracle
	}
	path, err := exec.LookPath("gopx-vpx-oracle")
	if err != nil {
		t.Skip("set LIBGOPX_ORACLE to the libvpx v1.16.0 checksum oracle binary")
	}
	return path
}

func runLibvpxChecksumOracle(t *testing.T, oracle string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "libgopx-keyframe.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cmd := exec.Command(oracle, "decode", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("libvpx oracle failed: %v\n%s", err, out)
	}
	frames, err := testutil.ParseFrameChecksumJSONLines(out)
	if err != nil {
		if errors.Is(err, testutil.ErrInvalidOracleOutput) {
			t.Fatalf("libvpx oracle produced invalid output:\n%s", out)
		}
		t.Fatalf("ParseFrameChecksumJSONLines returned error: %v", err)
	}
	return frames
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
