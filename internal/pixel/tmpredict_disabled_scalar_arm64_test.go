//go:build goexperiment.simd && arm64 && !purego

package pixel

import "testing"

func TestTMPredictDisabledScalarContract(t *testing.T) {
	if Enabled {
		t.Skip("ARM ASIMD is mandatory; exercised separately by test-only Enabled=false source overlay")
	}
	tmCheckDisabledScalarContract(t)
}
