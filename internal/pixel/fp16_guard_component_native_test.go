//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

func fp16GuardParentProbe(coeff, iscan, qcoeff, dqcoeff []int16, classes []byte, n int) bool {
	type span struct{ p, bytes uintptr }
	spans := [5]span{
		{reflect.ValueOf(coeff).Pointer(), uintptr(n) * 2},
		{reflect.ValueOf(iscan).Pointer(), uintptr(n) * 2},
		{reflect.ValueOf(dqcoeff).Pointer(), uintptr(n) * 2},
	}
	count := 3
	if qcoeff != nil {
		spans[count] = span{reflect.ValueOf(qcoeff).Pointer(), uintptr(n) * 2}
		count++
	}
	if classes != nil {
		spans[count] = span{reflect.ValueOf(classes).Pointer(), uintptr(n)}
		count++
	}
	for i := 0; i < count; i++ {
		for j := 0; j < i; j++ {
			a, b := spans[i], spans[j]
			if a.p <= b.p && b.p-a.p < a.bytes || b.p < a.p && a.p-b.p < b.bytes {
				return false
			}
		}
	}
	return true
}

func fp16GuardSingleProbe(coeff, iscan, qcoeff, dqcoeff []int16, classes []byte, n int) bool {
	type span struct{ p, bytes uintptr }
	spans := [5]span{
		{reflect.ValueOf(coeff).Pointer(), uintptr(n) * 2},
		{reflect.ValueOf(iscan).Pointer(), uintptr(n) * 2},
		{reflect.ValueOf(dqcoeff).Pointer(), uintptr(n) * 2},
	}
	count := 3
	if qcoeff != nil {
		spans[count] = span{reflect.ValueOf(qcoeff).Pointer(), uintptr(n) * 2}
		count++
	}
	if classes != nil {
		spans[count] = span{reflect.ValueOf(classes).Pointer(), uintptr(n)}
		count++
	}
	for i := 0; i < count; i++ {
		for j := 0; j < i; j++ {
			a, b := spans[i], spans[j]
			if a.p-b.p+a.bytes-1 < a.bytes+b.bytes-1 {
				return false
			}
		}
	}
	return true
}

func fp16GuardUnrolledProbe(coeff, iscan, qcoeff, dqcoeff []int16, classes []byte, n int) bool {
	c := reflect.ValueOf(coeff).Pointer()
	i := reflect.ValueOf(iscan).Pointer()
	d := reflect.ValueOf(dqcoeff).Pointer()
	bytes := uintptr(n) * 2
	sameLimit := bytes + bytes - 1
	if i-c+bytes-1 < sameLimit || d-c+bytes-1 < sameLimit || d-i+bytes-1 < sameLimit {
		return false
	}
	if qcoeff != nil {
		q := reflect.ValueOf(qcoeff).Pointer()
		if q-c+bytes-1 < sameLimit || q-i+bytes-1 < sameLimit || q-d+bytes-1 < sameLimit {
			return false
		}
	}
	if classes != nil {
		cl := reflect.ValueOf(classes).Pointer()
		classBytes := uintptr(n)
		classLimit := classBytes + bytes - 1
		if cl-c+classBytes-1 < classLimit || cl-i+classBytes-1 < classLimit || cl-d+classBytes-1 < classLimit {
			return false
		}
		if qcoeff != nil && cl-reflect.ValueOf(qcoeff).Pointer()+classBytes-1 < classLimit {
			return false
		}
	}
	return true
}

// These exact helper bodies are frozen diagnostic candidates, not production dispatch.
var fp16GuardProbeSink bool

type fp16GuardProbeCase struct {
	name         string
	n            int
	optional     int
	pairA, pairB int
}

func fp16GuardProbeCases() []fp16GuardProbeCase {
	var cases []fp16GuardProbeCase
	for _, n := range []int{64, 256, 1024} {
		for optional, label := range []string{"full", "q-nil", "classes-nil", "both-nil"} {
			cases = append(cases, fp16GuardProbeCase{fmt.Sprintf("disjoint/n%d/%s", n, label), n, optional, -1, -1})
		}
	}
	names := []string{"coeff", "iscan", "qcoeff", "dqcoeff"}
	for a := 0; a < 4; a++ {
		for b := a + 1; b < 4; b++ {
			cases = append(cases, fp16GuardProbeCase{fmt.Sprintf("decline/n256/%s-%s", names[a], names[b]), 256, 0, a, b})
		}
	}
	return cases
}

type fp16GuardProbeInputs struct {
	backing                       []int16
	classBacking                  []byte
	coeff, iscan, qcoeff, dqcoeff []int16
	classes                       []byte
}

