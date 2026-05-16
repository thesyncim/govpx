// Command govpx-dsp-matrix runs the per-routine DSP benchmarks across
// both VP8 and VP9 with and without the `purego` build tag and
// produces a single comparison matrix.
//
// It scrapes `go test -bench` output for each (package, tag) pair,
// aligns the same benchmark across builds, and reports SIMD-vs-scalar
// speedups in text / markdown / json.
//
// The default invocation runs every known DSP kernel benchmark:
//
//	govpx-dsp-matrix
//
// To filter to a subset:
//
//	govpx-dsp-matrix -benches=BenchmarkVP9Variance16x16,BenchmarkSAD16x16
//
// To emit markdown (useful for PR / README):
//
//	govpx-dsp-matrix -format=md
//
// To emit JSON for downstream tooling:
//
//	govpx-dsp-matrix -format=json
//
// The harness does not require libvpx — the encode-end-to-end vs
// libvpx comparison lives in `govpx-bench -suite`. This tool focuses
// on per-kernel SIMD coverage so the SIMD-on / SIMD-off contrast is
// visible at a glance.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type benchRow struct {
	Codec     string  `json:"codec"`
	Kernel    string  `json:"kernel"`
	Size      string  `json:"size"`
	Bench     string  `json:"bench"`
	ScalarNS  float64 `json:"scalar_ns_per_op"`
	SIMDNS    float64 `json:"simd_ns_per_op"`
	Speedup   float64 `json:"speedup"`
	ScalarOps int     `json:"scalar_ops"`
	SIMDOps   int     `json:"simd_ops"`
}

type matrixReport struct {
	Goos       string     `json:"goos"`
	Goarch     string     `json:"goarch"`
	SIMDLabel  string     `json:"simd_label"`
	Generated  string     `json:"generated"`
	BenchTime  string     `json:"benchtime"`
	BenchCount int        `json:"bench_count"`
	Rows       []benchRow `json:"rows"`
}

type packageSpec struct {
	codec        string
	importPath   string
	benchPattern string
}

var (
	flagFormat    = flag.String("format", "text", "output format: text, md (markdown), or json")
	flagBenchTime = flag.String("benchtime", "200ms", "go test -benchtime value (e.g. 200ms or 100x)")
	flagBenchCnt  = flag.Int("count", 1, "go test -count value")
	flagBenches   = flag.String("benches", "", "comma-separated benchmark names to filter (substring match); empty = all known kernels")
	flagShort     = flag.Bool("short", true, "pass -short to go test (recommended for benchmark sanity)")
	flagDryRun    = flag.Bool("dry-run", false, "print the go test commands that would be invoked")
	flagVerbose   = flag.Bool("verbose", false, "stream the go test stderr/stdout to this process")
	flagOutput    = flag.String("o", "", "write report to this path instead of stdout")
)

