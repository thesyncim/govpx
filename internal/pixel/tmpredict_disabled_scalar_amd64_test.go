//go:build goexperiment.simd && amd64 && !purego

package pixel

import "testing"

func TestTMPredictDisabledScalarContract(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	tmCheckDisabledScalarContract(t)
}
