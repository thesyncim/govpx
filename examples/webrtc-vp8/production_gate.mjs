#!/usr/bin/env node

import { spawn } from "node:child_process";

const rootOraclePattern = [
  "TestVP8OracleVpxdecDecodesEncodeIntoKeyFrame",
  "TestVP8OracleOutputParityMatrix",
  "TestVP8OracleEncoderStreamByteParityTemporalSVC",
].join("|");

const focusedGoPattern = [
  "TestSuperDemoEndToEnd",
  "TestResizeBigJump",
  "TestVP8WebRTCBrowserSmoke",
  "TestVP8WebRTCProductionGate",
  "TestReadmeDocumentsVP8BrowserGate",
].join("|");

const browserRepeat = integerEnv("VP8_WEBRTC_GATE_REPEAT", 1, { min: 1 });
const browserSoakMs = integerEnv("VP8_WEBRTC_GATE_SOAK_MS", 10000, { min: 1 });
const browserSampleMs = integerEnv("VP8_WEBRTC_GATE_SAMPLE_MS", 5000, { min: 1 });
const browserPollMs = integerEnv("VP8_WEBRTC_GATE_POLL_MS", 1000, { min: 1 });
const browserMinDecoded = integerEnv("VP8_WEBRTC_GATE_MIN_DECODED_DELTA", 80, { min: 0 });
const browserMinVideoRatio = numberEnv("VP8_WEBRTC_GATE_MIN_VIDEO_TIME_RATIO", 0.85, { min: 0 });
const browserCPUBurners = integerEnv("VP8_WEBRTC_GATE_CPU_BURNERS", 0, { min: 0 });
const browserWithholdCount = integerEnv("VP8_WEBRTC_GATE_WITHHOLD_COUNT", 2, { min: 1, max: 10 });

const browserArgs = [
  "browser_smoke.mjs",
  "--repeat", String(browserRepeat),
  "--soak-ms", String(browserSoakMs),
  "--sample-ms", String(browserSampleMs),
  "--poll-ms", String(browserPollMs),
  "--min-decoded-delta", String(browserMinDecoded),
  "--min-video-time-ratio", String(browserMinVideoRatio),
  "--max-rx-lost-delta", "0",
  "--max-rx-dropped-delta", "0",
  "--max-rx-freezes-delta", "0",
  "--max-rx-freeze-duration-delta", "0",
  "--max-rx-repair-delta", "0",
  "--max-rx-nack-delta", "0",
  "--max-rx-pli-delta", "0",
  "--max-rx-fir-delta", "0",
];
if (browserCPUBurners > 0) {
  browserArgs.push("--cpu-burners", String(browserCPUBurners));
}

const steps = [
  {
    name: "browser-smoke-js-syntax",
    command: "node",
    args: ["--check", "browser_smoke.mjs"],
  },
  {
    name: "focused-go",
    command: "go",
    args: ["test", ".", "-run", focusedGoPattern, "-count=1"],
  },
  {
    name: "libvpx-root-oracle",
    command: "go",
    args: [
      "test",
      "-v",
      "-tags", "govpx_oracle_trace",
      "../..",
      "-run",
      rootOraclePattern,
      "-count=1",
    ],
    kind: "go-test",
    env: {
      GOVPX_WITH_ORACLE: "1",
    },
    requiresOracle: true,
  },
  {
    name: "browser-smoke",
    command: "node",
    args: browserArgs,
    kind: "browser-json",
  },
  {
    name: "browser-local-withhold",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--local-withhold",
      "--local-withhold-count", String(browserWithholdCount),
      "--soak-ms", String(browserSoakMs),
      "--sample-ms", String(browserSampleMs),
      "--poll-ms", String(browserPollMs),
      "--min-decoded-delta", String(browserMinDecoded),
      "--min-video-time-ratio", String(browserMinVideoRatio),
      "--max-rx-lost-delta", "0",
      "--max-rx-dropped-delta", "0",
      "--max-rx-freezes-delta", "0",
      "--max-rx-freeze-duration-delta", "0",
      "--max-rx-repair-delta", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      ...(browserCPUBurners > 0 ? ["--cpu-burners", String(browserCPUBurners)] : []),
    ],
    kind: "browser-json",
  },
];

