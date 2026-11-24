package decoder

// Slice resize helpers preserve libvpx v1.16.0's reused VP8 decoder
// macroblock-mode/token buffers while keeping the allocation policy beside
// the decoder-owned types.

func ResizeMacroblockModeSlice(s []MacroblockMode, n int) []MacroblockMode {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = MacroblockMode{}
		}
		return s
	}
	return make([]MacroblockMode, n)
}

func ResizeMacroblockTokenSlice(s []MacroblockTokens, n int) []MacroblockTokens {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = MacroblockTokens{}
		}
		return s
	}
	return make([]MacroblockTokens, n)
}
