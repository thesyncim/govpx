package govpx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

func makeSingleFrameIVF(width int, height int, den uint32, num uint32, frame []byte) []byte {
	return makeIVF(width, height, den, num, [][]byte{frame})
}

func makeIVF(width int, height int, den uint32, num uint32, frames [][]byte) []byte {
	return testutil.BuildIVF(testutil.IVFHeader{
		FourCC:              testutil.IVFFourCCVP8,
		Width:               width,
		Height:              height,
		TimebaseDenominator: den,
		TimebaseNumerator:   num,
	}, frames)
}

func findChecksumOracle(t *testing.T) string {
	t.Helper()
	path, err := coracle.ChecksumOraclePath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrChecksumOracleNotBuilt) {
		t.Skip("set GOVPX_ORACLE to the libvpx v1.16.0 checksum oracle binary")
	}
	t.Fatalf("ChecksumOraclePath: %v", err)
	return ""
}

func findVpxenc(t *testing.T) string {
	t.Helper()
	path, err := coracle.VpxencPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencNotBuilt) {
		t.Skip("set GOVPX_VPXENC to a libvpx v1.16.0 vpxenc binary")
	}
	t.Fatalf("VpxencPath: %v", err)
	return ""
}

func findVpxTemporalSVCEncoder(t *testing.T) string {
	t.Helper()
	path, err := coracle.VpxTemporalSVCEncoderPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxTemporalSVCEncoderNotBuilt) {
		t.Skip("set GOVPX_VPX_TEMPORAL_SVC_ENCODER to a libvpx v1.16.0 vpx_temporal_svc_encoder binary")
	}
	t.Fatalf("VpxTemporalSVCEncoderPath: %v", err)
	return ""
}

func runLibvpxChecksumOracle(t *testing.T, oracle string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-keyframe.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return runLibvpxChecksumOracleFile(t, oracle, path)
}

func runLibvpxChecksumOracleMode(t *testing.T, oracle string, mode string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-"+mode+".ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return runLibvpxChecksumOracleFileMode(t, oracle, mode, path)
}

func runLibvpxChecksumOracleControlScriptWithCopyLog(t *testing.T, oracle string, mode string, script []string, ivf []byte) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "govpx-"+mode+".ivf")
	copyLogPath := filepath.Join(dir, "copy-reference.jsonl")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	frames := runLibvpxChecksumOracleArgs(t, oracle, []string{mode, strings.Join(script, ","), copyLogPath, path})
	return frames, readLibvpxCopyReferenceLog(t, copyLogPath)
}

func runLibvpxChecksumOracleThreadedControlScriptWithCopyLog(t *testing.T, oracle string, threads int, script []string, ivf []byte) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "govpx-decode-threaded-controls.ivf")
	copyLogPath := filepath.Join(dir, "copy-reference.jsonl")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	args := []string{"decode-threaded-controls-copylog", strconv.Itoa(threads), strings.Join(script, ","), copyLogPath, path}
	frames := runLibvpxChecksumOracleArgs(t, oracle, args)
	return frames, readLibvpxCopyReferenceLog(t, copyLogPath)
}

func runLibvpxChecksumOracleModeExpectError(t *testing.T, oracle string, mode string, ivf []byte) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-"+mode+".ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return runLibvpxChecksumOracleFileModeExpectError(t, oracle, mode, path)
}

func runLibvpxChecksumOracleFile(t *testing.T, oracle string, path string) []testutil.FrameChecksum {
	t.Helper()
	return runLibvpxChecksumOracleFileMode(t, oracle, "decode", path)
}

func runLibvpxChecksumOracleFileMode(t *testing.T, oracle string, mode string, path string) []testutil.FrameChecksum {
	t.Helper()
	frames, out, err := coracle.VpxdecVP8ChecksumFile(oracle, mode, path)
	if err != nil {
		failLibvpxChecksumOracle(t, out, err)
	}
	return frames
}

func runLibvpxChecksumOracleArgs(t *testing.T, oracle string, args []string) []testutil.FrameChecksum {
	t.Helper()
	frames, out, err := coracle.VpxdecVP8ChecksumArgs(oracle, args)
	if err != nil {
		failLibvpxChecksumOracle(t, out, err)
	}
	return frames
}

func readLibvpxCopyReferenceLog(t *testing.T, path string) []testutil.FrameChecksum {
	t.Helper()
	frames, data, err := coracle.ReadFrameChecksumJSONLFile(path)
	if err != nil {
		if errors.Is(err, testutil.ErrInvalidOracleOutput) {
			t.Fatalf("libvpx copy-reference log produced invalid output:\n%s", data)
		}
		t.Fatalf("ParseFrameChecksumJSONLines copy-reference log returned error: %v", err)
	}
	return frames
}

func runLibvpxChecksumOracleFileExpectError(t *testing.T, oracle string, path string) error {
	t.Helper()
	return runLibvpxChecksumOracleFileModeExpectError(t, oracle, "decode", path)
}

func runLibvpxChecksumOracleFileModeExpectError(t *testing.T, oracle string, mode string, path string) error {
	t.Helper()
	out, err := coracle.VpxdecVP8ChecksumFileExpectError(oracle, mode, path)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("libvpx oracle failed to start: %v\n%s", err, out)
	}
	return err
}

func failLibvpxChecksumOracle(t *testing.T, out []byte, err error) {
	t.Helper()
	if errors.Is(err, testutil.ErrInvalidOracleOutput) {
		t.Fatalf("libvpx oracle produced invalid output:\n%s", out)
	}
	t.Fatalf("libvpx oracle failed: %v\n%s", err, out)
}

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
