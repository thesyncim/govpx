#!/usr/bin/env node

import { spawn } from "node:child_process";

const browserStepCooldownMs = 5000;

const cfg = {
  cpuBurners: numberEnv("VP9_WEBRTC_STRESS_CPU_BURNERS", 12, { min: 0 }),
  serverFPS: numberEnv("VP9_WEBRTC_STRESS_SERVER_FPS", 25, { min: 1 }),
  loadedSoakMs: numberEnv("VP9_WEBRTC_STRESS_LOADED_SOAK_MS", 90000, { min: 10000 }),
  controlSoakMs: numberEnv("VP9_WEBRTC_STRESS_CONTROL_SOAK_MS", 45000, { min: 10000 }),
  withholdSoakMs: numberEnv("VP9_WEBRTC_STRESS_WITHHOLD_SOAK_MS", 20000, { min: 10000 }),
  maxAccessUnitMs: numberEnv("VP9_WEBRTC_STRESS_MAX_ACCESS_UNIT_MS", 200, { min: 1 }),
  maxScheduleLagMs: numberEnv("VP9_WEBRTC_STRESS_MAX_SCHEDULE_LAG_MS", 200, { min: 1 }),
  repeat: numberEnv("VP9_WEBRTC_STRESS_REPEAT", 1, { min: 1 }),
};

const cleanLoadedBudgets = [
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
  "--max-access-unit-ms", String(cfg.maxAccessUnitMs),
  "--max-schedule-lag-ms", String(cfg.maxScheduleLagMs),
  "--min-active-layers", "1",
  "--min-ending-active-layers", "1",
];

const recoveryLoadedBudgets = [
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
  "--max-access-unit-ms", String(cfg.maxAccessUnitMs),
  "--max-schedule-lag-ms", String(cfg.maxScheduleLagMs),
  "--min-active-layers", "1",
  "--min-ending-active-layers", "1",
];

const plainTemporalRecoveryLoadedBudgets = [
  "--sample-ms", "5000",
  "--poll-ms", "1000",
  "--min-decoded-delta", "70",
  "--min-video-time-ratio", "0.8",
  "--max-rx-repair-requests", "0",
  "--max-rx-nack-delta", "0",
  "--max-rx-pli-delta", "0",
  "--max-rx-fir-delta", "0",
  "--max-sender-failed-encode-aus", "0",
  "--max-sender-failed-encoded-aus", "0",
  "--max-access-unit-ms", String(cfg.maxAccessUnitMs),
  "--max-schedule-lag-ms", String(cfg.maxScheduleLagMs),
  "--min-active-layers", "1",
  "--min-ending-active-layers", "1",
];

const partialWriteLoadedBudgets = [
  "--sample-ms", "5000",
  "--poll-ms", "1000",
  "--min-decoded-delta", "80",
  "--min-video-time-ratio", "0.8",
  "--max-rx-repair-requests", "0",
  "--max-rx-nack-delta", "0",
  "--max-rx-pli-delta", "0",
  "--max-rx-fir-delta", "0",
  "--max-sender-failed-encode-aus", "0",
  "--max-sender-failed-encoded-aus", "2",
  "--max-access-unit-ms", String(cfg.maxAccessUnitMs),
  "--max-schedule-lag-ms", String(cfg.maxScheduleLagMs),
  "--min-active-layers", "1",
  "--min-ending-active-layers", "1",
];

const plainTemporalPartialWriteLoadedBudgets = [
  "--sample-ms", "5000",
  "--poll-ms", "1000",
  "--min-decoded-delta", "70",
  "--min-video-time-ratio", "0.8",
  "--max-rx-repair-requests", "0",
  "--max-rx-nack-delta", "0",
  "--max-rx-pli-delta", "0",
  "--max-rx-fir-delta", "0",
  "--max-sender-failed-encode-aus", "0",
  "--max-sender-failed-encoded-aus", "2",
  "--max-access-unit-ms", String(cfg.maxAccessUnitMs),
  "--max-schedule-lag-ms", String(cfg.maxScheduleLagMs),
  "--min-active-layers", "1",
  "--min-ending-active-layers", "1",
];

const steps = [
  {
    name: "browser-loaded-long-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--repeat", String(cfg.repeat),
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.loadedSoakMs),
      ...cleanLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "browser-loaded-control-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--control-churn",
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.controlSoakMs),
      ...recoveryLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "browser-plain-vp9-temporal-loaded-control-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--server-plain-vp9-temporal",
      "--control-churn",
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.controlSoakMs),
      ...plainTemporalRecoveryLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "browser-loaded-withhold-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--local-withhold",
      "--local-withhold-count", "2",
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.withholdSoakMs),
      ...recoveryLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "browser-plain-vp9-temporal-loaded-withhold-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--server-plain-vp9-temporal",
      "--local-withhold",
      "--local-withhold-count", "2",
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.withholdSoakMs),
      ...plainTemporalRecoveryLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "browser-loaded-partial-write-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--local-partial-write",
      "--local-partial-write-count", "2",
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.withholdSoakMs),
      ...partialWriteLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "browser-plain-vp9-temporal-loaded-partial-write-soak",
    command: "node",
    args: [
      "browser_smoke.mjs",
      "--server-plain-vp9-temporal",
      "--local-partial-write",
      "--local-partial-write-count", "2",
      "--cpu-burners", String(cfg.cpuBurners),
      "--server-fps", String(cfg.serverFPS),
      "--soak-ms", String(cfg.withholdSoakMs),
      ...plainTemporalPartialWriteLoadedBudgets,
    ],
    kind: "browser-json",
  },
  {
    name: "libvpx-vpxdec-recovery-oracle",
    command: "go",
    args: [
      "test",
      "-v",
      "-tags", "govpx_oracle_trace",
      ".",
      "-run",
      "Test(PlainVP9WebRTCPacketizerRecoveryAfter(PacketizedUnsent|PartialWrite)AccessUnits|VP9WebRTCPacketizerSVCNonFlexibleRecoveryAfter(ConsecutivePacketizedUnsentAccessUnits|PartialWriteAccessUnit))DecodesWithVpxdec",
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
    process.stderr.write(`[vp9-webrtc-stress] ${step.name}: ${formatCommand(step)}\n`);
    const result = await runStep(step);
    results.push(result);
    process.stderr.write(`[vp9-webrtc-stress] ${step.name}: ok in ${result.elapsedMs} ms\n`);
    if (step.kind === "browser-json" && i < steps.length - 1) {
      process.stderr.write(`[vp9-webrtc-stress] ${step.name}: cooldown ${browserStepCooldownMs} ms\n`);
      await sleep(browserStepCooldownMs);
    }
  }
  console.log(JSON.stringify({
    ok: true,
    elapsedMs: Date.now() - startedAt,
    config: cfg,
    results,
  }, null, 2));
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
      localWithhold: report.localWithhold,
      localPartialWrite: report.localPartialWrite,
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
      maxSenderPartialWriteAUs: aggregate.maxSenderPartialWriteAUs,
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
  if (/^[A-Za-z0-9_./:=+-]+$/.test(value)) {
    return value;
  }
  return JSON.stringify(value);
}

function numberEnv(name, fallback, opts = {}) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  const value = Number(raw);
  if (!Number.isFinite(value) || !Number.isInteger(value)) {
    throw new Error(`${name} must be an integer`);
  }
  if (opts.min !== undefined && value < opts.min) {
    throw new Error(`${name} must be >= ${opts.min}`);
  }
  if (opts.max !== undefined && value > opts.max) {
    throw new Error(`${name} must be <= ${opts.max}`);
  }
  return value;
}

await main();
