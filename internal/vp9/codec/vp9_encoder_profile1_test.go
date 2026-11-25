package codec_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9EncoderProfile1Validation(t *testing.T) {
	badOpts := []struct {
		name string
		opts govpx.VP9EncoderOptions
	}{
		{
			name: "profile_2_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 2},
		},
		{
			name: "profile_0_with_422_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 0, Subsampling: govpx.Subsampling422},
		},
		{
			name: "profile_1_with_420_rejected",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64, Profile: 1, Subsampling: govpx.Subsampling420},
		},
		{
			name: "profile_1_srgb_with_422_rejected",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64, Profile: 1,
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

	// Mismatched YCbCr subsample ratio on EncodeInto must return ErrInvalidConfig.
	enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: 64, Height: 64, Profile: 1, Subsampling: govpx.Subsampling444,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder 4:4:4: %v", err)
	}
	defer enc.Close()

	wrongImg := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	dst := make([]byte, 64*1024)
	if _, err := enc.EncodeInto(wrongImg, dst); err != govpx.ErrInvalidConfig {
		t.Fatalf("EncodeInto with 4:2:0 img on 4:4:4 encoder = %v, want ErrInvalidConfig", err)
	}
}

func makeTestYCbCr(w, h, frameIdx int, sub govpx.ChromaSubsampling) *image.YCbCr {
	var ratio image.YCbCrSubsampleRatio
	switch sub {
	case govpx.Subsampling422:
		ratio = image.YCbCrSubsampleRatio422
	case govpx.Subsampling440:
		ratio = image.YCbCrSubsampleRatio440
	case govpx.Subsampling444:
		ratio = image.YCbCrSubsampleRatio444
	default:
		ratio = image.YCbCrSubsampleRatio420
	}
	img := image.NewYCbCr(image.Rect(0, 0, w, h), ratio)
	for y := range h {
		for x := range w {
			img.Y[y*img.YStride+x] = byte((x*5 + y*3 + frameIdx*17 + ((x ^ y) & 15)) & 0xff)
		}
	}
	uvW, uvH := sub.ChromaDimensions(w, h)
	for y := range uvH {
		for x := range uvW {
			img.Cb[y*img.CStride+x] = byte((48 + x*7 + y*5 + frameIdx*11) & 0xff)
			img.Cr[y*img.CStride+x] = byte((208 - x*3 + y*9 + frameIdx*13) & 0xff)
		}
	}
	return img
}

