package benchcmd

import (
	"fmt"
	"os"
	"runtime/pprof"
)

func startBenchmarkCPUProfile(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create cpu profile: %w", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("start cpu profile: %w", err)
	}
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		pprof.StopCPUProfile()
		f.Close()
	}, nil
}
