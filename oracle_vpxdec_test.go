package libgopx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/libgopx/internal/testutil"
	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
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

func TestOracleLibvpxChecksumMatchesEncodeIntoGoldenReferenceInterFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	secondPacket := make([]byte, 4096)
	secondInter, err := e.EncodeInto(secondPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	goldenPacket := make([]byte, 4096)
	goldenInter, err := e.EncodeInto(goldenPacket, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("golden EncodeInto returned error: %v", err)
	}
	if goldenInter.KeyFrame {
		t.Fatalf("golden reference frame KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.GoldenFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, secondInter.Data, goldenInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	libgopxFrames := decodeFrameSequence(t, key.Data, secondInter.Data, goldenInter.Data)
	if len(oracleFrames) != len(libgopxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(libgopxFrames))
	}
	for i, frame := range libgopxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoResidualInterFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want residual interframe")
	}

	libgopxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(libgopxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(libgopxFrames))
	}
	for i, frame := range libgopxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoNewMVInterFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newSizedTestEncoder(t, 32, 16)
	first := testImage(32, 16)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte(32 + col*5)
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	shifted := shiftImageRightOne(reconstructed)
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, shifted, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want NEWMV interframe")
	}

	libgopxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(libgopxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(libgopxFrames))
	}
	for i, frame := range libgopxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoSubpixelNewMVInterFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	first := testImage(16, 16)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte(32 + ((row*17 + col*13) & 127))
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	second := testImage(16, 16)
	fillImage(second, 0, 90, 170)
	ref := &e.lastRef.Img
	start := ref.YOrigin - 2*ref.YStride - 2
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, 2, 2, second.Y, second.YStride)
	reconstructed := publicImageFromVP8(ref)
	copy(second.U, reconstructed.U)
	copy(second.V, reconstructed.V)

	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want subpixel NEWMV interframe")
	}
	if e.interFrameModes[0].Mode != vp8common.NewMV || e.interFrameModes[0].MV != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mode[0] = %+v, want subpixel NEWMV +2,+2", e.interFrameModes[0])
	}

	libgopxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(libgopxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(libgopxFrames))
	}
	for i, frame := range libgopxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoIntraMacroblockInterFrame(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 0, 90, 170)
	fillImage(second, 128, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want intra-macroblock interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.IntraFrame {
		t.Fatalf("mode[0] = %+v, want intra macroblock", e.interFrameModes[0])
	}

	libgopxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(libgopxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(libgopxFrames))
	}
	for i, frame := range libgopxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoLoopFilteredInterFrame(t *testing.T) {
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
		Sharpness:           3,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(32, 16)
	fillImage(first, 220, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 16; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = 40
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	second := testImage(32, 16)
	fillImage(second, 40, 90, 170)
	for row := 0; row < second.Height; row++ {
		for col := 16; col < second.Width; col++ {
			second.Y[row*second.YStride+col] = 220
		}
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want loop-filtered interframe")
	}

	libgopxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(libgopxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(libgopxFrames))
	}
	for i, frame := range libgopxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleExternalIVFTestDataMatchesLibvpx(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run external libvpx conformance tests")
	}
	root := os.Getenv("LIBGOPX_TEST_DATA_PATH")
	if root == "" {
		t.Skip("set LIBGOPX_TEST_DATA_PATH to a VP8 IVF file or directory")
	}
	oracle := findChecksumOracle(t)
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}

	for _, path := range paths {
		path := path
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := runLibvpxChecksumOracleFile(t, oracle, path)
			got := decodeIVFChecksums(t, ivf)
			if len(got) != len(want) {
				t.Fatalf("frame count = %d, want %d from libvpx", len(got), len(want))
			}
			for i := range want {
				if !testutil.SameFrameChecksum(got[i], want[i]) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(want[i]), formatChecksum(got[i]))
				}
			}
		})
	}
}

func TestFindVP8IVFTestData(t *testing.T) {
	dir := t.TempDir()
	vp8Path := filepath.Join(dir, "vp8.ivf")
	if err := os.WriteFile(vp8Path, makeIVF(16, 16, 30, 1, [][]byte{{1}}), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	vp9Path := filepath.Join(dir, "vp9.ivf")
	vp9 := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	copy(vp9[8:12], []byte("VP90"))
	if err := os.WriteFile(vp9Path, vp9, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("not ivf"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	paths := findVP8IVFTestData(t, dir)
	if len(paths) != 1 || paths[0] != vp8Path {
		t.Fatalf("paths = %v, want [%s]", paths, vp8Path)
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
	return runLibvpxChecksumOracleFile(t, oracle, path)
}

func runLibvpxChecksumOracleFile(t *testing.T, oracle string, path string) []testutil.FrameChecksum {
	t.Helper()
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

func decodeIVFChecksums(t *testing.T, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	if _, err := testutil.ParseIVFHeader(ivf); err != nil {
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
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", inputIndex, err)
		}
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

func findVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalIVFTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if isVP8IVFTestData(t, root) {
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
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ivf") {
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

func externalIVFTestLimit(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("LIBGOPX_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("LIBGOPX_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
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
	_, err = testutil.ParseIVFHeader(header)
	if err == nil {
		return true
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
