package coracle

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVpxdecVP8ChecksumArgsParsesOracleJSONL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is Unix-only")
	}
	oracle := writeExecutableScript(t, `#!/bin/sh
printf '%s\n' '{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"00112233445566778899aabbccddeeff","u_md5":"00112233445566778899aabbccddeeff","v_md5":"00112233445566778899aabbccddeeff","full_md5":"00112233445566778899aabbccddeeff"}'
`)

	frames, diag, err := VpxdecVP8ChecksumArgs(oracle, []string{"decode", "input.ivf"})
	if err != nil {
		t.Fatalf("VpxdecVP8ChecksumArgs: %v\n%s", err, diag)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}
	if frames[0].Index != 0 || !frames[0].KeyFrame || !frames[0].ShowFrame {
		t.Fatalf("unexpected frame checksum: %+v", frames[0])
	}
}

func TestVpxdecVP8ChecksumArgsKeepsInvalidOracleOutputDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is Unix-only")
	}
	oracle := writeExecutableScript(t, "#!/bin/sh\nprintf 'not-json\\n'\n")

	_, diag, err := VpxdecVP8ChecksumArgs(oracle, []string{"decode", "input.ivf"})
	if !errors.Is(err, testutil.ErrInvalidOracleOutput) {
		t.Fatalf("VpxdecVP8ChecksumArgs error = %v, want ErrInvalidOracleOutput", err)
	}
	if string(diag) != "not-json\n" {
		t.Fatalf("diag = %q, want invalid oracle output", diag)
	}
}

func TestVpxdecVP8SummaryIVFUsesConfiguredBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is Unix-only")
	}
	vpxdec := writeExecutableScript(t, "#!/bin/sh\nprintf 'summary %s\\n' \"$*\"\n")

	diag, err := VpxdecVP8SummaryIVF([]byte("DKIF"), VpxdecVP8Config{BinaryPath: vpxdec})
	if err != nil {
		t.Fatalf("VpxdecVP8SummaryIVF: %v\n%s", err, diag)
	}
	if !strings.Contains(string(diag), "--codec=vp8 --noblit --summary") {
		t.Fatalf("diag = %q, want summary args", diag)
	}
}

func TestVpxdecVP8DecodeI420ReturnsPartialOutputOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is Unix-only")
	}
	vpxdec := writeExecutableScript(t, `#!/bin/sh
out="${3#--output=}"
printf raw > "$out"
printf 'decode failed\n'
exit 3
`)

	raw, diag, err := VpxdecVP8DecodeI420([]byte("DKIF"), VpxdecVP8Config{BinaryPath: vpxdec})
	if err == nil {
		t.Fatal("VpxdecVP8DecodeI420 returned nil error, want process failure")
	}
	if string(raw) != "raw" {
		t.Fatalf("raw = %q, want partial output", raw)
	}
	if string(diag) != "decode failed\n" {
		t.Fatalf("diag = %q, want process output", diag)
	}
}

func TestReadFrameChecksumJSONLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checksums.jsonl")
	data := []byte(`{"frame":2,"width":16,"height":16,"keyframe":false,"show_frame":true,"y_md5":"00112233445566778899aabbccddeeff","u_md5":"00112233445566778899aabbccddeeff","v_md5":"00112233445566778899aabbccddeeff","full_md5":"00112233445566778899aabbccddeeff"}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	frames, got, err := ReadFrameChecksumJSONLFile(path)
	if err != nil {
		t.Fatalf("ReadFrameChecksumJSONLFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("data = %q, want %q", got, data)
	}
	if len(frames) != 1 || frames[0].Index != 2 {
		t.Fatalf("frames = %+v, want frame 2", frames)
	}
}

func writeExecutableScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oracle.sh")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
