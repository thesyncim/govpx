#!/usr/bin/env node

import { spawn } from "node:child_process";

const oraclePattern = [
  "TestPlainVP9WebRTC.*Vpxdec",
  "TestWebRTCEndToEnd.*Vpxdec",
  "TestWebRTCPacketizedSVC.*Vpxdec",
  "TestVP9WebRTCPacketizerSVC.*Vpxdec",
].join("|");

const browserStepCooldownMs = 5000;
const maxAccessUnitMs = numberEnv("VP9_WEBRTC_GATE_MAX_ACCESS_UNIT_MS", 200, { min: 1 });
const maxScheduleLagMs = numberEnv("VP9_WEBRTC_GATE_MAX_SCHEDULE_LAG_MS", 200, { min: 1 });
const browserLatencyBudgets = [
  "--max-access-unit-ms", String(maxAccessUnitMs),
  "--max-schedule-lag-ms", String(maxScheduleLagMs),
];

const steps = [
  {
    name: "focused-go",
    command: "go",
    args: [
      "test",
      ".",
      "-run",
      "TestSpatialCapBackoff|TestReadmeDocumentsStatefulVP9WebRTCPacketizer|TestIndexHTMLExposesBrowserRTCStatsForFreezeDiagnosis|TestBrowserSmokeEnforcesVP9WebRTCBudgets|TestProductionGateReportsVP9BrowserStallBudgets|TestStressGateReportsVP9HostileSoakBudgets|TestApplyControl.*|TestConsumeLocalWithholdAccessUnit|TestControlPauseResumeRequestsKeyFrame|TestPauseControlPreservesPendingKeyFrame",
      "-count=1",
    ],
    kind: "go-test",
  },
  {
    name: "browser-unloaded",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--repeat", "3",
      "--soak-ms", "30000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "100",
      "--min-video-time-ratio", "0.9",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "3",
      "--min-ending-active-layers", "3",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-loaded",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--repeat", "2",
      "--cpu-burners", "12",
      "--server-fps", "25",
      "--soak-ms", "30000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.9",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-control-churn",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--control-churn",
      "--soak-ms", "20000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.85",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "2",
      "--min-ending-active-layers", "2",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-tuning-churn",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--tuning-churn",
      "--soak-ms", "30000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.85",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "3",
      "--min-ending-active-layers", "3",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-pause-resume",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--pause-resume",
      "--pause-ms", "1500",
      "--soak-ms", "10000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.8",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "3",
      "--min-ending-active-layers", "3",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-receiver-stall-probe",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--receiver-stall-probe",
      "--soak-ms", "10000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.8",
      "--max-rx-repair-requests", "1",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-local-withhold",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--local-withhold",
      "--local-withhold-count", "2",
      "--soak-ms", "10000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.8",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "3",
      "--min-ending-active-layers", "3",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-loaded-local-withhold",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--local-withhold",
      "--local-withhold-count", "2",
      "--cpu-burners", "12",
      "--server-fps", "25",
      "--soak-ms", "10000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.8",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-loaded-control-churn",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--control-churn",
      "--cpu-burners", "12",
      "--server-fps", "25",
      "--soak-ms", "20000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.8",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
    ],
    kind: "browser-json",
  },
  {
    name: "browser-multiclient",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--clients", "2",
      "--soak-ms", "20000",
      "--sample-ms", "5000",
      "--poll-ms", "1000",
      "--min-decoded-delta", "80",
      "--min-video-time-ratio", "0.85",
      "--max-rx-repair-requests", "0",
      "--max-rx-nack-delta", "0",
      "--max-rx-pli-delta", "0",
      "--max-rx-fir-delta", "0",
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      ...browserLatencyBudgets,
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "libvpx-threaded-vpxenc-oracle",
    command: "go",
    args: [
      "test",
      "-v",
      "-tags", "govpx_oracle_trace",
      "../..",
      "-run",
      "TestVP9OracleThreadedTileEncodingMatchesLibvpx",
      "-count=1",
    ],
    kind: "go-test",
    env: {
      GOVPX_WITH_ORACLE: "1",
    },
    requiresOracle: true,
  },
  {
    name: "libvpx-vpxdec-oracle",
    command: "go",
    args: [
      "test",
      "-v",
      "-tags", "govpx_oracle_trace",
      ".",
      "-run", oraclePattern,
      "-count=1",
    ],
    kind: "go-test",
    env: {
      GOVPX_WITH_ORACLE: "1",
    },
    requiresOracle: true,
  },
];

