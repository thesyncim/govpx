package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func TestVP9DecoderRowMTFrameStorageLayout(t *testing.T) {
	var storage vp9DecoderRowMTFrameStorage
	const numSBs = 9
	storage.reset(numSBs)

	if storage.numSBs != numSBs {
		t.Fatalf("numSBs = %d, want %d", storage.numSBs, numSBs)
	}
	if got, want := len(storage.partition), numSBs*vp9DecoderRowMTPartitionsPerSB; got != want {
		t.Fatalf("partition len = %d, want %d", got, want)
	}
	if got := len(storage.reconMap); got != numSBs {
		t.Fatalf("reconMap len = %d, want %d", got, numSBs)
	}
	for plane := range vp9dec.MaxMbPlane {
		if got, want := len(storage.eob[plane]), numSBs<<vp9DecoderRowMTEobsPerSBLog2; got != want {
			t.Fatalf("plane %d eob len = %d, want %d", plane, got, want)
		}
		if got, want := len(storage.dqcoeff[plane]), numSBs<<vp9DecoderRowMTDQCoeffsPerSBLog2; got != want {
			t.Fatalf("plane %d dqcoeff len = %d, want %d", plane, got, want)
		}
		if !buffers.ByteSliceAligned(storage.dqcoeffBytes[plane], vp9DecoderRowMTDQCoeffAlign) {
			t.Fatalf("plane %d dqcoeff storage is not %d-byte aligned", plane, vp9DecoderRowMTDQCoeffAlign)
		}
	}

	storage.eobForSB(0, 3)[7] = 11
	storage.dqcoeffForSB(1, 4)[19] = -21
	storage.partitionsForSB(5)[2] = common.PartitionSplit
	storage.reconMap[6] = 1
	storage.reset(numSBs)

	if got := storage.eobForSB(0, 3)[7]; got != 0 {
		t.Fatalf("reset left stale eob = %d", got)
	}
	if got := storage.dqcoeffForSB(1, 4)[19]; got != 0 {
		t.Fatalf("reset left stale dqcoeff = %d", got)
	}
	if got := storage.partitionsForSB(5)[2]; got != common.PartitionNone {
		t.Fatalf("reset left stale partition = %d", got)
	}
	if got := storage.reconMap[6]; got != 0 {
		t.Fatalf("reset left stale reconMap = %d", got)
	}
}

func TestVP9DecoderRowMTFrameStorageSteadyStateAlloc(t *testing.T) {
	var storage vp9DecoderRowMTFrameStorage
	const numSBs = 20
	storage.reset(numSBs)

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		storage.reset(numSBs)
	})
	if allocs != 0 {
		t.Fatalf("row-mt frame storage reset allocs = %v, want 0", allocs)
	}
}

func TestVP9DecoderRowMTFrameStorageRelease(t *testing.T) {
	var storage vp9DecoderRowMTFrameStorage
	storage.reset(4)
	storage.release()

	if storage.numSBs != 0 {
		t.Fatalf("numSBs after release = %d, want 0", storage.numSBs)
	}
	if storage.partition != nil {
		t.Fatal("partition retained after release")
	}
	if storage.reconMap != nil {
		t.Fatal("reconMap retained after release")
	}
	for plane := range vp9dec.MaxMbPlane {
		if storage.eob[plane] != nil {
			t.Fatalf("plane %d eob retained after release", plane)
		}
		if storage.dqcoeffBytes[plane] != nil {
			t.Fatalf("plane %d dqcoeff bytes retained after release", plane)
		}
		if storage.dqcoeff[plane] != nil {
			t.Fatalf("plane %d dqcoeff retained after release", plane)
		}
	}
}
