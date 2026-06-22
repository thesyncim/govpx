#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";

async function main() {
  const opts = parseOptions();
  const runs = [];
  for (let i = 0; i < opts.repeat; i++) {
    runs.push(await runSmoke(opts, i + 1));
  }
  if (opts.repeat === 1) {
    console.log(JSON.stringify(runs[0], null, 2));
    return;
  }
  console.log(JSON.stringify({
    repeat: opts.repeat,
    aggregate: summarizeRuns(runs),
    runs,
  }, null, 2));
}

function parseOptions() {
  const sampleMs = numberFlag("--sample-ms", 5000);
  return {
    sampleMs,
    soakMs: numberFlag("--soak-ms", sampleMs),
    pollMs: numberFlag("--poll-ms", Math.min(sampleMs, 500)),
    timeoutMs: numberFlag("--timeout-ms", 45000),
    minDecodedDelta: numberFlag("--min-decoded-delta", 30),
    minVideoTimeRatio: numberFlag("--min-video-time-ratio", 0.7),
    maxRxRepairRequests: numberFlag("--max-rx-repair-requests", 0, { min: 0 }),
    clients: integerFlag("--clients", 1, { min: 1 }),
    repeat: numberFlag("--repeat", 1),
    serverFPS: optionalNumberFlag("--server-fps"),
    serverBitrateKbps: optionalNumberFlag("--server-bitrate-kbps"),
    cpuBurners: optionalNumberFlag("--cpu-burners") ?? 0,
    minActiveLayers: optionalNumberFlag("--min-active-layers"),
    minEndingActiveLayers: optionalNumberFlag("--min-ending-active-layers"),
    maxActiveLayerChanges: optionalNumberFlag("--max-active-layer-changes"),
    serverProcessGroup: process.platform !== "win32",
    chromePath: findChrome(),
  };
}

