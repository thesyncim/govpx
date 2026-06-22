#!/usr/bin/env node

import { spawn } from "node:child_process";

const oraclePattern = [
  "TestWebRTCEndToEnd.*Vpxdec",
  "TestWebRTCPacketizedSVC.*Vpxdec",
  "TestVP9WebRTCPacketizerSVC.*Vpxdec",
].join("|");

const steps = [
  {
    name: "focused-go",
    command: "go",
    args: [
      "test",
      ".",
      "-run",
      "TestSpatialCapBackoff|TestReadmeDocumentsStatefulVP9WebRTCPacketizer|TestIndexHTMLExposesBrowserRTCStatsForFreezeDiagnosis",
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
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
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
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
      "--require-threaded-top-layer",
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
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      "--min-active-layers", "2",
      "--min-ending-active-layers", "2",
      "--require-threaded-top-layer",
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
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
      "--require-threaded-top-layer",
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
      "--max-sender-failed-encode-aus", "0",
      "--max-sender-failed-encoded-aus", "0",
      "--min-active-layers", "1",
      "--min-ending-active-layers", "1",
      "--require-threaded-top-layer",
    ],
    kind: "browser-json",
  },
  {
    name: "libvpx-vpxdec-oracle",
    command: "go",
    args: [
      "test",
      "-tags", "govpx_oracle_trace",
      ".",
      "-run", oraclePattern,
      "-count=1",
    ],
    kind: "go-test",
  },
];

async function main() {
  const startedAt = Date.now();
  const results = [];
  for (const step of steps) {
    process.stderr.write(`[vp9-webrtc-gate] ${step.name}: ${formatCommand(step)}\n`);
    const result = await runStep(step);
    results.push(result);
    process.stderr.write(`[vp9-webrtc-gate] ${step.name}: ok in ${result.elapsedMs} ms\n`);
  }
  console.log(JSON.stringify({
    ok: true,
    elapsedMs: Date.now() - startedAt,
    results,
  }, null, 2));
}

async function runStep(step) {
  const startedAt = Date.now();
  const output = await runCommand(step.command, step.args);
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
      minClientDecoded: aggregate.minClientDecoded,
      dropped: aggregate.dropped,
      lost: aggregate.lost,
      freezes: aggregate.freezes,
      forcedKeys: aggregate.forcedKeys,
      minClientForcedKeys: aggregate.minClientForcedKeys,
      packetizerRecoveries: aggregate.packetizerRecoveries,
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

function runCommand(command, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"] });
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
  return [step.command, ...step.args.map(shellQuote)].join(" ");
}

function shellQuote(value) {
  return /^[A-Za-z0-9_./:=|-]+$/.test(value) ? value : JSON.stringify(value);
}

await main();
