// Package benchcmd: BD-rate (Bjøntegaard-Delta-rate) math + harness.
//
// BDRate fits a cubic polynomial of PSNR over log(rate) on each of two
// rate-distortion curves and integrates the rate gap across the overlapping
// PSNR range. The result is the percentage extra bitrate the second codec
// needs to match the first at equal quality. BDPSNR is the symmetric quality
// gap (in dB) at equal rate. The implementation follows VCEG-M33
// (Bjøntegaard 2001); standard reference implementations match within
// ~0.1% on canonical inputs.
package benchcmd

import (
	"errors"
	"math"
	"sort"
)

// QualityPoint is one operating point on an RD curve. Rate is in kbps,
// PSNR in dB. Both must be positive; rate must be strictly positive so
// log10 is finite.
type QualityPoint struct {
	Rate float64 // kbps
	PSNR float64 // dB
}

// BDRateResult bundles the inputs of a BD-rate measurement plus the
// computed deltas so callers can both consume the headline number and
// log the underlying operating points.
type BDRateResult struct {
	// Reference is the baseline curve (e.g., libvpx). Govpx is the curve
	// under test.
	Reference []QualityPoint
	Govpx     []QualityPoint
	// BDRate is the percentage extra bitrate Govpx needs to match Reference
	// at equal PSNR over the overlapping quality range. Negative is
	// better (Govpx saves bitrate); positive is worse.
	BDRate float64
	// BDPSNR is the PSNR gap in dB at equal rate over the overlapping
	// rate range. Positive is better (Govpx delivers more dB at the same
	// rate); negative is worse.
	BDPSNR float64
}

// Errors returned by BDRate when the inputs are degenerate.
var (
	errBDRateInsufficientPoints = errors.New("bdrate: need at least 4 operating points per curve")
	errBDRateBadPoint           = errors.New("bdrate: rate and psnr must be positive and finite")
	errBDRateNoOverlap          = errors.New("bdrate: overlap range is empty")
)

// BDRate computes the Bjøntegaard-Delta-rate between reference and test
// curves. Inputs are sorted internally by rate; duplicate rates are
// rejected (they make the cubic fit singular). Returns the percentage
// bitrate difference (test vs reference): negative means test saves
// bitrate at equal quality, positive means test needs more bitrate.
func BDRate(reference, test []QualityPoint) (float64, error) {
	return bdMetric(reference, test, true)
}

// BDPSNR computes the symmetric BD-PSNR: PSNR gap in dB at equal rate.
// Positive means test delivers more dB at the same rate, negative means
// test loses quality.
func BDPSNR(reference, test []QualityPoint) (float64, error) {
	return bdMetric(reference, test, false)
}

func bdMetric(reference, test []QualityPoint, rateMode bool) (float64, error) {
	refPts, err := sanitizeBDPoints(reference)
	if err != nil {
		return 0, err
	}
	testPts, err := sanitizeBDPoints(test)
	if err != nil {
		return 0, err
	}

	if rateMode {
		// Fit log10(rate) as a cubic of PSNR. Then integrate the
		// difference (logR_test - logR_ref) over the overlapping
		// PSNR range; convert the average log10-difference to a
		// percentage via 10^d - 1.
		refX, refY := psnrLogRate(refPts)
		testX, testY := psnrLogRate(testPts)
		coefRef, err := polyfitCubic(refX, refY)
		if err != nil {
			return 0, err
		}
		coefTest, err := polyfitCubic(testX, testY)
		if err != nil {
			return 0, err
		}
		lo := math.Max(refX[0], testX[0])
		hi := math.Min(refX[len(refX)-1], testX[len(testX)-1])
		if hi <= lo {
			return 0, errBDRateNoOverlap
		}
		intRef := integrateCubic(coefRef, lo, hi)
		intTest := integrateCubic(coefTest, lo, hi)
		avgDiff := (intTest - intRef) / (hi - lo)
		return (math.Pow(10, avgDiff) - 1) * 100, nil
	}
	// BD-PSNR: fit PSNR as a cubic of log10(rate); integrate over the
	// overlapping log10(rate) range; the average gap is the BD-PSNR.
	refX, refY := logRatePSNR(refPts)
	testX, testY := logRatePSNR(testPts)
	coefRef, err := polyfitCubic(refX, refY)
	if err != nil {
		return 0, err
	}
	coefTest, err := polyfitCubic(testX, testY)
	if err != nil {
		return 0, err
	}
	lo := math.Max(refX[0], testX[0])
	hi := math.Min(refX[len(refX)-1], testX[len(testX)-1])
	if hi <= lo {
		return 0, errBDRateNoOverlap
	}
	intRef := integrateCubic(coefRef, lo, hi)
	intTest := integrateCubic(coefTest, lo, hi)
	return (intTest - intRef) / (hi - lo), nil
}

