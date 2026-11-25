package codec_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9EncoderProfile2Validation(t *testing.T) {
	badOpts := []struct {
		name string
		opts govpx.VP9EncoderOptions
	}{
		{
			name: "profile_4_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 4},
		},
		{
			name: "profile_2_with_8bit_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 2, BitDepth: 8},
		},
		{
			name: "profile_2_with_444_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 2, BitDepth: 10, Subsampling: govpx.Subsampling444},
		},
		{
			name: "profile_0_with_10bit_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 0, BitDepth: 10},
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

func makeTestHighBDImage(w, h, frameIdx int, bd uint8) govpx.Image {
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
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
		Subsampling: govpx.Subsampling420,
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

func TestVP9EncoderProfile2RoundtripAndLibvpxDecode(t *testing.T) {
	ffmpeg := findFFmpegWithLibvpx(t)

	cases := []struct {
		name     string
		opts     govpx.VP9EncoderOptions
		pixFmt   string
		frames   int
		lossless bool
	}{
		{
			name: "10bit_160x120_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 2, BitDepth: 10, Quantizer: 80,
			},
			pixFmt: "yuv420p10le",
			frames: 5,
		},
		{
			name: "10bit_133x99_odd_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 133, Height: 99, Profile: 2, BitDepth: 10, Quantizer: 96,
			},
			pixFmt: "yuv420p10le",
			frames: 4,
		},
		{
			name: "10bit_64x48_lossless",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 48, Profile: 2, BitDepth: 10, Lossless: true,
			},
			pixFmt:   "yuv420p10le",
			frames:   3,
			lossless: true,
		},
		{
			name: "12bit_160x120_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 2, BitDepth: 12, Quantizer: 80,
			},
			pixFmt: "yuv420p12le",
			frames: 5,
		},
		{
			name: "12bit_133x99_odd_lossy",
			opts: govpx.VP9EncoderOptions{
				Width: 133, Height: 99, Profile: 2, BitDepth: 12, Quantizer: 96,
			},
			pixFmt: "yuv420p12le",
			frames: 4,
		},
		{
			name: "12bit_64x48_lossless",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 48, Profile: 2, BitDepth: 12, Lossless: true,
			},
			pixFmt:   "yuv420p12le",
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
			defer enc.Close()

			dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			defer dec.Close()

			dst := make([]byte, 512*1024)
			var packets [][]byte
			var goDecoded []byte
			for i := range tc.frames {
				src := makeTestHighBDImage(tc.opts.Width, tc.opts.Height, i, tc.opts.BitDepth)
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
					if si.Profile != 2 {
						t.Fatalf("PeekVP9StreamInfo.Profile = %d, want 2", si.Profile)
					}
				}
				if err := dec.Decode(pkt); err != nil {
					t.Fatalf("Decode frame %d: %v", i, err)
				}
				img, ok := dec.NextFrame()
				if !ok {
					t.Fatalf("NextFrame(%d) returned ok=false", i)
				}
				if img.BitDepth != tc.opts.BitDepth {
					t.Fatalf("frame %d BitDepth = %d, want %d", i, img.BitDepth, tc.opts.BitDepth)
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

			// Verify libvpx (via ffmpeg) decodes our Profile 2 stream byte-for-byte identical to govpx!
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
				t.Fatalf("ffmpeg/libvpx decode failed on govpx Profile 2 stream: %v\n%s", err, out)
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

func TestVP9Profile2DecodeIntoAndShowExistingFrame(t *testing.T) {
	w, h := 64, 48
	enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:    w,
		Height:   h,
		Profile:  2,
		BitDepth: 10,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer func() { _ = enc.Close() }()

	src := makeTestHighBDImage(w, h, 0, 10)
	buf := make([]byte, 256*1024)
	n, err := enc.EncodeImageInto(&src, buf)
	if err != nil {
		t.Fatalf("EncodeImageInto: %v", err)
	}
	pkt0 := append([]byte(nil), buf[:n]...)

	showPkt, err := enc.EncodeShowExistingFrame(0)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame: %v", err)
	}

	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer func() { _ = dec.Close() }()

	uvW, uvH := (w+1)>>1, (h+1)>>1
	// Wrong BitDepth (8-bit Image) must be rejected.
	wrong8 := govpx.Image{
		Width: w, Height: h,
		Y: make([]byte, w*h), U: make([]byte, uvW*uvH), V: make([]byte, uvW*uvH),
		YStride: w, UStride: uvW, VStride: uvW,
		Subsampling: govpx.Subsampling420,
		BitDepth:    8,
	}
	if _, err := dec.DecodeInto(pkt0, &wrong8); err != govpx.ErrInvalidConfig {
		t.Fatalf("DecodeInto(8-bit dst) = %v, want ErrInvalidConfig", err)
	}

	// Wrong BitDepth (12-bit dst for 10-bit stream) must be rejected.
	wrong12 := makeTestHighBDImage(w, h, 0, 12)
	if _, err := dec.DecodeInto(pkt0, &wrong12); err != govpx.ErrInvalidConfig {
		t.Fatalf("DecodeInto(12-bit dst) = %v, want ErrInvalidConfig", err)
	}

	// Matching 10-bit dst succeeds and matches lossless source pixel-for-pixel.
	dst10 := govpx.Image{
		Width: w, Height: h,
		Y16: make([]uint16, w*h), U16: make([]uint16, uvW*uvH), V16: make([]uint16, uvW*uvH),
		YStride: w, UStride: uvW, VStride: uvW,
		Subsampling: govpx.Subsampling420,
		BitDepth:    10,
	}
	info, err := dec.DecodeInto(pkt0, &dst10)
	if err != nil || !info.ShowFrame {
		t.Fatalf("DecodeInto(10-bit dst) = (%+v, %v), want ShowFrame=true, nil", info, err)
	}
	for i := range src.Y16 {
		if dst10.Y16[i] != src.Y16[i] {
			t.Fatalf("DecodeInto Y16[%d] = %d, want %d", i, dst10.Y16[i], src.Y16[i])
		}
	}

	// ShowExistingFrame on slot 0 also decodes pixel-exact into 10-bit dst.
	clear(dst10.Y16)
	info, err = dec.DecodeInto(showPkt, &dst10)
	if err != nil || !info.ShowFrame {
		t.Fatalf("DecodeInto(showExisting) = (%+v, %v), want ShowFrame=true, nil", info, err)
	}
	for i := range src.Y16 {
		if dst10.Y16[i] != src.Y16[i] {
			t.Fatalf("showExisting Y16[%d] = %d, want %d", i, dst10.Y16[i], src.Y16[i])
		}
	}
}

func TestVP9EncoderProfile2AdvancedFeatures(t *testing.T) {
	ffmpeg := findFFmpegWithLibvpx(t)
	tmpDir := t.TempDir()

	// Exercise multi-tile (512x128 with 2 tile cols), CBR rate control,
	// DeltaQUV, explicit RenderWidth/RenderHeight, BT.2020 full-range, and EncodeImage.
	w, h := 512, 128
	enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             w,
		Height:            h,
		RenderWidth:       640,
		RenderHeight:      160,
		Profile:           2,
		BitDepth:          10,
		ColorSpace:        govpx.VP9ColorSpaceBT2020,
		ColorRange:        govpx.VP9ColorRangeFull,
		DeltaQUV:          -4,
		RateControlMode:   govpx.RateControlCBR,
		TargetBitrateKbps: 800,
		Threads:           2,
		Log2TileRows:      0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer func() { _ = enc.Close() }()

	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{Threads: 2})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer func() { _ = dec.Close() }()

	var packets [][]byte
	var goDecoded []byte
	for i := range 4 {
		src := makeTestHighBDImage(w, h, i, 10)
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
		if i == 0 {
			info, ok := dec.LastFrameInfo()
			if !ok {
				t.Fatalf("LastFrameInfo() = false")
			}
			if info.RenderWidth != 640 || info.RenderHeight != 160 {
				t.Fatalf("RenderSize = %dx%d, want 640x160", info.RenderWidth, info.RenderHeight)
			}
		}
		goDecoded = appendVP9HighBDForTest(goDecoded, img)
	}

	ivfPath := filepath.Join(tmpDir, "advanced.ivf")
	refPath := filepath.Join(tmpDir, "advanced.ref.yuv")
	if err := writeVP9IVFForTest(ivfPath, w, h, packets); err != nil {
		t.Fatalf("writeVP9IVFForTest: %v", err)
	}
	if out, err := exec.Command(ffmpeg,
		"-y", "-hide_banner", "-loglevel", "error",
		"-c:v", "libvpx-vp9",
		"-i", ivfPath,
		"-f", "rawvideo", "-pix_fmt", "yuv420p10le", refPath,
	).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg/libvpx decode failed (pkt0 len=%d head=%x tail=%x): %v\n%s",
			len(packets[0]), packets[0][:min(16, len(packets[0]))],
			packets[0][max(0, len(packets[0])-8):], err, out)
	}
	want, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(goDecoded, want) {
		t.Fatalf("advanced Profile 2 govpx vs libvpx mismatch (got %d bytes, want %d bytes)", len(goDecoded), len(want))
	}
}
