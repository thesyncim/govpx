#!/usr/bin/env python3
"""Preliminary native packet feedback: two rounds, no acceptance speed threshold."""
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
import statistics
import subprocess
import tarfile
import tempfile

FIXTURES = ("internal/pixel/fp32_optional_native_test.go",
            "internal/vp9/encoder/quant_fp16_native_test.go",
            "internal/pixel/quantize_fp_packets_test.go")
SOURCES = ("internal/pixel/quantize_fp_amd64.go", "internal/pixel/quantize_fp32_avx512_amd64.go",
           "internal/pixel/quantize_fp_simd.go")
IGNORED = {"ns_per_frame", "encoded_ns_per_frame", "encode_fps", "macroblocks_per_second", "latency_ns", "allocs_per_frame"}
sha = lambda data: hashlib.sha256(data).hexdigest()


def names(kind):
    if kind == "pixel":
        return {f"BenchmarkFP32PublicOptionalNative/n{n}/qnil{q}/classes{c}/{p}/{a}"
                for n, q, c, p, a in itertools.product((64, 256), ("false", "true"), ("false", "true"),
                                                     ("zero", "sparse", "dense"), ("aligned64", "offset2-classes1"))}
    return {f"BenchmarkVP9FP16Native/n{n}/{c}/{p}/{a}" for n, c, p, a in itertools.product(
        (16, 64, 256), ("no-classes", "classes"), ("zero", "sparse", "dense"), ("aligned32", "offset2-classes1"))}


