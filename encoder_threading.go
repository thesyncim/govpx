package govpx

import "runtime"

// maxEncoderThreads bounds EncoderOptions.Threads. libvpx's
// VP8E_SET_NUMBER_OF_THREADS (vp8/encoder/onyx_if.c) clamps against
// processor_core_count and the macroblock-column / mt_sync_range budget;
// govpx applies a hard ceiling here to bound per-frame scratch
// allocations sized by Threads when the row-threaded macroblock pipeline
// (mirroring vp8/encoder/ethreading.c thread_encoding_proc) lands.
//
// 64 is comfortably above today's largest commodity NUMA node and well
// above the libvpx --threads=N ceiling (which the upstream vpxenc CLI
// also documents as practically capped near processor_core_count).
const maxEncoderThreads = 64

// effectiveThreadCount returns the runtime worker-goroutine count for
// per-frame parallel work, derived from EncoderOptions.Threads. Mirrors
// libvpx vp8cx_create_encoder_threads (vp8/encoder/ethreading.c) which
// clamps multi_threaded against cm->processor_core_count. govpx caps
// against runtime.NumCPU() so a misconfigured Threads value does not
// oversubscribe the host. The encoder's existing serial macroblock loop
// is currently invoked for any returned value (Threads=1 today is the
// canonical reference path); callers must not interpret a return value
// >1 as guaranteed parallelism, and the per-MB output remains
// byte-identical regardless of this number until the row-threaded
// pipeline lands.
func (e *VP8Encoder) effectiveThreadCount() int {
	if e == nil {
		return 1
	}
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
