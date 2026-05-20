package arith

// MaxInt returns the largest value of type int.
func MaxInt() int {
	return int(^uint(0) >> 1)
}

// SaturatingAdd returns a+b, clamped to the signed int range used by govpx's
// rate-control accounting.
func SaturatingAdd(a int, b int) int {
	maxInt := MaxInt()
	if b > 0 && a > maxInt-b {
		return maxInt
	}
	if b < 0 && a < -maxInt-b {
		return -maxInt
	}
	return a + b
}

// SaturatingSub returns a-b, clamped to the signed int range used by govpx's
// rate-control accounting.
func SaturatingSub(a int, b int) int {
	maxInt := MaxInt()
	if b == -maxInt {
		return SaturatingAdd(a, maxInt)
	}
	return SaturatingAdd(a, -b)
}

// PercentOf returns value*pct/100, saturating on multiplication overflow.
func PercentOf(value int, pct int) int {
	if value <= 0 || pct <= 0 {
		return 0
	}
	maxInt := MaxInt()
	if value > maxInt/pct {
		return maxInt
	}
	return (value * pct) / 100
}

// CheckedMul returns a*b and reports whether the non-negative multiplication
// fit in an int.
func CheckedMul(a int, b int) (int, bool) {
	if min(a, b) < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	maxInt := MaxInt()
	if a > maxInt/b {
		return 0, false
	}
	return a * b, true
}
