package codec_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9EncoderProfile3Validation(t *testing.T) {
	badOpts := []struct {
		name string
		opts govpx.VP9EncoderOptions
	}{
		{
			name: "profile_4_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 4, BitDepth: 10, Subsampling: govpx.Subsampling444},
		},
		{
			name: "profile_3_with_8bit_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 3, BitDepth: 8, Subsampling: govpx.Subsampling444},
		},
		{
			name: "profile_3_with_420_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 3, BitDepth: 10, Subsampling: govpx.Subsampling420},
		},
		{
			name: "profile_3_srgb_with_422_rejected",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64, Profile: 3, BitDepth: 10,
				Subsampling: govpx.Subsampling422,
				ColorSpace:  govpx.VP9ColorSpaceSRGB,
			},
		},
	}
	for _, tc := range badOpts {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := govpx.NewVP9Encoder(tc.opts); err != govpx.ErrInvalidConfig {
				t.Fatalf("NewVP9Encoder(%s) = %v, want ErrInvalidConfig", tc.name, err)
			}
		})
	}
}

func makeTestHighBDSubsampledImage(w, h, frameIdx int, bd uint8, sub govpx.ChromaSubsampling) govpx.Image {
	uvW, uvH := sub.ChromaDimensions(w, h)
	maxVal := (1 << uint(bd)) - 1
	img := govpx.Image{
		Width:       w,
		Height:      h,
		Y16:         make([]uint16, w*h),
		U16:         make([]uint16, uvW*uvH),
		V16:         make([]uint16, uvW*uvH),
		YStride:     w,
		UStride:     uvW,
		VStride:     uvW,
		Subsampling: sub,
		BitDepth:    bd,
	}
	for y := range h {
		for x := range w {
			img.Y16[y*w+x] = uint16((x*11 + y*17 + frameIdx*31 + ((x ^ y) << 2)) & maxVal)
		}
	}
	for y := range uvH {
		for x := range uvW {
			img.U16[y*uvW+x] = uint16((maxVal/4 + x*13 + y*7 + frameIdx*19) & maxVal)
			img.V16[y*uvW+x] = uint16((3*maxVal/4 - x*9 + y*15 + frameIdx*23) & maxVal)
		}
	}
	return img
}

