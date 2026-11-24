#!/usr/bin/env python3
"""Native two-reference FP zero-packet comparison; no speed threshold is a CI gate."""
from pathlib import Path
import argparse
import datetime
import hashlib
import io
import itertools
import json
import os
import platform
import re
import shutil
import statistics
import subprocess
import tarfile
import tempfile


QUANT = "internal/pixel/quantize_fp_amd64.go"
PRODUCTION = ['internal/pixel/quantize_fp_amd64.go', 'internal/pixel/quantize_fp32_avx512_amd64.go', 'internal/pixel/quantize_fp_simd.go']
EXPECTED_PARENT = {'internal/pixel/quantize_fp_amd64.go': '6402500232ee21792b751cad72aa24417e695f4bc0b49749308b839d93b1efdf', 'internal/pixel/quantize_fp32_avx512_amd64.go': '37f4343e66f4caf0a2fcb6e0c89a8c6e20950490baae1de117480ac33fb2e66f', 'internal/pixel/quantize_fp_simd.go': 'd5b14497b78cd193131f51663550ccad08e4f5230c906964aac72e6c10092dce'}
EXPECTED_CURRENT = {'internal/pixel/quantize_fp_amd64.go': '2bae132397f14f233f4f1bed27ff5b8950ae2526eb43e7863c4b0b40f2908806', 'internal/pixel/quantize_fp32_avx512_amd64.go': 'd487f136dbc121772b77abf7da0c49630b54acbc40ca355577f1cbb5e1b6e96b', 'internal/pixel/quantize_fp_simd.go': 'a7cd225853280e50303cd03d92d114336ba58286c496f68fa49d30718dbcfcfd'}
OVERLAYS = ("internal/pixel/quantize_fp_packets_test.go", "internal/pixel/fp32_optional_arm_native_test.go")
FIXTURES = ("internal/pixel/fp32_optional_native_test.go",
            "internal/vp9/encoder/quant_fp16_native_test.go")
IGNORED = {"ns_per_frame", "encode_fps", "macroblocks_per_second",
           "latency_ns", "allocs_per_frame"}
TRACE_SOURCE = '//go:build goexperiment.simd && amd64 && !purego\n\npackage pixel\n\nimport (\n "encoding/json"\n "fmt"\n "os"\n "sync/atomic"\n)\n\n// Diagnostic only: injected after all uninstrumented timers and profiles.\nvar fpPacketTraceCounts [3][2][1026][2]atomic.Uint64\n\nfunc fpPacketTraceObserve(coeff []int16, start, n, width int, r, d [2]int16, classes bool) {\n w := 0\n if width == 16 { w = 1 } else if width == 32 { w = 2 }\n cl := 0\n if classes { cl = 1 }\n bucket := n\n if bucket < 0 || bucket > 1024 { bucket = 1025 }\n zero := true\n for i := start; i < start+width; i++ {\n  k := 1\n  if i == 0 { k = 0 }\n  a := int(coeff[i])\n  if a < 0 { a = -a }\n  a += int(r[k])\n  if a > 32767 { a = 32767 }\n  if a >= int(d[k]) { zero = false }\n }\n fpPacketTraceCounts[w][cl][bucket][0].Add(1)\n if zero { fpPacketTraceCounts[w][cl][bucket][1].Add(1) }\n}\n\nfunc FPPacketTraceWrite() {\n result := make(map[string]map[string]uint64)\n for w, width := range [...]int{8,16,32} {\n  for cl := range 2 {\n   for n := range fpPacketTraceCounts[w][cl] {\n    count := fpPacketTraceCounts[w][cl][n][0].Load()\n    if count != 0 {\n     result[fmt.Sprintf("n%d/w%d/classes%t",n,width,cl!=0)] = map[string]uint64{"packets":count,"zero_packets":fpPacketTraceCounts[w][cl][n][1].Load()}\n    }\n   }\n  }\n }\n data, err := json.MarshalIndent(result,"","  ")\n if err != nil { panic(err) }\n if err = os.WriteFile(os.Getenv("GOVPX_FP_PACKETS_TRACE"),append(data,\'\\n\'),0600); err != nil { panic(err) }\n}\n'


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def expected_names(kind):
    if kind == "pixel":
        return {f"BenchmarkFP32PublicOptionalNative/n{n}/qnil{q}/classes{c}/{p}/{a}"
                for n, q, c, p, a in itertools.product(
                    (64, 256), ("false", "true"), ("false", "true"),
                    ("zero", "sparse", "dense"), ("aligned64", "offset2-classes1"))}
    return {f"BenchmarkVP9FP16Native/n{n}/{c}/{p}/{a}"
            for n, c, p, a in itertools.product(
                (16, 64, 256), ("no-classes", "classes"),
                ("zero", "sparse", "dense"), ("aligned32", "offset2-classes1"))}


