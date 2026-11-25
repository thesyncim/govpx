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

// WorkerWakeSlot is a cache-line-padded atomic epoch + lazy-park wakeup lane.
// Under steady-state encoding, helpers observe epoch updates via pure atomics
// (zero channel operations, zero runtime.acquireSudog allocations). When idle
// longer than spinBudget, the helper parks cleanly on wake.
type WorkerWakeSlot struct {
	epoch    atomic.Uint64
	sleeping atomic.Bool
	wake     chan struct{}
	_        [EncoderCacheLineSize - 24]byte
}

func (s *WorkerWakeSlot) Init() {
	s.wake = make(chan struct{}, 1)
}

func (s *WorkerWakeSlot) Signal(nextEpoch uint64) {
	s.epoch.Store(nextEpoch)
	if s.sleeping.Load() {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *WorkerWakeSlot) Close() {
	s.epoch.Store(WorkerWakeShutdownEpoch)
	if s.wake != nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *WorkerWakeSlot) Wait(seenEpoch uint64, spinBudget int) (uint64, bool) {
	for spins := range spinBudget {
		if cur := s.epoch.Load(); cur != seenEpoch {
			return cur, cur != WorkerWakeShutdownEpoch
		}
		RuntimeProcYield(30)
		if spins >= RowWorkerIdleSchedulerBackoff && spins%RowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
	for {
		s.sleeping.Store(true)
		if cur := s.epoch.Load(); cur != seenEpoch {
			s.sleeping.Store(false)
			return cur, cur != WorkerWakeShutdownEpoch
		}
		<-s.wake
		s.sleeping.Store(false)
		if cur := s.epoch.Load(); cur != seenEpoch {
			return cur, cur != WorkerWakeShutdownEpoch
		}
	}
}
