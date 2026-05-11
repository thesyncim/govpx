package govpx

import (
	"errors"
	"testing"
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
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
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
	baseline := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, distortion)

	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 0))
	wantFlat := libvpxRDCost(e.tunedRDMultiplier(rdMult, 0, 0), rdDiv, rate, distortion)
	if got := e.tunedRDModeScoreWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 0), 0, 0, rate, distortion); got != wantFlat {
		t.Fatalf("flat tuned RD = %d, want %d", got, wantFlat)
	}
	busy := e.tunedRDModeScoreWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 1), 0, 1, rate, distortion)
	if wantBusy := func() int {
		rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, e.tunedZbinOverQuant(zbinOverQuant, 0, 1))
		return libvpxRDCost(e.tunedRDMultiplier(rdMult, 0, 1), rdDiv, rate, distortion)
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
