package govpx

const (
	// Version is the package-level compatibility label for this govpx build.
	Version = "govpx-v0"
	// UpstreamLibvpxVersion is the libvpx release used for parity porting and
	// oracle tests.
	UpstreamLibvpxVersion = "v1.16.0"
)

// Codec identifies a codec family supported by govpx.
type Codec int

const (
	// CodecVP8 selects the VP8 bitstream format.
	CodecVP8 Codec = iota + 1
)

const (
	maxVP8Dimension = 16383
	maxQuantizer    = 63
)
