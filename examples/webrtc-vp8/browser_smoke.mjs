#!/usr/bin/env node

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";

async function main() {
  const opts = parseOptions();
  const runs = [];
  for (let i = 0; i < opts.repeat; i++) {
    runs.push(await runSmoke(opts, i + 1));
  }
  const result = opts.repeat === 1
    ? runs[0]
    : { repeat: opts.repeat, aggregate: summarizeRuns(runs), runs };
  console.log(JSON.stringify(result, null, 2));
}

function parseOptions() {
  const sampleMs = integerFlag("--sample-ms", 5000, { min: 1 });
  return {
    sampleMs,
    soakMs: integerFlag("--soak-ms", sampleMs, { min: 1 }),
    pollMs: integerFlag("--poll-ms", Math.min(sampleMs, 1000), { min: 1 }),
    timeoutMs: integerFlag("--timeout-ms", 45000, { min: 1 }),
    cdpTimeoutMs: integerFlag("--cdp-timeout-ms", 15000, { min: 1000 }),
    minDecodedDelta: integerFlag("--min-decoded-delta", 30, { min: 0 }),
    minVideoTimeRatio: numberFlag("--min-video-time-ratio", 0.7, { min: 0 }),
    maxRxLostDelta: integerFlag("--max-rx-lost-delta", 0, { min: 0 }),
    maxRxDroppedDelta: integerFlag("--max-rx-dropped-delta", 0, { min: 0 }),
    maxRxFreezesDelta: integerFlag("--max-rx-freezes-delta", 0, { min: 0 }),
    maxRxFreezeDurationDelta: numberFlag("--max-rx-freeze-duration-delta", 0, { min: 0 }),
    maxRxRepairDelta: integerFlag("--max-rx-repair-delta", 0, { min: 0 }),
    maxRxNackDelta: integerFlag("--max-rx-nack-delta", 0, { min: 0 }),
    maxRxPliDelta: integerFlag("--max-rx-pli-delta", 0, { min: 0 }),
    maxRxFirDelta: integerFlag("--max-rx-fir-delta", 0, { min: 0 }),
    expectedRenditions: integerFlag("--renditions", 3, { min: 1 }),
    clients: integerFlag("--clients", 1, { min: 1 }),
    repeat: integerFlag("--repeat", 1, { min: 1 }),
    serverFPS: optionalIntegerFlag("--server-fps", { min: 1 }),
    serverLowKbps: optionalIntegerFlag("--server-low-kbps", { min: 1 }),
    serverMidKbps: optionalIntegerFlag("--server-mid-kbps", { min: 1 }),
    serverHighKbps: optionalIntegerFlag("--server-high-kbps", { min: 1 }),
    localWithhold: booleanFlag("--local-withhold"),
    localWithholdCount: integerFlag("--local-withhold-count", 1, { min: 1, max: 10 }),
    cpuBurners: integerFlag("--cpu-burners", 0, { min: 0 }),
    chromePath: findChrome(),
    serverProcessGroup: process.platform !== "win32",
  };
}

