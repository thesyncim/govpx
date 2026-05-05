package libgopx

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/libgopx/internal/vp8/tables"
)

type DecoderOptions struct {
	Threads int

	ErrorResilient bool
	PostProcess    bool

	MaxWidth  int
	MaxHeight int

	// If true, Decode returns an explicit error when resolution changes.
	// If false, decoder may reallocate internal frame buffers on keyframe
	// resolution change.
	RejectResolutionChange bool
}

type VP8Decoder struct {
	opts        DecoderOptions
	closed      bool
	needKey     bool
	frameReady  bool
	lastFrame   Image
	lastInfo    FrameInfo
	currentPTS  uint64
	initialized bool

	frameWidth  int
	frameHeight int
	current     vp8common.FrameBuffer
	lastRef     vp8common.FrameBuffer
	goldenRef   vp8common.FrameBuffer
	altRef      vp8common.FrameBuffer

	mbRows             int
	mbCols             int
	modes              []vp8dec.MacroblockMode
	tokens             []vp8dec.MacroblockTokens
	tokenAbove         []vp8dec.EntropyContextPlanes
	frameHeader        vp8dec.FrameHeader
	previousQuant      vp8dec.QuantHeader
	state              vp8dec.StateHeader
	partitions         vp8dec.PartitionLayout
	modeReader         boolcoder.Decoder
	tokenReaders       [8]boolcoder.Decoder
	coefProbs          vp8tables.CoefficientProbs
	dequantTables      vp8common.FrameDequantTables
	dequants           [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	reconstructScratch vp8dec.IntraReconstructionScratch
}

func NewVP8Decoder(opts DecoderOptions) (*VP8Decoder, error) {
	if err := validateDecoderOptions(opts); err != nil {
		return nil, err
	}
	return &VP8Decoder{
		opts:      opts,
		needKey:   true,
		coefProbs: vp8tables.DefaultCoefProbs,
	}, nil
}

func (d *VP8Decoder) Decode(packet []byte) error {
	return d.DecodeWithPTS(packet, 0)
}

func (d *VP8Decoder) DecodeWithPTS(packet []byte, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	info, err := PeekVP8StreamInfo(packet)
	if err != nil {
		return err
	}
	if d.needKey && !info.KeyFrame {
		return ErrNeedKeyFrame
	}
	if err := d.validateStreamInfo(info); err != nil {
		return err
	}
	if err := d.parseState(packet); err != nil {
		return err
	}
	if err := d.ensureFrameBuffers(info); err != nil {
		return err
	}
	if err := d.decodeModeGrid(info); err != nil {
		return err
	}
	if err := d.decodeTokenGrid(); err != nil {
		return err
	}

	d.currentPTS = pts
	d.frameReady = false
	d.initialized = true
	if info.KeyFrame {
		d.needKey = false
	}
	d.lastInfo = FrameInfo{
		Width:     info.Width,
		Height:    info.Height,
		KeyFrame:  info.KeyFrame,
		ShowFrame: info.ShowFrame,
		PTS:       pts,
	}
	return ErrUnsupportedFeature
}

func (d *VP8Decoder) NextFrame() (Image, bool) {
	if d == nil || d.closed || !d.frameReady {
		return Image{}, false
	}
	d.frameReady = false
	return d.lastFrame, true
}

func (d *VP8Decoder) DecodeInto(packet []byte, dst *Image) (FrameInfo, error) {
	return d.DecodeIntoWithPTS(packet, dst, 0)
}

func (d *VP8Decoder) DecodeIntoWithPTS(packet []byte, dst *Image, pts uint64) (FrameInfo, error) {
	if d == nil || d.closed {
		return FrameInfo{}, ErrClosed
	}
	if dst == nil {
		return FrameInfo{}, ErrInvalidConfig
	}
	info, err := PeekVP8StreamInfo(packet)
	if err != nil {
		return FrameInfo{}, err
	}
	if d.needKey && !info.KeyFrame {
		return FrameInfo{}, ErrNeedKeyFrame
	}
	if err := d.validateStreamInfo(info); err != nil {
		return FrameInfo{}, err
	}
	if err := d.parseState(packet); err != nil {
		return FrameInfo{}, err
	}
	if err := d.ensureFrameBuffers(info); err != nil {
		return FrameInfo{}, err
	}
	if err := d.decodeModeGrid(info); err != nil {
		return FrameInfo{}, err
	}
	if err := d.decodeTokenGrid(); err != nil {
		return FrameInfo{}, err
	}
	d.currentPTS = pts
	d.frameReady = false
	d.initialized = true
	if info.KeyFrame {
		d.needKey = false
	}
	frameInfo := FrameInfo{
		Width:     info.Width,
		Height:    info.Height,
		KeyFrame:  info.KeyFrame,
		ShowFrame: info.ShowFrame,
		PTS:       pts,
	}
	d.lastInfo = frameInfo
	return frameInfo, ErrUnsupportedFeature
}

func (d *VP8Decoder) Reset() {
	if d == nil {
		return
	}
	d.needKey = true
	d.frameReady = false
	d.lastFrame = Image{}
	d.lastInfo = FrameInfo{}
	d.currentPTS = 0
	d.initialized = false
	d.previousQuant = vp8dec.QuantHeader{}
	d.state = vp8dec.StateHeader{}
	d.frameHeader = vp8dec.FrameHeader{}
	d.partitions = vp8dec.PartitionLayout{}
	d.coefProbs = vp8tables.DefaultCoefProbs
}

func (d *VP8Decoder) Close() error {
	if d == nil || d.closed {
		return ErrClosed
	}
	d.Reset()
	d.closed = true
	return nil
}

func validateDecoderOptions(opts DecoderOptions) error {
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.MaxWidth < 0 || opts.MaxHeight < 0 {
		return ErrInvalidConfig
	}
	if opts.MaxWidth > maxVP8Dimension || opts.MaxHeight > maxVP8Dimension {
		return ErrInvalidConfig
	}
	return nil
}

func (d *VP8Decoder) validateStreamInfo(info StreamInfo) error {
	if !info.KeyFrame {
		return nil
	}
	if info.Width <= 0 || info.Height <= 0 {
		return ErrInvalidData
	}
	if d.opts.MaxWidth > 0 && info.Width > d.opts.MaxWidth {
		return ErrUnsupportedFeature
	}
	if d.opts.MaxHeight > 0 && info.Height > d.opts.MaxHeight {
		return ErrUnsupportedFeature
	}
	if d.initialized && d.opts.RejectResolutionChange {
		if info.Width != d.lastInfo.Width || info.Height != d.lastInfo.Height {
			return ErrUnsupportedFeature
		}
	}
	return nil
}

func (d *VP8Decoder) ensureFrameBuffers(info StreamInfo) error {
	if !info.KeyFrame {
		return nil
	}
	if d.frameWidth == info.Width && d.frameHeight == info.Height && d.current.BufferLen() != 0 {
		return nil
	}
	if err := d.current.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.lastRef.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.goldenRef.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.altRef.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	d.ensureWorkspace(info.Width, info.Height)
	d.frameWidth = info.Width
	d.frameHeight = info.Height
	return nil
}

func (d *VP8Decoder) parseState(packet []byte) error {
	frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReader(packet, d.previousQuant)
	if err != nil {
		return ErrInvalidData
	}
	var partitions vp8dec.PartitionLayout
	if err := vp8dec.ParsePartitionLayout(packet, frame, state.TokenPartition, &partitions); err != nil {
		return ErrInvalidData
	}
	for i := 0; i < partitions.TokenCount; i++ {
		if err := d.tokenReaders[i].Init(partitions.Tokens[i]); err != nil {
			return ErrInvalidData
		}
	}
	d.frameHeader = frame
	d.state = state
	d.partitions = partitions
	d.modeReader = modeReader
	d.previousQuant = state.Quant
	vp8dec.InitSegmentDequants(state.Quant, &state.Segmentation, &d.dequantTables, &d.dequants)
	return nil
}

func (d *VP8Decoder) decodeModeGrid(info StreamInfo) error {
	if !info.KeyFrame {
		return ErrUnsupportedFeature
	}
	reader := d.modeReader
	if err := vp8dec.DecodeKeyFrameModeGrid(&reader, d.mbRows, d.mbCols, &d.state.Segmentation, d.state.Mode, d.modes); err != nil {
		return ErrInvalidData
	}
	if reader.Err() != nil {
		return ErrInvalidData
	}
	d.modeReader = reader
	return nil
}

func (d *VP8Decoder) decodeTokenGrid() error {
	readers := d.tokenReaders[:d.partitions.TokenCount]
	if _, err := vp8dec.DecodeTokenGrid(readers, d.mbRows, d.mbCols, &d.coefProbs, d.modes, d.tokenAbove, d.tokens); err != nil {
		return ErrInvalidData
	}
	for i := range readers {
		if readers[i].Err() != nil {
			return ErrInvalidData
		}
	}
	return nil
}

func (d *VP8Decoder) ensureWorkspace(width int, height int) {
	cols := (width + 15) >> 4
	rows := (height + 15) >> 4
	count := rows * cols
	if cap(d.modes) < count {
		d.modes = make([]vp8dec.MacroblockMode, count)
	} else {
		d.modes = d.modes[:count]
	}
	if cap(d.tokens) < count {
		d.tokens = make([]vp8dec.MacroblockTokens, count)
	} else {
		d.tokens = d.tokens[:count]
	}
	if cap(d.tokenAbove) < cols {
		d.tokenAbove = make([]vp8dec.EntropyContextPlanes, cols)
	} else {
		d.tokenAbove = d.tokenAbove[:cols]
	}
	d.mbRows = rows
	d.mbCols = cols
}
