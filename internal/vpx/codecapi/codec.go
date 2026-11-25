package codecapi

const (
	// UpstreamLibvpxVersion is the libvpx release tag used for parity
	// porting and the oracle test corpus. Matches the tag pinned by
	// internal/coracle.
	UpstreamLibvpxVersion = "v1.16.0"
)

// Codec identifies a codec family supported by govpx.
type Codec int

const (
	// CodecVP8 selects the VP8 bitstream format.
	CodecVP8 Codec = iota + 1
	// CodecVP9 selects the VP9 profile 0 bitstream format. Other VP9
	// profiles are outside the package scope.
	CodecVP9
)

// String returns the canonical lowercase name for a Codec, matching
// libvpx's vpx_codec_iface_name short tags. Useful for log lines and
// error messages.
func (c Codec) String() string {
	switch c {
	case CodecVP8:
		return "vp8"
	case CodecVP9:
		return "vp9"
	}
	return "unknown"
}

const (
	MaxVP8Dimension              = 16383
	MaxVP9Dimension              = 65536
	MaxQuantizer                 = 63
	MaxARNRFrames                = 15
	DefaultDropFramesWaterMark   = 60
	LibvpxDefaultBufferSizeMs    = 6000
	LibvpxDefaultBufferInitialMs = 4000
	LibvpxDefaultBufferOptimalMs = 5000
	MaxRateControlOvershootPct   = 100
	MaxRateControlUndershootPct  = 100
	LibvpxIntMax                 = 0x7fffffff
)

func MaxInt() int {
	return int(^uint(0) >> 1)
}

func VBRMinFrameBandwidthBits(perFrameBandwidth int, pct int) int {
	if min(perFrameBandwidth, pct) <= 0 {
		return 0
	}
	v := int64(perFrameBandwidth) * int64(pct) / 100
	if v > int64(LibvpxIntMax) {
		return LibvpxIntMax
	}
	return int(v)
}

func EncoderMacroblockCols(width int) int {
	return (width + 15) >> 4
}

func EncoderMacroblockRows(height int) int {
	return (height + 15) >> 4
}