async function runSmoke(opts, runIndex) {
  const port = await freePort();
  const url = `http://127.0.0.1:${port}/`;
  const serverArgs = ["run", ".", "-addr", `127.0.0.1:${port}`];
  if (opts.serverFPS !== null) serverArgs.push("-fps", String(opts.serverFPS));
  if (opts.serverBitrateKbps !== null) {
    serverArgs.push("-bitrate", String(opts.serverBitrateKbps));
  }
  const loadProcesses = startCPUBurners(opts.cpuBurners);
  const server = spawn("go", serverArgs, {
    stdio: ["ignore", "pipe", "pipe"],
    detached: opts.serverProcessGroup,
  });
  const tempProfile = await mkdtemp(join(tmpdir(), "govpx-vp9-browser-"));
  let chrome = null;
  let cdp = null;

  try {
    await waitForHTTP(url, opts.timeoutMs);
    chrome = await launchChrome(opts.chromePath, tempProfile);
    cdp = await CDP.connect(chrome.wsURL);
    const clients = [];
    for (let i = 0; i < opts.clients; i++) {
      clients.push(await createBrowserClient(cdp, url, i + 1));
    }

    const firstByClient = await Promise.all(clients.map((client) =>
      waitForDecodedStats(cdp, client.sessionId, opts.timeoutMs)
    ));
    let previousByClient = firstByClient;
    const samples = [];
    const sampleCount = Math.max(1, Math.ceil(opts.soakMs / opts.sampleMs));
    for (let i = 0; i < sampleCount; i++) {
      const observationsByClient = await Promise.all(clients.map((client) =>
        collectIntervalStats(cdp, client.sessionId, opts.sampleMs, opts.pollMs)
      ));
      const sampleClients = [];
      const currentByClient = [];
      for (let clientIndex = 0; clientIndex < clients.length; clientIndex++) {
        const previous = previousByClient[clientIndex];
        const observations = observationsByClient[clientIndex];
        const current = observations[observations.length - 1];
        currentByClient[clientIndex] = current;
        const delta = diffStats(previous, current);
        const summary = summarizeInterval([previous, ...observations]);
        assertSmoke(previous, current, delta, {
          intervalMs: opts.sampleMs,
          maxActiveLayerChanges: opts.maxActiveLayerChanges,
          minEndingActiveLayers: opts.minEndingActiveLayers,
          minActiveLayers: opts.minActiveLayers,
          minDecodedDelta: opts.minDecodedDelta,
          minVideoTimeRatio: opts.minVideoTimeRatio,
          maxRxRepairRequests: opts.maxRxRepairRequests,
          runIndex,
          clientIndex: clientIndex + 1,
          sampleIndex: i + 1,
          summary,
        });
        sampleClients.push({
          client: clientIndex + 1,
          summary,
          first: previous,
          second: current,
          delta,
        });
      }
      samples.push({
        intervalMs: opts.sampleMs,
        elapsedMs: (i + 1) * opts.sampleMs,
        summary: summarizeSampleClients(sampleClients),
        clients: sampleClients,
        first: sampleClients[0].first,
        second: sampleClients[0].second,
        delta: sampleClients[0].delta,
      });
      previousByClient = currentByClient;
    }
    const secondByClient = previousByClient;
    const deltaByClient = firstByClient.map((first, i) =>
      diffStats(first, secondByClient[i])
    );
    return {
      run: runIndex,
      url,
      clients: opts.clients,
      sampleMs: opts.sampleMs,
      soakMs: opts.soakMs,
      pollMs: opts.pollMs,
      serverFPS: opts.serverFPS,
      serverBitrateKbps: opts.serverBitrateKbps,
      cpuBurners: opts.cpuBurners,
      minDecodedDelta: opts.minDecodedDelta,
      minVideoTimeRatio: opts.minVideoTimeRatio,
      maxRxRepairRequests: opts.maxRxRepairRequests,
      minActiveLayers: opts.minActiveLayers,
      minEndingActiveLayers: opts.minEndingActiveLayers,
      maxActiveLayerChanges: opts.maxActiveLayerChanges,
      samples,
      first: firstByClient[0],
      second: secondByClient[0],
      delta: deltaByClient[0],
      clientResults: firstByClient.map((first, i) => ({
        client: i + 1,
        first,
        second: secondByClient[i],
        delta: deltaByClient[i],
      })),
      summary: summarizeRun(samples, deltaByClient, secondByClient),
    };
  } finally {
    if (cdp) cdp.close();
    if (chrome) {
      await stopProcess(chrome.process);
    }
    await stopProcess(server, "SIGTERM", opts.serverProcessGroup);
    await stopProcesses(loadProcesses);
    try {
      await rm(tempProfile, {
        recursive: true,
        force: true,
        maxRetries: 5,
        retryDelay: 100,
      });
    } catch (err) {
      console.warn(`warning: temp profile cleanup failed: ${err.message}`);
    }
  }
}

async function createBrowserClient(cdp, url, clientIndex) {
  const target = await cdp.send("Target.createTarget", { url: "about:blank" });
  const attached = await cdp.send("Target.attachToTarget", {
    targetId: target.targetId,
    flatten: true,
  });
  const sessionId = attached.sessionId;
  await cdp.send("Page.enable", {}, sessionId);
  await cdp.send("Runtime.enable", {}, sessionId);
  await cdp.send("Page.navigate", { url }, sessionId);
  return { client: clientIndex, targetId: target.targetId, sessionId };
}

function numberFlag(name, fallback, opts = {}) {
  const idx = process.argv.indexOf(name);
  if (idx < 0) return fallback;
  const value = Number(process.argv[idx + 1]);
  if (!Number.isFinite(value)) {
    throw new Error(`${name} must be a finite number`);
  }
  if (opts.min !== undefined) {
    if (value < opts.min) {
      throw new Error(`${name} must be >= ${opts.min}`);
    }
    return value;
  }
  if (value <= 0) {
    throw new Error(`${name} must be positive`);
  }
  return value;
}

function integerFlag(name, fallback, opts = {}) {
  const value = numberFlag(name, fallback, opts);
  if (!Number.isInteger(value)) {
    throw new Error(`${name} must be an integer`);
  }
  return value;
}