def parse_bench(text, expected=None):
    result = {}
    for line in text.splitlines():
        if not line.startswith("Benchmark"):
            continue
        match = re.fullmatch(r"(Benchmark\S+)\s+\d+\s+([\d.eE+\-]+) ns/op\s+0 B/op\s+0 allocs/op\s*", line)
        assert match and match[1] not in result and float(match[2]) > 0, line
        result[match[1]] = float(match[2])
    assert result, "benchmark regex matched no cases"
    if expected is not None:
        assert set(result) == expected, sorted(set(result) ^ expected)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("head_sha")
    parser.add_argument("base_sha")
    parser.add_argument("output_dir")
    parser.add_argument("--oracle-only", action="store_true", help="Run focused parent/current oracles without benchmarks or whole-codec builds")
    parser.add_argument("--bench", help="Custom benchmark regex; case inventory is inferred from parent")
    parser.add_argument("--test", help="Ordinary oracle regex (default: packet oracles in FP mode)")
    parser.add_argument("--package", default="./internal/pixel", help="Comma-separated packages for --bench/--test overrides")
    parser.add_argument("--fixture", action="append", default=[f.strip() for f in os.environ.get("GOVPX_QUICK_FIXTURES", "").splitlines() if f.strip()], help="Shared relative _test.go path; repeatable")
    parser.add_argument("--codec", choices=("vp8", "vp9"), help="Use two balanced 720p codec rounds (default codec: vp9; no options retains legacy 320/720 scopes)")
    parser.add_argument("--codec-cpu-used", action="append", default=[], help="CPU-used integers; repeatable or comma-separated, e.g. --codec-cpu-used=-8,+8")
    args = parser.parse_args()
    assert not args.oracle_only or args.test, "oracle-only mode requires an explicit --test regex"
    codec_mode = args.codec is not None or bool(args.codec_cpu_used)
    codec = args.codec or "vp9"
    cpu_used = [int(value.strip()) for item in args.codec_cpu_used for value in item.split(",")] if args.codec_cpu_used else [8]
    assert len(cpu_used) == len(set(cpu_used)), "codec CPU-used values must be distinct"
    for f in args.fixture:
        assert f.endswith("_test.go") and not Path(f).is_absolute() and ".." not in Path(f).parts and ".git" not in Path(f).parts and "\\" not in f, "fixture must be a relative _test.go path without traversal"
    assert all(re.fullmatch(r"[0-9a-f]{40}", c) for c in (args.head_sha, args.base_sha)), "use exact commit SHAs"
    assert platform.system() == "Linux" and platform.machine() == "x86_64", "native Linux/x64 required"
    assert os.environ.get("GOVPX_NATIVE_FP_QUICK") == "1", "explicit native timing opt-in required"
    root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip())
    out = Path(args.output_dir).resolve()
    out.mkdir(parents=True, exist_ok=True)
    assert not list(out.iterdir()), "preserve earlier evidence; output must be empty"
    wrapper = str(root / "scripts/go.sh")
    env = dict(os.environ, GOTOOLCHAIN="local", GOEXPERIMENT="simd", GOOS="linux", GOARCH="amd64", GOAMD64="v1",
               GOMAXPROCS="1", CGO_ENABLED="1", GOFLAGS="-p=2", GODEBUG="", GOVPX_REQUIRE_SIMD="1",
               GOVPX_REQUIRE_FP32_NATIVE="0", GOVPX_REQUIRE_FP32_OFF="0")
    commands = []
    phase = "environment"

    def run(argv, filename, allow_failure=False):
        command = {"argv": [str(x) for x in argv], "output": filename,
                   "start_utc": datetime.datetime.now(datetime.timezone.utc).isoformat()}
        with (out / filename).open("wb") as log, (out / (filename + ".stderr")).open("wb") as err:
            process = subprocess.run(command["argv"], cwd=root, env=env, stdout=log, stderr=err)
        command.update(exit_code=process.returncode, end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
        commands.append(command)
        (out / "commands.json").write_text(json.dumps(commands, indent=2) + "\n")
        assert allow_failure or process.returncode == 0, (filename, process.returncode)
        return (out / filename).read_text()

    try:
        run(["uname", "-a"], "uname.txt")
        run(["lscpu"], "lscpu.txt")
        assert run([wrapper, "env", "GOVERSION"], "goversion.txt").strip() == "go1.27.4"
        assert ".codex/worktrees" not in run([wrapper, "env", "GOCACHE"], "gocache.txt")
        commits = {"parent": args.base_sha, "current": args.head_sha}
        for c in commits.values():
            subprocess.run(["git", "cat-file", "-e", c + "^{commit}"], cwd=root, check=True)
        with tempfile.TemporaryDirectory(prefix="govpx-fp-quick-") as temporary:
            scratch = Path(temporary)
            sources = {}
            for variant, commit in commits.items():
                source = scratch / variant
                source.mkdir()
                # No repository evidence archives or unrelated files are needed by these pure-Go targets.
                archive_paths = ["*.go", "go.mod"]
                if subprocess.run(["git", "cat-file", "-e", commit + ":go.sum"], cwd=root,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                    archive_paths.append("go.sum")
                archive = subprocess.check_output(["git", "archive", commit, "--", *archive_paths], cwd=root)
                with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                    assert all(not Path(m.name).is_absolute() and ".." not in Path(m.name).parts for m in tar.getmembers())
                    tar.extractall(source)
                sources[variant] = source
            fixture_paths = dict.fromkeys((*FIXTURES, *args.fixture))
            assert all((sources["current"] / f).is_file() and not (sources["current"] / f).is_symlink() for f in fixture_paths), "fixture must be a regular tracked Go test file"
            fixtures = {f: (sources["current"] / f).read_bytes() for f in fixture_paths}
            binding = {"commits": commits, "PGO": "off", "CGO_ENABLED": 1, "GOMAXPROCS": 1,
                       "driver_sha256": sha(Path(__file__).read_bytes()), "shared_fixture_sha256": {f: sha(b) for f, b in fixtures.items()},
                       "scope": "Preliminary two-round feedback; no race/profiles/trace/codegen or full source audit.", "variants": {}}
            custom_packages = args.package.split(",")
            assert all(p == "." or (p.startswith("./") and ".." not in Path(p).parts) for p in custom_packages)
            assert len(custom_packages) == len(set(custom_packages))
            plan = [("custom" if len(custom_packages) == 1 else f"custom{i}", package, args.bench)
                    for i, package in enumerate(custom_packages)] if args.bench or args.oracle_only else [
                ("pixel", "./internal/pixel", "^BenchmarkFP32PublicOptionalNative$"),
                ("encoder", "./internal/vp9/encoder", "^BenchmarkVP9FP16Native$")]
            binding["benchmark_plan"] = plan
            binding["whole_codec_plan"] = {"mode": "two720p-rounds" if codec_mode else "legacy 320/720", "codec": codec, "cpu_used": cpu_used}
            binding["oracle_override"] = args.test
            bins = {}
            oracle_results = {}
            phase = "build-and-ordinary-oracles"
            for variant, source in sources.items():
                assert all(not (source / f).is_symlink() for f in fixtures), "reference fixture cannot be a symlink"
                originals = {f: sha((source / f).read_bytes()) if (source / f).exists() else None for f in fixtures}
                for f, data in fixtures.items():
                    (source / f).parent.mkdir(parents=True, exist_ok=True)
                    (source / f).write_bytes(data)
                    assert (source / f).read_bytes() == data
                bins[variant] = {}
                packages = {"pixel": "./internal/pixel", **{k: pkg for k, pkg, _ in plan}}
                if not args.oracle_only:
                    packages["whole"] = "./cmd/govpx-bench"
                for kind, package in packages.items():
                    binary = scratch / (variant + "-" + kind)
                    build = ["build"] if kind == "whole" else ["test", "-c"]
                    run([wrapper, "-C", source, *build, "-pgo=off", "-o", binary, package], f"build-{variant}-{kind}.txt")
                    bins[variant][kind] = binary
                oracle = "^Test(BackendSelected$|FP32PublicHost$|FP32ActualFeature$)" if args.bench or args.test else "^Test(FPPacket|BackendSelected$|FP32PublicHost$|FP32ActualFeature$|FP32NativeSelectedOracles$)"
                text = run([bins[variant]["pixel"], "-test.run=" + oracle,
                            "-test.count=1", "-test.v"], f"oracle-{variant}.txt")
                feature = re.search(r"FP32PublicHost AVX512=(true|false) AVX2=(true|false) Enabled=(true|false)", text)
                assert feature and feature.group(2, 3) == ("true", "true"), "actual native AVX2 required"
                if not args.bench and not args.test:
                    for name in ("Independent", "AliasOrder", "PanicWindows", "DequantWrapZero"):
                        assert "--- PASS: TestFPPacket" + name in text
                binding["variants"][variant] = {"actual_SDK_features": dict(zip(("AVX512", "AVX2", "Enabled"), (x == "true" for x in feature.groups()))),
                    "original_fixture_sha256": originals, "production_source_sha256": {f: sha((source / f).read_bytes()) for f in SOURCES},
                    "binaries": {k: {"sha256": sha(f.read_bytes()), "bytes": f.stat().st_size} for k, f in bins[variant].items()}}
                (out / "binding.json").write_text(json.dumps(binding, indent=2) + "\n")
                if args.test:
                    for test_kind in ([k for k, _, _ in plan] if args.bench or args.oracle_only else ["pixel"]):
                        test = run([bins[variant][test_kind], "-test.run=" + args.test, "-test.count=1", "-test.v"], f"custom-oracle-{variant}-{test_kind}.txt", allow_failure=args.oracle_only)
                        passed = commands[-1]["exit_code"] == 0 and re.search(r"^--- PASS:", test, re.M) is not None
                        oracle_results.setdefault(variant, {})[test_kind] = {"passed": passed, "exit_code": commands[-1]["exit_code"], "output": commands[-1]["output"]}
                        if not args.oracle_only:
                            assert passed, "custom oracle regex matched no passing tests"
            assert binding["variants"]["parent"]["actual_SDK_features"] == binding["variants"]["current"]["actual_SDK_features"]
            (out / "binding.json").write_text(json.dumps(binding, indent=2) + "\n")
            if args.oracle_only:
                passed = all(result["passed"] for cases in oracle_results.values() for result in cases.values())
                summary = {"oracle_only": True, "oracle_results": oracle_results, "all_passed": passed,
                           "benchmark_calls": 0, "whole_codec_builds": 0, "oracle_regex": args.test}
                (out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
                (out / "summary.md").write_text("Focused parent/current oracle comparison only; no timings or whole-codec builds. " + ("All selected oracles passed.\n" if passed else "Selected oracle failure preserved in the raw logs.\n"))
                phase = "complete" if passed else "focused-oracle-failed"
                assert passed, "focused oracle comparison failed; both variants and bindings are preserved"
                return
            phase = "preliminary-native-timing"
            samples = {}
            inventory = {} if args.bench else {k: names(k) for k, _, _ in plan}
            whole = {}
            for number, order in enumerate((("parent", "current"), ("current", "parent")), 1):
                for variant in order:
                    for kind, _, benchmark in plan:
                        text = run([bins[variant][kind], "-test.run=^$", "-test.bench=" + benchmark, "-test.benchtime=40ms",
                                    "-test.cpu=1", "-test.count=1", "-test.benchmem"], f"r{number}-{variant}-{kind}.txt")
                        result = parse_bench(text, inventory.get(kind))
                        if kind not in inventory:
                            assert variant == "parent" and number == 1
                            inventory[kind] = set(result)
                        samples[number, variant, kind] = result
                # Defaults preserve legacy scope/file names; codec options run 720p twice per CPU-used value.
                if codec_mode:
                    whole_plan = [(f"{codec}-720p-cpu{cpu}-r{number}", 1280, 720, 60, 2500, cpu) for cpu in cpu_used]
                else:
                    width, height, frames, bitrate = (320, 180, 120, 500) if number == 1 else (1280, 720, 60, 2500)
                    whole_plan = [(str(width), width, height, frames, bitrate, 8)]
                for scope, width, height, frames, bitrate, cpu in whole_plan:
                    whole[scope] = {}
                    for variant in order:
                        filename = f"r{number}-{variant}-whole-{codec}-cpu{cpu}.json" if codec_mode else f"r{number}-{variant}-whole.json"
                        text = run([bins[variant]["whole"], f"-codec={codec}", f"-width={width}", f"-height={height}", f"-frames={frames}",
                                    "-fps=30", f"-bitrate={bitrate}", "-mode=realtime", f"-cpu-used={cpu}", "-threads=1", "-encode-only",
                                    "-auto-libvpx=false", "-format=json"], filename)
                        whole[scope][variant] = json.loads(text)
                    semantic = [{k: v for k, v in row.items() if k not in IGNORED} for row in whole[scope].values()]
                    assert len(semantic[0]) == 22 and semantic[0] == semantic[1], "whole output semantics differ: " + scope
            components = {}
            for kind, package, _ in plan:
                for name in sorted(inventory[kind]):
                    ns = {v: [samples[i, v, kind][name] for i in (1, 2)] for v in commits}
                    key = f"{package}:{name}" if args.bench and len(custom_packages) > 1 else name
                    components[key] = {"raw_ns": ns, "speedup": statistics.median(ns["parent"]) / statistics.median(ns["current"]),
                                        "paired_speedups": [a / b for a, b in zip(ns["parent"], ns["current"])]}
            summary = {"preliminary": True, "acceptance_claim": False, "component_rows": 4 * len(components), "component_allocations_zero": True,
                       "orders": [["parent", "current"], ["current", "parent"]], "components": components, "whole": whole,
                       "whole_semantic_fields_equal": 22, "whole_codec_plan": binding["whole_codec_plan"], "limit": "Short component and codec samples are preliminary, not robust acceptance evidence."}
            (out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
            (out / "summary.md").write_text(f"Preliminary native feedback: {len(components)} cases, {4 * len(components)} zero-allocation samples; {len(whole)} whole {codec} scope pairs, all 22 output fields match. All losses remain in summary.json. No acceptance speed claim.\n")
            phase = "complete"
    finally:
        (out / "status.json").write_text(json.dumps({"phase": phase, "complete": phase == "complete"}, indent=2) + "\n")


if __name__ == "__main__":
    main()
