#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";

async function main() {
  const sampleMs = numberFlag("--sample-ms", 5000);
  const soakMs = numberFlag("--soak-ms", sampleMs);
  const pollMs = numberFlag("--poll-ms", Math.min(sampleMs, 500));
  const timeoutMs = numberFlag("--timeout-ms", 45000);
  const minDecodedDelta = numberFlag("--min-decoded-delta", 30);
  const serverFPS = optionalNumberFlag("--server-fps");
  const serverBitrateKbps = optionalNumberFlag("--server-bitrate-kbps");
  const cpuBurners = optionalNumberFlag("--cpu-burners") ?? 0;
  const minActiveLayers = optionalNumberFlag("--min-active-layers");
  const minEndingActiveLayers = optionalNumberFlag("--min-ending-active-layers");
  const maxActiveLayerChanges = optionalNumberFlag("--max-active-layer-changes");
  const serverProcessGroup = process.platform !== "win32";
  const chromePath = findChrome();
  const port = await freePort();
  const url = `http://127.0.0.1:${port}/`;
  const serverArgs = ["run", ".", "-addr", `127.0.0.1:${port}`];
  if (serverFPS !== null) serverArgs.push("-fps", String(serverFPS));
  if (serverBitrateKbps !== null) {
    serverArgs.push("-bitrate", String(serverBitrateKbps));
  }
  const loadProcesses = startCPUBurners(cpuBurners);
  const server = spawn("go", serverArgs, {
    stdio: ["ignore", "pipe", "pipe"],
    detached: serverProcessGroup,
  });
  const tempProfile = await mkdtemp(join(tmpdir(), "govpx-vp9-browser-"));
  let chrome = null;
  let cdp = null;

  try {
    await waitForHTTP(url, timeoutMs);
    chrome = await launchChrome(chromePath, tempProfile);
    cdp = await CDP.connect(chrome.wsURL);
    const target = await cdp.send("Target.createTarget", { url: "about:blank" });
    const attached = await cdp.send("Target.attachToTarget", {
      targetId: target.targetId,
      flatten: true,
    });
    const sessionId = attached.sessionId;
    await cdp.send("Page.enable", {}, sessionId);
    await cdp.send("Runtime.enable", {}, sessionId);
    await cdp.send("Page.navigate", { url }, sessionId);

    const first = await waitForDecodedStats(cdp, sessionId, timeoutMs);
    let previous = first;
    const samples = [];
    const sampleCount = Math.max(1, Math.ceil(soakMs / sampleMs));
    for (let i = 0; i < sampleCount; i++) {
      const observations = await collectIntervalStats(cdp, sessionId, sampleMs, pollMs);
      const current = observations[observations.length - 1];
      const delta = diffStats(previous, current);
      const summary = summarizeInterval([previous, ...observations]);
      assertSmoke(previous, current, delta, {
        intervalMs: sampleMs,
        maxActiveLayerChanges,
        minEndingActiveLayers,
        minActiveLayers,
        minDecodedDelta,
        sampleIndex: i + 1,
        summary,
      });
      samples.push({
        intervalMs: sampleMs,
        elapsedMs: (i + 1) * sampleMs,
        summary,
        first: previous,
        second: current,
        delta,
      });
      previous = current;
    }
    const second = previous;
    const delta = diffStats(first, second);
    console.log(JSON.stringify({
      url,
      sampleMs,
      soakMs,
      pollMs,
      serverFPS,
      serverBitrateKbps,
      cpuBurners,
      minActiveLayers,
      minEndingActiveLayers,
      maxActiveLayerChanges,
      samples,
      first,
      second,
      delta,
    }, null, 2));
  } finally {
    if (cdp) cdp.close();
    if (chrome) {
      await stopProcess(chrome.process);
    }
    await stopProcess(server, "SIGTERM", serverProcessGroup);
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

function numberFlag(name, fallback) {
  const idx = process.argv.indexOf(name);
  if (idx < 0) return fallback;
  const value = Number(process.argv[idx + 1]);
  if (!Number.isFinite(value) || value <= 0) {
    throw new Error(`${name} must be positive`);
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
        rxRepairRequests: num(rows["rx repair"]),
        rxSpatialCap: num(rows["rx cap"]),
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
  if (delta.videoTime === null || delta.videoTime < opts.intervalMs / 1000 * 0.7) {
    throw new Error(`${sampleLabel(opts)} video time did not advance enough: ${delta.videoTime}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  for (const key of ["rxLost", "rxDropped", "rxFreezes"]) {
    if (delta[key] !== null && delta[key] !== 0) {
      throw new Error(`${sampleLabel(opts)} ${key} changed during clean smoke: ${delta[key]}; ${sampleDetails(first, second, delta, opts.summary)}`);
    }
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
  return opts && opts.sampleIndex ? `sample ${opts.sampleIndex}:` : "sample:";
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
