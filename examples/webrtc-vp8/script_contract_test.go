package main

import (
	"os"
	"strings"
	"testing"
)

func TestVP8WebRTCBrowserSmokeScriptEnforcesBudgets(t *testing.T) {
	src := readLocalFile(t, "browser_smoke.mjs")

	for _, want := range []string{
		`spawn("go", serverArgs`,
		`"run", ".", "-addr"`,
		`RTCPeerConnection`,
		`pc.getStats()`,
		`video/VP8`,
		`--sample-ms`,
		`--soak-ms`,
		`--poll-ms`,
		`--cdp-timeout-ms`,
		`--min-decoded-delta`,
		`--min-video-time-ratio`,
		`--cpu-burners`,
		`CDP.connect(chrome.wsURL, opts.cdpTimeoutMs)`,
		`CDP ${method}${suffix} timed out after ${this.timeoutMs} ms`,
		`decoded frames did not advance enough`,
		`video time did not advance enough`,
		`receiver NACK`,
		`receiver PLI`,
		`receiver FIR`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("browser_smoke.mjs missing %q", want)
		}
	}

	for _, want := range []string{
		`maxRxLostDelta: integerFlag("--max-rx-lost-delta", 0`,
		`maxRxDroppedDelta: integerFlag("--max-rx-dropped-delta", 0`,
		`maxRxFreezesDelta: integerFlag("--max-rx-freezes-delta", 0`,
		`maxRxFreezeDurationDelta: numberFlag("--max-rx-freeze-duration-delta", 0`,
		`maxRxRepairDelta: integerFlag("--max-rx-repair-delta", 0`,
		`maxRxNackDelta: integerFlag("--max-rx-nack-delta", 0`,
		`maxRxPliDelta: integerFlag("--max-rx-pli-delta", 0`,
		`maxRxFirDelta: integerFlag("--max-rx-fir-delta", 0`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("browser_smoke.mjs does not default %q to zero", want)
		}
	}
}

func TestVP8WebRTCProductionGateRunsFocusedSmoke(t *testing.T) {
	src := readLocalFile(t, "production_gate.mjs")

	for _, want := range []string{
		`TestVP8OracleVpxdecDecodesEncodeIntoKeyFrame`,
		`TestVP8OracleOutputParityMatrix`,
		`TestVP8OracleEncoderStreamByteParityTemporalSVC`,
		`"go"`,
		`"test", ".", "-run", focusedGoPattern, "-count=1"`,
		`"-tags", "govpx_oracle_trace"`,
		`GOVPX_WITH_ORACLE: "1"`,
		`requiresOracle: true`,
		`assertNoOracleSkips(step, output.stdout)`,
		`line.startsWith("--- SKIP:")`,
		`"node"`,
		`"--check", "browser_smoke.mjs"`,
		`"browser_smoke.mjs"`,
		`"--min-decoded-delta", String(browserMinDecoded)`,
		`"--min-video-time-ratio", String(browserMinVideoRatio)`,
		`"--max-rx-lost-delta", "0"`,
		`"--max-rx-freezes-delta", "0"`,
		`"--max-rx-repair-delta", "0"`,
		`"--max-rx-nack-delta", "0"`,
		`"--max-rx-pli-delta", "0"`,
		`"--max-rx-fir-delta", "0"`,
		`VP8_WEBRTC_GATE_CPU_BURNERS`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("production_gate.mjs missing %q", want)
		}
	}
	if strings.Contains(src, "webrtc-vp9") || strings.Contains(src, "VP9") {
		t.Fatalf("production gate must stay VP8-scoped")
	}
}

func TestReadmeDocumentsVP8BrowserGate(t *testing.T) {
	src := readLocalFile(t, "README.md")

	for _, want := range []string{
		`node browser_smoke.mjs`,
		`node production_gate.mjs`,
		`--min-decoded-delta`,
		`--min-video-time-ratio`,
		`--cpu-burners`,
		`zero packet loss, freezes, repair packets`,
		`NACK, PLI, and FIR by default`,
		`The demo server itself only exposes`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
}

func readLocalFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return string(raw)
}