async function runSmoke(opts, runIndex) {
  const port = await freePort();
  const url = `http://127.0.0.1:${port}/`;
  const serverArgs = ["run", ".", "-addr", `127.0.0.1:${port}`];
  if (opts.serverFPS !== null) serverArgs.push("-fps", String(opts.serverFPS));
  if (opts.serverLowKbps !== null) serverArgs.push("-low-kbps", String(opts.serverLowKbps));
  if (opts.serverMidKbps !== null) serverArgs.push("-mid-kbps", String(opts.serverMidKbps));
  if (opts.serverHighKbps !== null) serverArgs.push("-high-kbps", String(opts.serverHighKbps));

  const loadProcesses = startCPUBurners(opts.cpuBurners);
  const server = spawn("go", serverArgs, {
    stdio: ["ignore", "pipe", "pipe"],
    detached: opts.serverProcessGroup,
  });
  const serverLog = captureProcessOutput(server);
  const tempProfile = await mkdtemp(join(tmpdir(), "govpx-vp8-browser-"));
  let chrome = null;
  let cdp = null;

  try {
    await waitForHTTP(url, opts.timeoutMs);
    chrome = await launchChrome(opts.chromePath, tempProfile);
    cdp = await CDP.connect(chrome.wsURL, opts.cdpTimeoutMs);

    const clients = [];
    for (let i = 0; i < opts.clients; i++) {
      clients.push(await createBrowserClient(cdp, url, i + 1));
    }

    const initialByClient = await Promise.all(clients.map((client) =>
      waitForDecodedStats(cdp, client.sessionId, opts.timeoutMs, opts)
    ));
    let firstByClient = initialByClient;
    const localWithhold = opts.localWithhold
      ? await exerciseLocalWithhold(cdp, clients, firstByClient, opts.timeoutMs, opts.localWithholdCount, opts.expectedRenditions)
      : null;
    if (localWithhold) {
      firstByClient = localWithhold.afterRecoveryByClient;
    }
    let previousByClient = firstByClient;
    const samples = [];
    const sampleCount = Math.max(1, Math.ceil(opts.soakMs / opts.sampleMs));

    for (let i = 0; i < sampleCount; i++) {
      const observationsByClient = await Promise.all(clients.map((client) =>
        collectIntervalStats(cdp, client.sessionId, opts.sampleMs, opts.pollMs)
      ));
      const currentByClient = [];
      const sampleClients = [];
      for (let clientIndex = 0; clientIndex < clients.length; clientIndex++) {
        const previous = previousByClient[clientIndex];
        const observations = observationsByClient[clientIndex];
        const current = observations[observations.length - 1];
        const delta = diffStats(previous, current);
        const summary = summarizeInterval([previous, ...observations]);
        assertSmoke(previous, current, delta, {
          ...opts,
          intervalMs: opts.sampleMs,
          runIndex,
          clientIndex: clientIndex + 1,
          sampleIndex: i + 1,
          summary,
        });
        currentByClient[clientIndex] = current;
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

    const finalByClient = previousByClient;
    const deltaByClient = firstByClient.map((first, i) =>
      diffStats(first, finalByClient[i])
    );
    const summary = summarizeRun(samples, deltaByClient, finalByClient, firstByClient);
    return {
      run: runIndex,
      url,
      clients: opts.clients,
      renditions: opts.expectedRenditions,
      sampleMs: opts.sampleMs,
      soakMs: opts.soakMs,
      pollMs: opts.pollMs,
      cdpTimeoutMs: opts.cdpTimeoutMs,
      serverFPS: opts.serverFPS,
      serverLowKbps: opts.serverLowKbps,
      serverMidKbps: opts.serverMidKbps,
      serverHighKbps: opts.serverHighKbps,
      localWithhold: opts.localWithhold,
      localWithholdCount: opts.localWithholdCount,
      localWithholdResult: localWithhold,
      cpuBurners: opts.cpuBurners,
      minDecodedDelta: opts.minDecodedDelta,
      minVideoTimeRatio: opts.minVideoTimeRatio,
      maxRxLostDelta: opts.maxRxLostDelta,
      maxRxDroppedDelta: opts.maxRxDroppedDelta,
      maxRxFreezesDelta: opts.maxRxFreezesDelta,
      maxRxFreezeDurationDelta: opts.maxRxFreezeDurationDelta,
      maxRxRepairDelta: opts.maxRxRepairDelta,
      maxRxNackDelta: opts.maxRxNackDelta,
      maxRxPliDelta: opts.maxRxPliDelta,
      maxRxFirDelta: opts.maxRxFirDelta,
      initial: firstByClient[0],
      samples,
      first: firstByClient[0],
      second: finalByClient[0],
      delta: deltaByClient[0],
      clientResults: firstByClient.map((first, i) => ({
        client: i + 1,
        first,
        second: finalByClient[i],
        delta: deltaByClient[i],
      })),
      summary,
    };
  } catch (err) {
    err.message += `\nserver stdout:\n${serverLog.stdout()}\nserver stderr:\n${serverLog.stderr()}`;
    throw err;
  } finally {
    if (cdp) cdp.close();
    if (chrome) await stopProcess(chrome.process);
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
  await cdp.send("Page.addScriptToEvaluateOnNewDocument", {
    source: peerCaptureScript(),
  }, sessionId);
  await cdp.send("Page.navigate", { url }, sessionId);
  return { client: clientIndex, targetId: target.targetId, sessionId };
}

function peerCaptureScript() {
  return `(() => {
    const NativeRTCPeerConnection = window.RTCPeerConnection;
    if (!NativeRTCPeerConnection || window.__govpxSmokeInstalled) return;
    window.__govpxSmokeInstalled = true;
    window.__govpxSmoke = { peers: [] };
    function WrappedRTCPeerConnection(...args) {
      const pc = new NativeRTCPeerConnection(...args);
      window.__govpxSmoke.peers.push(pc);
      return pc;
    }
    WrappedRTCPeerConnection.prototype = NativeRTCPeerConnection.prototype;
    Object.setPrototypeOf(WrappedRTCPeerConnection, NativeRTCPeerConnection);
    window.RTCPeerConnection = WrappedRTCPeerConnection;
  })()`;
}

async function waitForDecodedStats(cdp, sessionId, timeoutMs, opts) {
  const deadline = Date.now() + timeoutMs;
  let latest = null;
  while (Date.now() < deadline) {
    latest = await readStats(cdp, sessionId);
    if (
      isConnected(latest) &&
      latest.inbound.length >= opts.expectedRenditions &&
      latest.videos.length >= opts.expectedRenditions &&
      latest.inbound.filter((rx) => Number.isFinite(rx.framesDecoded) && rx.framesDecoded > 0).length >= opts.expectedRenditions &&
      latest.videos.filter((video) => video.readyState >= 2 && video.currentTime > 0).length >= opts.expectedRenditions &&
      hasVP8Codec(latest)
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

async function applyControlAction(cdp, sessionId, action) {
  const encoded = JSON.stringify(action);
  const result = await cdp.send("Runtime.evaluate", {
    expression: `(() => {
      const action = ${encoded};
      if (typeof sendCtl !== "function") throw new Error("missing sendCtl");
      if (!dc || dc.readyState !== "open") throw new Error("data channel not open");
      if (action.type === "withhold") {
        const count = Number.isFinite(action.count) ? action.count : 1;
        sendCtl({type: "withhold", id: -1, count});
        return {type: "withhold", id: -1, count};
      }
      throw new Error("unknown control action " + action.type);
    })()`,
    returnByValue: true,
    awaitPromise: true,
  }, sessionId);
  if (result.exceptionDetails) {
    throw new Error(`control action failed: ${JSON.stringify(result.exceptionDetails)}`);
  }
  return result.result.value;
}

async function exerciseLocalWithhold(cdp, clients, beforeByClient, timeoutMs, count, renditions) {
  await Promise.all(clients.map((client) =>
    applyControlAction(cdp, client.sessionId, { type: "withhold", count })
  ));
  const recoveredByClient = await Promise.all(clients.map((client, i) =>
    waitForLocalWithholdRecovery(cdp, client.sessionId, beforeByClient[i], timeoutMs, count * renditions)
  ));
  await sleep(1000);
  const afterRecoveryByClient = await Promise.all(clients.map((client) =>
    readStats(cdp, client.sessionId)
  ));
  return {
    count,
    renditions,
    expectedWithheldAUs: count * renditions,
    clients: recoveredByClient.map((stats, i) => ({
      client: i + 1,
      withheldAUs: numericDelta(
        beforeByClient[i]?.senderWithheldAUs,
        stats?.senderWithheldAUs,
      ),
      forcedKeys: numericDelta(
        beforeByClient[i]?.senderForcedKeys,
        stats?.senderForcedKeys,
      ),
      decodedAfterWithhold: numericDelta(
        beforeByClient[i]?.rxDecoded,
        stats?.rxDecoded,
      ),
      lostAfterWithhold: numericDelta(
        beforeByClient[i]?.rxLost,
        stats?.rxLost,
      ),
      repairedAfterWithhold: numericDelta(
        beforeByClient[i]?.rxRepairPackets,
        stats?.rxRepairPackets,
      ),
      nackAfterWithhold: numericDelta(
        beforeByClient[i]?.rxNackCount,
        stats?.rxNackCount,
      ),
      pliAfterWithhold: numericDelta(
        beforeByClient[i]?.rxPliCount,
        stats?.rxPliCount,
      ),
      firAfterWithhold: numericDelta(
        beforeByClient[i]?.rxFirCount,
        stats?.rxFirCount,
      ),
      recovered: stats,
      afterRecovery: afterRecoveryByClient[i],
    })),
    afterRecoveryByClient,
  };
}

async function waitForLocalWithholdRecovery(cdp, sessionId, before, timeoutMs, expectedWithheldAUs) {
  const deadline = Date.now() + timeoutMs;
  let latest = null;
  while (Date.now() < deadline) {
    await sleep(250);
    latest = await readStats(cdp, sessionId);
    const withheld = numericDelta(before?.senderWithheldAUs, latest?.senderWithheldAUs);
    const forcedKeys = numericDelta(before?.senderForcedKeys, latest?.senderForcedKeys);
    const decoded = numericDelta(before?.rxDecoded, latest?.rxDecoded);
    const lost = numericDelta(before?.rxLost, latest?.rxLost);
    const repairs = numericDelta(before?.rxRepairPackets, latest?.rxRepairPackets);
    const nacks = numericDelta(before?.rxNackCount, latest?.rxNackCount);
    const plis = numericDelta(before?.rxPliCount, latest?.rxPliCount);
    const firs = numericDelta(before?.rxFirCount, latest?.rxFirCount);
    if (
      withheld !== null &&
      withheld >= expectedWithheldAUs &&
      (forcedKeys === null || forcedKeys === 0) &&
      decoded !== null &&
      decoded >= 1 &&
      (lost === null || lost === 0) &&
      (repairs === null || repairs === 0) &&
      (nacks === null || nacks === 0) &&
      (plis === null || plis === 0) &&
      (firs === null || firs === 0) &&
      latest.videos.length >= before.videos.length &&
      latest.videos.every((video) =>
        video.readyState >= 2 &&
        Number.isFinite(video.currentTime) &&
        video.currentTime > 0
      ) &&
      (
        !Number.isFinite(before.minVideoTime) ||
        !Number.isFinite(latest.minVideoTime) ||
        latest.minVideoTime > before.minVideoTime
      )
    ) {
      return latest;
    }
  }
  throw new Error(`local withhold decode recovery did not become ready: ${JSON.stringify({ before, latest, expectedWithheldAUs })}`);
}

async function readStats(cdp, sessionId) {
  const result = await cdp.send("Runtime.evaluate", {
    expression: `(async () => {
      const peers = Array.isArray(window.__govpxSmoke?.peers) ? window.__govpxSmoke.peers : [];
      const pc = peers[0] || null;
      const reports = pc ? await pc.getStats() : new Map();
      const codecs = new Map();
      for (const report of reports.values()) {
        if (report.type === "codec") codecs.set(report.id, report);
      }
      const num = (value) => {
        const n = Number(value);
        return Number.isFinite(n) ? n : null;
      };
      const sumFinite = (values) => {
        let total = 0;
        let seen = false;
        for (const value of values) {
          const n = num(value);
          if (n !== null) {
            total += n;
            seen = true;
          }
        }
        return seen ? total : null;
      };
      const inbound = [];
      for (const report of reports.values()) {
        const isVideo = report.kind === "video" || report.mediaType === "video";
        if (report.type !== "inbound-rtp" || !isVideo || report.isRemote) continue;
        const codec = codecs.get(report.codecId);
        const repairPackets = sumFinite([
          report.retransmittedPacketsReceived,
          report.repairedPackets,
          report.fecPacketsReceived,
          report.fecPacketsDiscarded,
        ]);
        inbound.push({
          id: report.id,
          trackIdentifier: report.trackIdentifier ?? null,
          mid: report.mid ?? null,
          ssrc: report.ssrc ?? null,
          codecMimeType: codec?.mimeType ?? null,
          packetsReceived: num(report.packetsReceived),
          packetsLost: num(report.packetsLost),
          framesDecoded: num(report.framesDecoded),
          framesDropped: num(report.framesDropped),
          freezeCount: num(report.freezeCount),
          totalFreezesDuration: num(report.totalFreezesDuration),
          nackCount: num(report.nackCount),
          pliCount: num(report.pliCount),
          firCount: num(report.firCount),
          repairPackets,
        });
      }
      inbound.sort((a, b) => String(a.id).localeCompare(String(b.id)));
      const videos = Array.from(document.querySelectorAll("video")).map((video, index) => {
        const quality = typeof video.getVideoPlaybackQuality === "function"
          ? video.getVideoPlaybackQuality()
          : {};
        return {
          index,
          readyState: video.readyState,
          currentTime: num(video.currentTime),
          videoWidth: num(video.videoWidth),
          videoHeight: num(video.videoHeight),
          totalVideoFrames: num(quality.totalVideoFrames),
          droppedVideoFrames: num(quality.droppedVideoFrames),
          paused: video.paused,
          ended: video.ended,
        };
      });
      const sumReports = (key) => {
        const values = inbound.map((rx) => rx[key]).filter(Number.isFinite);
        return values.length === 0 ? null : values.reduce((a, b) => a + b, 0);
      };
      const sumVideos = (key) => {
        const values = videos.map((video) => video[key]).filter(Number.isFinite);
        return values.length === 0 ? null : values.reduce((a, b) => a + b, 0);
      };
      const codecMimeTypes = Array.from(new Set(inbound.map((rx) => rx.codecMimeType).filter(Boolean)));
      return {
        status: document.getElementById("status")?.textContent ?? null,
        href: location.href,
        peerCount: peers.length,
        peerConnectionState: pc?.connectionState ?? null,
        iceConnectionState: pc?.iceConnectionState ?? null,
        signalingState: pc?.signalingState ?? null,
        codecMimeTypes,
        inbound,
        videos,
        rxDecoded: sumReports("framesDecoded"),
        rxDropped: sumReports("framesDropped") ?? sumVideos("droppedVideoFrames"),
        rxLost: sumReports("packetsLost"),
        rxFreezes: sumReports("freezeCount"),
        rxFreezeDuration: sumReports("totalFreezesDuration"),
        rxRepairPackets: sumReports("repairPackets"),
        rxNackCount: sumReports("nackCount"),
        rxPliCount: sumReports("pliCount"),
        rxFirCount: sumReports("firCount"),
        senderForcedKeys: typeof senderForcedKeyCount === "number" ? senderForcedKeyCount : 0,
        senderWithheldAUs: typeof senderWithheldAUCount === "number" ? senderWithheldAUCount : 0,
        videoFrames: sumVideos("totalVideoFrames"),
        videoDroppedFrames: sumVideos("droppedVideoFrames"),
        minVideoTime: minFinite(videos.map((video) => video.currentTime)),
      };
      function minFinite(values) {
        const finite = values.filter(Number.isFinite);
        return finite.length === 0 ? null : Math.min(...finite);
      }
    })()`,
    returnByValue: true,
    awaitPromise: true,
  }, sessionId);
  if (result.exceptionDetails) {
    throw new Error(`readStats failed: ${JSON.stringify(result.exceptionDetails)}`);
  }
  return result.result.value;
}

function assertSmoke(first, second, delta, opts) {
  if (!isConnected(second)) {
    throw new Error(`${sampleLabel(opts)} peer did not stay connected: ${second.peerConnectionState}/${second.iceConnectionState}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (second.inbound.length < opts.expectedRenditions) {
    throw new Error(`${sampleLabel(opts)} expected ${opts.expectedRenditions} inbound VP8 streams, got ${second.inbound.length}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (second.videos.length < opts.expectedRenditions) {
    throw new Error(`${sampleLabel(opts)} expected ${opts.expectedRenditions} video elements, got ${second.videos.length}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (!hasVP8Codec(second)) {
    throw new Error(`${sampleLabel(opts)} inbound codec stats did not report video/VP8: ${JSON.stringify(second.codecMimeTypes)}`);
  }

  const decodedDeltas = streamDeltas(first.inbound, second.inbound, "framesDecoded");
  if (decodedDeltas.length >= opts.expectedRenditions) {
    const tooSlow = decodedDeltas.slice(0, opts.expectedRenditions)
      .map((value, index) => ({ index, value }))
      .filter(({ value }) => value < opts.minDecodedDelta);
    if (tooSlow.length > 0) {
      throw new Error(`${sampleLabel(opts)} decoded frames did not advance enough per VP8 stream: ${JSON.stringify(tooSlow)}; ${sampleDetails(first, second, delta, opts.summary)}`);
    }
  } else if (delta.videoFrames === null || delta.videoFrames < opts.minDecodedDelta * opts.expectedRenditions) {
    throw new Error(`${sampleLabel(opts)} decoded frames did not advance enough: stats=${delta.rxDecoded} videoFrames=${delta.videoFrames}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }

  const videoTimeDeltas = streamDeltas(first.videos, second.videos, "currentTime");
  const minVideoTime = opts.intervalMs / 1000 * opts.minVideoTimeRatio;
  if (videoTimeDeltas.length < opts.expectedRenditions) {
    throw new Error(`${sampleLabel(opts)} video time stats are missing for one or more renditions; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  const slowVideos = videoTimeDeltas.slice(0, opts.expectedRenditions)
    .map((value, index) => ({ index, value }))
    .filter(({ value }) => value < minVideoTime);
  if (slowVideos.length > 0) {
    throw new Error(`${sampleLabel(opts)} video time did not advance enough: ${JSON.stringify(slowVideos)} want >= ${minVideoTime}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }

  for (const video of second.videos.slice(0, opts.expectedRenditions)) {
    if (video.readyState < 2 || video.videoWidth <= 0 || video.videoHeight <= 0 || video.ended) {
      throw new Error(`${sampleLabel(opts)} video element is not rendering: ${JSON.stringify(video)}; ${sampleDetails(first, second, delta, opts.summary)}`);
    }
  }

  assertMaxDelta("packet loss", "rxLost", delta, opts.maxRxLostDelta, opts, first, second);
  assertMaxDelta("receiver dropped frames", "rxDropped", delta, opts.maxRxDroppedDelta, opts, first, second);
  assertMaxDelta("receiver freezes", "rxFreezes", delta, opts.maxRxFreezesDelta, opts, first, second);
  assertMaxDelta("receiver freeze duration", "rxFreezeDuration", delta, opts.maxRxFreezeDurationDelta, opts, first, second);
  assertMaxDelta("receiver repair packets", "rxRepairPackets", delta, opts.maxRxRepairDelta, opts, first, second);
  assertMaxDelta("receiver NACK", "rxNackCount", delta, opts.maxRxNackDelta, opts, first, second);
  assertMaxDelta("receiver PLI", "rxPliCount", delta, opts.maxRxPliDelta, opts, first, second);
  assertMaxDelta("receiver FIR", "rxFirCount", delta, opts.maxRxFirDelta, opts, first, second);
}

function assertMaxDelta(label, key, delta, max, opts, first, second) {
  if (delta[key] !== null && delta[key] > max) {
    throw new Error(`${sampleLabel(opts)} ${label} advanced by ${delta[key]}, want <= ${max}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
}

function streamDeltas(firstItems, secondItems, key) {
  const out = [];
  const count = Math.min(firstItems.length, secondItems.length);
  for (let i = 0; i < count; i++) {
    const delta = numericDelta(firstItems[i]?.[key], secondItems[i]?.[key]);
    if (delta !== null) out.push(delta);
  }
  return out;
}

function diffStats(first, second) {
  const delta = {};
  for (const key of [
    "rxDecoded",
    "rxDropped",
    "rxLost",
    "rxFreezes",
    "rxFreezeDuration",
    "rxRepairPackets",
    "rxNackCount",
    "rxPliCount",
    "rxFirCount",
    "senderForcedKeys",
    "senderWithheldAUs",
    "videoFrames",
    "videoDroppedFrames",
    "minVideoTime",
  ]) {
    delta[key] = numericDelta(first[key], second[key]);
  }
  return delta;
}

function summarizeInterval(stats) {
  const values = (key) => stats.map((s) => s[key]).filter(Number.isFinite);
  return {
    observations: stats.length,
    minInboundStreams: minNumber(stats.map((s) => s.inbound.length)),
    minVideos: minNumber(stats.map((s) => s.videos.length)),
    maxRxLost: maxNumber(values("rxLost")),
    maxRxDropped: maxNumber(values("rxDropped")),
    maxRxFreezes: maxNumber(values("rxFreezes")),
    maxRxFreezeDuration: maxNumber(values("rxFreezeDuration")),
    maxRxRepairPackets: maxNumber(values("rxRepairPackets")),
    maxRxNackCount: maxNumber(values("rxNackCount")),
    maxRxPliCount: maxNumber(values("rxPliCount")),
    maxRxFirCount: maxNumber(values("rxFirCount")),
    minVideoTime: minNumber(values("minVideoTime")),
  };
}

function summarizeSampleClients(sampleClients) {
  const summaries = sampleClients.map((client) => client.summary);
  const seconds = sampleClients.map((client) => client.second);
  const deltas = sampleClients.map((client) => client.delta);
  return summarizeStatsGroup(summaries, deltas, seconds, seconds);
}

function summarizeRun(samples, deltas, seconds, firsts = []) {
  const sampleClients = samples.flatMap((sample) => sample.clients);
  const summaries = sampleClients.map((client) => client.summary);
  const sampleSeconds = [
    ...firsts,
    ...sampleClients.map((client) => client.second),
  ];
  return summarizeStatsGroup(summaries, deltas, seconds, sampleSeconds);
}

function summarizeStatsGroup(summaries, deltas, seconds, sampleSeconds) {
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
    videoFrames: deltaSum("videoFrames"),
    lost: deltaSum("rxLost"),
    dropped: deltaSum("rxDropped"),
    freezes: deltaSum("rxFreezes"),
    freezeDuration: deltaSum("rxFreezeDuration"),
    repairPackets: deltaSum("rxRepairPackets"),
    nacks: deltaSum("rxNackCount"),
    plis: deltaSum("rxPliCount"),
    firs: deltaSum("rxFirCount"),
    forcedKeys: deltaSum("senderForcedKeys"),
    withheldAUs: deltaSum("senderWithheldAUs"),
    minInboundStreams: minNumber(summaryValues("minInboundStreams")),
    minVideos: minNumber(summaryValues("minVideos")),
    minVideoTime: minNumber(secondValues("minVideoTime")),
    minSampleVideoTime: minNumber(sampleSecondValues("minVideoTime")),
    maxRxLost: maxNumber(summaryValues("maxRxLost")),
    maxRxDropped: maxNumber(summaryValues("maxRxDropped")),
    maxRxFreezes: maxNumber(summaryValues("maxRxFreezes")),
    maxRxFreezeDuration: maxNumber(summaryValues("maxRxFreezeDuration")),
    maxRxRepairPackets: maxNumber(summaryValues("maxRxRepairPackets")),
    maxRxNackCount: maxNumber(summaryValues("maxRxNackCount")),
    maxRxPliCount: maxNumber(summaryValues("maxRxPliCount")),
    maxRxFirCount: maxNumber(summaryValues("maxRxFirCount")),
  };
}

function summarizeRuns(runs) {
  const summaries = runs.map((run) => run.summary);
  const sum = (key) => summaries.reduce((total, s) =>
    total + (Number.isFinite(s[key]) ? s[key] : 0), 0);
  const values = (key) => summaries.map((s) => s[key]).filter(Number.isFinite);
  return {
    runs: runs.length,
    clientRuns: sum("clients"),
    decoded: sum("decoded"),
    minClientDecoded: minNumber(values("minClientDecoded")),
    videoFrames: sum("videoFrames"),
    lost: sum("lost"),
    dropped: sum("dropped"),
    freezes: sum("freezes"),
    freezeDuration: sum("freezeDuration"),
    repairPackets: sum("repairPackets"),
    nacks: sum("nacks"),
    plis: sum("plis"),
    firs: sum("firs"),
    forcedKeys: sum("forcedKeys"),
    withheldAUs: sum("withheldAUs"),
    minInboundStreams: minNumber(values("minInboundStreams")),
    minVideos: minNumber(values("minVideos")),
    minVideoTime: minNumber(values("minVideoTime")),
    maxRxLost: maxNumber(values("maxRxLost")),
    maxRxDropped: maxNumber(values("maxRxDropped")),
    maxRxFreezes: maxNumber(values("maxRxFreezes")),
    maxRxFreezeDuration: maxNumber(values("maxRxFreezeDuration")),
    maxRxRepairPackets: maxNumber(values("maxRxRepairPackets")),
    maxRxNackCount: maxNumber(values("maxRxNackCount")),
    maxRxPliCount: maxNumber(values("maxRxPliCount")),
    maxRxFirCount: maxNumber(values("maxRxFirCount")),
  };
}

function isConnected(stats) {
  return stats && ["connected", "completed"].includes(stats.peerConnectionState);
}

function hasVP8Codec(stats) {
  return Array.isArray(stats?.codecMimeTypes) &&
    stats.codecMimeTypes.some((mime) => /^video\/vp8$/i.test(mime));
}

function minNumber(values) {
  const finite = values.filter(Number.isFinite);
  return finite.length === 0 ? null : Math.min(...finite);
}

function maxNumber(values) {
  const finite = values.filter(Number.isFinite);
  return finite.length === 0 ? null : Math.max(...finite);
}

function numericDelta(a, b) {
  return Number.isFinite(a) && Number.isFinite(b) ? b - a : null;
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

function flagValue(name) {
  const idx = process.argv.indexOf(name);
  if (idx === -1) return null;
  if (idx + 1 >= process.argv.length || process.argv[idx + 1].startsWith("--")) {
    throw new Error(`${name} requires a value`);
  }
  return process.argv[idx + 1];
}

function booleanFlag(name) {
  return process.argv.includes(name);
}

function integerFlag(name, defaultValue, limits = {}) {
  const value = flagValue(name);
  if (value === null) return defaultValue;
  const parsed = Number(value);
  if (!Number.isInteger(parsed)) throw new Error(`${name} must be an integer`);
  validateNumber(name, parsed, limits);
  return parsed;
}

function optionalIntegerFlag(name, limits = {}) {
  const value = flagValue(name);
  if (value === null) return null;
  const parsed = Number(value);
  if (!Number.isInteger(parsed)) throw new Error(`${name} must be an integer`);
  validateNumber(name, parsed, limits);
  return parsed;
}

function numberFlag(name, defaultValue, limits = {}) {
  const value = flagValue(name);
  if (value === null) return defaultValue;
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) throw new Error(`${name} must be a number`);
  validateNumber(name, parsed, limits);
  return parsed;
}

function validateNumber(name, value, { min = -Infinity, max = Infinity } = {}) {
  if (value < min || value > max) {
    throw new Error(`${name} must be between ${min} and ${max}`);
  }
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

async function waitForHTTP(target, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
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
      const match = chunk.toString().match(/DevTools listening on (ws:\/\/[^\s]+)/);
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

function captureProcessOutput(child) {
  const limit = 16 * 1024;
  const out = { stdout: "", stderr: "" };
  const append = (key, chunk) => {
    out[key] += chunk.toString();
    if (out[key].length > limit) out[key] = out[key].slice(-limit);
  };
  child.stdout?.on("data", (chunk) => append("stdout", chunk));
  child.stderr?.on("data", (chunk) => append("stderr", chunk));
  return {
    stdout: () => out.stdout,
    stderr: () => out.stderr,
  };
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
  constructor(socket, timeoutMs) {
    this.socket = socket;
    this.timeoutMs = timeoutMs;
    this.nextID = 1;
    this.pending = new Map();
    socket.addEventListener("message", (event) => {
      const msg = JSON.parse(event.data);
      if (!msg.id) return;
      const pending = this.pending.get(msg.id);
      if (!pending) return;
      this.pending.delete(msg.id);
      clearTimeout(pending.timer);
      if (msg.error) {
        pending.reject(new Error(JSON.stringify(msg.error)));
      } else {
        pending.resolve(msg.result ?? {});
      }
    });
    socket.addEventListener("close", () => {
      this.rejectPending(new Error("CDP socket closed"));
    });
    socket.addEventListener("error", () => {
      this.rejectPending(new Error("CDP socket error"));
    });
  }

  static async connect(url, timeoutMs) {
    const socket = new WebSocket(url);
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        reject(new Error(`CDP socket open timed out after ${timeoutMs} ms`));
        try {
          socket.close();
        } catch {
          // Ignore close failures while surfacing the original timeout.
        }
      }, timeoutMs);
      socket.addEventListener("open", () => {
        clearTimeout(timer);
        resolve();
      }, { once: true });
      socket.addEventListener("error", (event) => {
        clearTimeout(timer);
        reject(event instanceof Error ? event : new Error("CDP socket open failed"));
      }, { once: true });
    });
    return new CDP(socket, timeoutMs);
  }

  send(method, params = {}, sessionId = undefined) {
    const id = this.nextID++;
    const msg = { id, method, params };
    if (sessionId) msg.sessionId = sessionId;
    if (this.socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error(`CDP socket is not open for ${method}`));
    }
    this.socket.send(JSON.stringify(msg));
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        const suffix = sessionId ? ` session ${sessionId}` : "";
        reject(new Error(`CDP ${method}${suffix} timed out after ${this.timeoutMs} ms`));
      }, this.timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
    });
  }

  rejectPending(err) {
    for (const [id, pending] of this.pending) {
      clearTimeout(pending.timer);
      pending.reject(err);
      this.pending.delete(id);
    }
  }

  close() {
    this.socket.close();
  }
}

await main();