// sanitizeBDPoints sorts by rate ascending, validates positivity, and
// rejects duplicate rates (which would make the cubic system singular).
func sanitizeBDPoints(in []QualityPoint) ([]QualityPoint, error) {
	if len(in) < 4 {
		return nil, errBDRateInsufficientPoints
	}
	pts := make([]QualityPoint, len(in))
	copy(pts, in)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Rate < pts[j].Rate })
	for i := range pts {
		if !(pts[i].Rate > 0) || math.IsInf(pts[i].Rate, 0) || math.IsNaN(pts[i].Rate) {
			return nil, errBDRateBadPoint
		}
		if math.IsNaN(pts[i].PSNR) || math.IsInf(pts[i].PSNR, 0) {
			return nil, errBDRateBadPoint
		}
		if i > 0 && pts[i].Rate == pts[i-1].Rate {
			return nil, errBDRateBadPoint
		}
	}
	return pts, nil
}

// psnrLogRate maps each (rate, psnr) point to (psnr, log10(rate)) for
// the BD-rate fit. The returned arrays are aligned and sorted by PSNR
// ascending. Duplicate PSNR values (which would also make the fit
// singular) are rejected by polyfitCubic via its rank check.
func psnrLogRate(pts []QualityPoint) ([]float64, []float64) {
	x := make([]float64, len(pts))
	y := make([]float64, len(pts))
	for i, p := range pts {
		x[i] = p.PSNR
		y[i] = math.Log10(p.Rate)
	}
	sortPairs(x, y)
	return x, y
}

func logRatePSNR(pts []QualityPoint) ([]float64, []float64) {
	x := make([]float64, len(pts))
	y := make([]float64, len(pts))
	for i, p := range pts {
		x[i] = math.Log10(p.Rate)
		y[i] = p.PSNR
	}
	sortPairs(x, y)
	return x, y
}

func sortPairs(x, y []float64) {
	type pair struct {
		x, y float64
	}
	pairs := make([]pair, len(x))
	for i := range x {
		pairs[i] = pair{x[i], y[i]}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].x < pairs[j].x })
	for i, p := range pairs {
		x[i], y[i] = p.x, p.y
	}
}

// polyfitCubic returns the [a0, a1, a2, a3] coefficients such that
// y ≈ a0 + a1*x + a2*x^2 + a3*x^3 minimises sum-of-squared-residuals,
// solved via the normal equations. For 4 points the fit is exact (the
// classic Bjøntegaard reduction). For 5+ points it falls back to
// least-squares so future callers can pass denser ladders.
func polyfitCubic(x, y []float64) ([4]float64, error) {
	n := len(x)
	if n < 4 || len(y) != n {
		return [4]float64{}, errors.New("bdrate: polyfit needs >=4 aligned points")
	}
	// Build A^T A (4x4) and A^T y (4x1) for A_ij = x_i^j.
	var ata [4][4]float64
	var atb [4]float64
	for i := 0; i < n; i++ {
		xi := x[i]
		yi := y[i]
		row := [4]float64{1, xi, xi * xi, xi * xi * xi}
		for r := 0; r < 4; r++ {
			atb[r] += row[r] * yi
			for c := 0; c < 4; c++ {
				ata[r][c] += row[r] * row[c]
			}
		}
	}
	coef, ok := solve4x4(ata, atb)
	if !ok {
		return [4]float64{}, errors.New("bdrate: singular polynomial system")
	}
	return coef, nil
}

// solve4x4 solves a 4x4 system via Gaussian elimination with partial
// pivoting. It returns false when the matrix is singular (degenerate
// inputs: duplicate xs, collinear data).
func solve4x4(a [4][4]float64, b [4]float64) ([4]float64, bool) {
	var m [4][5]float64
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			m[r][c] = a[r][c]
		}
		m[r][4] = b[r]
	}
	for col := 0; col < 4; col++ {
		// Partial pivot
		pivot := col
		for r := col + 1; r < 4; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(m[pivot][col]) < 1e-18 {
			return [4]float64{}, false
		}
		if pivot != col {
			m[col], m[pivot] = m[pivot], m[col]
		}
		// Eliminate below
		for r := col + 1; r < 4; r++ {
			f := m[r][col] / m[col][col]
			for c := col; c < 5; c++ {
				m[r][c] -= f * m[col][c]
			}
		}
	}
	var x [4]float64
	for r := 3; r >= 0; r-- {
		v := m[r][4]
		for c := r + 1; c < 4; c++ {
			v -= m[r][c] * x[c]
		}
		x[r] = v / m[r][r]
	}
	return x, true
}

// integrateCubic returns the definite integral of
// a0 + a1*x + a2*x^2 + a3*x^3 from lo to hi.
func integrateCubic(coef [4]float64, lo, hi float64) float64 {
	primitive := func(x float64) float64 {
		return coef[0]*x +
			coef[1]*x*x/2 +
			coef[2]*x*x*x/3 +
			coef[3]*x*x*x*x/4
	}
	return primitive(hi) - primitive(lo)
}
