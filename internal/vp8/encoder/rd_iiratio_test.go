package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestRDConstantsIQ17Boundary(t *testing.T) {
	if gotMult, gotDiv := RDConstantsWithZbin(16, 0); gotMult != 907 || gotDiv != 100 {
		t.Fatalf("RDConstantsWithZbin(16,0) = (%d,%d), want (907,100)", gotMult, gotDiv)
	}
	if gotMult, gotDiv := RDConstantsWithZbin(17, 0); gotMult != 10 || gotDiv != 1 {
		t.Fatalf("RDConstantsWithZbin(17,0) = (%d,%d), want (10,1)", gotMult, gotDiv)
	}
	if gotRaw := RawRDMultiplierWithZbin(17, 0); gotRaw != 1010 {
		t.Fatalf("RawRDMultiplierWithZbin(17,0) = %d, want 1010", gotRaw)
	}
	if gotRaw := RawRDMultiplierWithZbin(16, 0); gotRaw != 907 {
		t.Fatalf("RawRDMultiplierWithZbin(16,0) = %d, want 907", gotRaw)
	}
	if got := common.DCQuant(16, 0); got != 18 {
		t.Fatalf("DCQuant(16,0) = %d, want 18", got)
	}
	if got := common.DCQuant(17, 0); got != 19 {
		t.Fatalf("DCQuant(17,0) = %d, want 19", got)
	}
}

func TestRDConstantsWithIIRatioAppliesPass2Lift(t *testing.T) {
	wantHead := [...]int{4, 4, 3, 2, 1, 0}
	for i, want := range wantHead {
		if rdIIFactor[i] != want {
			t.Fatalf("rdIIFactor[%d] = %d, want %d", i, rdIIFactor[i], want)
		}
	}
	for i := len(wantHead); i < len(rdIIFactor); i++ {
		if rdIIFactor[i] != 0 {
			t.Fatalf("rdIIFactor[%d] = %d, want 0", i, rdIIFactor[i])
		}
	}

	// q=16 starts below the >1000 split. next_iiratio=2 lifts raw RDMULT
	// from 907 to 1077, crossing the split and matching libvpx's pass-2
	// inter-frame vp8_initialize_rd_consts path.
	if gotMult, gotDiv := RDConstantsWithZbinAndIIRatio(16, 0, 2); gotMult != 10 || gotDiv != 1 {
		t.Fatalf("RDConstantsWithZbinAndIIRatio(16,0,2) = (%d,%d), want (10,1)", gotMult, gotDiv)
	}
	if got := ErrorPerBitWithZbinAndIIRatio(16, 0, 2); got != 9 {
		t.Fatalf("ErrorPerBitWithZbinAndIIRatio(16,0,2) = %d, want 9", got)
	}

	if gotMult, gotDiv := RDConstantsWithZbinAndIIRatio(16, 0, -1); gotMult != 907 || gotDiv != 100 {
		t.Fatalf("RDConstantsWithZbinAndIIRatio(16,0,-1) = (%d,%d), want no-lift (907,100)", gotMult, gotDiv)
	}
	if gotMult, gotDiv := RDConstantsWithZbinAndIIRatio(16, 0, 31); gotMult != 907 || gotDiv != 100 {
		t.Fatalf("RDConstantsWithZbinAndIIRatio(16,0,31) = (%d,%d), want zero-lift (907,100)", gotMult, gotDiv)
	}
	if gotMult, gotDiv := RDConstantsWithZbinAndIIRatio(16, 0, 99); gotMult != 907 || gotDiv != 100 {
		t.Fatalf("RDConstantsWithZbinAndIIRatio(16,0,99) = (%d,%d), want clamped zero-lift (907,100)", gotMult, gotDiv)
	}
}
