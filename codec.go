package govpx

const (
	// Version is the package-level compatibility label for this govpx
	// build. It is intended for crash reports and bug templates, not for
	// programmatic feature gating.
	Version = "govpx-v0"
	// UpstreamLibvpxVersion is the libvpx release tag used for parity
	// porting and the oracle test corpus. Matches the tag pinned by
	// internal/coracle.
	UpstreamLibvpxVersion = "v1.16.0"
)

// Codec identifies a codec family supported by govpx. Reserved for
// future multi-codec selection; for now the package only implements VP8.
type Codec int

const (
	// CodecVP8 selects the VP8 bitstream format.
	CodecVP8 Codec = iota + 1
)

const (
	maxVP8Dimension = 16383
	maxQuantizer    = 63
)