function optionalNumberFlag(name) {
  const idx = process.argv.indexOf(name);
  if (idx < 0) return null;
  const value = Number(process.argv[idx + 1]);
  if (!Number.isFinite(value) || value < 0) {
    throw new Error(`${name} must be non-negative`);
  }
  return value;
}

function startCPUBurners(count) {
  if (count <= 0) return [];
  const script = "let x = 0; for (;;) { x = (x + Math.sqrt(x + 1)) % 1000000; }";
  const children = [];
  for (let i = 0; i < count; i++) {
    children.push(spawn(process.execPath, ["-e", script], {
      stdio: ["ignore", "ignore", "ignore"],
    }));
  }
  return children;
}

function findChrome() {
  const candidates = [
    process.env.CHROME,
    process.env.CHROME_PATH,
    process.env.CHROME_BIN,
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "/usr/bin/google-chrome",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
  ].filter(Boolean);
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate;
  }
  throw new Error("Chrome not found; set CHROME=/path/to/chrome");
}

async function freePort() {
  return await new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const port = srv.address().port;
      srv.close(() => resolve(port));
    });
  });
}

async function waitForHTTP(target, timeout) {
  const deadline = Date.now() + timeout;
  let lastErr = null;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(target);
      if (res.ok) return;
      lastErr = new Error(`HTTP ${res.status}`);
    } catch (err) {
      lastErr = err;
    }
    await sleep(250);
  }
  throw new Error(`server did not become ready: ${lastErr}`);
}

async function launchChrome(bin, profile) {
  const args = [
    "--headless=new",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "--autoplay-policy=no-user-gesture-required",
    "--disable-background-timer-throttling",
    "--disable-backgrounding-occluded-windows",
    "--disable-renderer-backgrounding",
    "--no-first-run",
    "about:blank",
  ];
  const child = spawn(bin, args, { stdio: ["ignore", "pipe", "pipe"] });
  const wsURL = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("Chrome DevTools endpoint timeout")), 15000);
    child.stderr.on("data", (chunk) => {
      const text = chunk.toString();
      const match = text.match(/DevTools listening on (ws:\/\/[^\s]+)/);
      if (match) {
        clearTimeout(timer);
        resolve(match[1]);
      }
    });
    child.on("exit", (code) => {
      clearTimeout(timer);
      reject(new Error(`Chrome exited before DevTools endpoint: ${code}`));
    });
  });
  return { process: child, wsURL };
}

async function waitForDecodedStats(cdp, sessionId, timeout) {
  const deadline = Date.now() + timeout;
  let latest = null;
  while (Date.now() < deadline) {
    latest = await readStats(cdp, sessionId);
    if (
      latest.rxDecoded !== null &&
      latest.rxDecoded > 0 &&
      latest.videoReadyState >= 2 &&
      latest.videoTime > 0
    ) {
      return latest;
    }
    await sleep(500);
  }
  throw new Error(`decode stats did not become ready: ${JSON.stringify(latest)}`);
}

async function collectIntervalStats(cdp, sessionId, intervalMs, pollMs) {
  const out = [];
  const started = Date.now();
  while (Date.now() - started < intervalMs) {
    const remaining = intervalMs - (Date.now() - started);
    await sleep(Math.max(1, Math.min(pollMs, remaining)));
    out.push(await readStats(cdp, sessionId));
  }
  return out;
}

