#!/usr/bin/env python3
"""Compact native x64 screens of independent source overlays; no whole/C/profile run."""
from pathlib import Path
import argparse
import datetime
import hashlib
import io
import json
import os
import platform
import re
import shutil
import statistics
import subprocess
import tarfile
import tempfile

sha = lambda b: hashlib.sha256(b).hexdigest()


def relative(s):
    p = Path(s)
    assert not p.is_absolute() and '..' not in p.parts and '.git' not in p.parts and '\\' not in s, s
    return p


def bench_rows(text):
    rows = {}
    for line in text.splitlines():
        if not line.startswith('Benchmark'):
            continue
        m = re.fullmatch(r'(Benchmark\S+)\s+\d+\s+([\d.eE+\-]+) ns/op\s+0 B/op\s+0 allocs/op\s*', line)
        assert m and m[1] not in rows and float(m[2]) > 0, line
        rows[m[1]] = float(m[2])
    assert rows, 'no benchmark cases matched'
    return rows


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('head_sha')
    ap.add_argument('base_sha')
    ap.add_argument('output_dir')
    ap.add_argument('--manifest', required=True)
    args = ap.parse_args()
    assert platform.system() == 'Linux' and platform.machine() == 'x86_64', 'native Linux/x64 only'
    assert os.environ.get('GOVPX_NATIVE_FP_QUICK') == '1', 'explicit native quick opt-in required'
    assert all(re.fullmatch('[0-9a-f]{40}', x) for x in [args.head_sha, args.base_sha])
    root = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], text=True).strip())
    out = Path(args.output_dir).resolve()
    out.mkdir(parents=True, exist_ok=True)
    assert not list(out.iterdir()), 'preserve previous evidence'
    wrapper = root / 'scripts/go.sh'
    env = dict(os.environ, GOTOOLCHAIN='local', GOEXPERIMENT='simd', GOOS='linux', GOARCH='amd64',
               GOAMD64='v1', GOMAXPROCS='1', CGO_ENABLED='1', GOFLAGS='-p=2', GODEBUG='',
               GOVPX_REQUIRE_SIMD='1', GOVPX_REQUIRE_FP32_NATIVE='0', GOVPX_REQUIRE_FP32_OFF='0')
    commands = []
    archived_blobs = {}
    status = {'complete': False, 'phase': 'setup', 'variant_status': {}}

    def save_status():
        (out / 'status.json').write_text(json.dumps(status, indent=2) + '\n')

    def blob(path):
        relative(path)
        if path in archived_blobs:
            return archived_blobs[path]
        return subprocess.check_output(['git', 'show', args.head_sha + ':' + path], cwd=root)

    def run(argv, name):
        record = {'argv': [str(x) for x in argv], 'output': name,
                  'start_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
        with (out / name).open('wb') as log, (out / (name + '.stderr')).open('wb') as err:
            result = subprocess.run(record['argv'], cwd=root, env=env, stdout=log, stderr=err)
        record.update(exit_code=result.returncode, end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
        commands.append(record)
        (out / 'commands.json').write_text(json.dumps(commands, indent=2) + '\n')
        assert result.returncode == 0, (name, result.returncode)
        return (out / name).read_text()

    try:
        manifest_bytes = blob(args.manifest)
        manifest = json.loads(manifest_bytes)
        archive_digest = None
        if manifest.get('bundle_archive'):
            archive_bytes = blob(manifest['bundle_archive'])
            archive_digest = sha(archive_bytes)
            with tarfile.open(fileobj=io.BytesIO(archive_bytes)) as tar:
                members = tar.getmembers()
                assert sum(m.size for m in members) <= 32 * 1024 * 1024
                for member in members:
                    path = str(relative(member.name))
                    assert member.isfile() and path not in archived_blobs
                    archived_blobs[path] = tar.extractfile(member).read()
        assert manifest['base_sha'] == args.base_sha
        variants = manifest['variants']
        ids = [str(v['id']) for v in variants]
        assert len(ids) == len(set(ids)) and all(re.fullmatch('[0-9]+', i) for i in ids)
        assert all(v['package'] in ('./internal/pixel', './internal/vp9/encoder', '.') for v in variants)
        run(['uname', '-a'], 'uname.txt')
        run(['lscpu'], 'lscpu.txt')
        assert run([wrapper, 'env', 'GOVERSION'], 'goversion.txt').strip() == 'go1.27.4'
        assert '.codex/worktrees' not in run([wrapper, 'env', 'GOCACHE'], 'gocache.txt')
        fixtures = {str(relative(f)): blob(source) for f, source in manifest['fixtures'].items()}
        assert all(f.endswith('_test.go') for f in fixtures)
        (out / 'manifest.json').write_bytes(manifest_bytes)
        binding = {'base_sha': args.base_sha, 'bundle_commit': args.head_sha,
                   'manifest_sha256': sha(manifest_bytes), 'driver_sha256': sha(Path(__file__).read_bytes()),
                   'bundle_archive_sha256': archive_digest,
                   'PGO': 'off', 'CGO_ENABLED': 1, 'GOMAXPROCS': 1, 'GOAMD64': 'v1',
                   'shared_fixture_sha256': {f: sha(b) for f, b in fixtures.items()},
                   'scope': 'Two balanced short native component samples; no whole/C/profile/race/full suite.',
                   'baseline_binaries': {}, 'variants': {}}
        with tempfile.TemporaryDirectory(prefix='govpx-x64-variants-') as temp:
            temp = Path(temp)
            base = temp / 'baseline'
            base.mkdir()
            archive = subprocess.check_output(['git', 'archive', args.base_sha, '--', '*.go', 'go.mod'], cwd=root)
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                assert all(not Path(m.name).is_absolute() and '..' not in Path(m.name).parts for m in tar.getmembers())
                tar.extractall(base)
            for f, data in fixtures.items():
                path = base / f
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
            packages = sorted(set(v['package'] for v in variants) | {'./internal/pixel'})
            base_bins = {}
            status['phase'] = 'baseline-build'
            save_status()
            for k, package in enumerate(packages):
                binary = temp / f'baseline-{k}.test'
                run([wrapper, '-C', base, 'test', '-c', '-pgo=off', '-o', binary, package], f'build-baseline-{k}.txt')
                base_bins[package] = binary
                binding['baseline_binaries'][package] = {'sha256': sha(binary.read_bytes()), 'bytes': binary.stat().st_size}
            host = run([base_bins['./internal/pixel'], '-test.run=^Test(BackendSelected$|FP32PublicHost$|FP32ActualFeature$)', '-test.count=1', '-test.v'], 'native-host.txt')
            m = re.search(r'FP32PublicHost AVX512=(true|false) AVX2=(true|false) Enabled=(true|false)', host)
            assert m and m.group(2, 3) == ('true', 'true'), 'actual selected native AVX2 required'
            binding['actual_SDK_features'] = dict(zip(('AVX512', 'AVX2', 'Enabled'), (x == 'true' for x in m.groups())))
            eligible = []
            checked_baselines = set()
            for v in variants:
                i = str(v['id'])
                status['phase'] = 'build-and-oracle-' + i
                save_status()
                try:
                    package, oracle = v['package'], v['test']
                    if (package, oracle) not in checked_baselines:
                        text = run([base_bins[package], '-test.run=' + oracle, '-test.count=1', '-test.v'], f'oracle-baseline-{i}.txt')
                        assert re.search(r'^--- PASS:', text, re.M), 'baseline oracle matched no passing tests'
                        checked_baselines.add((package, oracle))
                    source = temp / ('variant-' + i)
                    shutil.copytree(base, source)
                    source_hashes = {}
                    for path, source_path in v['files'].items():
                        dest = source / relative(path)
                        assert path.endswith('.go') and not path.endswith('_test.go'), 'production-only overlay'
                        data = blob(source_path)
                        before = sha(dest.read_bytes()) if dest.exists() else None
                        dest.parent.mkdir(parents=True, exist_ok=True)
                        dest.write_bytes(data)
                        source_hashes[path] = {'baseline_sha256': before, 'candidate_sha256': sha(data)}
                    binary = temp / ('variant-' + i + '.test')
                    run([wrapper, '-C', source, 'test', '-c', '-pgo=off', '-o', binary, package], f'build-{i}.txt')
                    text = run([binary, '-test.run=' + oracle, '-test.count=1', '-test.v'], f'oracle-{i}.txt')
                    assert re.search(r'^--- PASS:', text, re.M), 'candidate oracle matched no passing tests'
                    binding['variants'][i] = {'hypothesis': v['hypothesis'], 'package': package,
                                              'source': source_hashes, 'binary_sha256': sha(binary.read_bytes()),
                                              'binary_bytes': binary.stat().st_size}
                    eligible.append((v, binary))
                    status['variant_status'][i] = 'correctness-pass'
                except (AssertionError, subprocess.CalledProcessError) as error:
                    status['variant_status'][i] = 'rejected-build-or-correctness: ' + str(error)
                save_status()
                (out / 'binding.json').write_text(json.dumps(binding, indent=2) + '\n')
            summaries = {}
            status['phase'] = 'native-components'
            save_status()
            for v, binary in eligible:
                i = str(v['id'])
                samples = {'baseline': [], 'candidate': []}
                expected = None
                try:
                    for rnd, order in enumerate((('baseline', 'candidate'), ('candidate', 'baseline')), 1):
                        for name in order:
                            executable = base_bins[v['package']] if name == 'baseline' else binary
                            text = run([executable, '-test.run=^$', '-test.bench=' + v['bench'], '-test.benchtime=40ms', '-test.cpu=1', '-test.count=1', '-test.benchmem'], f'r{rnd}-{i}-{name}.txt')
                            rows = bench_rows(text)
                            if expected is None:
                                expected = set(rows)
                            assert set(rows) == expected, 'case inventory differs'
                            samples[name].append(rows)
                    cases = {}
                    for name in sorted(expected):
                        ns = {k: [row[name] for row in sample] for k, sample in samples.items()}
                        speeds = [a / b for a, b in zip(ns['baseline'], ns['candidate'])]
                        cases[name] = {'raw_ns': ns, 'paired_speedups': speeds, 'median_speedup': statistics.median(speeds), 'slower_pairs': sum(x < 1 for x in speeds)}
                    summaries[i] = {'hypothesis': v['hypothesis'], 'components': cases, 'zero_allocations': True, 'preliminary': True}
                    status['variant_status'][i] = 'screened'
                except AssertionError as error:
                    status['variant_status'][i] = 'rejected-timing-contract: ' + str(error)
                (out / 'summary.json').write_text(json.dumps(summaries, indent=2) + '\n')
                save_status()
        status.update(complete=True, phase='complete')
    finally:
        save_status()


if __name__ == '__main__':
    main()
