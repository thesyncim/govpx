package encoder

// Scalar bounds guard for libvpx v1.16.0 VP8 4x4 transform input windows before
// dispatching to source-shaped C/ASM-equivalent FDCT helpers.
func transform4x4WindowOK(input []int16, stride int) bool {
	if stride < 4 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if stride > (maxInt-4)/3 {
		return false
	}
	return 3*stride+4 <= len(input)
}

func transform4x4BatchWindowOK(input []int16, output []int16, count int) bool {
	if count <= 0 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if count > maxInt/16 {
		return false
	}
	n := count * 16
	return len(input) >= n && len(output) >= n
}
