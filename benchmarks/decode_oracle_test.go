//go:build govpx_oracle_trace

package benchmarks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thesyncim/govpx/internal/coracle"
)

func BenchmarkDecodeLibvpxOracleSmoke(b *testing.B) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		b.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle benchmarks")
	}
	oracle, err := coracle.ChecksumOraclePath()
	if err != nil {
		if errors.Is(err, coracle.ErrChecksumOracleNotBuilt) {
			b.Skip("set GOVPX_ORACLE to the libvpx v1.16.0 checksum oracle binary")
		}
		b.Fatalf("ChecksumOraclePath returned error: %v", err)
	}
	ivf := loadLibvpxSmokeIVF(b)
	header, _ := splitIVFPackets(b, ivf)
	path := filepath.Join(b.TempDir(), "libvpx-smoke.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		b.Fatalf("WriteFile returned error: %v", err)
	}
	runLibvpxOracleDecode(b, oracle, path)

	b.ReportAllocs()
	b.SetBytes(int64(len(ivf)))
	b.ResetTimer()
	start := time.Now()
	decodedFrames := 0
	for i := 0; i < b.N; i++ {
		decodedFrames += runLibvpxOracleDecode(b, oracle, path)
	}
	elapsed := time.Since(start)
	b.StopTimer()
	reportDecodeMetrics(b, header, len(ivf)*b.N, decodedFrames, elapsed)
}

func runLibvpxOracleDecode(t testing.TB, oracle string, path string) int {
	t.Helper()
	frames, diag, err := coracle.VpxdecVP8ChecksumFile(oracle, "decode", path)
	if err != nil {
		t.Fatalf("VpxdecVP8ChecksumFile returned error: %v\n%s", err, diag)
	}
	if len(frames) == 0 {
		t.Fatalf("libvpx oracle decoded zero frames")
	}
	return len(frames)
}
