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
    maxRxNackDelta: numberFlag("--max-rx-nack-delta", 0, { min: 0 }),
    maxRxPliDelta: numberFlag("--max-rx-pli-delta", 0, { min: 0 }),
    maxRxFirDelta: numberFlag("--max-rx-fir-delta", 0, { min: 0 }),
    maxSenderFailedEncodeAUs: numberFlag("--max-sender-failed-encode-aus", 0, { min: 0 }),
    maxSenderFailedEncodedAUs: numberFlag("--max-sender-failed-encoded-aus", 0, { min: 0 }),
    clients: integerFlag("--clients", 1, { min: 1 }),
    repeat: numberFlag("--repeat", 1),
    serverFPS: optionalNumberFlag("--server-fps"),
    serverBitrateKbps: optionalNumberFlag("--server-bitrate-kbps"),
    cpuBurners: optionalNumberFlag("--cpu-burners") ?? 0,
    controlChurn: booleanFlag("--control-churn"),
    tuningChurn: booleanFlag("--tuning-churn"),
    pauseResume: booleanFlag("--pause-resume"),
    pauseMs: numberFlag("--pause-ms", 1500, { min: 0 }),
    receiverStallProbe: booleanFlag("--receiver-stall-probe"),
    localWithhold: booleanFlag("--local-withhold"),
    localWithholdCount: integerFlag("--local-withhold-count", 1, { min: 1, max: 3 }),
    minActiveLayers: optionalNumberFlag("--min-active-layers"),
    minEndingActiveLayers: optionalNumberFlag("--min-ending-active-layers"),
    maxActiveLayerChanges: optionalNumberFlag("--max-active-layer-changes"),
    requireThreadedTopLayer: booleanFlag("--require-threaded-top-layer"),
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

    const initialByClient = await Promise.all(clients.map((client) =>
      waitForDecodedStats(cdp, client.sessionId, opts.timeoutMs)
    ));
    let firstByClient = initialByClient;
    const pauseResume = opts.pauseResume
      ? await exercisePauseResume(cdp, clients, initialByClient, opts.timeoutMs, opts.pauseMs)
      : null;
    if (pauseResume) {
      firstByClient = pauseResume.afterResumeByClient;
    }
    const localWithhold = opts.localWithhold
      ? await exerciseLocalWithhold(cdp, clients, firstByClient, opts.timeoutMs, opts.localWithholdCount)
      : null;
    if (localWithhold) {
      firstByClient = localWithhold.afterRecoveryByClient;
    }
    const receiverStallProbe = opts.receiverStallProbe
      ? await exerciseReceiverStallProbe(cdp, clients, firstByClient, opts.timeoutMs)
      : null;
    if (receiverStallProbe) {
      firstByClient = receiverStallProbe.afterProbeByClient;
    }
    let previousByClient = firstByClient;
    const samples = [];
    const sampleCount = Math.max(1, Math.ceil(opts.soakMs / opts.sampleMs));
    for (let i = 0; i < sampleCount; i++) {
      const controlAction = nextControlAction(opts, i);
      if (controlAction) {
        await Promise.all(clients.map((client) =>
          applyControlAction(cdp, client.sessionId, controlAction)
        ));
        await sleep(250);
      }
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
          maxRxNackDelta: opts.maxRxNackDelta,
          maxRxPliDelta: opts.maxRxPliDelta,
          maxRxFirDelta: opts.maxRxFirDelta,
          maxSenderFailedEncodeAUs: opts.maxSenderFailedEncodeAUs,
          maxSenderFailedEncodedAUs: opts.maxSenderFailedEncodedAUs,
          controlAction,
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
        controlAction,
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
    const summary = summarizeRun(samples, deltaByClient, secondByClient, firstByClient);
    assertRunSmoke(summary, {
      ...opts,
      pauseResumeResult: pauseResume,
      localWithholdResult: localWithhold,
      receiverStallProbeResult: receiverStallProbe,
    });
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
      controlChurn: opts.controlChurn,
      tuningChurn: opts.tuningChurn,
      pauseResume: opts.pauseResume,
      pauseMs: opts.pauseMs,
      receiverStallProbe: opts.receiverStallProbe,
      localWithhold: opts.localWithhold,
      localWithholdCount: opts.localWithholdCount,
      minDecodedDelta: opts.minDecodedDelta,
      minVideoTimeRatio: opts.minVideoTimeRatio,
      maxRxRepairRequests: opts.maxRxRepairRequests,
      maxRxNackDelta: opts.maxRxNackDelta,
      maxRxPliDelta: opts.maxRxPliDelta,
      maxRxFirDelta: opts.maxRxFirDelta,
      maxSenderFailedEncodeAUs: opts.maxSenderFailedEncodeAUs,
      maxSenderFailedEncodedAUs: opts.maxSenderFailedEncodedAUs,
      minActiveLayers: opts.minActiveLayers,
      minEndingActiveLayers: opts.minEndingActiveLayers,
      maxActiveLayerChanges: opts.maxActiveLayerChanges,
      requireThreadedTopLayer: opts.requireThreadedTopLayer,
      pauseResumeResult: pauseResume,
      localWithholdResult: localWithhold,
      receiverStallProbeResult: receiverStallProbe,
      initial: initialByClient[0],
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
      summary,
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

async function applyControlAction(cdp, sessionId, action) {
  const expression = controlActionExpression(action);
  const result = await cdp.send("Runtime.evaluate", {
    expression,
    returnByValue: true,
    awaitPromise: true,
  }, sessionId);
  if (result.exceptionDetails) {
    throw new Error(`control action failed: ${JSON.stringify(result.exceptionDetails)}`);
  }
  return result.result.value;
}

async function exercisePauseResume(cdp, clients, beforeByClient, timeoutMs, pauseMs) {
  await Promise.all(clients.map((client) =>
    applyControlAction(cdp, client.sessionId, { type: "pause", paused: true })
  ));
  await sleep(pauseMs);
  const pausedStats = await Promise.all(clients.map((client) =>
    readStats(cdp, client.sessionId)
  ));
  await Promise.all(clients.map((client) =>
    applyControlAction(cdp, client.sessionId, { type: "pause", paused: false })
  ));
  const recoveredByClient = await Promise.all(clients.map((client, i) =>
    waitForPauseResumeRecovery(cdp, client.sessionId, beforeByClient[i], timeoutMs)
  ));
  await sleep(1000);
  const afterResumeByClient = await Promise.all(clients.map((client) =>
    readStats(cdp, client.sessionId)
  ));
  return {
    pauseMs,
    clients: pausedStats.map((stats, i) => ({
      client: i + 1,
      forcedKeysAfterResume: numericDelta(
        beforeByClient[i]?.senderForcedKeys,
        recoveredByClient[i]?.senderForcedKeys,
      ),
      decodedAfterResume: numericDelta(
        stats?.rxDecoded,
        recoveredByClient[i]?.rxDecoded,
      ),
      paused: stats,
      recovered: recoveredByClient[i],
      afterResume: afterResumeByClient[i],
    })),
    afterResumeByClient,
  };
}

async function waitForPauseResumeRecovery(cdp, sessionId, before, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let latest = null;
  while (Date.now() < deadline) {
    await sleep(250);
    latest = await readStats(cdp, sessionId);
    const forcedKeys = numericDelta(before?.senderForcedKeys, latest?.senderForcedKeys);
    const decoded = numericDelta(before?.rxDecoded, latest?.rxDecoded);
    if (
      forcedKeys !== null &&
      forcedKeys >= 1 &&
      decoded !== null &&
      decoded >= 1 &&
      latest.videoReadyState >= 2 &&
      latest.videoTime > before.videoTime
    ) {
      return latest;
    }
  }
  throw new Error(`pause/resume decode recovery did not become ready: ${JSON.stringify({ before, latest })}`);
}

async function exerciseReceiverStallProbe(cdp, clients, beforeByClient, timeoutMs) {
  const probes = await Promise.all(clients.map((client) =>
    triggerReceiverStallProbe(cdp, client.sessionId)
  ));
  const recoveredByClient = await Promise.all(clients.map((client, i) =>
    waitForReceiverStallProbeRecovery(cdp, client.sessionId, beforeByClient[i], timeoutMs, probes[i])
  ));
  await sleep(1000);
  const afterProbeByClient = await Promise.all(clients.map((client) =>
    readStats(cdp, client.sessionId)
  ));
  return {
    clients: probes.map((probe, i) => ({
      client: i + 1,
      sent: probe.sent,
      repairRequests: probe.receiverRepairRequests,
      receiverSpatialCap: probe.receiverRequestedSpatialCap,
      forcedKeysAfterStall: numericDelta(
        beforeByClient[i]?.senderForcedKeys,
        recoveredByClient[i]?.senderForcedKeys,
      ),
      decodedAfterStall: numericDelta(
        beforeByClient[i]?.rxDecoded,
        recoveredByClient[i]?.rxDecoded,
      ),
      lostAfterStall: numericDelta(
        beforeByClient[i]?.rxLost,
        recoveredByClient[i]?.rxLost,
      ),
      repairedAfterStall: numericDelta(
        beforeByClient[i]?.rxRepairRequests,
        recoveredByClient[i]?.rxRepairRequests,
      ),
      recovered: recoveredByClient[i],
      afterProbe: afterProbeByClient[i],
    })),
    afterProbeByClient,
  };
}

async function triggerReceiverStallProbe(cdp, sessionId) {
  const result = await cdp.send("Runtime.evaluate", {
    expression: `(() => {
      const sent = [];
      const oldSendCtl = sendCtl;
      sendCtl = (obj) => {
        sent.push(obj);
        oldSendCtl(obj);
      };
      try {
        const now = Date.now();
        const current = typeof latestRTCStats === "object" && latestRTCStats ? latestRTCStats : {};
        const packetsReceived = Number.isFinite(current.packetsReceived) ? current.packetsReceived : 100;
        const packetsLost = Number.isFinite(current.packetsLost) ? current.packetsLost : 0;
        const framesDecoded = Number.isFinite(current.framesDecoded) ? current.framesDecoded : 100;
        const freezeCount = Number.isFinite(current.freezeCount) ? current.freezeCount : 0;
        const totalFreezesDuration = Number.isFinite(current.totalFreezesDuration) ? current.totalFreezesDuration : 0;
        const pauseCount = Number.isFinite(current.pauseCount) ? current.pauseCount : 0;
        const totalPausesDuration = Number.isFinite(current.totalPausesDuration) ? current.totalPausesDuration : 0;
        const nackCount = Number.isFinite(current.nackCount) ? current.nackCount : 0;
        const pliCount = Number.isFinite(current.pliCount) ? current.pliCount : 0;
        const firCount = Number.isFinite(current.firCount) ? current.firCount : 0;
        receiverRepairRequests = 0;
        receiverRepairStreak = RECEIVER_REPAIR_CAP_BACKOFF_AFTER - 1;
        receiverRepairSuppressedUntilDecoded = false;
        receiverRepairSuppressUntil = 0;
        receiverRequestedSpatialCap = MAX_SPATIAL_CAP;
        setSpatialCapButtons(receiverRequestedSpatialCap);
        receiverLastRepairAt = now - RECEIVER_REPAIR_COOLDOWN_MS - 1;
        receiverLastDecoded = framesDecoded;
        receiverLastDecodedAt = now - RECEIVER_DECODE_STALL_MS - 1;
        previousRTCStats = {
          packetsReceived,
          packetsLost,
          framesDecoded,
          freezeCount,
          totalFreezesDuration,
          pauseCount,
          totalPausesDuration,
          nackCount,
          pliCount,
          firCount
        };
        const stats = {
          packetsReceived: packetsReceived + 1,
          packetsLost,
          framesDecoded,
          freezeCount,
          totalFreezesDuration,
          pauseCount,
          totalPausesDuration,
          nackCount,
          pliCount,
          firCount
        };
        maybeRequestReceiverRepair(stats);
        return {
          sent,
          receiverRepairRequests,
          receiverRepairStreak,
          receiverRequestedSpatialCap,
          stats
        };
      } finally {
        sendCtl = oldSendCtl;
      }
    })()`,
    returnByValue: true,
    awaitPromise: true,
  }, sessionId);
  if (result.exceptionDetails) {
    throw new Error(`receiver stall probe failed: ${JSON.stringify(result.exceptionDetails)}`);
  }
  const probe = result.result.value;
  const sentTypes = Array.isArray(probe?.sent) ? probe.sent.map((msg) => msg?.type) : [];
  if (
    !sentTypes.includes("keyframe") ||
    !sentTypes.includes("spatial") ||
    probe.receiverRepairRequests < 1 ||
    probe.receiverRequestedSpatialCap >= 3
  ) {
    throw new Error(`receiver stall probe did not emit repair controls: ${JSON.stringify(probe)}`);
  }
  return probe;
}

async function waitForReceiverStallProbeRecovery(cdp, sessionId, before, timeoutMs, probe) {
  const deadline = Date.now() + timeoutMs;
  let latest = null;
  while (Date.now() < deadline) {
    await sleep(250);
    latest = await readStats(cdp, sessionId);
    const forcedKeys = numericDelta(before?.senderForcedKeys, latest?.senderForcedKeys);
    const decoded = numericDelta(before?.rxDecoded, latest?.rxDecoded);
    const lost = numericDelta(before?.rxLost, latest?.rxLost);
    const repairs = numericDelta(before?.rxRepairRequests, latest?.rxRepairRequests);
    if (
      forcedKeys !== null &&
      forcedKeys >= 1 &&
      decoded !== null &&
      decoded >= 1 &&
      (lost === null || lost === 0) &&
      repairs !== null &&
      repairs >= 1 &&
      Number.isFinite(latest.rxSpatialCap) &&
      latest.rxSpatialCap <= probe.receiverRequestedSpatialCap &&
      latest.videoReadyState >= 2 &&
      latest.videoTime > before.videoTime
    ) {
      return latest;
    }
  }
  throw new Error(`receiver stall probe recovery did not become ready: ${JSON.stringify({ before, latest, probe })}`);
}

async function exerciseLocalWithhold(cdp, clients, beforeByClient, timeoutMs, count) {
  await Promise.all(clients.map((client) =>
    applyControlAction(cdp, client.sessionId, { type: "withhold", count })
  ));
  const recoveredByClient = await Promise.all(clients.map((client, i) =>
    waitForLocalWithholdRecovery(cdp, client.sessionId, beforeByClient[i], timeoutMs, count)
  ));
  await sleep(1000);
  const afterRecoveryByClient = await Promise.all(clients.map((client) =>
    readStats(cdp, client.sessionId)
  ));
  return {
    count,
    clients: recoveredByClient.map((stats, i) => ({
      client: i + 1,
      withheldAUs: numericDelta(
        beforeByClient[i]?.senderWithheldAUs,
        stats?.senderWithheldAUs,
      ),
      packetizerRecoveries: numericDelta(
        beforeByClient[i]?.senderPacketizerRecoveries,
        stats?.senderPacketizerRecoveries,
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
        beforeByClient[i]?.rxRepairRequests,
        stats?.rxRepairRequests,
      ),
      recovered: stats,
      afterRecovery: afterRecoveryByClient[i],
    })),
    afterRecoveryByClient,
  };
}

async function waitForLocalWithholdRecovery(cdp, sessionId, before, timeoutMs, count) {
  const deadline = Date.now() + timeoutMs;
  let latest = null;
  while (Date.now() < deadline) {
    await sleep(250);
    latest = await readStats(cdp, sessionId);
    const withheld = numericDelta(before?.senderWithheldAUs, latest?.senderWithheldAUs);
    const recoveries = numericDelta(
      before?.senderPacketizerRecoveries,
      latest?.senderPacketizerRecoveries,
    );
    const forcedKeys = numericDelta(before?.senderForcedKeys, latest?.senderForcedKeys);
    const decoded = numericDelta(before?.rxDecoded, latest?.rxDecoded);
    const lost = numericDelta(before?.rxLost, latest?.rxLost);
    const repairs = numericDelta(before?.rxRepairRequests, latest?.rxRepairRequests);
    if (
      withheld !== null &&
      withheld >= count &&
      recoveries !== null &&
      recoveries >= count &&
      forcedKeys !== null &&
      forcedKeys >= count &&
      decoded !== null &&
      decoded >= 1 &&
      (lost === null || lost === 0) &&
      (repairs === null || repairs === 0) &&
      latest.videoReadyState >= 2 &&
      latest.videoTime > before.videoTime
    ) {
      return latest;
    }
  }
  throw new Error(`local withhold decode recovery did not become ready: ${JSON.stringify({ before, latest, count })}`);
}

function controlActionExpression(action) {
  const encoded = JSON.stringify(action);
  return `(() => {
    const action = ${encoded};
    if (action.type === "spatial") {
      const button = document.querySelector("button[data-cap='" + action.cap + "']");
      if (!button) throw new Error("missing spatial cap button " + action.cap);
      button.click();
      return {type: "spatial", cap: action.cap};
    }
    if (action.type === "keyframe") {
      const button = document.getElementById("kf");
      if (!button) throw new Error("missing keyframe button");
      button.click();
      return {type: "keyframe"};
    }
    if (action.type === "screen") {
      const button = document.querySelector("button[data-screen='" + action.mode + "']");
      if (!button) throw new Error("missing screen mode button " + action.mode);
      button.click();
      return {type: "screen", mode: action.mode};
    }
    if (action.type === "bitrate") {
      const input = document.getElementById("bitrate");
      if (!input) throw new Error("missing bitrate input");
      input.value = String(action.kbps);
      input.dispatchEvent(new Event("input", {bubbles: true}));
      return {type: "bitrate", kbps: Number(input.value)};
    }
    if (action.type === "pause") {
      const button = document.getElementById("pause");
      if (!button) throw new Error("missing pause button");
      if (typeof paused !== "boolean") throw new Error("missing pause state");
      if (paused !== !!action.paused) button.click();
      return {type: "pause", paused};
    }
    if (action.type === "withhold") {
      const count = Number.isFinite(action.count) ? action.count : 1;
      sendCtl({type: "withhold", count});
      return {type: "withhold", count};
    }
    throw new Error("unknown control action " + action.type);
  })()`;
}

function nextControlAction(opts, sampleIndex) {
  if (opts.controlChurn) return controlChurnAction(sampleIndex);
  if (opts.tuningChurn) return tuningChurnAction(sampleIndex);
  return null;
}

function controlChurnAction(sampleIndex) {
  const sequence = [
    { type: "spatial", cap: 2, requiresForcedKey: true },
    { type: "keyframe", requiresForcedKey: true },
    { type: "spatial", cap: 3, requiresForcedKey: true },
    { type: "keyframe", requiresForcedKey: true },
  ];
  return sequence[sampleIndex % sequence.length];
}

function tuningChurnAction(sampleIndex) {
  const sequence = [
    { type: "bitrate", kbps: 1200 },
    { type: "screen", mode: 1, requiresForcedKey: true },
    { type: "bitrate", kbps: 600 },
    { type: "screen", mode: 2, requiresForcedKey: true },
    { type: "bitrate", kbps: 900 },
    { type: "screen", mode: 0, requiresForcedKey: true },
  ];
  return sequence[sampleIndex % sequence.length];
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
  } else if (value <= 0) {
    throw new Error(`${name} must be positive`);
  }
  if (opts.max !== undefined && value > opts.max) {
    throw new Error(`${name} must be <= ${opts.max}`);
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

function booleanFlag(name) {
  return process.argv.includes(name);
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
      const rtc = typeof latestRTCStats === "object" && latestRTCStats ? latestRTCStats : {};
      const repairRequests = typeof receiverRepairRequests === "number" ? receiverRepairRequests : num(rows["rx repair"]);
      const repairStreak = typeof receiverRepairStreak === "number" ? receiverRepairStreak : null;
      const receiverSpatialCap = typeof receiverRequestedSpatialCap === "number" ? receiverRequestedSpatialCap : num(rows["rx cap"]);
      return {
        status: document.getElementById("status")?.textContent ?? null,
        frame: num(rows["frame #"]),
        activeLayers: raw.settings?.active_spatial_layers ?? null,
        requestedLayers: raw.settings?.requested_spatial_layers ?? null,
        targetKbps: num(raw.settings?.target_kbps),
        screenMode: num(raw.settings?.screen_mode),
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
        senderForcedKeys: typeof senderForcedKeyCount === "number" ? senderForcedKeyCount : num(rows["forced keys"]),
        senderPacketizerRecoveries: typeof senderPacketizerRecoveryCount === "number" ? senderPacketizerRecoveryCount : num(rows["pkt recoveries"]),
        senderFailedEncodeAUs: num(sender.failed_encode_aus) ?? num(rows["encode fails"]) ?? 0,
        senderFailedEncodedAUs: num(sender.failed_encoded_aus) ?? num(rows["encoded drops"]) ?? 0,
        senderWithheldAUs: num(sender.withheld_aus) ?? num(rows["withheld AUs"]) ?? 0,
        rxDecoded: num(rows["rx decoded"]),
        rxDropped: num(rows["rx dropped"]),
        rxLost: num(rows["rx lost"]),
        rxFreezes: num(rows["rx freezes"]),
        rxFreezeDuration: num(rtc.totalFreezesDuration) ?? num(rows["rx freeze s"]),
        rxPauseCount: num(rtc.pauseCount) ?? num(rows["rx pauses"]),
        rxPauseDuration: num(rtc.totalPausesDuration) ?? num(rows["rx pause s"]),
        rxNackCount: num(rtc.nackCount) ?? num(rows["rx nack"]),
        rxPliCount: num(rtc.pliCount) ?? num(rows["rx pli"]),
        rxFirCount: num(rtc.firCount) ?? num(rows["rx fir"]),
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
    maxActiveTopLayerThreads: maxNumber(values("activeTopLayerThreads")),
    maxActiveTopLayerTileCols: maxNumber(values("activeTopLayerTileCols")),
    maxEncodeMs: maxNumber(values("encodeMs")),
    maxAccessUnitMs: maxNumber(values("accessUnitMs")),
    maxScheduleLagMs: maxNumber(values("scheduleLagMs")),
    maxSenderCapOverrunStreak: maxNumber(values("senderCapOverrunStreak")),
    maxSenderCapRecoveryStreak: maxNumber(values("senderCapRecoveryStreak")),
    maxRxRepairRequests: maxNumber(values("rxRepairRequests")),
    maxSenderFailedEncodeAUs: maxNumber(values("senderFailedEncodeAUs")),
    maxSenderFailedEncodedAUs: maxNumber(values("senderFailedEncodedAUs")),
    maxSenderWithheldAUs: maxNumber(values("senderWithheldAUs")),
    minRxSpatialCap: minNumber(values("rxSpatialCap")),
    maxSenderForcedKeys: maxNumber(values("senderForcedKeys")),
    maxSenderPacketizerRecoveries: maxNumber(values("senderPacketizerRecoveries")),
  };
}

function summarizeSampleClients(sampleClients) {
  const summaries = sampleClients.map((client) => client.summary);
  const seconds = sampleClients.map((client) => client.second);
  const deltas = sampleClients.map((client) => client.delta);
  return summarizeStatsGroup(summaries, deltas, seconds, seconds);
}

function summarizeRun(samples, deltas, seconds, firsts = []) {
  const sampleClients = samples.flatMap((sample) =>
    Array.isArray(sample.clients) ? sample.clients : [sample]
  );
  const summaries = sampleClients.map((client) => client.summary);
  const sampleSeconds = [
    ...(Array.isArray(firsts) ? firsts : [firsts]),
    ...sampleClients.map((client) => client.second),
  ];
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
    freezeDuration: deltaSum("rxFreezeDuration"),
    pauses: deltaSum("rxPauseCount"),
    pauseDuration: deltaSum("rxPauseDuration"),
    nacks: deltaSum("rxNackCount"),
    plis: deltaSum("rxPliCount"),
    firs: deltaSum("rxFirCount"),
    forcedKeys: deltaSum("senderForcedKeys"),
    minClientForcedKeys: minNumber(deltaValues("senderForcedKeys")),
    packetizerRecoveries: deltaSum("senderPacketizerRecoveries"),
    videoTime: deltaSum("videoTime"),
    minClientVideoTime: minNumber(deltaValues("videoTime")),
    endingActiveLayers: minNumber(secondValues("activeLayers")),
    minSampleEndingActiveLayers: minNumber(sampleSecondValues("activeLayers")),
    minPolledActiveLayers: minNumber(summaryValues("minActiveLayers")),
    maxActiveTopLayerThreads: maxNumber([
      ...summaryValues("maxActiveTopLayerThreads"),
      ...sampleSecondValues("activeTopLayerThreads"),
    ]),
    maxActiveTopLayerTileCols: maxNumber([
      ...summaryValues("maxActiveTopLayerTileCols"),
      ...sampleSecondValues("activeTopLayerTileCols"),
    ]),
    maxAccessUnitMs: maxNumber(summaryValues("maxAccessUnitMs")),
    maxScheduleLagMs: maxNumber(summaryValues("maxScheduleLagMs")),
    maxRxRepairRequests: maxNumber(summaryValues("maxRxRepairRequests")),
    maxSenderFailedEncodeAUs: maxNumber(summaryValues("maxSenderFailedEncodeAUs")),
    maxSenderFailedEncodedAUs: maxNumber(summaryValues("maxSenderFailedEncodedAUs")),
    maxSenderWithheldAUs: maxNumber(summaryValues("maxSenderWithheldAUs")),
    minRxSpatialCap: minNumber(summaryValues("minRxSpatialCap")),
    maxSenderForcedKeys: maxNumber(summaryValues("maxSenderForcedKeys")),
    maxSenderPacketizerRecoveries: maxNumber(summaryValues("maxSenderPacketizerRecoveries")),
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
    freezeDuration: sum("freezeDuration"),
    pauses: sum("pauses"),
    pauseDuration: sum("pauseDuration"),
    nacks: sum("nacks"),
    plis: sum("plis"),
    firs: sum("firs"),
    forcedKeys: sum("forcedKeys"),
    minClientForcedKeys: minNumber(values("minClientForcedKeys")),
    packetizerRecoveries: sum("packetizerRecoveries"),
    videoTime: sum("videoTime"),
    minClientVideoTime: minNumber(values("minClientVideoTime")),
    minEndingActiveLayers: minNumber(values("endingActiveLayers")),
    minSampleEndingActiveLayers: minNumber(values("minSampleEndingActiveLayers")),
    minPolledActiveLayers: minNumber(values("minPolledActiveLayers")),
    maxActiveTopLayerThreads: maxNumber(values("maxActiveTopLayerThreads")),
    maxActiveTopLayerTileCols: maxNumber(values("maxActiveTopLayerTileCols")),
    maxAccessUnitMs: maxNumber(values("maxAccessUnitMs")),
    maxScheduleLagMs: maxNumber(values("maxScheduleLagMs")),
    maxRxRepairRequests: maxNumber(values("maxRxRepairRequests")),
    maxSenderFailedEncodeAUs: maxNumber(values("maxSenderFailedEncodeAUs")),
    maxSenderFailedEncodedAUs: maxNumber(values("maxSenderFailedEncodedAUs")),
    maxSenderWithheldAUs: maxNumber(values("maxSenderWithheldAUs")),
    minRxSpatialCap: minNumber(values("minRxSpatialCap")),
    maxSenderForcedKeys: maxNumber(values("maxSenderForcedKeys")),
    maxSenderPacketizerRecoveries: maxNumber(values("maxSenderPacketizerRecoveries")),
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
  for (const key of ["frame", "rxDecoded", "rxDropped", "rxLost", "rxFreezes", "rxFreezeDuration", "rxPauseCount", "rxPauseDuration", "rxNackCount", "rxPliCount", "rxFirCount", "videoTime", "senderForcedKeys", "senderPacketizerRecoveries", "senderWithheldAUs", "rxRepairRequests"]) {
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
  for (const key of ["rxFreezeDuration", "rxPauseCount", "rxPauseDuration"]) {
    if (delta[key] !== null && delta[key] > 0) {
      throw new Error(`${sampleLabel(opts)} ${key} advanced during clean smoke: ${delta[key]}; ${sampleDetails(first, second, delta, opts.summary)}`);
    }
  }
  if (
    Number.isFinite(opts.summary.maxRxRepairRequests) &&
    opts.summary.maxRxRepairRequests > opts.maxRxRepairRequests
  ) {
    throw new Error(`${sampleLabel(opts)} receiver repair requests reached ${opts.summary.maxRxRepairRequests}, want <= ${opts.maxRxRepairRequests}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  for (const [key, max, label] of [
    ["rxNackCount", opts.maxRxNackDelta, "receiver NACK"],
    ["rxPliCount", opts.maxRxPliDelta, "receiver PLI"],
    ["rxFirCount", opts.maxRxFirDelta, "receiver FIR"],
  ]) {
    if (delta[key] !== null && delta[key] > max) {
      throw new Error(`${sampleLabel(opts)} ${label} count advanced by ${delta[key]}, want <= ${max}; ${sampleDetails(first, second, delta, opts.summary)}`);
    }
  }
  if (
    Number.isFinite(opts.summary.maxSenderFailedEncodeAUs) &&
    opts.summary.maxSenderFailedEncodeAUs > opts.maxSenderFailedEncodeAUs
  ) {
    throw new Error(`${sampleLabel(opts)} sender failed encode access units reached ${opts.summary.maxSenderFailedEncodeAUs}, want <= ${opts.maxSenderFailedEncodeAUs}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (
    Number.isFinite(opts.summary.maxSenderFailedEncodedAUs) &&
    opts.summary.maxSenderFailedEncodedAUs > opts.maxSenderFailedEncodedAUs
  ) {
    throw new Error(`${sampleLabel(opts)} sender failed encoded access units reached ${opts.summary.maxSenderFailedEncodedAUs}, want <= ${opts.maxSenderFailedEncodedAUs}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (opts.controlAction?.requiresForcedKey && (delta.senderForcedKeys === null || delta.senderForcedKeys < 1)) {
    throw new Error(`${sampleLabel(opts)} ${opts.controlAction.type} action did not produce a sender forced keyframe; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (
    opts.controlAction?.type === "bitrate" &&
    second.targetKbps !== opts.controlAction.kbps
  ) {
    throw new Error(`${sampleLabel(opts)} bitrate action target ${second.targetKbps}, want ${opts.controlAction.kbps}; ${sampleDetails(first, second, delta, opts.summary)}`);
  }
  if (
    opts.controlAction?.type === "screen" &&
    second.screenMode !== opts.controlAction.mode
  ) {
    throw new Error(`${sampleLabel(opts)} screen action mode ${second.screenMode}, want ${opts.controlAction.mode}; ${sampleDetails(first, second, delta, opts.summary)}`);
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

function assertRunSmoke(summary, opts) {
  if (
    opts.requireThreadedTopLayer &&
    (!Number.isFinite(summary.maxActiveTopLayerThreads) ||
      summary.maxActiveTopLayerThreads < 2 ||
      !Number.isFinite(summary.maxActiveTopLayerTileCols) ||
      summary.maxActiveTopLayerTileCols < 2)
  ) {
    throw new Error(`threaded top-layer tile layout was not observed: ${JSON.stringify(summary)}`);
  }
  if (
    opts.pauseResume &&
    (!opts.pauseResumeResult ||
      !Array.isArray(opts.pauseResumeResult.clients) ||
      opts.pauseResumeResult.clients.length !== opts.clients ||
      opts.pauseResumeResult.clients.some((client) =>
        client.forcedKeysAfterResume === null ||
        client.forcedKeysAfterResume < 1 ||
        client.decodedAfterResume === null ||
        client.decodedAfterResume < 1))
  ) {
    throw new Error(`pause/resume did not produce clean forced-key decode recovery: ${JSON.stringify({ summary, pauseResume: opts.pauseResumeResult })}`);
  }
  if (
    opts.localWithhold &&
    (!opts.localWithholdResult ||
      !Array.isArray(opts.localWithholdResult.clients) ||
      opts.localWithholdResult.clients.length !== opts.clients ||
      opts.localWithholdResult.clients.some((client) =>
        client.withheldAUs === null ||
        client.withheldAUs < opts.localWithholdCount ||
        client.packetizerRecoveries === null ||
        client.packetizerRecoveries < opts.localWithholdCount ||
        client.forcedKeys === null ||
        client.forcedKeys < opts.localWithholdCount ||
        client.decodedAfterWithhold === null ||
        client.decodedAfterWithhold < 1 ||
        (client.lostAfterWithhold !== null && client.lostAfterWithhold !== 0) ||
        (client.repairedAfterWithhold !== null && client.repairedAfterWithhold !== 0)))
  ) {
    throw new Error(`local withhold did not produce clean packetizer recovery: ${JSON.stringify({ summary, localWithhold: opts.localWithholdResult })}`);
  }
  if (
    opts.receiverStallProbe &&
    (!opts.receiverStallProbeResult ||
      !Array.isArray(opts.receiverStallProbeResult.clients) ||
      opts.receiverStallProbeResult.clients.length !== opts.clients ||
      opts.receiverStallProbeResult.clients.some((client) => {
        const sentTypes = Array.isArray(client.sent)
          ? client.sent.map((msg) => msg?.type)
          : [];
        return !sentTypes.includes("keyframe") ||
          !sentTypes.includes("spatial") ||
          client.repairRequests < 1 ||
          client.receiverSpatialCap >= 3 ||
          client.forcedKeysAfterStall === null ||
          client.forcedKeysAfterStall < 1 ||
          client.decodedAfterStall === null ||
          client.decodedAfterStall < 1 ||
          (client.lostAfterStall !== null && client.lostAfterStall !== 0) ||
          client.repairedAfterStall === null ||
          client.repairedAfterStall < 1;
      }))
  ) {
    throw new Error(`receiver stall probe did not produce clean forced-key recovery: ${JSON.stringify({ summary, receiverStallProbe: opts.receiverStallProbeResult })}`);
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
