package govpx

import (
	"errors"
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

func TestSetTuningValidation(t *testing.T) {
	e := newTestEncoder(t)
	e.activityMap = []uint32{64}
	e.activityAvg = 64
	e.activityMapValid = true

	if err := e.SetTuning(Tuning(2)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid tuning error = %v, want ErrInvalidConfig", err)
	}
	if !e.activityMapValid {
		t.Fatalf("invalid SetTuning cleared the active map")
	}
	if err := e.SetTuning(TuneSSIM); err != nil {
		t.Fatalf("SetTuning(TuneSSIM) returned error: %v", err)
	}
	if e.opts.Tuning != TuneSSIM {
		t.Fatalf("Tuning = %d, want TuneSSIM", e.opts.Tuning)
	}
	if e.activityMapValid {
		t.Fatalf("SetTuning(TuneSSIM) kept stale activity map")
	}
	e.activityMapValid = true
	if err := e.SetTuning(TunePSNR); err != nil {
		t.Fatalf("SetTuning(TunePSNR) returned error: %v", err)
	}
	if e.opts.Tuning != TunePSNR {
		t.Fatalf("Tuning = %d, want TunePSNR", e.opts.Tuning)
	}
	if e.activityMapValid {
		t.Fatalf("SetTuning(TunePSNR) kept stale activity map")
	}
}

func TestPrepareTuningActivityMapZeroCostForPSNR(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	src := sourceImageFromPublic(testImage(32, 16))
	if err := e.prepareTuningActivityMap(src, 1, 2); err != nil {
		t.Fatalf("prepareTuningActivityMap returned error: %v", err)
	}
	if e.activityMap != nil {
		t.Fatalf("default tuning allocated activity map")
	}
	if e.activityMapValid {
		t.Fatalf("default tuning marked activity map valid")
	}
}

func TestPrepareTuningActivityMapBuildsForSSIM(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	if err := e.SetTuning(TuneSSIM); err != nil {
		t.Fatalf("SetTuning(TuneSSIM) returned error: %v", err)
	}
	img := testImage(32, 16)
	fillImage(img, 128, 128, 128)
	for i := range img.Y {
		img.Y[i] = byte((i * 37) & 0xff)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	if err := e.prepareTuningActivityMap(sourceImageFromPublic(img), rows, cols); err != nil {
		t.Fatalf("prepareTuningActivityMap returned error: %v", err)
	}
	if !e.activityMapValid {
		t.Fatalf("SSIM tuning did not mark activity map valid")
	}
	if got, want := len(e.activityMap), rows*cols; got != want {
		t.Fatalf("activity map length = %d, want %d", got, want)
	}
	if e.activityAvg < vp8ActivityAvgMin {
		t.Fatalf("activity average = %d, want at least %d", e.activityAvg, vp8ActivityAvgMin)
	}
}

func TestTuneSSIMActivityMapAdjustsRDCost(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	e.opts.Tuning = TuneSSIM
	e.activityMap = []uint32{64, 200000}
	e.activityAvg = 100000
	e.activityMapValid = true

	qIndex, zbinOverQuant := 20, 10
	rate, distortion := 100, 1000
	baseline := vp8enc.RDModeScoreWithZbin(qIndex, zbinOverQuant, rate, distortion)

	rdMult, rdDiv := vp8enc.RDConstantsWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 0))
	wantFlat := vp8enc.RDCost(e.tunedRDMultiplier(rdMult, 0, 0), rdDiv, rate, distortion)
	if got := e.tunedRDModeScoreWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 0), 0, 0, rate, distortion); got != wantFlat {
		t.Fatalf("flat tuned RD = %d, want %d", got, wantFlat)
	}
	busy := e.tunedRDModeScoreWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 1), 0, 1, rate, distortion)
	if wantBusy := func() int {
		rdMult, rdDiv := vp8enc.RDConstantsWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 1))
		return vp8enc.RDCost(e.tunedRDMultiplier(rdMult, 0, 1), rdDiv, rate, distortion)
	}(); busy != wantBusy {
		t.Fatalf("busy tuned RD = %d, want %d", busy, wantBusy)
	}
	if got := e.tunedZbinOverQuant(zbinOverQuant, 0, 0); got >= zbinOverQuant {
		t.Fatalf("flat zbin = %d, want below %d", got, zbinOverQuant)
	}
	if got := e.tunedZbinOverQuant(zbinOverQuant, 0, 1); got <= zbinOverQuant {
		t.Fatalf("busy zbin = %d, want above %d", got, zbinOverQuant)
	}
	if wantFlat == baseline && busy == baseline {
		t.Fatalf("SSIM activity map did not change RD cost from baseline %d", baseline)
	}

	e.activityMapValid = false
	if got := e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, 0, 0, rate, distortion); got != baseline {
		t.Fatalf("inactive tuning RD = %d, want baseline %d", got, baseline)
	}
}

