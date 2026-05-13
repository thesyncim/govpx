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

// Codec identifies a codec family supported by govpx.
type Codec int

const (
	// CodecVP8 selects the VP8 bitstream format.
	CodecVP8 Codec = iota + 1
	// CodecVP9 selects the VP9 bitstream format. The VP9 surface is
	// still under construction (see internal/vp9 / UPSTREAM.md);
	// callers should treat this as a build-time gate, not a runtime
	// feature flag.
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
	maxVP8Dimension = 16383
	maxQuantizer    = 63
)
