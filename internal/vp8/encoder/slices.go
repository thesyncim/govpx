package encoder

// Slice resize helpers preserve libvpx v1.16.0's reused VP8 encoder
// macroblock analysis buffers while keeping the allocation policy beside
// the encoder-owned types.

func ResizeInt8Slice(s []int8, n int) []int8 {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = 0
		}
		return s
	}
	return make([]int8, n)
}

func ResizeUint8Slice(s []uint8, n int) []uint8 {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = 0
		}
		return s
	}
	return make([]uint8, n)
}

func ResizeBoolSlice(s []bool, n int) []bool {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = false
		}
		return s
	}
	return make([]bool, n)
}

func ResizeKeyFrameModeSlice(s []KeyFrameMacroblockMode, n int) []KeyFrameMacroblockMode {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = KeyFrameMacroblockMode{}
		}
		return s
	}
	return make([]KeyFrameMacroblockMode, n)
}

func ResizeInterFrameModeSlice(s []InterFrameMacroblockMode, n int) []InterFrameMacroblockMode {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = InterFrameMacroblockMode{}
		}
		return s
	}
	return make([]InterFrameMacroblockMode, n)
}

func ResizeMacroblockCoefficientSlice(s []MacroblockCoefficients, n int) []MacroblockCoefficients {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = MacroblockCoefficients{}
		}
		return s
	}
	return make([]MacroblockCoefficients, n)
}

func ResizeTokenContextSlice(s []TokenContextPlanes, n int) []TokenContextPlanes {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = TokenContextPlanes{}
		}
		return s
	}
	return make([]TokenContextPlanes, n)
}