func main() {
	flag.Parse()

	packages := []packageSpec{
		{
			codec:        "VP8",
			importPath:   "github.com/thesyncim/govpx/internal/vp8/dsp",
			benchPattern: "^Benchmark(SSE|Variance|SubpelVariance|Sixtap|Bilinear|IDCT|DCOnlyIDCT|InverseWalsh|DCOnlyInverseWalsh|Copy|AddResidual|DequantizeBlock|DequantIDCT|Intra4x4|Clip)",
		},
		{
			codec:        "VP9",
			importPath:   "github.com/thesyncim/govpx/internal/vp9/dsp",
			benchPattern: "^BenchmarkVP9",
		},
	}

	if *flagBenches != "" {
		// User-supplied filter: union of substrings as a regex.
		parts := strings.Split(*flagBenches, ",")
		quoted := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			quoted = append(quoted, regexp.QuoteMeta(p))
		}
		filter := strings.Join(quoted, "|")
		if filter != "" {
			for i := range packages {
				packages[i].benchPattern = "(" + filter + ")"
			}
		}
	}

	report := matrixReport{
		Goos:       envOr("GOOS", goos()),
		Goarch:     envOr("GOARCH", goarch()),
		BenchTime:  *flagBenchTime,
		BenchCount: *flagBenchCnt,
		Generated:  time.Now().UTC().Format(time.RFC3339),
	}
	report.SIMDLabel = pickSIMDLabel(report.Goarch)

	for _, pkg := range packages {
		scalar, err := runBenchmarks(pkg, true)
		if err != nil {
			fail("benchmark scalar (%s): %v", pkg.codec, err)
		}
		simd, err := runBenchmarks(pkg, false)
		if err != nil {
			fail("benchmark simd (%s): %v", pkg.codec, err)
		}
		for name, sres := range simd {
			pres, ok := scalar[name]
			if !ok {
				continue
			}
			kernel, size := decomposeBenchName(pkg.codec, name)
			row := benchRow{
				Codec:     pkg.codec,
				Kernel:    kernel,
				Size:      size,
				Bench:     name,
				ScalarNS:  pres.nsPerOp,
				SIMDNS:    sres.nsPerOp,
				ScalarOps: pres.ops,
				SIMDOps:   sres.ops,
			}
			if sres.nsPerOp > 0 {
				row.Speedup = pres.nsPerOp / sres.nsPerOp
			}
			report.Rows = append(report.Rows, row)
		}
	}

	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].Codec != report.Rows[j].Codec {
			return report.Rows[i].Codec < report.Rows[j].Codec
		}
		if report.Rows[i].Kernel != report.Rows[j].Kernel {
			return report.Rows[i].Kernel < report.Rows[j].Kernel
		}
		return blockPixelArea(report.Rows[i].Size) < blockPixelArea(report.Rows[j].Size)
	})

	out := os.Stdout
	if *flagOutput != "" {
		f, err := os.Create(*flagOutput)
		if err != nil {
			fail("create output: %v", err)
		}
		defer f.Close()
		out = f
	}

	switch *flagFormat {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fail("encode json: %v", err)
		}
	case "md":
		writeMarkdown(out, report)
	default:
		writeText(out, report)
	}
}

type benchResult struct {
	ops     int
	nsPerOp float64
}

// runBenchmarks executes `go test -bench=<pattern>` in the package's
// source directory and returns the parsed ns/op for each matched
// benchmark. scalar controls whether the `purego` build tag is set.
func runBenchmarks(pkg packageSpec, scalar bool) (map[string]benchResult, error) {
	args := []string{"test"}
	if scalar {
		args = append(args, "-tags=purego")
	}
	if *flagShort {
		args = append(args, "-short")
	}
	args = append(args,
		"-bench="+pkg.benchPattern,
		"-benchtime="+*flagBenchTime,
		"-count="+strconv.Itoa(*flagBenchCnt),
		"-run=^$",
		"-benchmem",
		pkg.importPath,
	)

	if *flagDryRun {
		fmt.Fprintln(os.Stderr, "+", "go", strings.Join(args, " "))
		return map[string]benchResult{}, nil
	}

	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(),
		"GOMAXPROCS="+strconv.Itoa(maxParallelism()),
	)
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	results := map[string]benchResult{}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if *flagVerbose {
			fmt.Fprintln(os.Stderr, line)
		}
		name, res, ok := parseBenchmarkLine(line)
		if !ok {
			continue
		}
		// If a bench was repeated via -count>1 we keep the fastest.
		if prior, exists := results[name]; exists && prior.nsPerOp <= res.nsPerOp {
			continue
		}
		results[name] = res
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go test failed: %v\nstderr: %s", err, stderr.String())
	}
	return results, nil
}

// benchLineRe matches `BenchmarkName-NN  OPS   NSPEROP ns/op ...`.
var benchLineRe = regexp.MustCompile(`^(Benchmark[\w/]+?)(?:-\d+)?\s+(\d+)\s+([\d.]+)\s+ns/op`)

func parseBenchmarkLine(line string) (string, benchResult, bool) {
	m := benchLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", benchResult{}, false
	}
	ops, _ := strconv.Atoi(m[2])
	ns, err := strconv.ParseFloat(m[3], 64)
	if err != nil {
		return "", benchResult{}, false
	}
	return m[1], benchResult{ops: ops, nsPerOp: ns}, true
}

// decomposeBenchName splits BenchmarkVP9Variance16x16 into ("Variance",
// "16x16"). VP8 names lack the VP9 prefix; we strip "Benchmark" only
// for them and let sub-kernels show through. For benchmarks that don't
// embed a WxH suffix, the size column is the whole tail after the
// kernel-recognised prefix.
var sizeTailRe = regexp.MustCompile(`(\d+x\d+(?:[A-Za-z][\w/]*)?)$`)