func TestVP9EncoderProfile3RoundtripAndLibvpxDecode(t *testing.T) {
	ffmpeg := findFFmpegWithLibvpx(t)

	cases := []struct {
		name     string
		opts     govpx.VP9EncoderOptions
		pixFmt   string
		frames   int
		lossless bool
	}{
		{
			name: "10bit_422_160x120_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 3, BitDepth: 10,
				Subsampling: govpx.Subsampling422, Quantizer: 80,
			},
			pixFmt: "yuv422p10le",
			frames: 4,
		},
		{
			name: "10bit_444_160x120_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 3, BitDepth: 10,
				Subsampling: govpx.Subsampling444, Quantizer: 80,
			},
			pixFmt: "yuv444p10le",
			frames: 4,
		},
		{
			name: "10bit_444_133x99_odd_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 133, Height: 99, Profile: 3, BitDepth: 10,
				Subsampling: govpx.Subsampling444, Quantizer: 96,
			},
			pixFmt: "yuv444p10le",
			frames: 4,
		},
		{
			name: "10bit_444_64x48_lossless",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 48, Profile: 3, BitDepth: 10,
				Subsampling: govpx.Subsampling444, Lossless: true,
			},
			pixFmt:   "yuv444p10le",
			frames:   3,
			lossless: true,
		},
		{
			name: "10bit_gbrp_128x96_srgb_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 128, Height: 96, Profile: 3, BitDepth: 10,
				Subsampling: govpx.Subsampling444,
				ColorSpace:  govpx.VP9ColorSpaceSRGB,
				ColorRange:  govpx.VP9ColorRangeFull,
				Quantizer:   80,
			},
			pixFmt: "gbrp10le",
			frames: 4,
		},
		{
			name: "12bit_422_160x120_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 3, BitDepth: 12,
				Subsampling: govpx.Subsampling422, Quantizer: 80,
			},
			pixFmt: "yuv422p12le",
			frames: 4,
		},
		{
			name: "12bit_444_160x120_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 3, BitDepth: 12,
				Subsampling: govpx.Subsampling444, Quantizer: 80,
			},
			pixFmt: "yuv444p12le",
			frames: 4,
		},
		{
			name: "12bit_444_64x48_lossless",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 48, Profile: 3, BitDepth: 12,
				Subsampling: govpx.Subsampling444, Lossless: true,
			},
			pixFmt:   "yuv444p12le",
			frames:   3,
			lossless: true,
		},
	}

	tmpDir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := govpx.NewVP9Encoder(tc.opts)
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer func() { _ = enc.Close() }()

			dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			defer func() { _ = dec.Close() }()

			dst := make([]byte, 1024*1024)
			var packets [][]byte
			var goDecoded []byte
			for i := range tc.frames {
				src := makeTestHighBDSubsampledImage(tc.opts.Width, tc.opts.Height, i, tc.opts.BitDepth, tc.opts.Subsampling)
				n, err := enc.EncodeImageInto(&src, dst)
				if err != nil {
					t.Fatalf("EncodeImageInto frame %d: %v", i, err)
				}
				pkt := append([]byte(nil), dst[:n]...)
				packets = append(packets, pkt)

				if i == 0 {
					si, err := govpx.PeekVP9StreamInfo(pkt)
					if err != nil {
						t.Fatalf("PeekVP9StreamInfo: %v", err)
					}
					if si.Profile != 3 {
						t.Fatalf("PeekVP9StreamInfo.Profile = %d, want 3", si.Profile)
					}
				}
				if err := dec.Decode(pkt); err != nil {
					t.Fatalf("Decode frame %d: %v", i, err)
				}
				img, ok := dec.NextFrame()
				if !ok {
					t.Fatalf("NextFrame(%d) returned ok=false", i)
				}
				if img.BitDepth != tc.opts.BitDepth || img.Subsampling != tc.opts.Subsampling {
					t.Fatalf("frame %d format = (%d, %v), want (%d, %v)",
						i, img.BitDepth, img.Subsampling, tc.opts.BitDepth, tc.opts.Subsampling)
				}
				if tc.lossless {
					uvW, uvH := img.Subsampling.ChromaDimensions(tc.opts.Width, tc.opts.Height)
					for y := range tc.opts.Height {
						for x := range tc.opts.Width {
							if img.Y16[y*img.YStride+x] != src.Y16[y*src.YStride+x] {
								t.Fatalf("lossless Y mismatch frame %d (%d,%d): got=%d want=%d",
									i, x, y, img.Y16[y*img.YStride+x], src.Y16[y*src.YStride+x])
							}
						}
					}
					for y := range uvH {
						for x := range uvW {
							if img.U16[y*img.UStride+x] != src.U16[y*src.UStride+x] {
								t.Fatalf("lossless U mismatch frame %d (%d,%d)", i, x, y)
							}
							if img.V16[y*img.VStride+x] != src.V16[y*src.VStride+x] {
								t.Fatalf("lossless V mismatch frame %d (%d,%d)", i, x, y)
							}
						}
					}
				}
				goDecoded = appendVP9HighBDForTest(goDecoded, img)
			}

			ivfPath := filepath.Join(tmpDir, tc.name+".ivf")
			refPath := filepath.Join(tmpDir, tc.name+".ref.yuv")
			if err := writeVP9IVFForTest(ivfPath, tc.opts.Width, tc.opts.Height, packets); err != nil {
				t.Fatalf("writeVP9IVFForTest: %v", err)
			}
			if out, err := exec.Command(ffmpeg,
				"-y", "-hide_banner", "-loglevel", "error",
				"-c:v", "libvpx-vp9",
				"-i", ivfPath,
				"-f", "rawvideo", "-pix_fmt", tc.pixFmt, refPath,
			).CombinedOutput(); err != nil {
				t.Fatalf("ffmpeg/libvpx decode failed on govpx Profile 3 stream: %v\n%s", err, out)
			}
			want, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("ReadFile ref: %v", err)
			}
			if !bytes.Equal(goDecoded, want) {
				for i := range min(len(goDecoded), len(want)) {
					if goDecoded[i] != want[i] {
						t.Fatalf("govpx vs libvpx decode mismatch at byte %d (got len=%d want len=%d): got=%d want=%d",
							i, len(goDecoded), len(want), goDecoded[i], want[i])
					}
				}
				t.Fatalf("govpx vs libvpx decode length mismatch: got=%d want=%d", len(goDecoded), len(want))
			}
		})
	}
}

