package encoder

func forward4x4WindowOK(input []int16, stride int, output []int16) bool {
	if stride < 4 || len(output) < 16 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if stride > (maxInt-4)/3 {
		return false
	}
	return 3*stride+4 <= len(input)
}

func forwardWHT4x4WindowOK(input []int16, stride int, output []int16) bool {
	return forward4x4WindowOK(input, stride, output)
}
