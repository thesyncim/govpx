package analysis

import (
	"errors"
	"sync"
)

// ErrGPUAnalyzerNotRegistered is returned by [New] when the caller
// requests [VP8AnalysisObserveGPU] but no GPU constructor has been
// registered. The GPU implementation lives in a separate public
// package (github.com/thesyncim/govpx/gpuanalysis) that must be
// blank-imported to register its constructor at program init time.
//
// This pattern keeps the default govpx build free of any GPU runtime
// dependency: callers who do not opt in pay no binary cost, no
// startup cost, and no import-graph cost.
var ErrGPUAnalyzerNotRegistered = errors.New(
	"vp8/analysis: GPU analyzer not registered; blank-import " +
		"\"github.com/thesyncim/govpx/gpuanalysis\" to enable VP8AnalysisObserveGPU",
)

// GPUConstructor is the constructor signature that the gpuanalysis
// package registers via [RegisterGPUConstructor]. It returns a
// concrete [Analyzer] backed by a GPU compute kernel, or an error if
// the GPU stack cannot be initialised on this machine.
type GPUConstructor func(cfg Config) (Analyzer, error)

var (
	registryMu       sync.RWMutex
	gpuConstructor   GPUConstructor
	gpuPackageLoaded bool
)

// RegisterGPUConstructor installs the GPU analyzer factory. It is
// intended to be called from an init() function in the optional
// gpuanalysis package. Calling it more than once panics so the
// registration order is unambiguous.
func RegisterGPUConstructor(c GPUConstructor) {
	if c == nil {
		panic("vp8/analysis: RegisterGPUConstructor: nil constructor")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if gpuPackageLoaded {
		panic("vp8/analysis: RegisterGPUConstructor: already registered")
	}
	gpuConstructor = c
	gpuPackageLoaded = true
}

// gpuConstructorFor returns the registered GPU constructor, or nil if
// none is registered.
func gpuConstructorFor() GPUConstructor {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return gpuConstructor
}
