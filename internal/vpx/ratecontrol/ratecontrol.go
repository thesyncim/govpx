package ratecontrol

func EncodedSizeBits(sizeBytes int) int {
	if sizeBytes <= 0 {
		return 0
	}
	if sizeBytes > maxInt()/8 {
		return maxInt()
	}
	return sizeBytes * 8
}

func NormalizePercent(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// BitsPerFrame returns round(target_bandwidth / frame_rate), matching the
// libvpx rate-control convention for average frame bandwidth.
func BitsPerFrame(targetBandwidthBits int, frameRate float64, timebaseNum, timebaseDen, frameDuration int) int {
	if targetBandwidthBits <= 0 {
		return 0
	}
	if frameRate > 0 {
		v := float64(targetBandwidthBits)/frameRate + 0.5
		if v > float64(maxInt()) {
			return 0
		}
		return int(v)
	}
	if timebaseNum <= 0 || timebaseDen <= 0 || frameDuration <= 0 {
		return 0
	}
	num := int64(targetBandwidthBits) * int64(timebaseNum) * int64(frameDuration)
	den := int64(timebaseDen)
	if den <= 0 {
		return 0
	}
	v := (num + den/2) / den
	if v > int64(maxInt()) {
		return 0
	}
	return int(v)
}

// RawTargetRateKbps returns libvpx's raw uncompressed target-rate envelope:
// width * height * bit_depth * 3 * frame_rate / 1000, truncated to kbps.
func RawTargetRateKbps(width, height, bitDepth int, frameRate float64) int {
	if width <= 0 || height <= 0 || bitDepth <= 0 || frameRate <= 0 {
		return 0
	}
	rawBitsPerFrame := float64(width) * float64(height) *
		float64(bitDepth) * 3
	rawKbps := rawBitsPerFrame * frameRate / 1000
	if rawKbps <= 0 {
		return 0
	}
	if rawKbps > float64(maxInt()) {
		return maxInt()
	}
	return int(rawKbps)
}

// ClampToRawTargetRateKbps caps kbps to RawTargetRateKbps when the envelope is
// known. Invalid dimensions or frame rates leave the requested value unchanged.
func ClampToRawTargetRateKbps(kbps, width, height, bitDepth int, frameRate float64) int {
	if kbps <= 0 {
		return kbps
	}
	rawKbps := RawTargetRateKbps(width, height, bitDepth, frameRate)
	if rawKbps <= 0 || kbps <= rawKbps {
		return kbps
	}
	return rawKbps
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
