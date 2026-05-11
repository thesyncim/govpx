//go:build govpx_oracle_trace

package govpx

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

type staleY2Snapshot struct {
	set    bool
	eob    uint8
	qcoeff [16]int16
}

func makeOracleStaleY2Snapshot(eob uint8, qcoeff [16]int16) staleY2Snapshot {
	return staleY2Snapshot{set: true, eob: eob, qcoeff: qcoeff}
}

func oracleStaleY2SnapshotSet(snapshot staleY2Snapshot) bool {
	return snapshot.set
}

func applyOracleStaleY2Snapshot(coeffs *vp8enc.MacroblockCoefficients, snapshot staleY2Snapshot) {
	if coeffs == nil || !snapshot.set {
		return
	}
	coeffs.OracleStaleY2Set = true
	coeffs.OracleStaleY2EOB = snapshot.eob
	coeffs.OracleStaleY2QCoeff = snapshot.qcoeff
}

func recordOracleY1DCEOB1(coeffs *vp8enc.MacroblockCoefficients, block int, value uint8) {
	if coeffs == nil || block < 0 || block >= len(coeffs.OracleY1DCEOB1) {
		return
	}
	coeffs.OracleY1DCEOB1[block] = value
}

func recordOracleStaleY2(coeffs *vp8enc.MacroblockCoefficients, eob uint8, qcoeff [16]int16) {
	if coeffs == nil {
		return
	}
	coeffs.OracleStaleY2EOB = eob
	coeffs.OracleStaleY2QCoeff = qcoeff
	coeffs.OracleStaleY2Set = true
}

func libvpxY1DCWouldQuantizeNonzero(dct0 int16, quant *vp8enc.BlockQuant, zbinOverQuant int, zbinModeBoost int, fastQuant bool) uint8 {
	if quant == nil {
		return 0
	}
	z := int(dct0)
	if z == 0 {
		return 0
	}
	x := z
	if x < 0 {
		x = -x
	}
	if fastQuant {
		y := ((x + int(quant.Round[0])) * int(quant.QuantFast[0])) >> 16
		if y != 0 {
			return 1
		}
		return 0
	}
	zbin := int(quant.Zbin[0])
	zbin += int(quant.ZbinBoost[0])
	zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
	if x < zbin {
		return 0
	}
	x += int(quant.Round[0])
	y := ((((x * int(quant.Quant[0])) >> 16) + x) * int(quant.QuantShift[0])) >> 16
	if y != 0 {
		return 1
	}
	return 0
}