func decomposeBenchName(codec, name string) (kernel, size string) {
	trimmed := strings.TrimPrefix(name, "Benchmark")
	if codec == "VP9" {
		trimmed = strings.TrimPrefix(trimmed, "VP9")
	}
	if m := sizeTailRe.FindStringSubmatch(trimmed); m != nil {
		size = m[1]
		kernel = strings.TrimSuffix(trimmed, size)
		kernel = strings.TrimRight(kernel, "_")
		return kernel, size
	}
	// Trailing digits only (no x), e.g. "Sad16xNPtrFast" — try splitting at
	// the first digit.
	for i, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			return trimmed[:i], trimmed[i:]
		}
	}
	return trimmed, "-"
}

func blockPixelArea(size string) int {
	// "16x16" -> 256; non-conforming -> 0.
	idx := strings.IndexByte(size, 'x')
	if idx <= 0 || idx == len(size)-1 {
		return 0
	}
	w, errW := strconv.Atoi(size[:idx])
	h, errH := strconv.Atoi(size[idx+1:])
	if errW != nil || errH != nil {
		return 0
	}
	return w * h
}

func writeText(w *os.File, r matrixReport) {
	fmt.Fprintf(w, "govpx DSP per-routine matrix\n")
	fmt.Fprintf(w, "  goos=%s goarch=%s simd=%s benchtime=%s count=%d\n",
		r.Goos, r.Goarch, r.SIMDLabel, r.BenchTime, r.BenchCount)
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "codec\tkernel\tsize\tscalar ns/op\t%s ns/op\tspeedup\n", r.SIMDLabel)
	fmt.Fprintf(tw, "-----\t------\t----\t------------\t------------\t-------\n")
	for _, row := range r.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Codec, row.Kernel, row.Size,
			formatNS(row.ScalarNS), formatNS(row.SIMDNS),
			formatSpeedup(row.Speedup))
	}
	tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "generated %s\n", r.Generated)
}

func writeMarkdown(w *os.File, r matrixReport) {
	fmt.Fprintf(w, "# govpx DSP per-routine matrix\n\n")
	fmt.Fprintf(w, "`goos=%s goarch=%s simd=%s benchtime=%s count=%d generated=%s`\n\n",
		r.Goos, r.Goarch, r.SIMDLabel, r.BenchTime, r.BenchCount, r.Generated)

	current := ""
	for _, row := range r.Rows {
		if row.Codec != current {
			if current != "" {
				fmt.Fprintln(w)
			}
			current = row.Codec
			fmt.Fprintf(w, "## %s\n\n", row.Codec)
			fmt.Fprintf(w, "| kernel | size | scalar ns/op | %s ns/op | speedup |\n", r.SIMDLabel)
			fmt.Fprintln(w, "|--------|------|--------------|---------|---------|")
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
			row.Kernel, row.Size,
			formatNS(row.ScalarNS), formatNS(row.SIMDNS),
			formatSpeedup(row.Speedup))
	}
}

func formatNS(ns float64) string {
	if ns <= 0 {
		return "-"
	}
	if ns < 1000 {
		return fmt.Sprintf("%.1f", ns)
	}
	if ns < 1e6 {
		return fmt.Sprintf("%.1fk", ns/1e3)
	}
	return fmt.Sprintf("%.2fm", ns/1e6)
}

func formatSpeedup(s float64) string {
	if s <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2fx", s)
}

func pickSIMDLabel(arch string) string {
	switch arch {
	case "arm64":
		return "neon"
	case "amd64":
		return "sse2"
	default:
		return "simd"
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func goos() string {
	out, err := exec.Command("go", "env", "GOOS").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func goarch() string {
	out, err := exec.Command("go", "env", "GOARCH").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func maxParallelism() int {
	if v := os.Getenv("GOMAXPROCS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	// Default to 1 for repeatable bench numbers.
	return 1
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "govpx-dsp-matrix: "+format+"\n", args...)
	os.Exit(1)
}

// Compatibility shim — keeps filepath import alive if we add file output
// fallbacks later.
var _ = filepath.Join
