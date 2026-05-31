package vp9oracle

import (
	"image"
	"strconv"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vpxbuffers "github.com/thesyncim/govpx/internal/vpx/buffers"
)

// AltRefRefreshMask is the VP9 refresh-frame mask bit for the ALTREF slot.
const AltRefRefreshMask uint8 = 1 << 2

func EncodeBufferSize(width, height int) (int, error) {
	return vpxbuffers.I420EncodeBufferSize(width, height, 4096, 65536)
}

func CBROptions(width, height, targetKbps int) govpx.VP9EncoderOptions {
	return govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	}
}

func CBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return []string{
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--buf-sz=" + strconv.Itoa(bufSizeMs),
		"--buf-initial-sz=" + strconv.Itoa(bufInitialMs),
		"--buf-optimal-sz=" + strconv.Itoa(bufOptimalMs),
		"--drop-frame=" + strconv.Itoa(dropFrame),
		"--exact-fps-timebase",
	}
}

func CyclicRefreshCBROptions(width, height, targetKbps int) govpx.VP9EncoderOptions {
	opts := CBROptions(width, height, targetKbps)
	opts.AQMode = govpx.VP9AQCyclicRefresh
	opts.Deadline = govpx.DeadlineRealtime
	opts.CpuUsed = -8
	return opts
}

func CyclicRefreshCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return append(CBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame),
		"--cpu-used=8",
		"--aq-mode=3",
	)
}

func CyclicRefreshCBRVpxencArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return []string{
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--buf-sz=" + strconv.Itoa(bufSizeMs),
		"--buf-initial-sz=" + strconv.Itoa(bufInitialMs),
		"--buf-optimal-sz=" + strconv.Itoa(bufOptimalMs),
		"--drop-frame=" + strconv.Itoa(dropFrame),
		"--cpu-used=8",
		"--aq-mode=3",
	}
}

func DropFrameArg(opts govpx.VP9EncoderOptions) int {
	if !opts.DropFrameAllowed {
		return 0
	}
	return opts.DropFrameWaterMark
}

func MustRuntime(t testing.TB, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func ApplyRuntimeControlTransition(t testing.TB, enc *govpx.VP9Encoder, frame int) {
	t.Helper()
	switch frame {
	case 2:
		if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 300}); err != nil {
			t.Fatalf("SetRealtimeTarget bitrate at frame %d: %v", frame, err)
		}
	case 4:
		if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
			MinQuantizer: 20,
			MaxQuantizer: 20,
		}); err != nil {
			t.Fatalf("SetRealtimeTarget quantizers at frame %d: %v", frame, err)
		}
	case 5:
		if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{FPS: 15}); err != nil {
			t.Fatalf("SetRealtimeTarget fps at frame %d: %v", frame, err)
		}
	case 6:
		if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
			FrameDrop: govpx.RealtimeFrameDropDisabled,
		}); err != nil {
			t.Fatalf("SetRealtimeTarget disable drop at frame %d: %v", frame, err)
		}
	case 8:
		if err := enc.SetFrameDropAllowed(true); err != nil {
			t.Fatalf("SetFrameDropAllowed at frame %d: %v", frame, err)
		}
	}
}

func TransitionSources(width, height, frames int) []*image.YCbCr {
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	return sources
}

func TransitionPanningSources(width, height, count, offset int) []*image.YCbCr {
	sources := make([]*image.YCbCr, count)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i+offset)
	}
	return sources
}

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

	dstSize, err := EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
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

func RateTraceFlagMapper(flags uint32) uint32 {
	return FrameFlagsForLibvpx(govpx.EncodeFlags(flags))
}

const NoUpdateRefFlags govpx.EncodeFlags = govpx.EncodeNoUpdateLast |
	govpx.EncodeNoUpdateGolden | govpx.EncodeNoUpdateAltRef

func FlagAt(frames, index int, flag govpx.EncodeFlags) []govpx.EncodeFlags {
	flags := make([]govpx.EncodeFlags, frames)
	if uint(index) < uint(frames) {
		flags[index] = flag
	}
	return flags
}

func RepeatInterFlag(frames int, flag govpx.EncodeFlags) []govpx.EncodeFlags {
	flags := make([]govpx.EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		flags[i] = flag
	}
	return flags
}

func RepeatAllFramesFlag(frames int, flag govpx.EncodeFlags) []govpx.EncodeFlags {
	flags := make([]govpx.EncodeFlags, frames)
	for i := range flags {
		flags[i] = flag
	}
	return flags
}

