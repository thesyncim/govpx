package codec_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
)

func findLocalVP9OracleTools(t *testing.T) (vpxenc, vpxdec string) {
	t.Helper()
	candidates := [][2]string{
		{"../../coracle/build/vpxenc-vp9", "../../coracle/build/vpxdec-vp9"},
		{"/opt/homebrew/opt/libvpx/bin/vpxenc", "/opt/homebrew/opt/libvpx/bin/vpxdec"},
	}
	for _, pair := range candidates {
		if _, err1 := os.Stat(pair[0]); err1 == nil {
			if _, err2 := os.Stat(pair[1]); err2 == nil {
				return pair[0], pair[1]
			}
		}
	}
	t.Skip("local libvpx vpxenc-vp9/vpxdec-vp9 binaries not available")
	return "", ""
}

func makeSyntheticSubsampledFrames(w, h, frames int, sub govpx.ChromaSubsampling) []byte {
	uvW, uvH := sub.ChromaDimensions(w, h)
	ySize := w * h
	uvSize := uvW * uvH
	out := make([]byte, frames*(ySize+2*uvSize))
	off := 0
	for f := range frames {
		for y := range h {
			for x := range w {
				out[off+y*w+x] = byte((x*3 + y*5 + f*19 + ((x ^ y) & 31)) & 0xff)
			}
		}
		off += ySize
		for y := range uvH {
			for x := range uvW {
				out[off+y*uvW+x] = byte((64 + x*7 + y*3 + f*13) & 0xff)
			}
		}
		off += uvSize
		for y := range uvH {
			for x := range uvW {
				out[off+y*uvW+x] = byte((192 - x*5 + y*11 + f*23) & 0xff)
			}
		}
		off += uvSize
	}
	return out
}

func appendVP9SubsampledForTest(dst []byte, img govpx.Image) []byte {
	for y := range img.Height {
		row := img.Y[y*img.YStride : y*img.YStride+img.Width]
		dst = append(dst, row...)
	}
	uvW, uvH := img.Subsampling.ChromaDimensions(img.Width, img.Height)
	for y := range uvH {
		row := img.U[y*img.UStride : y*img.UStride+uvW]
		dst = append(dst, row...)
	}
	for y := range uvH {
		row := img.V[y*img.VStride : y*img.VStride+uvW]
		dst = append(dst, row...)
	}
	return dst
}

func decodeVP9IVFVisibleSubsampled(t *testing.T, ivf []byte, opts govpx.VP9DecoderOptions,
	wantSub govpx.ChromaSubsampling,
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
			if si.Profile != 1 {
				t.Fatalf("PeekVP9StreamInfo.Profile = %d, want 1", si.Profile)
			}
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d: %v", idx, err)
		}
		if img, ok := d.NextFrame(); ok {
			if img.Subsampling != wantSub {
				t.Fatalf("frame %d Subsampling = %v, want %v", idx, img.Subsampling, wantSub)
			}
			info, ok := d.LastFrameInfo()
			if !ok || info.Subsampling != wantSub {
				t.Fatalf("frame %d LastFrameInfo.Subsampling = %v (ok=%v), want %v",
					idx, info.Subsampling, ok, wantSub)
			}
			out = appendVP9SubsampledForTest(out, img)
		}
		offset = next
	}
	return out
}

