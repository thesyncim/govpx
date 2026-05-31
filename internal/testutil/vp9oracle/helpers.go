//go:build govpx_oracle_trace

package vp9oracle

import (
	"bytes"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vpxbuffers "github.com/thesyncim/govpx/internal/vpx/buffers"
)

func EncodeFramesWithGovpx(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr, flags []govpx.EncodeFlags,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("EncodeFramesWithGovpx: no sources")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	dstSize, err := vpxbuffers.I420EncodeBufferSize(width, height, 4096, 65536)
	if err != nil {
		t.Fatalf("I420EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		var f govpx.EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("VP9 EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			out = append(out, nil)
			continue
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

func LibvpxFrameFlags(flags []govpx.EncodeFlags) []uint32 {
	if len(flags) == 0 {
		return nil
	}
	out := make([]uint32, len(flags))
	for i, flag := range flags {
		out[i] = FrameFlagsForLibvpx(flag)
	}
	return out
}

func FrameFlagsForLibvpx(f govpx.EncodeFlags) uint32 {
	const (
		libvpxForceKF      = 1 << 0
		libvpxNoRefLast    = 1 << 16
		libvpxNoRefGF      = 1 << 17
		libvpxNoUpdLast    = 1 << 18
		libvpxForceGF      = 1 << 19
		libvpxNoUpdEntropy = 1 << 20
		libvpxNoRefARF     = 1 << 21
		libvpxNoUpdGF      = 1 << 22
		libvpxNoUpdARF     = 1 << 23
		libvpxForceARF     = 1 << 24
	)
	var out uint32
	if f&govpx.EncodeForceKeyFrame != 0 {
		out |= libvpxForceKF
	}
	if f&govpx.EncodeNoReferenceLast != 0 {
		out |= libvpxNoRefLast
	}
	if f&govpx.EncodeNoReferenceGolden != 0 {
		out |= libvpxNoRefGF
	}
	if f&govpx.EncodeNoUpdateLast != 0 {
		out |= libvpxNoUpdLast
	}
	if f&govpx.EncodeForceGoldenFrame != 0 {
		out |= libvpxForceGF
	}
	if f&govpx.EncodeNoUpdateEntropy != 0 {
		out |= libvpxNoUpdEntropy
	}
	if f&govpx.EncodeNoReferenceAltRef != 0 {
		out |= libvpxNoRefARF
	}
	if f&govpx.EncodeNoUpdateGolden != 0 {
		out |= libvpxNoUpdGF
	}
	if f&govpx.EncodeNoUpdateAltRef != 0 {
		out |= libvpxNoUpdARF
	}
	if f&govpx.EncodeForceAltRefFrame != 0 {
		out |= libvpxForceARF
	}
	return out
}

func NormalizeEncodeFlags(flags govpx.EncodeFlags) govpx.EncodeFlags {
	if flags&govpx.EncodeForceGoldenFrame != 0 {
		flags &^= govpx.EncodeNoUpdateGolden
	}
	if flags&govpx.EncodeForceAltRefFrame != 0 {
		flags &^= govpx.EncodeNoUpdateAltRef
	}
	return flags
}

func AssertEncoderVpxdecI420Match(t *testing.T, width, height int, packets ...[]byte) {
	t.Helper()
	want := vp9test.VpxdecI420(t, vp9test.BuildVP9IVF(width, height, packets...))
	got := DecodeVisibleI420(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for encoder stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func DecodeVisibleI420(t testing.TB, packets ...[]byte) []byte {
	t.Helper()
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	var out []byte
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, ok := d.NextFrame(); ok {
			out = AppendI420(out, &frame)
		}
	}
	return out
}

func DecodeIVFVisibleI420(ivf []byte) ([]byte, error) {
	return DecodeIVFVisibleI420WithOptions(ivf, govpx.VP9DecoderOptions{})
}

func DecodeIVFVisibleI420WithOptions(ivf []byte, opts govpx.VP9DecoderOptions) (out []byte, err error) {
	d, err := govpx.NewVP9Decoder(opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := d.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		return nil, testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return nil, err
		}
		if err := d.Decode(frame.Data); err != nil {
			return nil, err
		}
		if img, ok := d.NextFrame(); ok {
			out = AppendI420(out, &img)
		}
		offset = next
	}
	return out, nil
}

func DecodeWebMVisibleI420(webm []byte) ([]byte, error) {
	packets, err := testutil.ExtractVP9WebMPackets(webm)
	if err != nil {
		return nil, err
	}
	if len(packets) == 0 {
		return nil, govpx.ErrInvalidVP9Data
	}
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, packet := range packets {
		if err := d.Decode(packet); err != nil {
			return nil, err
		}
		if img, ok := d.NextFrame(); ok {
			out = AppendI420(out, &img)
		}
	}
	return out, nil
}

func DecodeIVFExpectError(ivf []byte) error {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		return err
	}
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		return testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	var firstErr error
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		if err := d.Decode(frame.Data); err != nil {
			firstErr = err
			break
		}
		offset = next
	}
	return firstErr
}

func NewImage(width int, height int) govpx.Image {
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
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

func PackI420(img *govpx.Image) []byte {
	out := make([]byte, 0, I420FrameSize(img.Width, img.Height))
	return AppendI420(out, img)
}

func AppendI420(out []byte, img *govpx.Image) []byte {
	w := img.Width
	h := img.Height
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	for y := 0; y < h; y++ {
		out = append(out, img.Y[y*img.YStride:y*img.YStride+w]...)
	}
	for y := 0; y < uvH; y++ {
		out = append(out, img.U[y*img.UStride:y*img.UStride+uvW]...)
	}
	for y := 0; y < uvH; y++ {
		out = append(out, img.V[y*img.VStride:y*img.VStride+uvW]...)
	}
	return out
}

func I420FrameSize(width int, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	return width*height + 2*uvWidth*uvHeight
}
