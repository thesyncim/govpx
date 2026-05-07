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

func LoopFilterHeaderForVersion(version int, header LoopFilterHeader) LoopFilterHeader {
	if version == 1 || version == 3 {
		header.Type = SimpleLoopFilter
	}
	return header
}

func IsSupportedVersion(version int) bool {
	return version >= 0 && version <= 3
}
