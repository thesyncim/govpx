package benchmarks

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
)

type decodeJSONReport struct {
	Source               string  `json:"source"`
	Decoder              string  `json:"decoder"`
	Operation            string  `json:"operation"`
	Width                int     `json:"width"`
	Height               int     `json:"height"`
	Frames               int     `json:"frames"`
	InputBytes           int     `json:"input_bytes"`
	DecodedFrames        int     `json:"decoded_frames"`
	NSPerFrame           int64   `json:"ns_per_frame"`
	DecodeFPS            float64 `json:"decode_fps"`
	MacroblocksPerSecond float64 `json:"macroblocks_per_second"`
	CodedMegabytesPerSec float64 `json:"coded_megabytes_per_second"`
	AllocsPerFrame       float64 `json:"allocs_per_frame"`
}

func TestDecodeSmokeBenchmarkReportJSON(t *testing.T) {
	ivf := loadLibvpxSmokeIVF(t)
	header, packets := splitIVFPackets(t, ivf)
	report := measureGopvxDecode(t, ivf, header, packets)

	if report.Source != "libvpx-v1.16.0-simple_encoder" || report.Decoder != "govpx" || report.Operation != "decode" {
		t.Fatalf("identity = %s/%s/%s, want libvpx-v1.16.0-simple_encoder/govpx/decode", report.Source, report.Decoder, report.Operation)
	}
	if report.Width != 32 || report.Height != 32 || report.Frames != 2 || report.DecodedFrames != 2 || report.InputBytes != len(ivf) {
		t.Fatalf("dimensions/counts = %+v", report)
	}
	if report.NSPerFrame <= 0 || report.DecodeFPS <= 0 || report.MacroblocksPerSecond <= 0 || report.CodedMegabytesPerSec <= 0 {
		t.Fatalf("timing metrics = %+v", report)
	}
	if report.AllocsPerFrame != 0 {
		t.Fatalf("AllocsPerFrame = %f, want 0", report.AllocsPerFrame)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
}

func BenchmarkDecodeGopvxSmoke(b *testing.B) {
	ivf := loadLibvpxSmokeIVF(b)
	header, packets := splitIVFPackets(b, ivf)
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		b.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	decodePackets(b, d, packets)

	b.ReportAllocs()
	b.SetBytes(int64(len(ivf)))
	b.ResetTimer()
	start := time.Now()
	decodedFrames := 0
	for i := 0; i < b.N; i++ {
		decodedFrames += decodePackets(b, d, packets)
	}
	elapsed := time.Since(start)
	b.StopTimer()
	reportDecodeMetrics(b, header, len(ivf)*b.N, decodedFrames, elapsed)
}

func BenchmarkDecodeIntoGopvxSmoke(b *testing.B) {
	ivf := loadLibvpxSmokeIVF(b)
	header, packets := splitIVFPackets(b, ivf)
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		b.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := benchmarkImage(header.Width, header.Height)
	decodeIntoPackets(b, d, packets, &dst)

	b.ReportAllocs()
	b.SetBytes(int64(len(ivf)))
	b.ResetTimer()
	start := time.Now()
	decodedFrames := 0
	for i := 0; i < b.N; i++ {
		decodedFrames += decodeIntoPackets(b, d, packets, &dst)
	}
	elapsed := time.Since(start)
	b.StopTimer()
	reportDecodeMetrics(b, header, len(ivf)*b.N, decodedFrames, elapsed)
}

func BenchmarkDecodeLibvpxOracleSmoke(b *testing.B) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		b.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle benchmarks")
	}
	oracle := os.Getenv("GOVPX_ORACLE")
	if oracle == "" {
		b.Skip("set GOVPX_ORACLE to the libvpx v1.16.0 checksum oracle binary")
	}
	oracle = resolveBenchmarkPath(oracle)
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

