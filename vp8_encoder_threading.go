package govpx

import "runtime"

// maxEncoderThreads bounds EncoderOptions.Threads. libvpx's
// VP8E_SET_NUMBER_OF_THREADS (vp8/encoder/onyx_if.c) clamps against
// processor_core_count and the macroblock-column / mt_sync_range budget;
// govpx applies a hard ceiling here to bound persistent row-worker state.
//
// 64 is comfortably above today's largest commodity NUMA node and well
// above the libvpx --threads=N ceiling (which the upstream vpxenc CLI
// also documents as practically capped near processor_core_count).
const maxEncoderThreads = 64

// effectiveThreadCount returns the runtime worker-goroutine count for
// row-parallel inter-frame work, derived from EncoderOptions.Threads.
// Mirrors libvpx vp8cx_create_encoder_threads (vp8/encoder/ethreading.c),
// which clamps multi_threaded against cm->processor_core_count. govpx caps
// against runtime.NumCPU() so a misconfigured Threads value does not
// oversubscribe the host. A value >1 permits row threading; individual
// frames may still fall back to the serial loop when they are too small or
// when oracle tracing requires byte-stable serial instrumentation.
func (e *VP8Encoder) effectiveThreadCount() int {
	threads := e.opts.Threads
	if threads <= 0 {
		threads = 1
	}
	cores := max(runtime.NumCPU(), 1)
	if threads > cores {
		threads = cores
	}
	if threads > maxEncoderThreads {
		threads = maxEncoderThreads
	}
	return threads
}