async function main() {
  const startedAt = Date.now();
  const results = [];
  for (let i = 0; i < steps.length; i++) {
    const step = steps[i];
    process.stderr.write(`[vp9-webrtc-gate] ${step.name}: ${formatCommand(step)}\n`);
    const result = await runStep(step);
    results.push(result);
    process.stderr.write(`[vp9-webrtc-gate] ${step.name}: ok in ${result.elapsedMs} ms\n`);
    if (step.kind === "browser-json" && i < steps.length - 1) {
      process.stderr.write(`[vp9-webrtc-gate] ${step.name}: cooldown ${browserStepCooldownMs} ms\n`);
      await sleep(browserStepCooldownMs);
    }
  }
  console.log(JSON.stringify({
    ok: true,
    elapsedMs: Date.now() - startedAt,
    config: {
      maxAccessUnitMs,
      maxScheduleLagMs,
    },
    results,
  }, null, 2));
}

function numberEnv(name, fallback, opts = {}) {
  if (process.env[name] === undefined || process.env[name] === "") {
    return fallback;
  }
  const value = Number(process.env[name]);
  if (!Number.isFinite(value)) {
    throw new Error(`${name} must be a finite number`);
  }
  if (opts.min !== undefined && value < opts.min) {
    throw new Error(`${name} must be >= ${opts.min}`);
  }
  return value;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function runStep(step) {
  const startedAt = Date.now();
  const output = await runCommand(step.command, step.args, step.env);
  if (step.requiresOracle) {
    assertNoOracleSkips(step, output.stdout);
  }
  return {
    name: step.name,
    command: formatCommand(step),
    elapsedMs: Date.now() - startedAt,
    summary: summarizeStep(step, output.stdout),
  };
}

function summarizeStep(step, stdout) {
  if (step.kind === "browser-json") {
    const report = JSON.parse(stdout);
    const aggregate = report.aggregate || report.summary;
    return {
      decoded: aggregate.decoded,
      clients: aggregate.clients,
      clientRuns: aggregate.clientRuns,
      pauseResume: report.pauseResume,
      receiverStallProbe: report.receiverStallProbe,
      localWithhold: report.localWithhold,
      minClientDecoded: aggregate.minClientDecoded,
      dropped: aggregate.dropped,
      lost: aggregate.lost,
      freezes: aggregate.freezes,
      freezeDuration: aggregate.freezeDuration,
      pauses: aggregate.pauses,
      pauseDuration: aggregate.pauseDuration,
      nacks: aggregate.nacks,
      plis: aggregate.plis,
      firs: aggregate.firs,
      forcedKeys: aggregate.forcedKeys,
      minClientForcedKeys: aggregate.minClientForcedKeys,
      packetizerRecoveries: aggregate.packetizerRecoveries,
      maxSenderWithheldAUs: aggregate.maxSenderWithheldAUs,
      minEndingActiveLayers: aggregate.minEndingActiveLayers ?? aggregate.endingActiveLayers,
      minSampleEndingActiveLayers: aggregate.minSampleEndingActiveLayers,
      minPolledActiveLayers: aggregate.minPolledActiveLayers,
      maxActiveTopLayerThreads: aggregate.maxActiveTopLayerThreads,
      maxActiveTopLayerTileCols: aggregate.maxActiveTopLayerTileCols,
      maxAccessUnitMs: aggregate.maxAccessUnitMs,
      maxScheduleLagMs: aggregate.maxScheduleLagMs,
      maxRxRepairRequests: aggregate.maxRxRepairRequests,
      maxSenderFailedEncodeAUs: aggregate.maxSenderFailedEncodeAUs,
      maxSenderFailedEncodedAUs: aggregate.maxSenderFailedEncodedAUs,
      minRxSpatialCap: aggregate.minRxSpatialCap,
      maxSenderForcedKeys: aggregate.maxSenderForcedKeys,
      maxSenderPacketizerRecoveries: aggregate.maxSenderPacketizerRecoveries,
    };
  }
  return {
    output: stdout.trim().split("\n").filter(Boolean).slice(-3),
  };
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

function runCommand(command, args, extraEnv = null) {
  return new Promise((resolve, reject) => {
    const env = extraEnv ? { ...process.env, ...extraEnv } : process.env;
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"], env });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += chunk.toString();
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString();
    });
    child.on("error", reject);
    child.on("close", (code, signal) => {
      if (code === 0) {
        resolve({ stdout, stderr });
        return;
      }
      const err = new Error(`${command} exited with code ${code}${signal ? ` signal ${signal}` : ""}`);
      err.stdout = stdout;
      err.stderr = stderr;
      process.stderr.write(stderr);
      process.stderr.write(stdout);
      reject(err);
    });
  });
}

function formatCommand(step) {
  const env = step.env
    ? Object.entries(step.env).map(([key, value]) => `${key}=${shellQuote(value)}`)
    : [];
  return [...env, step.command, ...step.args.map(shellQuote)].join(" ");
}

function shellQuote(value) {
  return /^[A-Za-z0-9_./:=|-]+$/.test(value) ? value : JSON.stringify(value);
}

await main();
