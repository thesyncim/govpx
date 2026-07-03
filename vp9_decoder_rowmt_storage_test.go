package govpx

import (
	"testing"
	"time"

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
	storage.ensureJobQueue(3, 2)
	if got, want := storage.numJobs, 6; got != want {
		t.Fatalf("numJobs = %d, want %d", got, want)
	}
	if got, want := cap(storage.jobq.jobs), 14; got != want {
		t.Fatalf("jobq cap = %d, want %d", got, want)
	}
	if got, want := len(storage.reconCond), 6; got != want {
		t.Fatalf("reconCond len = %d, want %d", got, want)
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
	if storage.reconMu != nil {
		t.Fatal("reconMu retained after release")
	}
	if storage.reconCond != nil {
		t.Fatal("reconCond retained after release")
	}
	if storage.jobq.jobs != nil {
		t.Fatal("jobq retained after release")
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

func TestVP9DecoderRowMTJobQueueLifecycle(t *testing.T) {
	var q vp9DecoderRowMTJobQueue
	q.ensureCapacity(2)
	q.reset()

	if _, ok := q.dequeue(false); ok {
		t.Fatal("empty nonblocking dequeue returned a job")
	}
	if !q.queue(vp9DecoderRowMTJob{
		rowNum: 0, tileCol: 1, jobType: vp9DecoderRowMTJobParse,
	}) {
		t.Fatal("queue parse job failed")
	}
	if !q.queue(vp9DecoderRowMTJob{
		rowNum: 8, tileCol: 1, jobType: vp9DecoderRowMTJobRecon,
	}) {
		t.Fatal("queue recon job failed")
	}
	if q.queue(vp9DecoderRowMTJob{jobType: vp9DecoderRowMTJobLPF}) {
		t.Fatal("queue accepted job past fixed capacity")
	}

	job, ok := q.dequeue(false)
	if !ok || job.rowNum != 0 || job.tileCol != 1 ||
		job.jobType != vp9DecoderRowMTJobParse {
		t.Fatalf("first dequeue = %+v, ok=%v; want parse row 0 tile 1", job, ok)
	}
	job, ok = q.dequeue(false)
	if !ok || job.rowNum != 8 || job.tileCol != 1 ||
		job.jobType != vp9DecoderRowMTJobRecon {
		t.Fatalf("second dequeue = %+v, ok=%v; want recon row 8 tile 1", job, ok)
	}
	q.terminate()
	if _, ok := q.dequeue(false); ok {
		t.Fatal("terminated empty queue returned a job")
	}

	q.reset()
	if !q.queue(vp9DecoderRowMTJob{rowNum: 16, jobType: vp9DecoderRowMTJobLPF}) {
		t.Fatal("queue lpf job after reset failed")
	}
	job, ok = q.dequeue(false)
	if !ok || job.rowNum != 16 || job.jobType != vp9DecoderRowMTJobLPF {
		t.Fatalf("dequeue after reset = %+v, ok=%v; want lpf row 16", job, ok)
	}
}

func TestVP9DecoderRowMTJobQueueBlockingWakeups(t *testing.T) {
	var q vp9DecoderRowMTJobQueue
	q.ensureCapacity(1)
	q.reset()

	got := make(chan vp9DecoderRowMTJob, 1)
	go func() {
		job, ok := q.dequeue(true)
		if !ok {
			got <- vp9DecoderRowMTJob{rowNum: -1}
			return
		}
		got <- job
	}()
	if !q.queue(vp9DecoderRowMTJob{rowNum: 24, tileCol: 2,
		jobType: vp9DecoderRowMTJobRecon}) {
		t.Fatal("queue blocking wakeup job failed")
	}
	select {
	case job := <-got:
		if job.rowNum != 24 || job.tileCol != 2 ||
			job.jobType != vp9DecoderRowMTJobRecon {
			t.Fatalf("blocking dequeue job = %+v", job)
		}
	case <-time.After(time.Second):
		t.Fatal("blocking dequeue did not wake after queue")
	}

	q.reset()
	done := make(chan bool, 1)
	go func() {
		_, ok := q.dequeue(true)
		done <- ok
	}()
	q.terminate()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("terminated blocking dequeue returned ok")
		}
	case <-time.After(time.Second):
		t.Fatal("blocking dequeue did not wake after terminate")
	}
}

func TestVP9DecoderRowMTReconMapSync(t *testing.T) {
	var storage vp9DecoderRowMTFrameStorage
	storage.reset(4)
	storage.ensureJobQueue(2, 2)

	done := make(chan bool, 1)
	go func() {
		done <- storage.reconMapRead(3, 1)
	}()
	if !storage.reconMapWrite(3, 1) {
		t.Fatal("reconMapWrite returned false")
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("reconMapRead returned false")
		}
	case <-time.After(time.Second):
		t.Fatal("reconMapRead did not wake after reconMapWrite")
	}
	if !storage.reconMapWrite(0, 0) {
		t.Fatal("reconMapWrite row 0 returned false")
	}
	if !storage.reconMapRead(0, 0) {
		t.Fatal("reconMapRead row 0 returned false")
	}
	if storage.reconMapRead(99, 0) {
		t.Fatal("out-of-range reconMapRead returned true")
	}
	if storage.reconMapWrite(0, 99) {
		t.Fatal("out-of-range reconMapWrite returned true")
	}
}