func writeVP9IVFForTest(path string, w, h int, packets [][]byte) error {
	var buf bytes.Buffer
	hdr := make([]byte, 32)
	copy(hdr[0:4], "DKIF")
	binary.LittleEndian.PutUint16(hdr[4:6], 0)
	binary.LittleEndian.PutUint16(hdr[6:8], 32)
	copy(hdr[8:12], "VP90")
	binary.LittleEndian.PutUint16(hdr[12:14], uint16(w))
	binary.LittleEndian.PutUint16(hdr[14:16], uint16(h))
	binary.LittleEndian.PutUint32(hdr[16:20], 30)
	binary.LittleEndian.PutUint32(hdr[20:24], 1)
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(len(packets)))
	buf.Write(hdr)
	var fhdr [12]byte
	for i, pkt := range packets {
		binary.LittleEndian.PutUint32(fhdr[0:4], uint32(len(pkt)))
		binary.LittleEndian.PutUint64(fhdr[4:12], uint64(i))
		buf.Write(fhdr[:])
		buf.Write(pkt)
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func TestVP9EncoderProfile1RoundtripAndLibvpxDecode(t *testing.T) {
	_, vpxdec := findLocalVP9OracleTools(t)

	cases := []struct {
		name     string
		opts     govpx.VP9EncoderOptions
		frames   int
		lossless bool
	}{
		{
			name: "422_160x120_realtime",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 1, Subsampling: govpx.Subsampling422,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6, Quantizer: 100,
			},
			frames: 5,
		},
		{
			name: "422_133x99_odd_good",
			opts: govpx.VP9EncoderOptions{
				Width: 133, Height: 99, Profile: 1, Subsampling: govpx.Subsampling422,
				Deadline: govpx.DeadlineGoodQuality, CpuUsed: 5, Quantizer: 96,
			},
			frames: 4,
		},
		{
			name: "422_64x48_lossless",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 48, Profile: 1, Subsampling: govpx.Subsampling422,
				Lossless: true, Deadline: govpx.DeadlineRealtime, CpuUsed: 6,
			},
			frames:   3,
			lossless: true,
		},
		{
			name: "440_160x120_realtime",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 1, Subsampling: govpx.Subsampling440,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6, Quantizer: 100,
			},
			frames: 5,
		},
		{
			name: "440_133x99_odd",
			opts: govpx.VP9EncoderOptions{
				Width: 133, Height: 99, Profile: 1, Subsampling: govpx.Subsampling440,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6, Quantizer: 96,
			},
			frames: 4,
		},
		{
			name: "444_160x120_realtime",
			opts: govpx.VP9EncoderOptions{
				Width: 160, Height: 120, Profile: 1, Subsampling: govpx.Subsampling444,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6, Quantizer: 100,
			},
			frames: 5,
		},
		{
			name: "444_133x99_odd_good",
			opts: govpx.VP9EncoderOptions{
				Width: 133, Height: 99, Profile: 1, Subsampling: govpx.Subsampling444,
				Deadline: govpx.DeadlineGoodQuality, CpuUsed: 5, Quantizer: 96,
			},
			frames: 4,
		},
		{
			name: "444_64x48_lossless_srgb",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 48, Profile: 1, Subsampling: govpx.Subsampling444,
				ColorSpace: govpx.VP9ColorSpaceSRGB, Lossless: true,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6,
			},
			frames:   3,
			lossless: true,
		},
		{
			name: "444_512x128_multitile_rowmt",
			opts: govpx.VP9EncoderOptions{
				Width: 512, Height: 128, Profile: 1, Subsampling: govpx.Subsampling444,
				Threads: 2, RowMT: true,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6, Quantizer: 100,
			},
			frames: 4,
		},
		{
			name: "422_512x128_multitile_rowmt",
			opts: govpx.VP9EncoderOptions{
				Width: 512, Height: 128, Profile: 1, Subsampling: govpx.Subsampling422,
				Threads: 2, RowMT: true,
				Deadline: govpx.DeadlineRealtime, CpuUsed: 6, Quantizer: 100,
			},
			frames: 4,
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
				src := makeTestYCbCr(tc.opts.Width, tc.opts.Height, i, tc.opts.Subsampling)
				n, err := enc.EncodeInto(src, dst)
				if err != nil {
					t.Fatalf("EncodeInto frame %d: %v", i, err)
				}
				pkt := append([]byte(nil), dst[:n]...)
				packets = append(packets, pkt)

				if i == 0 {
					si, err := govpx.PeekVP9StreamInfo(pkt)
					if err != nil {
						t.Fatalf("PeekVP9StreamInfo: %v", err)
					}
					if si.Profile != 1 {
						t.Fatalf("PeekVP9StreamInfo.Profile = %d, want 1", si.Profile)
					}
				}
				if err := dec.Decode(pkt); err != nil {
					t.Fatalf("Decode frame %d: %v", i, err)
				}
				img, ok := dec.NextFrame()
				if !ok {
					t.Fatalf("NextFrame(%d) returned ok=false", i)
				}
				if img.Subsampling != tc.opts.Subsampling {
					t.Fatalf("frame %d Subsampling = %v, want %v", i, img.Subsampling, tc.opts.Subsampling)
				}
				if tc.lossless {
					uvW, uvH := tc.opts.Subsampling.ChromaDimensions(tc.opts.Width, tc.opts.Height)
					for y := range tc.opts.Height {
						if !bytes.Equal(img.Y[y*img.YStride:y*img.YStride+tc.opts.Width],
							src.Y[y*src.YStride:y*src.YStride+tc.opts.Width]) {
							t.Fatalf("lossless Y mismatch frame %d row %d", i, y)
						}
					}
					for y := range uvH {
						if !bytes.Equal(img.U[y*img.UStride:y*img.UStride+uvW],
							src.Cb[y*src.CStride:y*src.CStride+uvW]) {
							t.Fatalf("lossless U mismatch frame %d row %d", i, y)
						}
						if !bytes.Equal(img.V[y*img.VStride:y*img.VStride+uvW],
							src.Cr[y*src.CStride:y*src.CStride+uvW]) {
							t.Fatalf("lossless V mismatch frame %d row %d", i, y)
						}
					}
				}
				goDecoded = appendVP9SubsampledForTest(goDecoded, img)
			}

			// Verify libvpx decodes our Profile 1 stream byte-for-byte identical to govpx!
			ivfPath := filepath.Join(tmpDir, tc.name+".ivf")
			refPath := filepath.Join(tmpDir, tc.name+".ref.yuv")
			if err := writeVP9IVFForTest(ivfPath, tc.opts.Width, tc.opts.Height, packets); err != nil {
				t.Fatalf("writeVP9IVFForTest: %v", err)
			}
			if out, err := exec.Command(vpxdec, "--rawvideo", "-o", refPath, ivfPath).CombinedOutput(); err != nil {
				t.Fatalf("vpxdec failed on govpx Profile 1 stream: %v\n%s", err, out)
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
