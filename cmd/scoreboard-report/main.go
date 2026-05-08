// scoreboard-report runs the govpx scoreboard tests via `go test -json` and
// renders a clean per-test summary plus the reference baseline JSON for each
// scoreboard, instead of the raw verbose go-test output.
//
// Usage (typically invoked from the Makefile):
//
//	go run ./cmd/scoreboard-report -- ./... -run 'TestOracleX|...' -count=1 -timeout 10m
//
// All flag args after `--` are forwarded to `go test`; this tool injects
// `-json` and `-v`. Verbose go-test output is captured to .scoreboard.log
// next to the working directory; the structured summary is written to stdout.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type baselineSpec struct {
	test string
	path string
}

var baselines = []baselineSpec{
	{test: "TestOracleInterDecisionMatchRate", path: "testdata/mb_match_rate_baseline.json"},
	{test: "TestOracleSplitMVDecisionMatchRate", path: "testdata/splitmv_match_rate_baseline.json"},
	{test: "TestOracleEncoderQHistogramScoreboard", path: "testdata/q_histogram_baseline.json"},
	{test: "TestOracleEncoderTraceInterCandidateScoreboard", path: "testdata/realtime_candidate_scoreboard.json"},
	{test: "TestOracle128x128InterQDriftScoreboard", path: "testdata/qdrift_128_baseline.json"},
	{test: "TestOracleLoopFilterHeaderMatchRate", path: "testdata/loop_filter_match_rate_baseline.json"},
	{test: "TestOracleSecondPassAllocationCompare", path: "testdata/second_pass_alloc_baseline.json"},
	{test: "TestOracleImprovedMVScoreboard", path: "testdata/improved_mv_match_rate_baseline.json"},
	{test: "TestOracleCBRDropFrameScoreboard", path: "testdata/cbr_drop_scoreboard_baseline.json"},
	{test: "TestOracleCandidateRateScoreboard", path: "testdata/candidate_rate_scoreboard_baseline.json"},
}

type testEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

type testState struct {
	outputs []string
	passed  bool
	elapsed float64
	saw     bool // saw a pass/fail/skip terminal action
	skipped bool
}

