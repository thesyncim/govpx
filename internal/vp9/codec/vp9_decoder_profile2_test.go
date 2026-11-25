package codec_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
)

func findFFmpegWithLibvpx(t *testing.T) string {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	return ffmpeg
}

func makeSyntheticHighBD420LE(w, h, frames, bd int) []byte {
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	maxVal := (1 << uint(bd)) - 1
	ySamples := w * h
	uvSamples := uvW * uvH
	out := make([]byte, frames*2*(ySamples+2*uvSamples))
	off := 0
	put := func(v int) {
		binary.LittleEndian.PutUint16(out[off:], uint16(v&maxVal))
		off += 2
	}
	for f := range frames {
		for y := range h {
			for x := range w {
				put((x*11 + y*17 + f*53 + ((x ^ y) << 2)) & maxVal)
			}
		}
		for y := range uvH {
			for x := range uvW {
				put((maxVal/4 + x*19 + y*7 + f*37) & maxVal)
			}
		}
		for y := range uvH {
			for x := range uvW {
				put((3*maxVal/4 - x*13 + y*23 + f*41) & maxVal)
			}
		}
	}
	return out
}

func appendVP9HighBDForTest(dst []byte, img govpx.Image) []byte {
	var tmp [2]byte
	for y := range img.Height {
		row := img.Y16[y*img.YStride : y*img.YStride+img.Width]
		for _, v := range row {
			binary.LittleEndian.PutUint16(tmp[:], v)
			dst = append(dst, tmp[:]...)
		}
	}
	uvW, uvH := img.Subsampling.ChromaDimensions(img.Width, img.Height)
	for y := range uvH {
		row := img.U16[y*img.UStride : y*img.UStride+uvW]
		for _, v := range row {
			binary.LittleEndian.PutUint16(tmp[:], v)
			dst = append(dst, tmp[:]...)
		}
	}
	for y := range uvH {
		row := img.V16[y*img.VStride : y*img.VStride+uvW]
		for _, v := range row {
			binary.LittleEndian.PutUint16(tmp[:], v)
			dst = append(dst, tmp[:]...)
		}
	}
	return dst
}

func decodeVP9IVFVisibleHighBD(t *testing.T, ivf []byte, opts govpx.VP9DecoderOptions,
	wantBD uint8,
) []byte {
	t.Helper()
	d, err := govpx.NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		t.Fatalf("invalid IVF header")
	}
	var out []byte
	offset := testutil.IVFFileHeaderSize
	for idx := 0; offset < len(ivf); idx++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, idx)
		if err != nil {
			t.Fatalf("NextIVFFrame(%d): %v", idx, err)
		}
		if idx == 0 {
			si, err := govpx.PeekVP9StreamInfo(frame.Data)
			if err != nil {
				t.Fatalf("PeekVP9StreamInfo: %v", err)
			}
			if si.Profile != 2 {
				t.Fatalf("PeekVP9StreamInfo.Profile = %d, want 2", si.Profile)
			}
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d: %v", idx, err)
		}
		if img, ok := d.NextFrame(); ok {
			if img.BitDepth != wantBD {
				t.Fatalf("frame %d BitDepth = %d, want %d", idx, img.BitDepth, wantBD)
			}
			info, ok := d.LastFrameInfo()
			if !ok || info.BitDepth != int(wantBD) {
				t.Fatalf("frame %d LastFrameInfo.BitDepth = %d (ok=%v), want %d",
					idx, info.BitDepth, ok, wantBD)
			}
			out = appendVP9HighBDForTest(out, img)
		}
		offset = next
	}
	return out
}