func TestVP9DecoderProfile1MatchesLibvpxByteExact(t *testing.T) {
	vpxenc, vpxdec := findLocalVP9OracleTools(t)

	cases := []struct {
		name      string
		sub       govpx.ChromaSubsampling
		inputFlag string
		w, h      int
		frames    int
		extraArgs []string
		checkMT   bool
	}{
		{
			name:      "422_160x120_inter",
			sub:       govpx.Subsampling422,
			inputFlag: "--i422",
			w:         160,
			h:         120,
			frames:    6,
		},
		{
			name:      "422_133x99_odd",
			sub:       govpx.Subsampling422,
			inputFlag: "--i422",
			w:         133,
			h:         99,
			frames:    4,
		},
		{
			name:      "422_96x64_lossless",
			sub:       govpx.Subsampling422,
			inputFlag: "--i422",
			w:         96,
			h:         64,
			frames:    3,
			extraArgs: []string{"--lossless=1"},
		},
		{
			name:      "440_160x120_inter",
			sub:       govpx.Subsampling440,
			inputFlag: "--i440",
			w:         160,
			h:         120,
			frames:    6,
		},
		{
			name:      "440_133x99_odd",
			sub:       govpx.Subsampling440,
			inputFlag: "--i440",
			w:         133,
			h:         99,
			frames:    4,
		},
		{
			name:      "444_160x120_inter",
			sub:       govpx.Subsampling444,
			inputFlag: "--i444",
			w:         160,
			h:         120,
			frames:    6,
		},
		{
			name:      "444_133x99_odd",
			sub:       govpx.Subsampling444,
			inputFlag: "--i444",
			w:         133,
			h:         99,
			frames:    4,
		},
		{
			name:      "444_96x64_lossless",
			sub:       govpx.Subsampling444,
			inputFlag: "--i444",
			w:         96,
			h:         64,
			frames:    3,
			extraArgs: []string{"--lossless=1"},
		},
		{
			name:      "444_512x128_multitile",
			sub:       govpx.Subsampling444,
			inputFlag: "--i444",
			w:         512,
			h:         128,
			frames:    4,
			extraArgs: []string{"--tile-columns=1", "--frame-parallel=1"},
			checkMT:   true,
		},
		{
			name:      "422_512x128_multitile",
			sub:       govpx.Subsampling422,
			inputFlag: "--i422",
			w:         512,
			h:         128,
			frames:    4,
			extraArgs: []string{"--tile-columns=1", "--frame-parallel=1"},
			checkMT:   true,
		},
	}

	tmpDir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rawPath := filepath.Join(tmpDir, tc.name+".yuv")
			ivfPath := filepath.Join(tmpDir, tc.name+".ivf")
			refPath := filepath.Join(tmpDir, tc.name+".ref.yuv")

			raw := makeSyntheticSubsampledFrames(tc.w, tc.h, tc.frames, tc.sub)
			if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
				t.Fatalf("WriteFile raw: %v", err)
			}

			encArgs := []string{
				"--codec=vp9",
				"--ivf",
				"--profile=1",
				tc.inputFlag,
				"--width=" + strconv.Itoa(tc.w),
				"--height=" + strconv.Itoa(tc.h),
				"--fps=30/1",
				"--passes=1",
				"--cpu-used=6",
				"--end-usage=cq",
				"--cq-level=32",
				"--lag-in-frames=0",
				"-o", ivfPath,
			}
			encArgs = append(encArgs, tc.extraArgs...)
			encArgs = append(encArgs, rawPath)

			if out, err := exec.Command(vpxenc, encArgs...).CombinedOutput(); err != nil {
				t.Fatalf("vpxenc failed: %v\n%s", err, out)
			}
			if out, err := exec.Command(vpxdec, "--rawvideo", "-o", refPath, ivfPath).CombinedOutput(); err != nil {
				t.Fatalf("vpxdec failed: %v\n%s", err, out)
			}

			ivf, err := os.ReadFile(ivfPath)
			if err != nil {
				t.Fatalf("ReadFile ivf: %v", err)
			}
			want, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("ReadFile ref: %v", err)
			}

			got := decodeVP9IVFVisibleSubsampled(t, ivf, govpx.VP9DecoderOptions{}, tc.sub)
			if !bytes.Equal(got, want) {
				for i := range min(len(got), len(want)) {
					if got[i] != want[i] {
						t.Fatalf("serial mismatch at byte %d (got len=%d want len=%d): got=%d want=%d",
							i, len(got), len(want), got[i], want[i])
					}
				}
				t.Fatalf("serial length mismatch: got=%d want=%d", len(got), len(want))
			}

			if tc.checkMT {
				for _, mtOpts := range []govpx.VP9DecoderOptions{
					{Threads: 2, DecoderLoopFilterOpt: true},
					{Threads: 2, DecoderRowMT: true, DecoderLoopFilterOpt: true},
				} {
					gotMT := decodeVP9IVFVisibleSubsampled(t, ivf, mtOpts, tc.sub)
					if !bytes.Equal(gotMT, want) {
						t.Fatalf("threaded (%+v) mismatch vs libvpx", mtOpts)
					}
				}
			}
		})
	}
}

func TestVP9DecoderProfile1DecodeIntoValidation(t *testing.T) {
	vpxenc, vpxdec := findLocalVP9OracleTools(t)
	tmpDir := t.TempDir()
	rawPath := filepath.Join(tmpDir, "in.yuv")
	ivfPath := filepath.Join(tmpDir, "in.ivf")
	refPath := filepath.Join(tmpDir, "ref.yuv")

	const w, h = 64, 48
	raw := makeSyntheticSubsampledFrames(w, h, 2, govpx.Subsampling444)
	if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(vpxenc,
		"--codec=vp9", "--ivf", "--profile=1", "--i444",
		"--width=64", "--height=48", "--fps=30/1",
		"--passes=1", "--cpu-used=6", "--end-usage=cq", "--cq-level=32", "--lag-in-frames=0",
		"-o", ivfPath, rawPath,
	).CombinedOutput(); err != nil {
		t.Fatalf("vpxenc: %v\n%s", err, out)
	}
	if out, err := exec.Command(vpxdec, "--rawvideo", "-o", refPath, ivfPath).CombinedOutput(); err != nil {
		t.Fatalf("vpxdec: %v\n%s", err, out)
	}
	ivf, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	frame, _, err := testutil.NextIVFFrame(ivf, testutil.IVFFileHeaderSize, 0)
	if err != nil {
		t.Fatal(err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Wrong subsampling (default 4:2:0) must be rejected cleanly.
	wrongDst := govpx.Image{
		Width:   w,
		Height:  h,
		Y:       make([]byte, w*h),
		U:       make([]byte, (w/2)*(h/2)),
		V:       make([]byte, (w/2)*(h/2)),
		YStride: w,
		UStride: w / 2,
		VStride: w / 2,
	}
	if _, err := d.DecodeInto(frame.Data, &wrongDst); err != govpx.ErrInvalidConfig {
		t.Fatalf("DecodeInto with 4:2:0 dst on 4:4:4 stream = %v, want ErrInvalidConfig", err)
	}

	// Matching 4:4:4 dst must decode byte-exact.
	dst := govpx.Image{
		Width:       w,
		Height:      h,
		Y:           make([]byte, w*h),
		U:           make([]byte, w*h),
		V:           make([]byte, w*h),
		YStride:     w,
		UStride:     w,
		VStride:     w,
		Subsampling: govpx.Subsampling444,
	}
	info, err := d.DecodeInto(frame.Data, &dst)
	if err != nil {
		t.Fatalf("DecodeInto 4:4:4: %v", err)
	}
	if info.Subsampling != govpx.Subsampling444 {
		t.Fatalf("info.Subsampling = %v, want 4:4:4", info.Subsampling)
	}
	got := appendVP9SubsampledForTest(nil, dst)
	frameBytes := 3 * w * h
	if !bytes.Equal(got, want[:frameBytes]) {
		t.Fatalf("DecodeInto 4:4:4 pixels mismatch vs libvpx")
	}
}
