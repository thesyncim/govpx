#!/usr/bin/env python3
"""Drive the existing same-input helper; keep encode, wall and quality separate."""
import json
import pathlib
import statistics
import subprocess
import sys

bench, vpxenc, output = sys.argv[1:]
output = pathlib.Path(output)
cases = [
    ("vp9-320p", "vp9", 320, 180, 120, 500, 8),
    ("vp9-720p", "vp9", 1280, 720, 60, 2500, 8),
    ("vp8-720p-adaptive", "vp8", 1280, 720, 60, 2500, 8),
    ("vp8-720p-fixed", "vp8", 1280, 720, 60, 2500, -8),
]
manifest = []


def run(case, label, first=False, quality=False):
    name, codec, width, height, frames, bitrate, cpu = case
    reference_flag = "-libvpx-vpxenc-vp9" if codec == "vp9" else "-libvpx-vpxenc"
    args = [bench, "-codec=" + codec, "-width=" + str(width),
            "-height=" + str(height), "-frames=" + str(frames), "-fps=30",
            "-bitrate=" + str(bitrate), "-mode=realtime", "-threads=1",
            "-cpu-used=" + str(cpu), "-auto-libvpx=false", "-format=json",
            reference_flag + "=" + vpxenc,
            "-reference-first=" + str(first).lower()]
    if not quality:
        args.append("-encode-only")
    path = output / (name + "-" + label + ".json")
    manifest.append({"file": path.name, "arguments": args, "quality": quality,
                     "reference_first": first, "adaptive_vp8_speed": codec == "vp8" and cpu > 0})
    (output / "commands.json").write_text(json.dumps(manifest, indent=2) + "\n")
    with path.open("w") as stdout, path.with_suffix(".stderr.txt").open("w") as stderr:
        subprocess.run(args, stdout=stdout, stderr=stderr, check=True)
    report = json.loads(path.read_text())
    ref = report.get("reference", {})
    if ref.get("timing_source") != "vpxenc-stats" or ref.get("ns_per_frame", 0) <= 0:
        raise RuntimeError(path.name + ": codec-internal encode timing unavailable")
    if bool(report.get("reference_first", False)) != first:
        raise RuntimeError(path.name + ": comparison order was not applied")
    if quality:
        if ref.get("quality_error") or min(report["quality_frames"], ref["quality_frames"]) <= 0:
            raise RuntimeError(path.name + ": decoded quality unavailable")
    return report


# Warm code/pages using the same public path; discard these timing samples.
for case in cases:
    run(case, "warmup")
samples = {case[0]: [] for case in cases}
for round_index in range(6):
    order = cases[round_index % len(cases):] + cases[:round_index % len(cases)]
    for case in order:
        samples[case[0]].append(run(case, "round-" + str(round_index + 1), first=bool(round_index % 2)))
# Decode and PSNR/SSIM are deliberately outside all six timing rounds.
quality = {case[0]: run(case, "quality", quality=True) for case in cases}
summary = {"timing_contract": {
    "govpx": "sum of EncodeInto call durations; input construction, warmup and allocation sampling excluded",
    "libvpx": "vpxenc cumulative vpx_codec_encode timer; subprocess/file wall timing reported separately",
    "libvpx_latency": "legacy helper latency fields repeat the mean, not measured latency quantiles; not used here",
    "order": "three govpx-first and three reference-first runs per case",
    "quality": "separate post-timing decode/PSNR/SSIM pass, same inputs and flags",
    "scope": "synthetic same-input/controls comparison; differing output/quality/drop work is reported, not assumed equivalent",
}, "cases": {}}
for case in cases:
    name = case[0]
    reports = samples[name]
    references = [r["reference"] for r in reports]
    def median(rows, field):
        return statistics.median(r[field] for r in rows)
    q = quality[name]
    fields = ["output_bytes", "output_bitrate_kbps", "bitrate_error_pct", "encoded_frames", "dropped_frames", "keyframe_bytes", "avg_interframe_bytes"]
    summary["cases"][name] = {
        "govpx_median_ns_per_input": median(reports, "ns_per_frame"),
        "libvpx_median_ns_per_input": median(references, "ns_per_frame"),
        "govpx_time_delta_percent": 100 * (median(reports, "ns_per_frame") / median(references, "ns_per_frame") - 1),
        "paired_govpx_time_delta_percent": [100 * (r["ns_per_frame"] / r["reference"]["ns_per_frame"] - 1) for r in reports],
        "libvpx_wall_median_ns_per_input": median(references, "wall_ns_per_frame"),
        "libvpx_subprocess_overhead_ns": [r["subprocess_overhead_ns"] for r in references],
        "actual_libvpx_controls": references[0]["parity_flags"],
        "govpx_output_samples": [{f: r[f] for f in fields} for r in reports],
        "libvpx_output_samples": [{f: r[f] for f in fields} for r in references],
        "govpx_allocations_per_input": [r["allocs_per_frame"] for r in reports],
        "govpx_quality": {f: q[f] for f in fields + ["psnr", "ssim", "quality_frames"]},
        "libvpx_quality": {f: q["reference"][f] for f in fields + ["psnr", "ssim", "quality_frames"]},
        "quality_output_matches_timing": {
            "govpx": all(all(q[f] == r[f] for f in fields) for r in reports),
            "libvpx": all(all(q["reference"][f] == r[f] for f in fields) for r in references),
        },
    }
(output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
