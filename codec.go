package govpx

import "github.com/thesyncim/govpx/internal/vpx/codecapi"

const (
	// UpstreamLibvpxVersion is the libvpx release tag used for parity
	// porting and the oracle test corpus. Matches the tag pinned by
	// internal/coracle.
	UpstreamLibvpxVersion = codecapi.UpstreamLibvpxVersion
)

// Codec identifies a codec family supported by govpx.
type Codec = codecapi.Codec

const (
	// CodecVP8 selects the VP8 bitstream format.
	CodecVP8 = codecapi.CodecVP8
	// CodecVP9 selects the VP9 profile 0 bitstream format. Other VP9
	// profiles are outside the package scope.
	CodecVP9 = codecapi.CodecVP9
)
