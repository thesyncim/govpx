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
	vp9 := false
	for _, arg := range os.Args {
		if after, ok := strings.CutPrefix(arg, "--output="); ok {
			output = after
		}
		if arg == "--codec=vp9" {
			vp9 = true
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
	var err error
	if vp9 {
		err = writeFakeVP9IVF(output, width, height, fps, bitrate, limit)
	} else {
		err = writeFakeIVF(output, width, height, fps, bitrate, limit)
	}
	if err != nil {
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
	if vp9 && os.Getenv("GOVPX_LIBVPX_VP9_CALL_STATS") != "" {
		fields := []string{
			"LIBVPX_VP9_CALL_STATS",
			"inter_mode_picks=11",
			"inter_mode_sub8x8_picks=1",
			"build_sby=2",
			"build_sbp=3",
			"build_sbuv=4",
			"build_sb=5",
			"build_planes=6",
			"single_predictor_builds=7",
			"fullpel_searches=8",
			"sad_calls=9",
			"sad_candidates=10",
			"sad_batch_calls=2",
			"predictor_copy=12",
			"predictor_avg=13",
			"predictor_vert=14",
			"predictor_avg_vert=15",
			"predictor_horiz=16",
			"predictor_avg_horiz=17",
			"predictor_2d=18",
			"predictor_avg_2d=19",
			"mode_block_64x64=20",
			"mode_block_32x32=21",
			"mode_block_32x16=22",
			"mode_block_16x32=23",
			"mode_block_16x16=24",
			"mode_block_16x8=25",
			"mode_block_8x16=26",
			"mode_block_8x8=27",
			"mode_block_sub8=28",
			"varpart_choose_calls=29",
			"varpart_copy_hits=30",
			"varpart_content_state_invalid=31",
			"varpart_content_state_low_sad_low_sumdiff=32",
			"varpart_content_state_low_sad_high_sumdiff=33",
			"varpart_content_state_high_sad_low_sumdiff=34",
			"varpart_content_state_high_sad_high_sumdiff=35",
			"varpart_content_state_low_var_high_sumdiff=36",
			"varpart_content_state_very_high_sad=37",
			"varpart_ysad_valid=38",
			"varpart_ysad_select_64x64=39",
			"varpart_copy_partition_select=40",
			"varpart_force_split_64=41",
			"varpart_force_split_32=42",
			"varpart_force_split_16=43",
			"varpart_setvt_calls=44",
			"varpart_setvt_64x64=45",
			"varpart_setvt_32x32=46",
			"varpart_setvt_16x16=47",
			"varpart_setvt_8x8=48",
			"varpart_setvt_force_split=49",
			"varpart_setvt_force_split_64x64=50",
			"varpart_setvt_force_split_32x32=51",
			"varpart_setvt_force_split_16x16=52",
			"varpart_setvt_select=53",
			"varpart_setvt_split=54",
		}
		fmt.Fprintln(os.Stderr, strings.Join(fields, " "))
	}
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

func TestFakeVpxdecVP9Helper(t *testing.T) {
	if os.Getenv("GOVPX_FAKE_VPXDEC_VP9") != "1" {
		return
	}
	input := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.HasSuffix(arg, ".ivf") {
			input = arg
		}
	}
	if input == "" {
		fmt.Fprintln(os.Stderr, "fake vpxdec-vp9 missing input")
		os.Exit(2)
	}
	ivf, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake vpxdec-vp9 read input: %v\n", err)
		os.Exit(1)
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake vpxdec-vp9 parse input: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%d decoded frames/%.3f seconds\n", len(sizes), float64(len(sizes))*0.0005)
	os.Exit(0)
}
