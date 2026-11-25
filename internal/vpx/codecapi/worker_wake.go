package codecapi

import (
	"runtime"
	"sync/atomic"
)

const EncoderCacheLineSize = 64

const (
	RowWorkerIdleSpinBudget       = 65536
	VP9WorkerIdleSpinBudget       = 16384
	RowWorkerIdleSchedulerBackoff = 4096
	WorkerWakeShutdownEpoch       = ^uint64(0)
)

// workerWakeState is the explicit lifecycle state packed into the top 2 bits
// of WorkerWakeSlot.word, with the 62-bit epoch in the lower bits:
//
//	Uninitialized (0) --Init()--> Active (1) <--> Sleeping (2)
//	Any               --Close()-> Closed (3, terminal)
//
// Packing state and epoch into a single atomic.Uint64 makes Signal a single
// atomic Swap (replacing separate epoch.Store + sleeping.Load) and keeps the
// hot spin loop in Wait a single atomic Load.
type workerWakeState uint64

const (
	workerWakeUninitialized workerWakeState = 0
	workerWakeActive        workerWakeState = 1
	workerWakeSleeping      workerWakeState = 2
	workerWakeClosed        workerWakeState = 3

	workerWakeStateShift = 62
	workerWakeEpochMask  = (uint64(1) << workerWakeStateShift) - 1
	workerWakeClosedWord = uint64(workerWakeClosed) << workerWakeStateShift
)

func packWakeWord(st workerWakeState, epoch uint64) uint64 {
	return (uint64(st) << workerWakeStateShift) | (epoch & workerWakeEpochMask)
}

func unpackWakeState(word uint64) workerWakeState {
	return workerWakeState(word >> workerWakeStateShift)
}

func unpackWakeEpoch(word uint64) uint64 {
	return word & workerWakeEpochMask
}

// WorkerWakeSlot is a cache-line-padded atomic epoch + lazy-park wakeup lane.
// Under steady-state encoding, helpers observe epoch updates via pure atomics
// (zero channel operations, zero runtime.acquireSudog allocations). When idle
// longer than spinBudget, the helper parks cleanly on wake.
type WorkerWakeSlot struct {
	word atomic.Uint64
	wake chan struct{}
	_    [EncoderCacheLineSize - 16]byte
}

func (s *WorkerWakeSlot) Init() {
	s.wake = make(chan struct{}, 1)
	s.word.Store(packWakeWord(workerWakeActive, 0))
}

func (s *WorkerWakeSlot) Signal(nextEpoch uint64) {
	if nextEpoch == WorkerWakeShutdownEpoch {
		s.Close()
		return
	}
	prev := s.word.Swap(packWakeWord(workerWakeActive, nextEpoch))
	switch unpackWakeState(prev) {
	case workerWakeClosed:
		s.word.Store(workerWakeClosedWord)
	case workerWakeSleeping:
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *WorkerWakeSlot) Close() {
	s.word.Store(workerWakeClosedWord)
	if s.wake != nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *WorkerWakeSlot) Wait(seenEpoch uint64, spinBudget int) (uint64, bool) {
	if s.wake == nil {
		return WorkerWakeShutdownEpoch, false
	}
	seenMasked := seenEpoch & workerWakeEpochMask
	for spins := range spinBudget {
		cur := s.word.Load()
		st := unpackWakeState(cur)
		if st == workerWakeClosed || st == workerWakeUninitialized {
			return WorkerWakeShutdownEpoch, false
		}
		if ep := unpackWakeEpoch(cur); ep != seenMasked {
			return ep, true
		}
		RuntimeProcYield(30)
		if spins >= RowWorkerIdleSchedulerBackoff && spins%RowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
	for {
		cur := s.word.Load()
		st := unpackWakeState(cur)
		if st == workerWakeClosed || st == workerWakeUninitialized {
			return WorkerWakeShutdownEpoch, false
		}
		if ep := unpackWakeEpoch(cur); ep != seenMasked {
			return ep, true
		}
		if !s.word.CompareAndSwap(cur, packWakeWord(workerWakeSleeping, seenMasked)) {
			continue
		}
		<-s.wake
		cur = s.word.Load()
		st = unpackWakeState(cur)
		if st == workerWakeClosed || st == workerWakeUninitialized {
			return WorkerWakeShutdownEpoch, false
		}
		if ep := unpackWakeEpoch(cur); ep != seenMasked {
			return ep, true
		}
	}
}