func TestVP9EncoderProfile3Subsampling440AndAdvanced(t *testing.T) {
	ffmpeg := findFFmpegWithLibvpx(t)
	tmpDir := t.TempDir()

	// 1. 4:4:0 10-bit lossless roundtrip in govpx (exercises SS01 chroma path end-to-end).
	w440, h440 := 64, 48
	enc440, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: w440, Height: h440, Profile: 3, BitDepth: 10,
		Subsampling: govpx.Subsampling440, Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder 440: %v", err)
	}
	defer func() { _ = enc440.Close() }()
	dec440, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder 440: %v", err)
	}
	defer func() { _ = dec440.Close() }()

	for i := range 3 {
		src := makeTestHighBDSubsampledImage(w440, h440, i, 10, govpx.Subsampling440)
		pkt, err := enc440.EncodeImage(&src)
		if err != nil {
			t.Fatalf("EncodeImage 440(%d): %v", i, err)
		}
		if err := dec440.Decode(pkt); err != nil {
			t.Fatalf("Decode 440(%d): %v", i, err)
		}
		img, ok := dec440.NextFrame()
		if !ok || img.Subsampling != govpx.Subsampling440 || img.BitDepth != 10 {
			t.Fatalf("NextFrame 440(%d): ok=%v sub=%v bd=%d", i, ok, img.Subsampling, img.BitDepth)
		}
		uvW, uvH := img.Subsampling.ChromaDimensions(w440, h440)
		for y := range uvH {
			for x := range uvW {
				if img.U16[y*img.UStride+x] != src.U16[y*src.UStride+x] ||
					img.V16[y*img.VStride+x] != src.V16[y*src.VStride+x] {
					t.Fatalf("440 lossless chroma mismatch frame %d (%d,%d)", i, x, y)
				}
			}
		}
	}

	// 2. 512x128 10-bit 4:2:2 multi-tile + CBR + BT.2020 + RenderSize + ShowExistingFrame vs libvpx.
	w, h := 512, 128
	enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             w,
		Height:            h,
		RenderWidth:       640,
		RenderHeight:      160,
		Profile:           3,
		BitDepth:          10,
		Subsampling:       govpx.Subsampling422,
		ColorSpace:        govpx.VP9ColorSpaceBT2020,
		ColorRange:        govpx.VP9ColorRangeFull,
		DeltaQUV:          -4,
		RateControlMode:   govpx.RateControlCBR,
		TargetBitrateKbps: 1000,
		Threads:           2,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder advanced: %v", err)
	}
	defer func() { _ = enc.Close() }()

	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{Threads: 2})
	if err != nil {
		t.Fatalf("NewVP9Decoder advanced: %v", err)
	}
	defer func() { _ = dec.Close() }()

	var packets [][]byte
	var goDecoded []byte
	for i := range 3 {
		src := makeTestHighBDSubsampledImage(w, h, i, 10, govpx.Subsampling422)
		pkt, err := enc.EncodeImage(&src)
		if err != nil {
			t.Fatalf("EncodeImage(%d): %v", i, err)
		}
		packets = append(packets, pkt)
		if err := dec.Decode(pkt); err != nil {
			t.Fatalf("Decode(%d): %v", i, err)
		}
		img, ok := dec.NextFrame()
		if !ok {
			t.Fatalf("NextFrame(%d) = false", i)
		}
		goDecoded = appendVP9HighBDForTest(goDecoded, img)
	}
	showPkt, err := enc.EncodeShowExistingFrame(0)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame: %v", err)
	}
	packets = append(packets, showPkt)
	if err := dec.Decode(showPkt); err != nil {
		t.Fatalf("Decode showExisting: %v", err)
	}
	if img, ok := dec.NextFrame(); !ok {
		t.Fatalf("NextFrame showExisting = false")
	} else {
		goDecoded = appendVP9HighBDForTest(goDecoded, img)
	}

	ivfPath := filepath.Join(tmpDir, "advanced_p3.ivf")
	refPath := filepath.Join(tmpDir, "advanced_p3.ref.yuv")
	if err := writeVP9IVFForTest(ivfPath, w, h, packets); err != nil {
		t.Fatalf("writeVP9IVFForTest: %v", err)
	}
	if out, err := exec.Command(ffmpeg,
		"-y", "-hide_banner", "-loglevel", "error",
		"-c:v", "libvpx-vp9",
		"-i", ivfPath,
		"-f", "rawvideo", "-pix_fmt", "yuv422p10le", refPath,
	).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg/libvpx decode failed on advanced Profile 3 stream: %v\n%s", err, out)
	}
	want, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(goDecoded, want) {
		t.Fatalf("advanced Profile 3 govpx vs libvpx mismatch (got %d, want %d)", len(goDecoded), len(want))
	}
}