func fp16GuardProbeMake(c fp16GuardProbeCase) fp16GuardProbeInputs {
	span := c.n + 32
	backing := make([]int16, 4*span+64)
	for i := range backing {
		backing[i] = int16(i*37 + i/7 + 11)
	}
	classBacking := make([]byte, c.n+64)
	for i := range classBacking {
		classBacking[i] = byte(i*19 + 5)
	}
	offsets := [4]int{32, 32 + span, 32 + 2*span, 32 + 3*span}
	if c.pairA >= 0 {
		offsets[c.pairB] = offsets[c.pairA] + 1
	}
	view := func(off int) []int16 { return backing[off : off+c.n] }
	v := fp16GuardProbeInputs{backing, classBacking, view(offsets[0]), view(offsets[1]), view(offsets[2]), view(offsets[3]), classBacking[32 : 32+c.n : 32+c.n]}
	if c.optional&1 != 0 {
		v.qcoeff = nil
	}
	if c.optional&2 != 0 {
		v.classes = nil
	}
	return v
}

// Independent interval oracle uses ordered starts/ends, not candidate modular math.
func fp16GuardProbeOracle(v fp16GuardProbeInputs, n int) bool {
	type span struct{ start, end uintptr }
	windows := []span{}
	add := func(start, size uintptr) {
		end := start + size
		if end < start {
			panic("oracle span wrapped")
		}
		windows = append(windows, span{start, end})
	}
	add(reflect.ValueOf(v.coeff).Pointer(), uintptr(n)*2)
	add(reflect.ValueOf(v.iscan).Pointer(), uintptr(n)*2)
	add(reflect.ValueOf(v.dqcoeff).Pointer(), uintptr(n)*2)
	if v.qcoeff != nil {
		add(reflect.ValueOf(v.qcoeff).Pointer(), uintptr(n)*2)
	}
	if v.classes != nil {
		add(reflect.ValueOf(v.classes).Pointer(), uintptr(n))
	}
	for i := range windows {
		for j := 0; j < i; j++ {
			a, b := windows[i], windows[j]
			if a.start < b.end && b.start < a.end {
				return false
			}
		}
	}
	runtime.KeepAlive(v)
	return true
}
func TestFP16GuardComponentOracle(t *testing.T) {
	for _, c := range fp16GuardProbeCases() {
		t.Run(c.name, func(t *testing.T) {
			v := fp16GuardProbeMake(c)
			original := slices.Clone(v.backing)
			originalClasses := bytes.Clone(v.classBacking)
			want := fp16GuardProbeOracle(v, c.n)
			if want != (c.pairA < 0) {
				t.Fatal("fixture did not exercise declared policy")
			}
			if got := fp16GuardParentProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n); got != want {
				t.Fatal("parent", got, want)
			}
			if got := fp16GuardSingleProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n); got != want {
				t.Fatal("single", got, want)
			}
			if got := fp16GuardUnrolledProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n); got != want {
				t.Fatal("unrolled", got, want)
			}
			if got := fp16WindowsDisjoint(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n); got != want || got != fp16GuardUnrolledProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n) {
				t.Fatal("trial selected vs frozen unrolled/oracle mismatch")
			}
			if !slices.Equal(v.backing, original) || !bytes.Equal(v.classBacking, originalClasses) {
				t.Fatal("guard modified input backing")
			}
		})
	}
}

// Native Linux x64 only. Rosetta runs may execute the oracle test, never this timer.
// Every timed iteration calls one statically named guard; switch dispatch is outside b.Loop.
func BenchmarkFP16GuardComponent(b *testing.B) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || os.Getenv("GOVPX_REQUIRE_NATIVE_GUARD") != "1" {
		b.Skip("native Linux x64 diagnostic only")
	}
	for _, variant := range []string{"parent", "single", "unrolled"} {
		for _, c := range fp16GuardProbeCases() {
			b.Run(variant+"/"+c.name, func(b *testing.B) {
				v := fp16GuardProbeMake(c)
				original := slices.Clone(v.backing)
				originalClasses := bytes.Clone(v.classBacking)
				want := fp16GuardProbeOracle(v, c.n)
				var got bool
				// Preflight all candidate results before timing; all backing checks remain outside timer.
				if fp16GuardParentProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n) != want || fp16GuardSingleProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n) != want || fp16GuardUnrolledProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n) != want {
					b.Fatal("independent oracle mismatch")
				}
				if fp16WindowsDisjoint(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n) != want {
					b.Fatal("actual trial guard mismatch")
				}
				b.ReportAllocs()
				switch variant {
				case "parent":
					for b.Loop() {
						got = fp16GuardParentProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n)
					}
				case "single":
					for b.Loop() {
						got = fp16GuardSingleProbe(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n)
					}
				case "unrolled":
					for b.Loop() {
						got = fp16WindowsDisjoint(v.coeff, v.iscan, v.qcoeff, v.dqcoeff, v.classes, c.n)
					}
				default:
					b.Fatal("unknown variant")
				}
				fp16GuardProbeSink = got
				if got != want || !slices.Equal(v.backing, original) || !bytes.Equal(v.classBacking, originalClasses) {
					b.Fatal("result/input backing mismatch")
				}
				runtime.KeepAlive(v)
			})
		}
	}
}
