package gpuanalysis

// Backend is the minimal GPU abstraction needed to run the VP8
// analysis kernel. Each platform implements one concrete backend
// (darwin: Metal via purego). The shape is deliberately small —
// just enough to upload a current source plane, dispatch one
// compute, and read per-MB output back — so a new backend (Vulkan
// via purego on Linux, D3D12 on Windows) can be slotted in later
// without changing the analyzer.
//
// All methods are called from a single goroutine; backends do not
// need to be safe for concurrent use.
type Backend interface {
	// Resize ensures GPU-side buffers exist for a width×height frame
	// and returns the per-MB output stride in bytes. Re-allocates
	// only when the requested size exceeds prior allocations.
	Resize(width, height int) error

	// Upload copies the current source luma plane into the "current"
	// ping-pong buffer and updates the uniform parameters. The
	// plane is treated as a packed `width*height` slice; callers
	// pass a stride-folded view.
	Upload(plane []byte, width, height int, havePrev bool) error

	// Dispatch submits the compute pass that reads cur+prev planes
	// and writes per-MB outputs. Returns when the work has been
	// submitted; callers serialize the next frame's work by waiting
	// inside Readback.
	Dispatch() error

	// Readback blocks until Dispatch completes and returns a view
	// into the per-MB output buffer. The view is valid until the
	// next Dispatch call. Layout: 16 bytes per MB (sad, variance,
	// texture, packed flags|radius|staticScore), all little-endian
	// u32.
	Readback() ([]byte, error)

	// SwapPlanes flips the ping-pong roles so the just-uploaded
	// "current" plane becomes "previous" for the next frame.
	SwapPlanes()

	// UploadReconstructedRef stores a reconstructed reference frame
	// in a dedicated GPU buffer separate from the ping-pong planes.
	// The encoder calls this after each frame's reconstruction so
	// the next frame's analyzer can compute SAD in the
	// reconstruction domain (which is what the encoder's own motion
	// search uses), instead of source-vs-source SAD. Foundation for
	// GPU motion search.
	//
	// Backends that have not yet implemented this return nil and
	// the analyzer falls back to source-domain comparison.
	UploadReconstructedRef(plane []byte, width, height int) error

	// Close releases all GPU resources.
	Close() error

	// Name returns a stable identifier for the backend (e.g.
	// "metal", "vulkan"). Used in logs and telemetry only.
	Name() string
}
