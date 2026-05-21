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

func maxInt() int {
	return int(^uint(0) >> 1)
}