func measureGopvxDecode(t testing.TB, ivf []byte, header testutil.IVFHeader, packets [][]byte) decodeJSONReport {
	t.Helper()
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	decodePackets(t, d, packets)

	start := time.Now()
	decodedFrames := decodePackets(t, d, packets)
	elapsed := time.Since(start)
	nsPerFrame := benchmarkNSPerFrame(elapsed, decodedFrames)
	allocs := testing.AllocsPerRun(100, func() {
		decodePackets(t, d, packets)
	})

	return decodeJSONReport{
		Source:               "libvpx-v1.16.0-simple_encoder",
		Decoder:              "govpx",
		Operation:            "decode",
		Width:                header.Width,
		Height:               header.Height,
		Frames:               len(packets),
		InputBytes:           len(ivf),
		DecodedFrames:        decodedFrames,
		NSPerFrame:           nsPerFrame,
		DecodeFPS:            1e9 / float64(nsPerFrame),
		MacroblocksPerSecond: benchmarkMacroblocks(header.Width, header.Height) * 1e9 / float64(nsPerFrame),
		CodedMegabytesPerSec: benchmarkCodedMegabytesPerSecond(len(ivf), elapsed),
		AllocsPerFrame:       allocs / float64(decodedFrames),
	}
}

func loadLibvpxSmokeIVF(t testing.TB) []byte {
	t.Helper()
	ivf, err := hex.DecodeString(testutil.LibvpxEncodedSmokeIVFHex)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	return ivf
}

func splitIVFPackets(t testing.TB, ivf []byte) (testutil.IVFHeader, [][]byte) {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	packets := make([][]byte, 0, int(header.FrameCount))
	for i := 0; offset < len(ivf); i++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		packets = append(packets, frame.Data)
		offset = next
	}
	if len(packets) != int(header.FrameCount) {
		t.Fatalf("IVF frame count = %d, want %d", len(packets), header.FrameCount)
	}
	return header, packets
}

func decodePackets(t testing.TB, d *govpx.VP8Decoder, packets [][]byte) int {
	t.Helper()
	d.Reset()
	decodedFrames := 0
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame frame %d returned no frame", i)
		}
		decodedFrames++
	}
	return decodedFrames
}

func decodeIntoPackets(t testing.TB, d *govpx.VP8Decoder, packets [][]byte, dst *govpx.Image) int {
	t.Helper()
	d.Reset()
	decodedFrames := 0
	for i, packet := range packets {
		info, err := d.DecodeInto(packet, dst)
		if err != nil {
			t.Fatalf("DecodeInto frame %d returned error: %v", i, err)
		}
		if _, ok := d.NextFrame(); ok {
			t.Fatalf("DecodeInto frame %d queued a NextFrame output", i)
		}
		if info.ShowFrame {
			decodedFrames++
		}
	}
	return decodedFrames
}

func runLibvpxOracleDecode(t testing.TB, oracle string, path string) int {
	t.Helper()
	cmd := exec.Command(oracle, "decode", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("libvpx oracle failed: %v\n%s", err, out)
	}
	frames, err := testutil.ParseFrameChecksumJSONLines(out)
	if err != nil {
		t.Fatalf("ParseFrameChecksumJSONLines returned error: %v", err)
	}
	if len(frames) == 0 {
		t.Fatalf("libvpx oracle decoded zero frames")
	}
	return len(frames)
}

func resolveBenchmarkPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	parent := filepath.Join("..", path)
	if _, err := os.Stat(parent); err == nil {
		return parent
	}
	return path
}

func benchmarkImage(width int, height int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func reportDecodeMetrics(b *testing.B, header testutil.IVFHeader, inputBytes int, decodedFrames int, elapsed time.Duration) {
	if decodedFrames <= 0 || elapsed <= 0 {
		return
	}
	seconds := elapsed.Seconds()
	b.ReportMetric(float64(decodedFrames)/seconds, "frames/s")
	b.ReportMetric(float64(decodedFrames)/float64(b.N), "frames/op")
	b.ReportMetric(benchmarkMacroblocks(header.Width, header.Height)*float64(decodedFrames)/seconds, "macroblocks/s")
	b.ReportMetric(benchmarkCodedMegabytesPerSecond(inputBytes, elapsed), "coded_MB/s")
}

func benchmarkNSPerFrame(elapsed time.Duration, frames int) int64 {
	if frames <= 0 {
		return 0
	}
	ns := elapsed.Nanoseconds() / int64(frames)
	if ns <= 0 {
		return 1
	}
	return ns
}

func benchmarkMacroblocks(width int, height int) float64 {
	cols := (width + 15) >> 4
	rows := (height + 15) >> 4
	return float64(cols * rows)
}

func benchmarkCodedMegabytesPerSecond(bytes int, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	const megabyte = 1024 * 1024
	return (float64(bytes) / megabyte) / elapsed.Seconds()
}