async function readStats(cdp, sessionId) {
  const result = await cdp.send("Runtime.evaluate", {
    expression: `(() => {
      const rows = Object.fromEntries(Array.from(document.querySelectorAll("#totals dt")).map((dt) => [dt.textContent, dt.nextElementSibling?.textContent ?? ""]));
      const rawText = document.getElementById("raw")?.textContent || "{}";
      let raw = {};
      try { raw = JSON.parse(rawText); } catch {}
      const v = document.getElementById("v");
      const num = (value) => {
        const n = Number(value);
        return Number.isFinite(n) ? n : null;
      };
      const layers = Array.isArray(raw.layers) ? raw.layers : [];
      const activeTop = layers.length > 0 ? layers[layers.length - 1] : {};
      const sender = raw.sender || {};
      const repairRequests = typeof receiverRepairRequests === "number" ? receiverRepairRequests : num(rows["rx repair"]);
      const repairStreak = typeof receiverRepairStreak === "number" ? receiverRepairStreak : null;
      const receiverSpatialCap = typeof receiverRequestedSpatialCap === "number" ? receiverRequestedSpatialCap : num(rows["rx cap"]);
      return {
        status: document.getElementById("status")?.textContent ?? null,
        frame: num(rows["frame #"]),
        activeLayers: raw.settings?.active_spatial_layers ?? null,
        requestedLayers: raw.settings?.requested_spatial_layers ?? null,
        activeTopLayerSP: num(activeTop.sp),
        activeTopLayerThreads: num(activeTop.threads),
        activeTopLayerTileCols: num(activeTop.tile_cols),
        activeTopLayerRowMT: activeTop.row_mt ?? null,
        fps: num(rows["fps"]),
        lagMs: num(rows["lag ms"]),
        encodeMs: num(sender.encode_ms),
        accessUnitMs: num(sender.access_unit_ms),
        scheduleLagMs: num(sender.schedule_lag_ms),
        senderSpatialCapMax: num(sender.spatial_cap_max),
        senderCapOverrunStreak: num(sender.spatial_cap_overrun_streak),
        senderCapRecoveryStreak: num(sender.spatial_cap_recovery_streak),
        rxDecoded: num(rows["rx decoded"]),
        rxDropped: num(rows["rx dropped"]),
        rxLost: num(rows["rx lost"]),
        rxFreezes: num(rows["rx freezes"]),
        rxRepairRequests: repairRequests ?? 0,
        rxRepairStreak: repairStreak,
        rxSpatialCap: receiverSpatialCap,
        videoReadyState: v?.readyState ?? null,
        videoTime: v?.currentTime ?? null,
        videoWidth: v?.videoWidth ?? null,
        videoHeight: v?.videoHeight ?? null
      };
    })()`,
    returnByValue: true,
    awaitPromise: true,
  }, sessionId);
  return result.result.value;
}

function summarizeInterval(stats) {
  const values = (key) => stats.map((s) => s[key]).filter(Number.isFinite);
  const activeLayers = values("activeLayers");
  return {
    observations: stats.length,
    minActiveLayers: minNumber(activeLayers),
    maxActiveLayers: maxNumber(activeLayers),
    activeLayerChanges: countChanges(activeLayers),
    maxEncodeMs: maxNumber(values("encodeMs")),
    maxAccessUnitMs: maxNumber(values("accessUnitMs")),
    maxScheduleLagMs: maxNumber(values("scheduleLagMs")),
    maxSenderCapOverrunStreak: maxNumber(values("senderCapOverrunStreak")),
    maxSenderCapRecoveryStreak: maxNumber(values("senderCapRecoveryStreak")),
    maxRxRepairRequests: maxNumber(values("rxRepairRequests")),
    minRxSpatialCap: minNumber(values("rxSpatialCap")),
  };
}

function summarizeSampleClients(sampleClients) {
  const summaries = sampleClients.map((client) => client.summary);
  const seconds = sampleClients.map((client) => client.second);
  const deltas = sampleClients.map((client) => client.delta);
  return summarizeStatsGroup(summaries, deltas, seconds, seconds);
}

function summarizeRun(samples, deltas, seconds) {
  const sampleClients = samples.flatMap((sample) =>
    Array.isArray(sample.clients) ? sample.clients : [sample]
  );
  const summaries = sampleClients.map((client) => client.summary);
  const sampleSeconds = sampleClients.map((client) => client.second);
  return summarizeStatsGroup(summaries, deltas, seconds, sampleSeconds);
}

