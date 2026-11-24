#!/bin/sh
# Fast, isolated native SSE8 feedback. The full native/C workflows remain the
# final codec/performance evidence; this lane has no timing pass/fail threshold.
set -eu
case "$(uname -s)/$(uname -m)" in
 Linux/x86_64) ;;
 *) printf '%s\n' 'Focused SSE8 timing requires native Linux/amd64.' >&2; exit 1 ;;
esac
driver_path=$0
case "$driver_path" in /*) ;; *) driver_path="$(pwd)/$driver_path" ;; esac
root=$(git rev-parse --show-toplevel)
cd "$root"
head_ref=${1:?provide the exact head commit or ref}
base_ref=${2:?provide the comparison base commit or ref}
output_dir=${3:?provide an output directory}
assembly_ref=${4:-691cd0512c4829d9ee40d483205dd223f9eb028d}
head_commit=$(git rev-parse --verify --end-of-options "$head_ref^{commit}")
base_commit=$(git rev-parse --verify --end-of-options "$base_ref^{commit}")
assembly_commit=$(git rev-parse --verify --end-of-options "$assembly_ref^{commit}")
mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
scratch=
phase=environment
finish() {
 status=$?
 trap - EXIT HUP INT TERM
 if [ -n "$scratch" ]; then rm -rf -- "$scratch" || status=1; fi
 printf '{"exit_status":%s,"phase":"%s"}\n' "$status" "$phase" > "$output_dir/status.json"
 python3 - "$output_dir" <<'PY' || status=1
from pathlib import Path
import hashlib,sys
root=Path(sys.argv[1])
files=sorted(p for p in root.rglob('*') if p.is_file() and p.name!='SHA256SUMS')
(root/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(root))+'\n' for p in files))
PY
 exit "$status"
}
trap finish EXIT
trap 'exit 130' HUP INT TERM
scratch=$(mktemp -d "${TMPDIR:-/tmp}/govpx-sse8-focused.XXXXXX")
export GOTOOLCHAIN=local GOEXPERIMENT=simd GOAMD64=v1 GOMAXPROCS=1 GOOS=linux GOARCH=amd64
version=$(scripts/go.sh env GOVERSION)
if [ "$version" != go1.27.4 ]; then
 printf 'Expected go1.27.4, got %s\n' "$version" >&2
 exit 1
fi
{
 printf 'head commit: %s\nbase commit: %s\nassembly commit: %s\n' "$head_commit" "$base_commit" "$assembly_commit"
 printf 'driver checkout commit: %s\n' "$(git rev-parse HEAD)"
 printf '%s\n' 'PGO=off GOMAXPROCS=1 GOAMD64=v1 rounds=6 benchtime=80ms'
 printf '%s\n' '36 live public B.Loop fixtures and 4 separate legacy B.N diagnostics'
 scripts/go.sh version
 scripts/go.sh env GOOS GOARCH GOAMD64 GOEXPERIMENT GOTOOLCHAIN GOCACHE GOFLAGS
 uname -a
 lscpu
 sha256sum "$driver_path" "$root/scripts/go.sh" "$root/.github/workflows/sse8-amd64-bench.yml" "$root/scripts/capture-sse8-codegen.py" "$root/scripts/capture-stats-codegen.py"
} > "$output_dir/environment.txt"
printf '{"head":"%s","base":"%s","assembly":"%s"}\n' "$head_commit" "$base_commit" "$assembly_commit" > "$output_dir/commits.json"

phase=archive
for variant in head base assembly; do
 case "$variant" in head) commit=$head_commit ;; base) commit=$base_commit ;; assembly) commit=$assembly_commit ;; esac
 source_dir="$scratch/$variant"
 mkdir -p "$source_dir"
 git archive "$commit" | tar -xf - -C "$source_dir"
 # Git blob identities and SHA256 of the exact archived source, before overlay.
 git ls-tree -r "$commit" -- go.mod go.sum internal/pixel internal/vp8/dsp > "$output_dir/$variant-git-tree.txt"
 git ls-tree -r --name-only "$commit" -- go.mod go.sum internal/pixel internal/vp8/dsp |
 while IFS= read -r path; do
  digest=$(sha256sum "$source_dir/$path")
  printf '%s  %s\n' "${digest%% *}" "$path"
 done > "$output_dir/$variant-source-sha256.txt"
done
fixture=internal/vp8/dsp/sse8_rows_native_test.go
[ -f "$scratch/head/$fixture" ]
sha256sum "$scratch/head/$fixture" > "$output_dir/fixture-sha256.txt"
cp "$scratch/head/$fixture" "$output_dir/sse8_rows_native_test.go"
for variant in base assembly; do
 # This is the only file overlaid on either archived production reference.
 cp "$scratch/head/$fixture" "$scratch/$variant/$fixture"
 cmp "$scratch/head/$fixture" "$scratch/$variant/$fixture"
done

# Add the same paired-DC fixtures without altering any original SSE8 timing.
dc_available=0
if [ -f "$scratch/head/internal/vp8/dsp/dc_pair_native_bench_test.go" ] && [ -f "$scratch/head/internal/vp8/dsp/dc_pair_inplace_native_bench_test.go" ]; then
 dc_available=1
 for variant in head base assembly; do
  for fixture in dc_pair_native_bench_test.go dc_pair_inplace_native_bench_test.go; do
   if [ "$variant" != head ]; then cp "$scratch/head/internal/vp8/dsp/$fixture" "$scratch/$variant/internal/vp8/dsp/$fixture"; fi
   cmp "$scratch/head/internal/vp8/dsp/$fixture" "$scratch/$variant/internal/vp8/dsp/$fixture"
  done
  if ! grep -q '^func DCOnlyIDCT4x4AddPairInPlaceInt32(' "$scratch/$variant/internal/vp8/dsp/idct.go"; then
   cp "$root/scripts/benchmark-fixtures/dc_pair_inplace_reference_shim.go.txt" "$scratch/$variant/internal/vp8/dsp/dc_pair_inplace_reference_shim_test.go"
   printf '%s shim=reference-only\n' "$variant" >> "$output_dir/dc-pair-fixtures.txt"
  else
   printf '%s shim=absent-original-API-retained\n' "$variant" >> "$output_dir/dc-pair-fixtures.txt"
  fi
 done
 for fixture in dc_pair_native_bench_test.go dc_pair_inplace_native_bench_test.go; do
  cp "$scratch/head/internal/vp8/dsp/$fixture" "$output_dir/$fixture"
  sha256sum "$scratch/head/internal/vp8/dsp/$fixture" >> "$output_dir/dc-pair-fixtures.txt"
 done
 cp "$root/scripts/benchmark-fixtures/dc_pair_inplace_reference_shim.go.txt" "$output_dir/dc-pair-reference-shim.go.txt"
 sha256sum "$root/scripts/benchmark-dc-pair-focused.sh" "$root/scripts/benchmark-fixtures/dc_pair_inplace_reference_shim.go.txt" >> "$output_dir/dc-pair-fixtures.txt"
fi

phase=dispatch-and-oracles
# AVX2 must be genuinely selected on this runner. The old assembly reference
# retains its own backend; copied public fixtures verify its actual outputs.
GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel -run '^TestBackendSelected$' -count=1 -v > "$output_dir/head-dispatch.txt" 2>&1
python3 - "$output_dir/head-dispatch.txt" <<'PY'
from pathlib import Path
import sys
assert '--- PASS: TestBackendSelected ' in Path(sys.argv[1]).read_text(), 'native dispatch test did not execute'
PY
GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel -run '^Test(SquaredError|SSE8)' -count=1 -v > "$output_dir/head-correctness.txt" 2>&1
GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSSE8' -count=1 -v > "$output_dir/head-race-checkptr2.txt" 2>&1
benchmarks='^Benchmark(SSE8RowsNative|SSE8x4|SSE8x8|SSE8x16|SSE8x8PtrFast)$'
for variant in head base assembly; do
 phase="build-$variant"
 scripts/go.sh -C "$scratch/$variant" test -mod=readonly -pgo=off -c ./internal/vp8/dsp -o "$scratch/$variant.test" > "$output_dir/$variant-build.txt" 2>&1
 sha256sum "$scratch/$variant.test" > "$output_dir/$variant-binary-sha256.txt"
 # One-shot oracle/canary validation before the measured rounds. Its timing
 # values are never included in the performance summary.
 GOVPX_REQUIRE_SIMD=1 "$scratch/$variant.test" -test.run '^$' -test.bench "$benchmarks" -test.benchtime=1x -test.cpu=1 -test.benchmem > "$output_dir/$variant-one-shot.txt" 2>&1
done

phase=timing
for round in 1 2 3 4 5 6; do
 case "$round" in
  1) order='assembly base head' ;;
  2) order='base head assembly' ;;
  3) order='head assembly base' ;;
  4) order='assembly head base' ;;
  5) order='head base assembly' ;;
  6) order='base assembly head' ;;
 esac
 printf 'round %s: %s\n' "$round" "$order" >> "$output_dir/orders.txt"
 for variant in $order; do
  printf 'round %s: %s\n' "$round" "$variant"
  GOVPX_REQUIRE_SIMD=1 "$scratch/$variant.test" -test.run '^$' -test.bench "$benchmarks" -test.benchtime=80ms -test.count=1 -test.cpu=1 -test.benchmem > "$output_dir/$variant-round-$round.txt" 2>&1
 done
done

# Preserve only relevant native code after every timed process has finished.
# Wrapper functions may be inlined/dead-stripped; keep actual B.Loop caller
# plus current helper or old lowercase/variance assembly symbols instead.
phase=codegen
for variant in head base assembly; do
 python3 "$root/scripts/capture-sse8-codegen.py" "$variant" "$scratch/$variant.test" "$output_dir"
done

phase=summary
python3 - "$output_dir" <<'PY'
from pathlib import Path
import collections,json,math,re,statistics,sys
root=Path(sys.argv[1])
expected={f'BenchmarkSSE8RowsNative/{api}/pitch{a}-{b}/pattern{p}' for api in ('slice8x4','slice8x8','slice8x16','pointer8x8') for a,b in ((8,8),(32,37),(1312,1320)) for p in range(3)}
legacy={'BenchmarkSSE8x4','BenchmarkSSE8x8','BenchmarkSSE8x16','BenchmarkSSE8x8PtrFast'}
all_cases=expected|legacy
orders=[line.split(': ',1)[1].split() for line in (root/'orders.txt').read_text().splitlines()]
assert len(orders)==6 and len({tuple(order) for order in orders})==6, 'all six order permutations required'
assert all(set(order)=={'head','base','assembly'} for order in orders), 'invalid variant order'
for position in range(3):
 assert collections.Counter(order[position] for order in orders)=={'head':2,'base':2,'assembly':2}, 'unbalanced variant positions'
pattern=re.compile(r'^(Benchmark\S+)\s+(\d+)\s+(?:([0-9.eE+-]+) ns/op\s+)?(?:[0-9.eE+-]+ MB/s\s+)?(\d+) B/op\s+(\d+) allocs/op$',re.M)
def parse(path, measured=True):
 text=path.read_text();samples={}
 for match in pattern.finditer(text):
  case,it,ns,byte,alloc=match.groups()
  assert case not in samples, f'duplicate {case}: {path}'
  sample={'iterations':int(it),'ns_per_op':float(ns) if ns is not None else None,'bytes_per_op':int(byte),'allocs_per_op':int(alloc)}
  assert sample['iterations']>0, f'invalid iteration count: {path} {case}'
  if measured:
   assert sample['ns_per_op'] is not None and math.isfinite(sample['ns_per_op']) and sample['ns_per_op']>0, f'invalid timed sample: {path} {case}'
  if measured or case in expected:
   assert sample['bytes_per_op']==sample['allocs_per_op']==0, f'allocation regression: {path} {case}'
  samples[case]=sample
 assert set(samples)==all_cases, f'case inventory mismatch in {path}: missing={all_cases-set(samples)}, extra={set(samples)-all_cases}'
 assert re.search(r'^PASS$',text,re.M), f'benchmark did not finish: {path}'
 return samples
# Check all one-shot outputs too; none of their timing fields enter medians.
for variant in ('head','base','assembly'):parse(root/f'{variant}-one-shot.txt', measured=False)
rounds={v:[parse(root/f'{v}-round-{r}.txt') for r in range(1,7)] for v in ('head','base','assembly')}
results=[]
for case in sorted(all_cases):
 values={v:[rnd[case]['ns_per_op'] for rnd in rounds[v]] for v in rounds}
 med={v:statistics.median(vals) for v,vals in values.items()}
 ratios=[b/h for b,h in zip(values['base'],values['head'])]
 results.append({'case':case,'protocol':'B.Loop public' if case in expected else 'B.N legacy diagnostic','samples':{v:[rnd[case] for rnd in rounds[v]] for v in rounds},'median_ns':med,'base_over_head':med['base']/med['head'],'assembly_over_head':med['assembly']/med['head'],'head_time_change_percent':(med['head']/med['base']-1)*100,'base_head_paired_ratios':ratios,'pairs_head_slower':sum(h>b for h,b in zip(values['head'],values['base'])),'median_ge_1_5':med['base']/med['head']>=1.5,'all_pairs_ge_1_5':all(r>=1.5 for r in ratios)})
summary={'commits':json.loads((root/'commits.json').read_text()),'scope':'native Linux/amd64 focused SSE8; no whole-codec claim','rounds':6,'round_orders':orders,'benchtime':'80ms','pgo':'off','public_cases':36,'legacy_diagnostic_cases':4,'measured_samples':720,'one_shot_samples_excluded':120,'legacy_one_shot_setup_allocations_are_not_timed_acceptance':True,'all_zero_alloc':True,'performance_ratios_are_informational':True,'results':results}
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
lines=['# Native amd64 SSE8 focused results','','Six balanced rounds; all 36 public B.Loop cases and four legacy B.N diagnostics.','Every sample is retained; timing ratios do not determine job success. No whole-codec claim.','',f"Head `{summary['commits']['head']}`; base `{summary['commits']['base']}`; old assembly `{summary['commits']['assembly']}`.",'']
for protocol in ('B.Loop public','B.N legacy diagnostic'):
 lines += [f'## {protocol}','','| Fixture | Head ns | Base ns | Old ASM ns | Base/head | Head time change | Pairs slower |','|---|---:|---:|---:|---:|---:|---:|']
 for result in results:
  if result['protocol']!=protocol:continue
  m=result['median_ns'];case=result['case'].removeprefix('BenchmarkSSE8RowsNative/').removeprefix('Benchmark')
  lines.append(f"| {case} | {m['head']:.4f} | {m['base']:.4f} | {m['assembly']:.4f} | {result['base_over_head']:.4f}× | {result['head_time_change_percent']:+.2f}% | {result['pairs_head_slower']}/6 |")
 lines.append('')
(root/'summary.md').write_text('\n'.join(lines)+'\n')
PY
# Keep the paired-DC component comparison separate from the original SSE8 data.
phase=dc-pair
if [ "$dc_available" = 1 ]; then
 {
  GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
  GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
  GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
  GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
 } > "$output_dir/dc-pair-validation.txt" 2>&1
 sh "$root/scripts/benchmark-dc-pair-focused.sh" "$scratch/assembly.test" "$scratch/base.test" "$scratch/head.test" "$output_dir/dc-pair"
else
 printf '# DCPAIR_UNAVAILABLE_SOURCE_COMPATIBILITY\n' > "$output_dir/dc-pair-fixtures.txt"
fi

# Separate current/base pixel API coverage AFTER every original SSE8 timing,
# code capture and summary. Old691 has no pixel package and receives no adapter.
phase=pixel-optional
fp_fixture=internal/pixel/fp32_optional_native_test.go
stats_fixture=internal/pixel/stats_reduction_native_test.go
printf '{"head":"%s","base":"%s","assembly_excluded":true}\n' "$head_commit" "$base_commit" > "$output_dir/pixel-optional-commits.json"
if [ -f "$scratch/head/$fp_fixture" ] && [ -f "$scratch/head/$stats_fixture" ] && [ -d "$scratch/base/internal/pixel" ]; then
 mkdir -p "$output_dir/pixel-optional-fixtures"
 for fixture in "$fp_fixture" "$stats_fixture"; do
  cp "$scratch/head/$fixture" "$scratch/base/$fixture"
  cmp "$scratch/head/$fixture" "$scratch/base/$fixture"
  cp "$scratch/head/$fixture" "$output_dir/pixel-optional-fixtures/$(basename "$fixture")"
  digest=$(sha256sum "$scratch/head/$fixture")
  printf '%s  %s\n' "${digest%% *}" "$fixture" >> "$output_dir/pixel-optional-fixture-sha256.txt"
 done
 # Selected ordinary/race and genuinely disabled fallback states execute on
 # this SAME runner. Only actual SDK AVX512 can enable selected32 assertions.
 {
  GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel -run '^Test(FP16|FP32|FPPacket|.*Stats)' -count=1 -v
  GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^Test(FP16|FP32|FPPacket|.*Stats)' -count=1 -v
  GODEBUG=cpu.avx512f=off GOVPX_REQUIRE_SIMD=1 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel -run '^Test(FP16|FP32|FPPacket|.*Stats)' -count=1 -v
  GODEBUG=cpu.avx512f=off GOVPX_REQUIRE_SIMD=1 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^Test(FP16|FP32|FPPacket|.*Stats)' -count=1 -v
  GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_FP16_AVX_OFF=1 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off ./internal/pixel -run '^Test(FP16|FP32|FPPacket|.*Stats)' -count=1 -v
  GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_FP16_AVX_OFF=1 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh -C "$scratch/head" test -mod=readonly -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^Test(FP16|FP32|FPPacket|.*Stats)' -count=1 -v
 } > "$output_dir/pixel-optional-validation.txt" 2>&1
 for variant in head base; do
  scripts/go.sh -C "$scratch/$variant" test -mod=readonly -pgo=off -c -o "$scratch/$variant-pixel.test" ./internal/pixel > "$output_dir/pixel-$variant-build.txt" 2>&1
 done
 # Reuse identical strict runtime feature gating and the repaired inventory
 # parser. Extra arguments bind source hashes to the archived head, not driver.
 sh "$root/scripts/benchmark-pixel-optional.sh" "$scratch/base-pixel.test" "$scratch/head-pixel.test" "$output_dir/pixel-optional.txt" "$scratch/head" "$head_commit" "$base_commit" "$scratch/base"
else
 # Preserve historical manual SSE8-only head/base comparisons. No pixel or
 # selected512 claim can be made when those older sources lack this API.
 printf '# OPTIONAL_PIXEL_UNAVAILABLE_SOURCE_COMPATIBILITY; no selected512 evidence\n' > "$output_dir/pixel-optional.txt"
fi

phase=complete
