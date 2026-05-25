//go:build govpx_oracle_trace

package govpx

import "github.com/thesyncim/govpx/internal/testutil"

var vp8OracleRuntimeFullPermutationSeed = []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

const (
	vp8OracleRuntimeFuzzConfigPhase uint8 = iota
	vp8OracleRuntimeFuzzCodecPhase
)

func vp8OracleRuntimeFullPermutationActionReader(kind int) testutil.ByteCursor {
	switch kind {
	case 3, 4:
		return testutil.NewByteCursor([]byte{2})
	case 8:
		return testutil.NewByteCursor([]byte{})
	case 17, 19:
		return testutil.NewByteCursor([]byte{1})
	default:
		return testutil.NewByteCursor([]byte{0})
	}
}
