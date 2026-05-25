package benchcmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFakeVpxencHelper(t *testing.T) {
	if os.Getenv("GOVPX_FAKE_VPXENC") != "1" {
		return
	}
	output := ""
	limit := 1
	width := 16
	height := 16
	fps := 30
	bitrate := 1200
	for _, arg := range os.Args {
		if after, ok := strings.CutPrefix(arg, "--output="); ok {
			output = after
		}
		if after, ok := strings.CutPrefix(arg, "--limit="); ok {
			n, err := strconv.Atoi(after)
			if err == nil && n > 0 {
				limit = n
			}
		}
		if after, ok := strings.CutPrefix(arg, "--width="); ok {
			width = atoiPositive(after, width)
		}
		if after, ok := strings.CutPrefix(arg, "--height="); ok {
			height = atoiPositive(after, height)
		}
		if after, ok := strings.CutPrefix(arg, "--fps="); ok {
			fps = atoiPositive(strings.TrimSuffix(after, "/1"), fps)
		}
		if after, ok := strings.CutPrefix(arg, "--target-bitrate="); ok {
			bitrate = atoiPositive(after, bitrate)
		}
	}
	if output == "" {
		fmt.Fprintln(os.Stderr, "fake vpxenc missing --output")
		os.Exit(2)
	}
	if err := writeFakeIVF(output, width, height, fps, bitrate, limit); err != nil {
		fmt.Fprintf(os.Stderr, "fake vpxenc write output: %v\n", err)
		os.Exit(1)
	}
	// Mimic vpxenc's per-pass progress output so the bench's stderr
	// parser has something deterministic to read. 1000 us per frame is
	// arbitrary but small enough to leave room for non-zero subprocess
	// overhead in the wall-clock measurement.
	const usPerFrame = 1000
	totalUS := usPerFrame * limit
	fmt.Fprintf(os.Stderr, "Pass 1/1 frame %4d/%-4d %7dB %7d us %7.2f fps    \n", limit, limit, 0, totalUS, 1e6/float64(usPerFrame))
	os.Exit(0)
}

func TestFakeLibvpxOracleHelper(t *testing.T) {
	if os.Getenv("GOVPX_FAKE_LIBVPX_ORACLE") != "1" {
		return
	}
	subcmd := ""
	input := ""
	for i, arg := range os.Args {
		if arg == "decode" || arg == "decode-bench" {
			subcmd = arg
			if i+1 < len(os.Args) {
				input = os.Args[i+1]
			}
		}
	}
	if input == "" {
		fmt.Fprintln(os.Stderr, "fake libvpx oracle missing decode input")
		os.Exit(2)
	}
	ivf, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake libvpx oracle read input: %v\n", err)
		os.Exit(1)
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake libvpx oracle parse input: %v\n", err)
		os.Exit(1)
	}
	if subcmd == "decode-bench" {
		// Emit a deterministic oracle-bench summary so the bench's
		// stderr parser has something to read. 500 us/frame is
		// arbitrary but small enough to leave room for non-zero
		// subprocess overhead in the wall-clock measurement.
		const nsPerFrame = int64(500 * time.Microsecond)
		sumNS := nsPerFrame * int64(len(sizes))
		fmt.Fprintf(os.Stderr,
			"oracle-bench frames=%d decoded=%d sum_ns=%d loop_ns=%d p50_ns=%d p95_ns=%d p99_ns=%d\n",
			len(sizes), len(sizes), sumNS, sumNS, nsPerFrame, nsPerFrame, nsPerFrame)
		fmt.Println(len(sizes))
		os.Exit(0)
	}
	for i := range sizes {
		fmt.Printf("{\"frame\":%d}\n", i)
	}
	os.Exit(0)
}
