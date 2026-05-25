package govpx

// libvpxFormulaForRD returns the libvpx source-line citation for the
// step_param formula expected on the RD vs picker path. Used for
// regression-test error messages.
func libvpxFormulaForRD(rd bool) string {
	if rd {
		return "rdopt.c:2076: step_param = max(first_step, sr) with no speed_adjust"
	}
	return "pickinter.c:932/971/973: step_param = max(first_step+speed_adjust, sr+speed_adjust)"
}

// libvpxFurtherFormulaForRD returns the libvpx source-line citation for
// the further_steps formula expected on the RD vs picker path.
func libvpxFurtherFormulaForRD(rd bool) string {
	if rd {
		return "rdopt.c:2086: further_steps = max_step-1-step_param (no Speed>=8 cap)"
	}
	return "pickinter.c:1005-1008: further_steps = (Speed>=8 ? 0 : max_step-1-step_param)"
}

// cpuUsedTag formats a subtest name from a signed cpu_used. Negative
// values produce "cpu-neg-N" so the test output is greppable.
func cpuUsedTag(cpuUsed int) string {
	if cpuUsed < 0 {
		return "cpu-neg-" + itoaPositive(-cpuUsed)
	}
	return "cpu-" + itoaPositive(cpuUsed)
}

func itoaPositive(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
