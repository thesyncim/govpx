#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";

async function main() {
  const sampleMs = numberFlag("--sample-ms", 5000);
  const timeoutMs = numberFlag("--timeout-ms", 45000);
  const minDecodedDelta = numberFlag("--min-decoded-delta", 30);
  const serverProcessGroup = process.platform !== "win32";
  const chromePath = findChrome();
  const port = await freePort();
  const url = `http://127.0.0.1:${port}/`;
  const server = spawn("go", ["run", ".", "-addr", `127.0.0.1:${port}`], {
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
    await sleep(sampleMs);
    const second = await readStats(cdp, sessionId);
    const delta = diffStats(first, second);
    assertSmoke(first, second, delta, { minDecodedDelta, sampleMs });
    console.log(JSON.stringify({ url, sampleMs, first, second, delta }, null, 2));
  } finally {
    if (cdp) cdp.close();
    if (chrome) {
      await stopProcess(chrome.process);
    }
    await stopProcess(server, "SIGTERM", serverProcessGroup);
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
      return {
        status: document.getElementById("status")?.textContent ?? null,
        frame: num(rows["frame #"]),
        activeLayers: raw.settings?.active_spatial_layers ?? null,
        requestedLayers: raw.settings?.requested_spatial_layers ?? null,
        fps: num(rows["fps"]),
        lagMs: num(rows["lag ms"]),
        rxDecoded: num(rows["rx decoded"]),
        rxDropped: num(rows["rx dropped"]),
        rxLost: num(rows["rx lost"]),
        rxFreezes: num(rows["rx freezes"]),
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
    throw new Error(`peer did not stay connected: ${second.status}`);
  }
  if (delta.rxDecoded === null || delta.rxDecoded < opts.minDecodedDelta) {
    throw new Error(`decoded frames did not advance enough: ${delta.rxDecoded}`);
  }
  if (delta.videoTime === null || delta.videoTime < opts.sampleMs / 1000 * 0.7) {
    throw new Error(`video time did not advance enough: ${delta.videoTime}`);
  }
  for (const key of ["rxLost", "rxDropped", "rxFreezes"]) {
    if (delta[key] !== null && delta[key] !== 0) {
      throw new Error(`${key} changed during clean smoke: ${delta[key]}`);
    }
  }
  if (second.videoWidth <= 0 || second.videoHeight <= 0) {
    throw new Error(`video dimensions are invalid: ${second.videoWidth}x${second.videoHeight}`);
  }
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