async function main() {
  const started = Date.now();
  const results = [];
  for (const step of steps) {
    results.push(await runStep(step));
  }
  console.log(JSON.stringify({
    ok: true,
    elapsedMs: Date.now() - started,
    steps: results,
  }, null, 2));
}

async function runStep(step) {
  const started = Date.now();
  process.stderr.write(`[gate] ${step.name}: ${step.command} ${step.args.join(" ")}\n`);
  const child = spawn(step.command, step.args, {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, ...(step.env || {}) },
  });
  const output = await collect(child);
  if (output.code !== 0) {
    process.stderr.write(output.stdout);
    process.stderr.write(output.stderr);
    throw new Error(`${step.name} failed with exit code ${output.code}`);
  }
  if (step.requiresOracle) {
    assertNoOracleSkips(step, output.stdout);
  }
  let parsed = null;
  if (step.kind === "browser-json") {
    parsed = JSON.parse(output.stdout);
  }
  const summary = parsed?.summary ?? parsed?.aggregate ?? null;
  if (summary && parsed?.localWithholdResult) {
    summary.localWithhold = summarizeLocalWithhold(parsed.localWithholdResult);
  }
  return {
    name: step.name,
    command: step.command,
    args: step.args,
    elapsedMs: Date.now() - started,
    stdoutBytes: output.stdout.length,
    stderrBytes: output.stderr.length,
    summary,
  };
}

function summarizeLocalWithhold(result) {
  const clients = Array.isArray(result?.clients) ? result.clients : [];
  const values = (key) => clients.map((client) => client?.[key]).filter(Number.isFinite);
  return {
    count: result?.count ?? null,
    renditions: result?.renditions ?? null,
    expectedWithheldAUs: result?.expectedWithheldAUs ?? null,
    minWithheldAUs: minNumber(values("withheldAUs")),
    maxForcedKeys: maxNumber(values("forcedKeys")),
    decodedAfterWithhold: sumNumber(values("decodedAfterWithhold")),
    lostAfterWithhold: sumNumber(values("lostAfterWithhold")),
    repairedAfterWithhold: sumNumber(values("repairedAfterWithhold")),
    nacksAfterWithhold: sumNumber(values("nackAfterWithhold")),
    plisAfterWithhold: sumNumber(values("pliAfterWithhold")),
    firsAfterWithhold: sumNumber(values("firAfterWithhold")),
  };
}

function minNumber(values) {
  return values.length === 0 ? null : Math.min(...values);
}

function maxNumber(values) {
  return values.length === 0 ? null : Math.max(...values);
}

function sumNumber(values) {
  return values.reduce((total, value) => total + value, 0);
}

function collect(child) {
  return new Promise((resolve, reject) => {
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => { stdout += chunk.toString(); });
    child.stderr.on("data", (chunk) => { stderr += chunk.toString(); });
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stdout, stderr }));
  });
}

function assertNoOracleSkips(step, stdout) {
  const skipped = stdout.split("\n").filter((line) => line.startsWith("--- SKIP:"));
  if (skipped.length === 0) {
    return;
  }
  const err = new Error(`${step.name} skipped required oracle tests: ${skipped.join("; ")}`);
  err.stdout = stdout;
  throw err;
}

function integerEnv(name, defaultValue, limits = {}) {
  const raw = process.env[name];
  if (!raw) return defaultValue;
  const parsed = Number(raw);
  if (!Number.isInteger(parsed)) throw new Error(`${name} must be an integer`);
  validateNumber(name, parsed, limits);
  return parsed;
}

function numberEnv(name, defaultValue, limits = {}) {
  const raw = process.env[name];
  if (!raw) return defaultValue;
  const parsed = Number(raw);
  if (!Number.isFinite(parsed)) throw new Error(`${name} must be a number`);
  validateNumber(name, parsed, limits);
  return parsed;
}

function validateNumber(name, value, { min = -Infinity, max = Infinity } = {}) {
  if (value < min || value > max) {
    throw new Error(`${name} must be between ${min} and ${max}`);
  }
}

await main();