function summarizeStatsGroup(summaries, deltas, seconds, sampleSeconds) {
  deltas = Array.isArray(deltas) ? deltas : [deltas];
  seconds = Array.isArray(seconds) ? seconds : [seconds];
  sampleSeconds = Array.isArray(sampleSeconds) ? sampleSeconds : seconds;
  const deltaSum = (key) => deltas.reduce((total, delta) =>
    total + (Number.isFinite(delta?.[key]) ? delta[key] : 0), 0);
  const deltaValues = (key) => deltas.map((delta) => delta?.[key]).filter(Number.isFinite);
  const summaryValues = (key) => summaries.map((summary) => summary?.[key]).filter(Number.isFinite);
  const secondValues = (key) => seconds.map((second) => second?.[key]).filter(Number.isFinite);
  const sampleSecondValues = (key) => sampleSeconds.map((second) => second?.[key]).filter(Number.isFinite);
  return {
    clients: deltas.length,
    decoded: deltaSum("rxDecoded"),
    minClientDecoded: minNumber(deltaValues("rxDecoded")),
    dropped: deltaSum("rxDropped"),
    lost: deltaSum("rxLost"),
    freezes: deltaSum("rxFreezes"),
    videoTime: deltaSum("videoTime"),
    minClientVideoTime: minNumber(deltaValues("videoTime")),
    endingActiveLayers: minNumber(secondValues("activeLayers")),
    minSampleEndingActiveLayers: minNumber(sampleSecondValues("activeLayers")),
    minPolledActiveLayers: minNumber(summaryValues("minActiveLayers")),
    maxAccessUnitMs: maxNumber(summaryValues("maxAccessUnitMs")),
    maxScheduleLagMs: maxNumber(summaryValues("maxScheduleLagMs")),
    maxRxRepairRequests: maxNumber(summaryValues("maxRxRepairRequests")),
    minRxSpatialCap: minNumber(summaryValues("minRxSpatialCap")),
  };
}

function summarizeRuns(runs) {
  const summaries = runs.map((run) => run.summary);
  const sum = (key) => summaries.reduce((total, s) => total + (Number.isFinite(s[key]) ? s[key] : 0), 0);
  const values = (key) => summaries.map((s) => s[key]).filter(Number.isFinite);
  return {
    runs: runs.length,
    clients: maxNumber(values("clients")),
    clientRuns: sum("clients"),
    decoded: sum("decoded"),
    minClientDecoded: minNumber(values("minClientDecoded")),
    dropped: sum("dropped"),
    lost: sum("lost"),
    freezes: sum("freezes"),
    videoTime: sum("videoTime"),
    minClientVideoTime: minNumber(values("minClientVideoTime")),
    minEndingActiveLayers: minNumber(values("endingActiveLayers")),
    minSampleEndingActiveLayers: minNumber(values("minSampleEndingActiveLayers")),
    minPolledActiveLayers: minNumber(values("minPolledActiveLayers")),
    maxAccessUnitMs: maxNumber(values("maxAccessUnitMs")),
    maxScheduleLagMs: maxNumber(values("maxScheduleLagMs")),
    maxRxRepairRequests: maxNumber(values("maxRxRepairRequests")),
    minRxSpatialCap: minNumber(values("minRxSpatialCap")),
  };
}

function minNumber(values) {
  return values.length === 0 ? null : Math.min(...values);
}

function maxNumber(values) {
  return values.length === 0 ? null : Math.max(...values);
}

function countChanges(values) {
  let changes = 0;
  for (let i = 1; i < values.length; i++) {
    if (values[i] !== values[i - 1]) changes++;
  }
  return changes;
}

function diffStats(first, second) {
  const delta = {};
  for (const key of ["frame", "rxDecoded", "rxDropped", "rxLost", "rxFreezes", "videoTime"]) {
    delta[key] = numericDelta(first[key], second[key]);
  }
  return delta;
}

function numericDelta(a, b) {
  return Number.isFinite(a) && Number.isFinite(b) ? b - a : null;
}

