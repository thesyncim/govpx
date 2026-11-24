package benchcmd

import (
	"encoding/binary"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func atoiPositive(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func fakeVpxencPath(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-vpxenc")
	body := fmt.Sprintf("#!/bin/sh\nGOVPX_FAKE_VPXENC=1 exec %s -test.run=TestFakeVpxencHelper -- \"$@\"\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return script
}

func fakeLibvpxOraclePath(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-libvpx-oracle")
	body := fmt.Sprintf("#!/bin/sh\nGOVPX_FAKE_LIBVPX_ORACLE=1 exec %s -test.run=TestFakeLibvpxOracleHelper -- \"$@\"\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return script
}

func fakeVpxdecVP9Path(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-vpxdec-vp9")
	body := fmt.Sprintf("#!/bin/sh\nGOVPX_FAKE_VPXDEC_VP9=1 exec %s -test.run=TestFakeVpxdecVP9Helper -- \"$@\"\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake vpxdec-vp9: %v", err)
	}
	return script
}

func fakeVpxdecVP9PathWithArgsLog(t *testing.T, argsPath string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-vpxdec-vp9")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %s\nGOVPX_FAKE_VPXDEC_VP9=1 exec %s -test.run=TestFakeVpxdecVP9Helper -- \"$@\"\n",
		shellQuote(argsPath), shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake vpxdec-vp9: %v", err)
	}
	return script
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func writeFakeIVF(path string, width int, height int, fps int, bitrate int, frames int) error {
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   bitrate,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    fps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return err
	}
	packets := make([][]byte, 0, frames)
	packet := make([]byte, max(4096, width*height*6))
	for i := range frames {
		result, err := enc.EncodeInto(packet, makeBenchmarkFrame(width, height, i), uint64(i), 1, 0)
		if err != nil {
			return err
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}

	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for _, packet := range packets {
		size += frameHeaderSize + len(packet)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[4:], 0)
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(ivf[12:], uint16(width))
	binary.LittleEndian.PutUint16(ivf[14:], uint16(height))
	binary.LittleEndian.PutUint32(ivf[16:], uint32(fps))
	binary.LittleEndian.PutUint32(ivf[20:], 1)
	binary.LittleEndian.PutUint32(ivf[24:], uint32(len(packets)))
	offset := fileHeaderSize
	for i, packet := range packets {
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(len(packet)))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(i))
		offset += frameHeaderSize
		copy(ivf[offset:], packet)
		offset += len(packet)
	}
	return os.WriteFile(path, ivf, 0o600)
}

func writeFakeVP9IVF(path string, width int, height int, fps int, bitrate int, frames int) error {
	enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                fps,
		Deadline:           govpx.DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  bitrate,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	})
	if err != nil {
		return err
	}
	defer enc.Close()

	packets := make([][]byte, 0, frames)
	packet := make([]byte, max(4096, width*height*6))
	for i := range frames {
		result, err := enc.EncodeIntoWithResult(imageToYCbCr(makeBenchmarkFrame(width, height, i)), packet)
		if err != nil {
			return err
		}
		if len(result.Data) == 0 {
			continue
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}

	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for _, packet := range packets {
		size += frameHeaderSize + len(packet)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[4:], 0)
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP90"))
	binary.LittleEndian.PutUint16(ivf[12:], uint16(width))
	binary.LittleEndian.PutUint16(ivf[14:], uint16(height))
	binary.LittleEndian.PutUint32(ivf[16:], uint32(fps))
	binary.LittleEndian.PutUint32(ivf[20:], 1)
	binary.LittleEndian.PutUint32(ivf[24:], uint32(len(packets)))
	offset := fileHeaderSize
	for i, packet := range packets {
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(len(packet)))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(i))
		offset += frameHeaderSize
		copy(ivf[offset:], packet)
		offset += len(packet)
	}
	return os.WriteFile(path, ivf, 0o600)
}