const logFileName = ".scoreboard.log"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	testArgs := append([]string{"test", "-json", "-v"}, args...)

	logFile, err := os.Create(logFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scoreboard-report: create log: %v\n", err)
		os.Exit(2)
	}
	defer logFile.Close()

	cmd := exec.Command("go", testArgs...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scoreboard-report: pipe: %v\n", err)
		os.Exit(2)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "scoreboard-report: start: %v\n", err)
		os.Exit(2)
	}

	states := map[string]*testState{}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var ev testEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		// Mirror raw output to log file so users can drill into noise.
		if ev.Output != "" {
			_, _ = logFile.WriteString(ev.Output)
		}
		if ev.Test == "" {
			continue
		}
		// Track top-level scoreboard tests only (no '/').
		if strings.Contains(ev.Test, "/") {
			continue
		}
		st := states[ev.Test]
		if st == nil {
			st = &testState{}
			states[ev.Test] = st
		}
		switch ev.Action {
		case "output":
			st.outputs = append(st.outputs, ev.Output)
		case "pass":
			st.passed = true
			st.elapsed = ev.Elapsed
			st.saw = true
		case "fail":
			st.passed = false
			st.elapsed = ev.Elapsed
			st.saw = true
		case "skip":
			st.skipped = true
			st.elapsed = ev.Elapsed
			st.saw = true
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "scoreboard-report: scan: %v\n", err)
	}
	waitErr := cmd.Wait()

	// Render the report.
	names := make([]string, 0, len(states))
	for n, st := range states {
		if !st.saw {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	pass, fail, skip := 0, 0, 0
	for _, name := range names {
		st := states[name]
		switch {
		case st.skipped:
			skip++
		case st.passed:
			pass++
		default:
			fail++
		}
		printSection(name, st)
	}

	printBanner(pass, fail, skip, waitErr)

	if fail > 0 || (waitErr != nil && pass+fail+skip == 0) {
		os.Exit(1)
	}
}

func printSection(name string, st *testState) {
	bar := strings.Repeat("─", 72)
	status := "PASS"
	switch {
	case st.skipped:
		status = "SKIP"
	case !st.passed:
		status = "FAIL"
	}
	fmt.Println()
	fmt.Println(bar)
	fmt.Printf(" %-58s [%s] %6.2fs\n", name, status, st.elapsed)
	fmt.Println(bar)

	baselinePath := ""
	for _, b := range baselines {
		if b.test == name {
			baselinePath = b.path
			break
		}
	}

	if baselinePath != "" {
		raw, err := os.ReadFile(baselinePath)
		if err != nil {
			fmt.Printf(" gap vs libvpx: <baseline missing: %v>\n", err)
		} else {
			headline, table := formatGapReport(raw)
			fmt.Printf(" gap vs libvpx: %s\n", headline)
			fmt.Printf(" reference:     %s\n", baselinePath)
			if table != "" {
				fmt.Println()
				fmt.Println(indent(table, " "))
			}
		}
	} else {
		fmt.Println(" gap vs libvpx: (no baseline registered for this test)")
	}

	// Surface the test's own log body when there's no baseline (so the user
	// still sees what the test reported) or when the test failed (so the
	// regression message is right next to the gap).
	if baselinePath == "" || !st.passed {
		body := cleanBody(strings.Join(st.outputs, ""))
		if body != "" {
			fmt.Println()
			fmt.Println(" test output:")
			fmt.Println(indent(body, "   "))
		}
	}
}

func printBanner(pass, fail, skip int, waitErr error) {
	bar := strings.Repeat("═", 72)
	fmt.Println()
	fmt.Println(bar)
	total := pass + fail + skip
	if total == 0 {
		fmt.Println(" SCOREBOARD: NO TESTS RAN")
	} else if fail == 0 {
		fmt.Printf(" SCOREBOARD: PASS  %d/%d", pass, total)
		if skip > 0 {
			fmt.Printf("  (%d skipped)", skip)
		}
		fmt.Println()
	} else {
		fmt.Printf(" SCOREBOARD: FAIL  %d pass, %d fail", pass, fail)
		if skip > 0 {
			fmt.Printf(", %d skipped", skip)
		}
		fmt.Println()
	}
	if waitErr != nil && total > 0 && fail == 0 {
		fmt.Printf(" go test exited %v (build/setup error?)\n", waitErr)
	}
	wd, _ := os.Getwd()
	fmt.Printf(" full log: %s\n", filepath.Join(wd, logFileName))
	fmt.Println(bar)
}

var (
	logfPrefix     = regexp.MustCompile(`^    [^ \t][^ \t]*\.go:[0-9]+:[ \t]?`)
	frameworkLine  = regexp.MustCompile(`^=== (RUN|PAUSE|CONT|NAME|UPDATE)\b|^--- (PASS|FAIL|SKIP):|^(PASS|FAIL)$|^(ok|FAIL|\?|---)[ \t]+\S`)
	subtestLogfPfx = regexp.MustCompile(`^        [^ \t][^ \t]*\.go:[0-9]+:`)
)

// cleanBody strips Go test framework chatter and the "    file.go:NN:" prefix
// on Logf lines, dedents continuation lines, and drops Logf bodies that
// originated from sub-tests (kept only via the higher-indented prefix).
func cleanBody(s string) string {
	var out strings.Builder
	lines := strings.Split(s, "\n")
	dropContinuation := false
	for _, line := range lines {
		if frameworkLine.MatchString(line) {
			dropContinuation = false
			continue
		}
		if subtestLogfPfx.MatchString(line) {
			// Subtest Logf — drop the first line and following continuations
			// until we hit framework or a top-level Logf.
			dropContinuation = true
			continue
		}
		if logfPrefix.MatchString(line) {
			dropContinuation = false
			line = logfPrefix.ReplaceAllString(line, "")
			if strings.HasPrefix(line, "wrote baseline ") {
				continue
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		// Continuation: dedent up to 8 spaces (subtest) / 4 (top-level).
		if strings.HasPrefix(line, "        ") {
			if dropContinuation {
				continue
			}
			line = line[8:]
		} else if strings.HasPrefix(line, "    ") {
			line = line[4:]
		} else if line == "" {
			out.WriteByte('\n')
			continue
		} else {
			// Random other line — keep as-is.
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}

func indent(s, prefix string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// fieldKind classifies a baseline JSON field by how its value relates to the
// libvpx oracle, so the report can present each consistently as a "gap".
type fieldKind int

const (
	// kindReferenceValue: a govpx-only descriptive value (e.g. q_mean) that
	// isn't itself a comparison against libvpx. Rendered raw, alongside gaps.
	kindReferenceValue fieldKind = iota
	// kindGapPP: a *_match_pct field where 100 means perfect parity. We invert
	// it to a deficit measured in percentage points (0pp = perfect).
	kindGapPP
	// kindGapRaw: the value already encodes a delta from libvpx (e.g. an L1
	// histogram distance, a quantizer delta, a "divergent_rows" count). 0
	// means perfect parity.
	kindGapRaw
)

// classify decides what semantic role a baseline field plays.
func classify(key string) fieldKind {
	switch {
	case strings.HasSuffix(key, "_match_pct"):
		return kindGapPP
	case strings.Contains(key, "_to_libvpx"),
		strings.HasSuffix(key, "_delta"),
		strings.HasSuffix(key, "_delta_pct"),
		strings.HasSuffix(key, "_drift"),
		strings.HasSuffix(key, "_diff"),
		strings.HasSuffix(key, "_l1"),
		key == "divergent_rows":
		return kindGapRaw
	default:
		return kindReferenceValue
	}
}

// formatGapReport renders a baseline JSON as a gap table plus a one-line
// headline that names the worst observed gap against the libvpx oracle.
func formatGapReport(raw []byte) (headline, table string) {
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "(unparseable baseline)", strings.TrimSpace(string(raw))
	}
	if fxRaw, ok := generic["fixtures"]; ok {
		if fx, ok := fxRaw.(map[string]any); ok {
			return renderFixtureGap(fx)
		}
	}
	return renderFlatGap(generic)
}

type colSpec struct {
	key   string // raw JSON key
	label string // display label
	kind  fieldKind
}

func columnSpecs(fx map[string]any) []colSpec {
	keys := unionColumns(fx)
	specs := make([]colSpec, 0, len(keys))
	for _, k := range keys {
		c := colSpec{key: k, label: k, kind: classify(k)}
		if c.kind == kindGapPP {
			c.label = strings.TrimSuffix(k, "_match_pct")
		}
		specs = append(specs, c)
	}
	rank := func(k fieldKind) int {
		switch k {
		case kindGapPP:
			return 0
		case kindGapRaw:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(specs, func(i, j int) bool {
		if rank(specs[i].kind) != rank(specs[j].kind) {
			return rank(specs[i].kind) < rank(specs[j].kind)
		}
		return specs[i].key < specs[j].key
	})
	return specs
}

func renderFixtureGap(fx map[string]any) (headline, table string) {
	specs := columnSpecs(fx)
	if len(specs) == 0 {
		return "(empty baseline)", ""
	}
	names := make([]string, 0, len(fx))
	for n := range fx {
		names = append(names, n)
	}
	sort.Strings(names)

	type worst struct {
		val     float64
		fixture string
		col     string
	}
	var worstPP, worstRaw worst
	var sawPP, sawRaw bool

	headers := []string{"fixture"}
	for _, s := range specs {
		headers = append(headers, s.label)
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	rows := make([][]string, 0, len(names))
	for _, n := range names {
		row := []string{n}
		entry, _ := fx[n].(map[string]any)
		for _, s := range specs {
			cell := gapCell(s, entry[s.key])
			row = append(row, cell)
			// Track worst gaps for headline.
			if x, ok := entry[s.key].(float64); ok {
				switch s.kind {
				case kindGapPP:
					gap := 100 - x
					if !sawPP || gap > worstPP.val {
						worstPP = worst{val: gap, fixture: n, col: s.label}
						sawPP = true
					}
				case kindGapRaw:
					if !sawRaw || abs(x) > abs(worstRaw.val) {
						worstRaw = worst{val: x, fixture: n, col: s.key}
						sawRaw = true
					}
				}
			}
		}
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
		rows = append(rows, row)
	}

	var out strings.Builder
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				out.WriteString("  ")
			}
			out.WriteString(padRight(cell, widths[i]))
		}
		out.WriteByte('\n')
	}
	writeRow(headers)
	sep := make([]string, len(headers))
	for i := range sep {
		sep[i] = strings.Repeat("─", widths[i])
	}
	writeRow(sep)
	for _, row := range rows {
		writeRow(row)
	}
	table = strings.TrimRight(out.String(), "\n")

	headline = gapHeadline(sawPP, worstPP, sawRaw, worstRaw)
	return headline, table
}

func renderFlatGap(m map[string]any) (headline, table string) {
	type entry struct {
		spec  colSpec
		value any
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]entry, 0, len(keys))
	specs := make(map[string]colSpec, len(keys))
	for _, k := range keys {
		s := colSpec{key: k, label: k, kind: classify(k)}
		if s.kind == kindGapPP {
			s.label = strings.TrimSuffix(k, "_match_pct")
		}
		specs[k] = s
		entries = append(entries, entry{spec: s, value: m[k]})
	}
	// Sort: gaps first, then references.
	rank := func(k fieldKind) int {
		switch k {
		case kindGapPP:
			return 0
		case kindGapRaw:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if rank(entries[i].spec.kind) != rank(entries[j].spec.kind) {
			return rank(entries[i].spec.kind) < rank(entries[j].spec.kind)
		}
		return entries[i].spec.key < entries[j].spec.key
	})

	var maxK int
	for _, e := range entries {
		if len(e.spec.label) > maxK {
			maxK = len(e.spec.label)
		}
	}
	type worst struct {
		val float64
		col string
	}
	var worstPP, worstRaw worst
	var sawPP, sawRaw bool

	var out strings.Builder
	for _, e := range entries {
		cell := gapCell(e.spec, e.value)
		fmt.Fprintf(&out, "%s = %s\n", padRight(e.spec.label, maxK), cell)
		if x, ok := e.value.(float64); ok {
			switch e.spec.kind {
			case kindGapPP:
				gap := 100 - x
				if !sawPP || gap > worstPP.val {
					worstPP = worst{val: gap, col: e.spec.label}
					sawPP = true
				}
			case kindGapRaw:
				if !sawRaw || abs(x) > abs(worstRaw.val) {
					worstRaw = worst{val: x, col: e.spec.key}
					sawRaw = true
				}
			}
		}
	}
	table = strings.TrimRight(out.String(), "\n")

	type fxWorst struct {
		val     float64
		fixture string
		col     string
	}
	pp := fxWorst{val: worstPP.val, col: worstPP.col}
	rw := fxWorst{val: worstRaw.val, col: worstRaw.col}
	headline = gapHeadline(sawPP, pp, sawRaw, rw)
	return headline, table
}

// gapCell formats a single cell in the gap report, converting *_match_pct
// values to a percentage-point deficit.
func gapCell(s colSpec, v any) string {
	if v == nil {
		return ""
	}
	switch s.kind {
	case kindGapPP:
		x, ok := v.(float64)
		if !ok {
			return formatVal(s.key, v)
		}
		return trimFloat(100-x) + "pp"
	default:
		return formatVal(s.key, v)
	}
}

// gapHeadline builds the one-line summary printed above the gap table.
type gapWorst = struct {
	val     float64
	fixture string
	col     string
}

func gapHeadline(sawPP bool, pp gapWorst, sawRaw bool, raw gapWorst) string {
	switch {
	case sawPP && pp.val > 0:
		s := fmt.Sprintf("max %spp deficit on %s", trimFloat(pp.val), pp.col)
		if pp.fixture != "" {
			s += " (" + pp.fixture + ")"
		}
		if sawRaw && raw.val != 0 {
			extra := fmt.Sprintf(", %s=%s", raw.col, trimFloat(raw.val))
			if raw.fixture != "" && raw.fixture != pp.fixture {
				extra += " (" + raw.fixture + ")"
			}
			s += extra
		}
		return s
	case sawRaw && raw.val != 0:
		s := fmt.Sprintf("%s=%s", raw.col, trimFloat(raw.val))
		if raw.fixture != "" {
			s += " (" + raw.fixture + ")"
		}
		return s
	case sawPP || sawRaw:
		return "PERFECT (matches libvpx exactly)"
	default:
		return "(no comparable metrics in baseline)"
	}
}

func trimFloat(x float64) string {
	if x == float64(int64(x)) {
		return strconv.FormatInt(int64(x), 10)
	}
	s := strconv.FormatFloat(x, 'f', 4, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// unionColumns collects the set of metric keys across all fixtures, sorted
// with a stable preference for "_pct" metrics first then alphabetical.
func unionColumns(fx map[string]any) []string {
	seen := map[string]bool{}
	for _, v := range fx {
		entry, _ := v.(map[string]any)
		for k := range entry {
			seen[k] = true
		}
	}
	cols := make([]string, 0, len(seen))
	for k := range seen {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	return cols
}

func formatVal(key string, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case float64:
		var s string
		if x == float64(int64(x)) {
			s = strconv.FormatInt(int64(x), 10)
		} else {
			s = strconv.FormatFloat(x, 'f', 4, 64)
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		}
		if strings.HasSuffix(key, "_pct") {
			return s + "%"
		}
		return s
	case string:
		return x
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