func RefRefreshTransitions(frames int) []govpx.EncodeFlags {
	flags := make([]govpx.EncodeFlags, frames)
	if frames > 2 {
		flags[2] = govpx.EncodeForceGoldenFrame | govpx.EncodeNoUpdateLast
	}
	if frames > 4 {
		flags[4] = govpx.EncodeForceAltRefFrame | govpx.EncodeNoUpdateGolden
	}
	if frames > 6 {
		flags[6] = govpx.EncodeForceGoldenFrame | govpx.EncodeNoUpdateLast
	}
	return flags
}

func AlternatingReferenceControls(frames int) []govpx.EncodeFlags {
	flags := make([]govpx.EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		if i&1 == 0 {
			flags[i] = govpx.EncodeNoUpdateGolden | govpx.EncodeNoReferenceAltRef
		} else {
			flags[i] = govpx.EncodeNoUpdateAltRef | govpx.EncodeNoReferenceGolden
		}
	}
	return flags
}

func ROIMap(width, height int, pattern string) *govpx.ROIMap {
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	roi := &govpx.ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			idx := row*cols + col
			switch pattern {
			case "checker":
				roi.SegmentID[idx] = uint8((row + col) & 1)
			case "left1":
				if col < (cols+1)/2 {
					roi.SegmentID[idx] = 1
				}
			case "quadrants":
				roi.SegmentID[idx] = 0
				if row >= rows/2 {
					roi.SegmentID[idx] += 2
				}
				if col >= cols/2 {
					roi.SegmentID[idx]++
				}
			case "border1":
				if row == 0 || col == 0 || row == rows-1 || col == cols-1 {
					roi.SegmentID[idx] = 1
				}
			default:
				panic("unknown VP9 ROI pattern")
			}
		}
	}
	switch pattern {
	case "checker", "left1":
		roi.DeltaQuantizer[1] = -10
		roi.DeltaLoopFilter[1] = -3
	case "quadrants":
		roi.DeltaQuantizer[1] = -8
		roi.DeltaQuantizer[2] = 8
		roi.DeltaLoopFilter[3] = 4
	case "border1":
		roi.DeltaQuantizer[1] = -6
	}
	return roi
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

func DecodeVisibleI420(t testing.TB, packets ...[]byte) []byte {
	t.Helper()
	return DecodeVisibleI420WithOptions(t, govpx.VP9DecoderOptions{}, packets...)
}

func DecodeVisibleI420WithOptions(t testing.TB, opts govpx.VP9DecoderOptions, packets ...[]byte) []byte {
	t.Helper()
	d, err := govpx.NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
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

func DecodeIntoVisibleI420(t testing.TB, width, height int, packets ...[]byte) []byte {
	t.Helper()
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	dst := NewImage(width, height)
	var out []byte
	for i, packet := range packets {
		info, err := d.DecodeInto(packet, &dst)
		if err != nil {
			t.Fatalf("DecodeInto packet %d: %v", i, err)
		}
		if info.ShowFrame {
			out = AppendI420(out, &dst)
		}
	}
	return out
}

func DecodeLastVisibleFrame(t testing.TB, packets ...[]byte) govpx.Image {
	t.Helper()
	return DecodeLastVisibleFrameWithOptions(t, govpx.VP9DecoderOptions{}, packets...)
}

func DecodeLastVisibleFrameWithOptions(t testing.TB,
	opts govpx.VP9DecoderOptions, packets ...[]byte,
) govpx.Image {
	t.Helper()
	d, err := govpx.NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	var last govpx.Image
	ok := false
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, frameOK := d.NextFrame(); frameOK {
			last = CloneImage(frame)
			ok = true
		}
	}
	if !ok {
		t.Fatal("packet sequence did not publish a visible frame")
	}
	return last
}

func EncodedKeyframe(t testing.TB, width, height int, y byte) []byte {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 37,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	defer e.Close()
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, y, 128, 128))
	if err != nil {
		t.Fatalf("Encode %dx%d keyframe: %v", width, height, err)
	}
	if len(packet) == 0 {
		t.Fatalf("Encode %dx%d keyframe returned empty packet", width, height)
	}
	return packet
}

func SVCStyleSuperframe(t testing.TB) []byte {
	t.Helper()
	return vp9test.SuperframePacket(t,
		EncodedKeyframe(t, 32, 32, 80),
		EncodedKeyframe(t, 64, 64, 160),
	)
}

func ShowExistingStream(t testing.TB, width, height int) ([][]byte, []byte) {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	packets := [][]byte{
		key,
		inter,
		vp9test.ShowExistingFramePacket(5),
	}
	return packets, vp9test.BuildVP9IVF(width, height, packets...)
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

func CloneImage(src govpx.Image) govpx.Image {
	dst := NewImage(src.Width, src.Height)
	uvWidth, uvHeight := (src.Width+1)>>1, (src.Height+1)>>1
	vpxbuffers.CopyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	vpxbuffers.CopyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	vpxbuffers.CopyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
	return dst
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
