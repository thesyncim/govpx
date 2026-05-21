//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"image"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func bpredEdgeClampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func makeBPredEdgeGridFrame(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(idx)*9973 + 113))
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			row[x] = bpredEdgeClampByte(112 + r.Intn(3) - 1)
		}
	}
	xoff := idx
	yoff := idx
	const block = 4
	renderBlock := func(dir, x0, y0 int, lumaHi, lumaLo byte) {
		for dy := range block {
			y := y0 + dy
			if y < 0 || y >= height {
				continue
			}
			row := img.Y[y*img.YStride:]
			for dx := range block {
				x := x0 + dx
				if x < 0 || x >= width {
					continue
				}
				var on bool
				switch dir & 0x07 {
				case 0:
					on = dy < 2
				case 1:
					on = dx < 2
				case 2:
					on = dx+dy < 3
				case 3:
					on = dx >= dy
				case 4:
					on = 2*dx+dy < 5
				case 5:
					on = 2*dx-dy < 3
				case 6:
					on = dx+2*dy < 5
				case 7:
					on = dx-2*dy < 1
				}
				if on {
					row[x] = lumaHi
				} else {
					row[x] = lumaLo
				}
			}
		}
	}
	const bandHeight = 64
	for gy := 0; gy < height; gy += block {
		if (gy/bandHeight)&1 != 0 {
			continue
		}
		for gx := 0; gx < width; gx += block {
			cx := gx / block
			cy := gy / block
			dir := (cx*3 + cy*5) & 0x07
			hash := cx*1103515245 + cy*12345
			lumaHi := byte(128 + (hash>>3)&0x0F)
			lumaLo := byte(112 - (hash>>11)&0x0F)
			renderBlock(dir, gx+xoff, gy+yoff, lumaHi, lumaLo)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = byte(128 + ((x+idx)*3)&0x07)
			cr[x] = byte(128 + ((y+idx*2)*3)&0x07)
		}
	}
	return img
}

func TestVP8BPredEdgeGridRecodeParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run B_PRED edge-grid recode parity")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := coracletest.VpxencOracle(t)

	const (
		width  = 1280
		height = 720
		frames = 12
	)
	targetKbps := 4000
	if env := os.Getenv("GOVPX_TASK384_TARGET_KBPS"); env != "" {
		n, err := strconv.Atoi(env)
		if err != nil || n <= 0 {
			t.Fatalf("invalid GOVPX_TASK384_TARGET_KBPS=%q", env)
		}
		targetKbps = n
	}

	sources := make([]Image, frames)
	for i := range frames {
		yc := makeBPredEdgeGridFrame(width, height, i)
		sources[i] = Image{
			Width:   width,
			Height:  height,
			Y:       yc.Y,
			U:       yc.Cb,
			V:       yc.Cr,
			YStride: yc.YStride,
			UStride: yc.CStride,
			VStride: yc.CStride,
		}
	}

	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		Deadline:          DeadlineGoodQuality,
		MinQuantizer:      4,
		MaxQuantizer:      63,
		QuantizerRangeSet: true,
		KeyFrameInterval:  999,
		Threads:           1,
	}

	var govpxTrace bytes.Buffer
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(&govpxTrace)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		if _, err := enc.EncodeInto(packet, src, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	libvpxTrace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(
			vpxencOracle,
			opts,
			len(sources),
			targetKbps,
			nil,
			[]string{"--end-usage=cbr"},
		),
	)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}
	t.Logf("bpred_edge target=%d govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		targetKbps, govpxTrace.Len(), len(libvpxTrace))
}
