package decoder

// Ported from libvpx v1.16.0 vp8/common/alloccommon.c vp8_setup_version.

type InterPredictionConfig struct {
	UseBilinear bool
	FullPixel   bool
}

func InterPredictionConfigForVersion(version int) InterPredictionConfig {
	switch version {
	case 1, 2:
		return InterPredictionConfig{UseBilinear: true}
	case 3:
		return InterPredictionConfig{UseBilinear: true, FullPixel: true}
	default:
		return InterPredictionConfig{}
	}
}

func VersionSkipsLoopFilter(version int) bool {
	return version == 2 || version == 3
}

// LoopFilterHeaderForVersion returns the loop-filter header as-is. The version
// field in libvpx's vp8_setup_version sets filter_type as a *default* (SIMPLE
// for versions 1/3), but the subsequent bitstream read of filter_type in
// vp8/decoder/decodeframe.c always overwrites it, so the bitstream value is
// the effective filter_type. We pass the header through unchanged to mirror
// that libvpx-observed behavior. Earlier code applied an unconditional SIMPLE
// override here; that caused a decoder divergence (the LF
// kernel diverged when a version-1 stream specified NORMAL in its bitstream).
func LoopFilterHeaderForVersion(_ int, header LoopFilterHeader) LoopFilterHeader {
	return header
}

func IsSupportedVersion(version int) bool {
	return version >= 0 && version <= 7
}