def parse_bench(text, kind):
    result = {}
    for line in text.splitlines():
        if not line.startswith("Benchmark"):
            continue
        m = re.fullmatch(r"(Benchmark\S+)\s+\d+\s+([\d.eE+\-]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op\s*", line)
        assert m, ("unparsed benchmark", line)
        name, ns, byte_count, allocs = m.groups()
        assert name not in result and float(ns) > 0
        assert int(byte_count) == 0 and int(allocs) == 0, line
        result[name] = float(ns)
    assert set(result) == expected_names(kind), (kind, sorted(set(result) ^ expected_names(kind)))
    return result


def semantic(record):
    answer = {k: v for k, v in record.items() if k not in IGNORED}
    assert len(answer) == 22, sorted(answer)
    return answer


def instrument(source):
    for rel in PRODUCTION[:2]:
        path = source / rel
        text = path.read_text()
        lines = text.splitlines(keepends=True)
        modified = []
        hits = 0
        for line in lines:
            if "a := c.Abs().ToBits().Add(" in line:
                indent = line[:len(line)-len(line.lstrip())]
                width = 32 if "fp32" in rel else (8 if len(indent) == 2 else 16)
                modified.append(indent + f"fpPacketTraceObserve(coeff, i, n, {width}, r, d, classes != nil)\n")
                hits += 1
            modified.append(line)
        assert hits == (2 if "fp32" in rel else 3), (rel, hits)
        path.write_text("".join(modified))
    (source / "internal/pixel/fp_packets_trace_diagnostic_amd64.go").write_text(TRACE_SOURCE)
    main = source / "cmd/govpx-bench/main.go"
    text = main.read_text()
    marker = 'import "github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"'
    assert text.count(marker) == 1 and text.count("\tbenchcmd.Main()") == 1
    text = text.replace(marker, 'import ("github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"; "github.com/thesyncim/govpx/internal/pixel")')
    main.write_text(text.replace("\tbenchcmd.Main()", "\tbenchcmd.Main()\n\tpixel.FPPacketTraceWrite()"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("head_ref")
    parser.add_argument("base_ref")
    parser.add_argument("output_dir")
    parser.add_argument("--profile-passes", type=int, default=24)
    args = parser.parse_args()
    assert platform.system() == "Linux" and platform.machine() == "x86_64", "native Linux/x64 required; Rosetta is correctness-only"
    assert os.environ.get("GOVPX_NATIVE_FP_PACKETS_BENCH") == "1", "explicit native timing opt-in required"
    assert 1 <= args.profile_passes <= 48
    root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip())
    out = Path(args.output_dir).resolve()
    out.mkdir(parents=True, exist_ok=True)
    assert not list(out.iterdir()), "output must be empty; preserve earlier evidence"
    wrapper = str(root / "scripts/go.sh")
    env = os.environ.copy()
    env.update(GOTOOLCHAIN="local", GOEXPERIMENT="simd", GOOS="linux", GOARCH="amd64",
               GOAMD64="v1", GOMAXPROCS="1", CGO_ENABLED="1", GOFLAGS="-p=2", GODEBUG="",
               GOVPX_REQUIRE_SIMD="1", GOVPX_REQUIRE_FP32_NATIVE="0", GOVPX_REQUIRE_FP32_OFF="0")
    commands = []
    phase = "environment"

    def run(argv, filename, extra=None, raw=False):
        target = out / filename
        target.parent.mkdir(parents=True, exist_ok=True)
        rec = {"argv": [str(x) for x in argv], "start_utc": now(), "output": filename,
               "environment_overrides": extra or {}}
        with target.open("wb") as stdout, target.with_suffix(target.suffix + ".stderr").open("wb") as stderr:
            proc = subprocess.run(rec["argv"], cwd=root, env=env | (extra or {}), stdout=stdout, stderr=stderr)
        rec.update(end_utc=now(), exit_code=proc.returncode)
        commands.append(rec)
        (out / "commands.json").write_text(json.dumps(commands, indent=2) + "\n")
        assert proc.returncode == 0, (argv, target)
        return target.read_bytes() if raw else target.read_text()

    def go(source, argv, filename, extra=None):
        return run([wrapper, "-C", source, *argv], filename, extra)

    try:
        run(["uname", "-a"], "uname.txt")
        run(["lscpu"], "lscpu.txt")
        version = run([wrapper, "env", "GOVERSION"], "goversion.txt").strip()
        assert version == "go1.27.4", version
        cache = run([wrapper, "env", "GOCACHE"], "gocache.txt").strip()
        assert ".codex/worktrees" not in cache
        assert run(["objdump", "--version"], "objdump-version.txt").startswith("GNU objdump")
        commits = {v: run(["git", "rev-parse", "--verify", "--end-of-options", ref + "^{commit}"], v + "-commit.txt").strip()
                   for v, ref in (("parent", args.base_ref), ("current", args.head_ref))}
        assert all(re.fullmatch(r"[0-9a-f]{40}", x) for x in commits.values())
        with tempfile.TemporaryDirectory(prefix="govpx-fp-packets-") as temporary:
            scratch = Path(temporary)
            sources = {v: scratch / v for v in commits}
            for v, source in sources.items():
                source.mkdir()
                archive = subprocess.check_output(["git", "archive", commits[v]], cwd=root)
                with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                    assert all(not Path(m.name).is_absolute() and ".." not in Path(m.name).parts for m in tar.getmembers())
                    tar.extractall(source)
            for v, expected in (("parent", EXPECTED_PARENT), ("current", EXPECTED_CURRENT)):
                assert {rel: sha(sources[v] / rel) for rel in PRODUCTION} == expected, "unexpected production source"
            for rel in OVERLAYS:
                shutil.copy2(sources["current"] / rel, sources["parent"] / rel)
            assert all("if archsimd.X86.AVX512() && n >= 256 && n&31 == 0 {" in (source / QUANT).read_text() for source in sources.values()), "guard experiment must stay separate"
            for fixture in FIXTURES:
                assert (sources["parent"] / fixture).read_bytes() == (sources["current"] / fixture).read_bytes()
            bindings = {"commits": commits, "PGO": "off", "CGO_ENABLED": 1, "GOMAXPROCS": 1,
                        "driver_sha256": sha(__file__), "variants": {}}
            bins = {}
            phase = "build-and-correctness"
            for v, source in sources.items():
                print("building", v, now(), flush=True)
                bins[v] = {kind: scratch / (v + "-" + kind) for kind in ("pixel", "encoder", "whole")}
                for kind, package in (("pixel", "./internal/pixel"), ("encoder", "./internal/vp9/encoder")):
                    go(source, ["test", "-pgo=off", "-c", "-o", bins[v][kind], package], f"build/{v}-{kind}.txt")
                go(source, ["build", "-pgo=off", "-o", bins[v]["whole"], "./cmd/govpx-bench"], f"build/{v}-whole.txt")
                # Only the two amd64 packet bodies may differ in the selected Linux production build.
                listing = go(source, ["list", "-deps", "-json", "./cmd/govpx-bench"], f"build/{v}-deps.json")
                decoder = json.JSONDecoder(); used = {}; rest = listing
                while rest.strip():
                    obj, count = decoder.raw_decode(rest.lstrip()); rest = rest.lstrip()[count:]
                    if obj.get("Module", {}).get("Path") != "github.com/thesyncim/govpx": continue
                    for name in obj.get("GoFiles", []) + obj.get("CgoFiles", []):
                        f = Path(obj["Dir"]) / name
                        used[str(f.relative_to(source))] = sha(f)
                all_go = {str(f.relative_to(source)): sha(f) for f in sorted(source.rglob("*.go"))}
                bindings["variants"][v] = {"all_go_sha256": all_go, "compiled_production_go_sha256": used,
                    "binaries": {k: {"path": str(f), "sha256": sha(f)} for k, f in bins[v].items()}}
                for rel in (*PRODUCTION, *FIXTURES, *OVERLAYS):
                    dest = out / "sources" / v / (rel + ".txt"); dest.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(source / rel, dest)
            a, b = (bindings["variants"][v]["compiled_production_go_sha256"] for v in ("parent", "current"))
            assert a.keys() == b.keys() and set(k for k in a if a[k] != b[k]) == set(PRODUCTION[:2]), "other compiled production source changed"
            all_a, all_b = (bindings["variants"][v]["all_go_sha256"] for v in ("parent", "current"))
            assert all_a.keys() == all_b.keys() and set(k for k in all_a if all_a[k] != all_b[k]) == set(PRODUCTION), "other Go source changed"
            probes = {}
            for v in sources:
                text = run([bins[v]["pixel"], "-test.run=^Test(FP32PublicHost|FP32ActualFeature|FP16Selected)$", "-test.count=1", "-test.v"], f"correctness/{v}-host.txt")
                match = re.search(r"FP32PublicHost AVX512=(true|false) AVX2=(true|false) Enabled=(true|false)", text)
                assert match and match.group(2, 3) == ("true", "true"), "actual AVX2 SIMD required"
                probes[v] = match.groups()
            assert probes["parent"] == probes["current"]
            wide = probes["current"][0] == "true"
            bindings["actual_SDK_features"] = {"AVX512": wide, "AVX2": True, "Enabled": True}
            bindings["selected_AVX512_validation"] = "required" if wide else "unavailable; actual AVX2 path only"
            (out / "binding.json").write_text(json.dumps(bindings, indent=2) + "\n")
            for v, source in sources.items():
                print("correctness", v, now(), flush=True)
                selected = {"GOVPX_REQUIRE_FP32_NATIVE": "1" if wide else "0"}
                for race in (False, True):
                    flags = ["-race", "-gcflags=all=-d=checkptr=2"] if race else []
                    evidence = go(source, ["test", "-pgo=off", *flags, "./internal/pixel", "./internal/vp9/encoder", "-run=^Test(FP16|FP32|FPPacket|QuantizeFP)", "-count=1", "-v"], f"correctness/{v}-selected-race{race}.txt", selected)
                    for marker in ("numeric cases=7568", "alias full-backing cases=450", "pre-write panic cases=30", "full-backing cases=60 plus literal"):
                        assert marker in evidence, ("missing required packet oracle", marker, v, race)
                for mode, extra in (("512-off", {"GODEBUG": "cpu.avx512f=off", "GOVPX_REQUIRE_FP32_OFF": "1"}),
                                    ("avx-off", {"GODEBUG": "cpu.avx=off,cpu.avx2=off,cpu.avx512f=off", "GOVPX_REQUIRE_SIMD": "0", "GOVPX_REQUIRE_FP16_AVX_OFF": "1", "GOVPX_REQUIRE_FP32_OFF": "1"})):
                    run([bins[v]["pixel"], "-test.run=^Test(FP16|FP32|FPPacket)", "-test.count=1", "-test.v"], f"correctness/{v}-{mode}.txt", extra)
                for kind, bench in (("pixel", "BenchmarkFP32PublicOptionalNative"), ("encoder", "BenchmarkVP9FP16Native")):
                    text = run([bins[v][kind], "-test.run=^$", "-test.bench=^" + bench + "$", "-test.benchtime=1x", "-test.cpu=1", "-test.count=1", "-test.benchmem"], f"preflight/{v}-{kind}.txt")
                    parse_bench(text, kind)
            phase = "native-timing"
            components = {}; whole = {case: {v: [] for v in sources} for case in ("vp9-320", "vp9-720")}
            whole_args = ["-codec=vp9", "-width=320", "-height=180", "-frames=120", "-fps=30", "-bitrate=500", "-mode=realtime", "-cpu-used=8", "-threads=1", "-encode-only", "-auto-libvpx=false", "-format=json"]
            workloads = {"vp9-320": whole_args, "vp9-720": ["-codec=vp9", "-width=1280", "-height=720", "-frames=60", "-fps=30", "-bitrate=2500", "-mode=realtime", "-cpu-used=8", "-threads=1", "-encode-only", "-auto-libvpx=false", "-format=json"]}
            orders = [["parent", "current"], ["current", "parent"]] * 3
            for number, order in enumerate(orders, 1):
                for v in order:
                    for kind, bench in (("pixel", "BenchmarkFP32PublicOptionalNative"), ("encoder", "BenchmarkVP9FP16Native")):
                        text = run([bins[v][kind], "-test.run=^$", "-test.bench=^" + bench + "$", "-test.benchtime=100ms", "-test.cpu=1", "-test.count=1", "-test.benchmem"], f"timed/r{number}-{v}-{kind}.txt")
                        components[number, v, kind] = parse_bench(text, kind)
                    for case, workload in workloads.items():
                        whole[case][v].append(json.loads(run([bins[v]["whole"], *workload], f"timed/r{number}-{v}-{case}.json")))
                print("native pair", number, "PASS", now(), flush=True)
            reference = {case: semantic(variants["parent"][0]) for case, variants in whole.items()}
            assert all(semantic(x) == reference[case] for case, variants in whole.items() for xs in variants.values() for x in xs)
            summary = {"orders": orders, "component_rows": 1008, "excluded_preflight_rows": 168,
                       "actual_SDK_features": bindings["actual_SDK_features"], "components": {}, "whole": {}}
            for kind in ("pixel", "encoder"):
                for name in sorted(expected_names(kind)):
                    ns = {v: [components[i, v, kind][name] for i in range(1, 7)] for v in sources}
                    ratios = [x / y for x, y in zip(ns["parent"], ns["current"])]
                    summary["components"][name] = {"raw_ns": ns, "speedup": statistics.median(ns["parent"]) / statistics.median(ns["current"]),
                        "paired_speedups": ratios, "slower_pairs": sum(x < 1 for x in ratios), "zero_allocations": True}
            for case, variants in whole.items():
                ns = {v: [x["ns_per_frame"] for x in xs] for v, xs in variants.items()}
                summary["whole"][case] = {"ns_per_frame": ns, "median_time_change_pct": 100 * (statistics.median(ns["current"]) / statistics.median(ns["parent"]) - 1),
                    "paired_time_change_pct": [100 * (x / y - 1) for x, y in zip(ns["current"], ns["parent"])],
                    "semantic_fields": reference[case], "allocations_per_frame": {v: [x["allocs_per_frame"] for x in xs] for v, xs in variants.items()}}
            (out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
            print("END all uninstrumented timing", now(), flush=True)
            for v in sources:
                evidence = run([bins[v]["pixel"], "-test.run=^TestFPPacket", "-test.count=1", "-test.v"], f"correctness/{v}-post-timing-packets.txt")
                assert "numeric cases=7568" in evidence and "full-backing cases=60 plus literal" in evidence
            phase = "diagnostic-profiles-and-codegen"
            profiles = {v: [] for v in sources}
            for number in range(1, args.profile_passes + 1):
                for v in orders[(number - 1) % 2]:
                    profile = out / f"profiles/{v}-{number}.pgo"; profile.parent.mkdir(exist_ok=True)
                    output = json.loads(run([bins[v]["whole"], *whole_args, "-cpuprofile=" + str(profile)], f"profiles/{v}-{number}.json"))
                    assert semantic(output) == reference["vp9-320"]
                    profiles[v].append(profile)
            for v in sources:
                merged = out / f"profiles/{v}-merged.pgo"
                run([wrapper, "tool", "pprof", "-proto", bins[v]["whole"], *profiles[v]], str(merged.relative_to(out)), raw=True)
                for label, flags in (("top", ["-top", "-nodecount=0"]), ("cumulative", ["-top", "-cum", "-nodecount=0"]), ("raw", ["-raw"]), ("quantize-lines", ["-list=quantizeFPBlockSIMD"])):
                    run([wrapper, "tool", "pprof", *flags, bins[v]["whole"], merged], f"profiles/{v}-{label}.txt")
                for kind in ("pixel", "encoder", "whole"):
                    nm = run([wrapper, "tool", "nm", "-size", "-sort=address", bins[v][kind]], f"codegen/{v}-{kind}-symbols.txt")
                    run([wrapper, "version", "-m", bins[v][kind]], f"codegen/{v}-{kind}-buildinfo.txt")
                    owners = []
                    for line in nm.splitlines():
                        m = re.fullmatch(r"\s*([0-9a-fA-F]+)\s+(\d+)\s+[Tt]\s+(\S+)\s*", line)
                        if m and re.search(r"\.(QuantizeFPBlock|quantizeFPBlockSIMD|quantizeFPBlock32AVX512|fp16WindowsDisjoint|BenchmarkFP32PublicOptionalNative(?:\.func1)?|BenchmarkVP9FP16Native(?:\.func1)?)$", m[3]):
                            owners.append({"symbol": m[3], "address": int(m[1], 16), "size": int(m[2])})
                            run(["objdump", "-d", "--disassemble=" + m[3], bins[v][kind]], f"codegen/{v}-{kind}-{m[3].rsplit('/', 1)[-1]}.asm.txt")
                    assert any(x["symbol"].endswith(".quantizeFPBlockSIMD") for x in owners)
                    (out / f"codegen/{v}-{kind}-owners.json").write_text(json.dumps(owners, indent=2) + "\n")
            phase = "excluded-packet-counts"
            counts = {}
            for v, source in sources.items():
                instrument(source)
                trace = scratch / (v + "-trace")
                go(source, ["build", "-pgo=off", "-o", trace, "./cmd/govpx-bench"], f"counts/{v}-build.txt")
                counts[v] = {}
                for case, workload in workloads.items():
                    count_path = out / f"counts/{v}-{case}.json"
                    output = json.loads(run([trace, *workload], f"counts/{v}-{case}-excluded-output.json", {"GOVPX_FP_PACKETS_TRACE": str(count_path)}))
                    assert semantic(output) == reference[case]
                    counts[v][case] = json.loads(count_path.read_text())
                    assert sum(row["packets"] for row in counts[v][case].values()) > 0
                for rel in (*PRODUCTION[:2], "internal/pixel/fp_packets_trace_diagnostic_amd64.go", "cmd/govpx-bench/main.go"):
                    dest = out / "counts" / v / (rel + ".txt"); dest.parent.mkdir(parents=True, exist_ok=True); shutil.copy2(source / rel, dest)
                (out / f"counts/{v}-binding.json").write_text(json.dumps({"binary_sha256": sha(trace), "all_go_sha256": {str(f.relative_to(source)): sha(f) for f in sorted(source.rglob("*.go"))}, "throughput_excluded": True}, indent=2) + "\n")
            assert counts["parent"] == counts["current"], "packet input/predicate semantics differ"
            interpretation = {case: {"packets": sum(row["packets"] for row in rows.values()), "zero_packets": sum(row["zero_packets"] for row in rows.values())} for case, rows in counts["current"].items()}
            (out / "counts/interpretation.json").write_text(json.dumps({"workloads": interpretation, "throughput_excluded": True,
                "limit": "Exact zero predicates counted separately; this is not a Go/C throughput comparison or attribution of historical guard results."}, indent=2) + "\n")
            phase = "complete"
            print("PASS exact isolated packet comparison; all slower controls retained", now(), flush=True)
    finally:
        (out / "status.json").write_text(json.dumps({"phase": phase, "complete": phase == "complete", "time": now()}, indent=2) + "\n")
        manifest = {str(f.relative_to(out)): sha(f) for f in sorted(out.rglob("*")) if f.is_file() and f.name != "MANIFEST.json"}
        (out / "MANIFEST.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