func TestVP9DecoderProfile2MatchesLibvpxByteExact(t *testing.T) {
	ffmpeg := findFFmpegWithLibvpx(t)

	cases := []struct {
		name      string
		bd        int
		pixFmt    string
		w, h      int
		frames    int
		extraArgs []string
		checkMT   bool
	}{
		{
			name:   "10bit_160x120_inter",
			bd:     10,
			pixFmt: "yuv420p10le",
			w:      160,
			h:      120,
			frames: 6,
		},
		{
			name:   "10bit_133x99_odd",
			bd:     10,
			pixFmt: "yuv420p10le",
			w:      133,
			h:      99,
			frames: 4,
		},
		{
			name:      "10bit_96x64_lossless",
			bd:        10,
			pixFmt:    "yuv420p10le",
			w:         96,
			h:         64,
			frames:    3,
			extraArgs: []string{"-lossless", "1"},
		},
		{
			name:   "12bit_160x120_inter",
			bd:     12,
			pixFmt: "yuv420p12le",
			w:      160,
			h:      120,
			frames: 6,
		},
		{
			name:   "12bit_133x99_odd",
			bd:     12,
			pixFmt: "yuv420p12le",
			w:      133,
			h:      99,
			frames: 4,
		},
		{
			name:      "12bit_96x64_lossless",
			bd:        12,
			pixFmt:    "yuv420p12le",
			w:         96,
			h:         64,
			frames:    3,
			extraArgs: []string{"-lossless", "1"},
		},
		{
			name:      "10bit_512x128_multitile",
			bd:        10,
			pixFmt:    "yuv420p10le",
			w:         512,
			h:         128,
			frames:    4,
			extraArgs: []string{"-tile-columns", "1", "-frame-parallel", "1"},
			checkMT:   true,
		},
		{
			name:      "10bit_160x120_altref_compound",
			bd:        10,
			pixFmt:    "yuv420p10le",
			w:         160,
			h:         120,
			frames:    12,
			extraArgs: []string{"-auto-alt-ref", "1", "-lag-in-frames", "10", "-cpu-used", "2"},
		},
		{
			name:      "12bit_160x120_altref_compound",
			bd:        12,
			pixFmt:    "yuv420p12le",
			w:         160,
			h:         120,
			frames:    12,
			extraArgs: []string{"-auto-alt-ref", "1", "-lag-in-frames", "10", "-cpu-used", "2"},
		},
		{
			name:      "10bit_160x120_bt2020_full",
			bd:        10,
			pixFmt:    "yuv420p10le",
			w:         160,
			h:         120,
			frames:    4,
			extraArgs: []string{"-colorspace", "bt2020nc", "-color_range", "pc"},
		},
	}

	tmpDir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rawPath := filepath.Join(tmpDir, tc.name+".yuv")
			ivfPath := filepath.Join(tmpDir, tc.name+".ivf")
			refPath := filepath.Join(tmpDir, tc.name+".ref.yuv")

			raw := makeSyntheticHighBD420LE(tc.w, tc.h, tc.frames, tc.bd)
			if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
				t.Fatalf("WriteFile raw: %v", err)
			}

			encArgs := []string{
				"-y", "-hide_banner", "-loglevel", "error",
				"-f", "rawvideo",
				"-pix_fmt", tc.pixFmt,
				"-s", strconv.Itoa(tc.w) + "x" + strconv.Itoa(tc.h),
				"-r", "30",
				"-i", rawPath,
				"-c:v", "libvpx-vp9",
				"-profile:v", "2",
				"-pix_fmt", tc.pixFmt,
				"-b:v", "0",
			}
			hasLag := false
			hasLossless := false
			for _, a := range tc.extraArgs {
				if a == "-lag-in-frames" {
					hasLag = true
				}
				if a == "-lossless" {
					hasLossless = true
				}
			}
			if !hasLag {
				encArgs = append(encArgs, "-cpu-used", "6", "-lag-in-frames", "0")
			}
			if !hasLossless {
				encArgs = append(encArgs, "-crf", "32")
			}
			encArgs = append(encArgs, tc.extraArgs...)
			encArgs = append(encArgs, "-f", "ivf", ivfPath)

			if out, err := exec.Command(ffmpeg, encArgs...).CombinedOutput(); err != nil {
				t.Fatalf("ffmpeg encode failed: %v\n%s", err, out)
			}
			if out, err := exec.Command(ffmpeg,
				"-y", "-hide_banner", "-loglevel", "error",
				"-c:v", "libvpx-vp9",
				"-i", ivfPath,
				"-f", "rawvideo", "-pix_fmt", tc.pixFmt, refPath,
			).CombinedOutput(); err != nil {
				t.Fatalf("ffmpeg decode failed: %v\n%s", err, out)
			}

			ivf, err := os.ReadFile(ivfPath)
			if err != nil {
				t.Fatalf("ReadFile ivf: %v", err)
			}
			want, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("ReadFile ref: %v", err)
			}

			got := decodeVP9IVFVisibleHighBD(t, ivf, govpx.VP9DecoderOptions{}, uint8(tc.bd))
			if !bytes.Equal(got, want) {
				for i := 0; i+1 < min(len(got), len(want)); i += 2 {
					gv := binary.LittleEndian.Uint16(got[i:])
					wv := binary.LittleEndian.Uint16(want[i:])
					if gv != wv {
						t.Fatalf("serial mismatch at sample %d (got len=%d want len=%d): got=%d want=%d",
							i/2, len(got), len(want), gv, wv)
					}
				}
				t.Fatalf("serial length mismatch: got=%d want=%d", len(got), len(want))
			}

			if tc.checkMT {
				for _, mtOpts := range []govpx.VP9DecoderOptions{
					{Threads: 2, DecoderLoopFilterOpt: true},
					{Threads: 2, DecoderRowMT: true, DecoderLoopFilterOpt: true},
				} {
					gotMT := decodeVP9IVFVisibleHighBD(t, ivf, mtOpts, uint8(tc.bd))
					if !bytes.Equal(gotMT, want) {
						t.Fatalf("threaded (%+v) mismatch vs libvpx", mtOpts)
					}
				}
			}
		})
	}
}
