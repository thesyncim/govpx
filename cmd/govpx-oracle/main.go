package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	args := os.Args[1:]
	helper := ""
	if len(args) >= 2 && args[0] == "-oracle" {
		helper = args[1]
		args = args[2:]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: govpx-oracle [-oracle path] decode input.ivf\n")
		os.Exit(2)
	}
	if helper == "" {
		helper = findHelper()
	}
	if helper == "" {
		fmt.Fprintf(os.Stderr, "govpx-oracle: set GOVPX_VPX_ORACLE or pass -oracle path\n")
		os.Exit(2)
	}

	cmd := exec.Command(helper, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "govpx-oracle: %v\n", err)
		os.Exit(1)
	}
}

func findHelper() string {
	if helper := os.Getenv("GOVPX_VPX_ORACLE"); helper != "" {
		return helper
	}
	if helper, err := exec.LookPath("govpx-vpx-oracle"); err == nil {
		return helper
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	helper := filepath.Join(filepath.Dir(exe), "govpx-vpx-oracle")
	if info, err := os.Stat(helper); err == nil && !info.IsDir() {
		return helper
	}
	return ""
}