func TestActivityRDConstantsCarryLastMacroblock(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	baseMult, baseDiv := vp8enc.RDConstantsWithZbin(4, 0)
	if baseMult != 179 || baseDiv != 100 {
		t.Fatalf("q4 RD constants = %d/%d, want 179/100", baseMult, baseDiv)
	}
	if gotMult, gotDiv := e.activityProbeRDConstants(4, 0); gotMult != baseMult || gotDiv != baseDiv {
		t.Fatalf("initial activity probe RD = %d/%d, want frame constants %d/%d", gotMult, gotDiv, baseMult, baseDiv)
	}

	e.activityProbeRDMult = 256
	e.activityProbeRDDiv = 100
	e.activityProbeRDValid = true
	if gotMult, gotDiv := e.activityProbeRDConstants(4, 0); gotMult != 256 || gotDiv != 100 {
		t.Fatalf("carried activity probe RD = %d/%d, want stale macroblock 256/100", gotMult, gotDiv)
	}
}

func TestUpdateActivityRDStateUsesBottomRightActivityMask(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	e.activityMap = []uint32{64, 200000}
	e.activityAvg = 100000
	e.activityMapValid = true

	baseMult, baseDiv := vp8enc.RDConstantsWithZbin(4, 0)
	e.updateActivityProbeRDState(4, 0, 1, 2)

	wantMult := e.tunedRDMultiplier(baseMult, 0, 1)
	if e.activityProbeRDMult != wantMult || e.activityProbeRDDiv != baseDiv || !e.activityProbeRDValid {
		t.Fatalf("activity probe RD state = %d/%d valid=%t, want %d/%d valid",
			e.activityProbeRDMult, e.activityProbeRDDiv, e.activityProbeRDValid, wantMult, baseDiv)
	}
}

func TestTunedErrorPerBitCarriesZbinOverQuant(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.rc.currentZbinOverQuant = 128
	if got := e.tunedErrorPerBit(127, 0, 0); got != 899 {
		t.Fatalf("zbin-adjusted errorperbit = %d, want libvpx q127/zbin128 value 899", got)
	}
	if got := e.tunedErrorPerBit(127, 0, 0); got == vp8enc.ErrorPerBit(127) {
		t.Fatalf("zbin-adjusted errorperbit collapsed to no-zbin value %d", got)
	}
}

func TestTunedErrorPerBitActivityMaskStartsFromZbinRD(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.activityMap = []uint32{200000}
	e.activityAvg = 100000
	e.activityMapValid = true
	e.rc.currentZbinOverQuant = 128

	rdMult, rdDiv := vp8enc.RDConstantsWithZbin(127, 128)
	tuned := e.tunedRDMultiplier(rdMult, 0, 0)
	want := max((tuned*100)/(110*rdDiv), 1)
	if got := e.tunedErrorPerBit(127, 0, 0); got != want {
		t.Fatalf("activity zbin-adjusted errorperbit = %d, want %d", got, want)
	}
}

func TestTuneSSIMActivityZbinAdjustmentCanApplyBelowZeroBase(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.Tuning = TuneSSIM
	e.activityMap = []uint32{64}
	e.activityAvg = 100000
	e.activityMapValid = true
	actZbinAdj, ok := e.tunedZbinAdjustment(0, 0)
	if !ok || actZbinAdj >= 0 {
		t.Fatalf("flat activity zbin adjustment = %d ok=%t, want negative adjustment", actZbinAdj, ok)
	}
	if got := e.tunedZbinOverQuant(0, 0, 0); got != 0 {
		t.Fatalf("tunedZbinOverQuant(0) = %d, want clamped zero", got)
	}

	for qIndex := 0; qIndex <= maxQuantizer; qIndex++ {
		quant := testRegularMacroblockQuant(t, qIndex)
		for v := int16(1); v < 512; v++ {
			var coeff [16]int16
			var noActQ, noActDQ [16]int16
			var actQ, actDQ [16]int16
			coeff[1] = v
			noActEOB := vp8enc.QuantizeDecisionBlockWithActivity(false, &coeff, &quant.Y1, 0, 0, &noActQ, &noActDQ)
			actEOB := vp8enc.QuantizeDecisionBlockWithActivity(false, &coeff, &quant.Y1, 0, actZbinAdj, &actQ, &actDQ)
			if noActEOB == 0 && actEOB > 0 {
				return
			}
		}
	}
	t.Fatalf("negative activity zbin adjustment did not admit a coefficient at base zbin 0")
}

func TestTuneSSIMEncodeSmoke(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            16,
		FPS:               30,
		TargetBitrateKbps: 1200,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		Tuning:            TuneSSIM,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	img := testImage(32, 16)
	fillImage(img, 128, 128, 128)
	if _, err := e.EncodeInto(make([]byte, 4096), img, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if !e.activityMapValid {
		t.Fatalf("TuneSSIM encode did not prepare activity map")
	}
}
