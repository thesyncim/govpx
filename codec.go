package govpx

const (
	Version               = "govpx-v0"
	UpstreamLibvpxVersion = "v1.16.0"
)

type Codec int

const (
	CodecVP8 Codec = iota + 1
)

const (
	maxVP8Dimension = 16383
	maxQuantizer    = 63
)
