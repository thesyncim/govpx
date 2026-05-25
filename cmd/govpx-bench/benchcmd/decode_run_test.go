package benchcmd

import (
	"encoding/binary"
	"encoding/json"
	"slices"
	"testing"
)

func TestRunDecodeBenchmarkOutputsJSONMetrics(t *testing.T) {
	report, err := runDecodeBenchmark(benchConfig{
		Width:       16,
		Height:      16,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
	})
	if err != nil {
		t.Fatalf("runDecodeBenchmark returned error: %v", err)
	}
	if report.Decoder != "govpx" || report.Operation != "decode" || report.Mode != "realtime" {
		t.Fatalf("identity = %s/%s/%s, want govpx/decode/realtime", report.Decoder, report.Operation, report.Mode)
	}
	if report.Width != 16 || report.Height != 16 || report.Frames != 3 || report.DecodedFrames != 3 || report.InputBytes <= 0 {
		t.Fatalf("dimensions/counts = %+v", report)
	}
	if report.NSPerFrame <= 0 || report.DecodeFPS <= 0 || report.MacroblocksPerSec <= 0 || report.CodedMegabytesPerSec <= 0 || report.LatencyNS.P50 <= 0 {
		t.Fatalf("decode timing metrics = ns:%d fps:%f mbps:%f coded:%f p50:%d", report.NSPerFrame, report.DecodeFPS, report.MacroblocksPerSec, report.CodedMegabytesPerSec, report.LatencyNS.P50)
	}
	maxAllocs := 0.0
	if puregoBuild {
		maxAllocs = 1
	}
	if report.AllocsPerFrame > maxAllocs {
		t.Fatalf("AllocsPerFrame = %f, want <= %f for measured decode pass", report.AllocsPerFrame, maxAllocs)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
}

func TestRunDecodeBenchmarkIncludesLibvpxReference(t *testing.T) {
	report, err := runDecodeBenchmark(benchConfig{
		Width:        16,
		Height:       16,
		Frames:       3,
		FPS:          30,
		BitrateKbps:  1200,
		Mode:         "realtime",
		LibvpxOracle: fakeLibvpxOraclePath(t),
	})
	if err != nil {
		t.Fatalf("runDecodeBenchmark returned error: %v", err)
	}
	if report.Reference == nil {
		t.Fatalf("reference = nil, want fake libvpx decode report")
	}
	if report.Reference.Decoder != "libvpx-vp8" || report.Reference.DecodedFrames != 3 {
		t.Fatalf("reference = %+v, want libvpx-vp8 with 3 decoded frames", *report.Reference)
	}
	if report.Reference.NSPerFrame <= 0 || report.Reference.DecodeFPS <= 0 || report.Reference.MacroblocksPerSec <= 0 || report.RelativeSpeedVsReference <= 0 {
		t.Fatalf("reference timing = %+v relative=%f, want positive values", *report.Reference, report.RelativeSpeedVsReference)
	}
}

func TestParseIVFFrameInfoClassifiesAllKeyframes(t *testing.T) {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	payloads := [][]byte{
		{0x10, 0x00, 0x9d, 0x01}, // key frame: low bit clear
		{0x11, 0x00, 0x00, 0x00}, // inter frame: low bit set
		{0x20, 0x00, 0x9d, 0x01}, // later forced key frame
	}
	size := fileHeaderSize
	for _, payload := range payloads {
		size += frameHeaderSize + len(payload)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP80"))
	offset := fileHeaderSize
	for i, payload := range payloads {
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(len(payload)))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(i))
		offset += frameHeaderSize
		copy(ivf[offset:], payload)
		offset += len(payload)
	}

	frames, err := parseIVFFrameInfo(ivf)
	if err != nil {
		t.Fatalf("parseIVFFrameInfo returned error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("frames len = %d, want 3", len(frames))
	}
	if !frames[0].keyFrame || frames[1].keyFrame || !frames[2].keyFrame {
		t.Fatalf("key classification = [%v %v %v], want [true false true]", frames[0].keyFrame, frames[1].keyFrame, frames[2].keyFrame)
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		t.Fatalf("parseIVFFrameSizes returned error: %v", err)
	}
	if !slices.Equal(sizes, []int{4, 4, 4}) {
		t.Fatalf("sizes = %v, want [4 4 4]", sizes)
	}
}
