package codecapi

import (
	"testing"
	"time"
	"unsafe"
)

func TestWorkerWakeSlotSize(t *testing.T) {
	if got := unsafe.Sizeof(WorkerWakeSlot{}); got != EncoderCacheLineSize {
		t.Fatalf("sizeof(WorkerWakeSlot) = %d, want %d", got, EncoderCacheLineSize)
	}
}

func TestWorkerWakeSlotStateMachine(t *testing.T) {
	// Uninitialized Wait must not deadlock on nil channel.
	var uninit WorkerWakeSlot
	if ep, ok := uninit.Wait(0, 4); ok || ep != WorkerWakeShutdownEpoch {
		t.Fatalf("uninit.Wait = (%d, %v), want (shutdown, false)", ep, ok)
	}

	var slot WorkerWakeSlot
	slot.Init()

	// Spin wakeup (no channel park).
	done := make(chan uint64, 1)
	go func() {
		ep, ok := slot.Wait(0, 1024)
		if !ok {
			done <- 0
			return
		}
		done <- ep
	}()
	slot.Signal(1)
	select {
	case ep := <-done:
		if ep != 1 {
			t.Fatalf("spin Wait epoch = %d, want 1", ep)
		}
	case <-time.After(time.Second):
		t.Fatal("spin Wait timed out")
	}

	// Parked wakeup (spinBudget = 0 forces immediate park).
	go func() {
		ep, ok := slot.Wait(1, 0)
		if !ok {
			done <- 0
			return
		}
		done <- ep
	}()
	time.Sleep(5 * time.Millisecond)
	slot.Signal(2)
	select {
	case ep := <-done:
		if ep != 2 {
			t.Fatalf("parked Wait epoch = %d, want 2", ep)
		}
	case <-time.After(time.Second):
		t.Fatal("parked Wait timed out")
	}

	// Close is terminal: subsequent Signal must not resurrect the slot.
	slot.Close()
	slot.Signal(3)
	if ep, ok := slot.Wait(2, 4); ok || ep != WorkerWakeShutdownEpoch {
		t.Fatalf("Wait after Close+Signal = (%d, %v), want (shutdown, false)", ep, ok)
	}
}

func BenchmarkWorkerWakeSlotSignalWait(b *testing.B) {
	var slot WorkerWakeSlot
	slot.Init()
	b.ReportAllocs()
	b.ResetTimer()
	var seen uint64
	for i := 1; i <= b.N; i++ {
		slot.Signal(uint64(i))
		var ok bool
		seen, ok = slot.Wait(seen, 16)
		if !ok {
			b.Fatal("unexpected shutdown")
		}
	}
}
