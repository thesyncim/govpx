//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

import "testing"

func TestTMPredictDisabledScalarContract(t *testing.T) {
	tmCheckDisabledScalarContract(t)
}