function assertSmoke(first, second, delta, opts) {
  if (second.status !== "peer: connected | dc open") {
    throw new Error(`${sampleLabel(opts)} peer did not stay connected: ${second.status}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (delta.rxDecoded === null || delta.rxDecoded < opts.minDecodedDelta) {
    throw new Error(`${sampleLabel(opts)} decoded frames did not advance enough: ${delta.rxDecoded}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (delta.videoTime === null ||
      delta.videoTime < opts.intervalMs / 1000 * opts.minVideoTimeRatio) {
    throw new Error(`${sampleLabel(opts)} video time did not advance enough: ${delta.videoTime}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  for (const key of ["rxLost", "rxDropped", "rxFreezes"]) {
    if (delta[key] !== null && delta[key] !== 0) {
      throw new Error(`${sampleLabel(opts)} ${key} changed during clean smoke: ${delta[key]}; ${sampleDetails(first, second, delta, opts.summary)}`);
    }
  }
  if (
    Number.isFinite(opts.summary.maxRxRepairRequests) &&
    opts.summary.maxRxRepairRequests > opts.maxRxRepairRequests
  ) {
    throw new Error(`${sampleLabel(opts)} receiver repair requests reached ${opts.summary.maxRxRepairRequests}, want <= ${opts.maxRxRepairRequests}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (second.videoWidth <= 0 || second.videoHeight <= 0) {
    throw new Error(`${sampleLabel(opts)} video dimensions are invalid: ${second.videoWidth}x${second.videoHeight}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (
    opts.minActiveLayers !== null &&
    opts.summary.minActiveLayers !== null &&
    opts.summary.minActiveLayers < opts.minActiveLayers
  ) {
    throw new Error(`${sampleLabel(opts)} active layers dropped to ${opts.summary.minActiveLayers}, want >= ${opts.minActiveLayers}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (
    opts.minEndingActiveLayers !== null &&
    Number.isFinite(second.activeLayers) &&
    second.activeLayers < opts.minEndingActiveLayers
  ) {
    throw new Error(`${sampleLabel(opts)} ending active layers ${second.activeLayers}, want >= ${opts.minEndingActiveLayers}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (
    opts.maxActiveLayerChanges !== null &&
    opts.summary.activeLayerChanges > opts.maxActiveLayerChanges
  ) {
    throw new Error(`${sampleLabel(opts)} active layers changed ${opts.summary.activeLayerChanges} times, want <= ${opts.maxActiveLayerChanges}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
}

function sampleLabel(opts) {
  if (!opts) return "sample:";
  const run = opts.runIndex ? `run ${opts.runIndex} ` : "";
  const client = opts.clientIndex ? `client ${opts.clientIndex} ` : "";
  const sample = opts.sampleIndex ? `sample ${opts.sampleIndex}` : "sample";
  return `${run}${client}${sample}:`;
}

function sampleDetails(first, second, delta, summary) {
  return JSON.stringify({ summary, first, second, delta });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function stopProcess(child, signal = "SIGTERM", processGroup = false) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  signalProcess(child, signal, processGroup);
  await new Promise((resolve) => {
    const timer = setTimeout(resolve, 2000);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function stopProcesses(children) {
  await Promise.all(children.map((child) => stopProcess(child)));
}

function signalProcess(child, signal, processGroup) {
  try {
    if (processGroup && child.pid) {
      process.kill(-child.pid, signal);
    } else {
      child.kill(signal);
    }
  } catch (err) {
    if (err.code !== "ESRCH") throw err;
  }
}

class CDP {
  constructor(socket) {
    this.socket = socket;
    this.nextID = 1;
    this.pending = new Map();
    socket.addEventListener("message", (event) => {
      const msg = JSON.parse(event.data);
      if (!msg.id) return;
      const pending = this.pending.get(msg.id);
      if (!pending) return;
      this.pending.delete(msg.id);
      if (msg.error) {
        pending.reject(new Error(JSON.stringify(msg.error)));
      } else {
        pending.resolve(msg.result ?? {});
      }
    });
  }

  static async connect(url) {
    const socket = new WebSocket(url);
    await new Promise((resolve, reject) => {
      socket.addEventListener("open", resolve, { once: true });
      socket.addEventListener("error", reject, { once: true });
    });
    return new CDP(socket);
  }

  send(method, params = {}, sessionId = undefined) {
    const id = this.nextID++;
    const msg = { id, method, params };
    if (sessionId) msg.sessionId = sessionId;
    this.socket.send(JSON.stringify(msg));
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });
  }

  close() {
    this.socket.close();
  }
}

await main();
