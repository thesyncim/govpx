package govpx

// vp9AdjustFrameRate ports libvpx adjust_frame_rate (vp9/encoder/vp9_encoder.c:5753),
// which runs at the top of vp9_get_compressed_data for every visible frame,
// before vp9_rc_get_one_pass_{cbr,vbr}_params. It derives cpi->framerate from
// the source presentation-timestamp deltas and feeds the result into
// vp9_new_framerate / vp9_rc_update_framerate so rc->avg_frame_bandwidth tracks
// the real frame cadence.
//
// govpx has no caller-supplied PTS: the byte-parity oracle (vpxenc-vp9-frameflags)
// drives libvpx with pts += frame_duration and frame_duration == 1 under the
// exact-fps timebase, so the implicit PTS is the encode-call index. The
// timebase ratio (g_timebase_in_ts) is recomputed whenever the configured fps
// changes (vp9_change_config -> set_encoder_config), which is what makes a
// mid-stream fps change reach rate control on the *next* encoded frame rather
// than instantly at config time.
//
// This must run after any pending runtime config change has been applied (the
// runtime-control hook fires before EncodeInto, mirroring vpx_codec_enc_config_set
// preceding vpx_codec_encode) and before the frame target / pre-encode buffer
// update in vp9_encoder_frame.go.
func (e *VP9Encoder) vp9AdjustFrameRate(showFrame bool) {
	pts := e.vp9PTS
	// Advance the implicit PTS once per encode call, matching the driver's
	// `if (have_input) pts += frame_duration` with frame_duration == 1.
	e.vp9PTS = saturatingAddUint64(pts, 1)

	if !showFrame || !e.rc.enabled {
		return
	}
	if e.sourceTS.ratioNum <= 0 || e.sourceTS.ratioDen <= 0 {
		return
	}

	start := e.sourceTS.timestampTicks(pts)
	end := max(e.sourceTS.timestampTicks(saturatingAddUint64(pts, 1)), start)

	if start < e.sourceTS.firstTimestampEver {
		e.sourceTS.firstTimestampEver = start
		e.sourceTS.lastEndSeen = start
	}

	var thisDuration int64
	step := 0
	if start == e.sourceTS.firstTimestampEver {
		thisDuration = end - start
		step = 1
	} else {
		thisDuration = end - e.sourceTS.lastEndSeen
		lastDuration := e.sourceTS.lastEndSeen - e.sourceTS.lastTimestampSeen
		if thisDuration > maxInt64Value/10 {
			thisDuration = maxInt64Value / 10
		}
		if lastDuration != 0 {
			step = int((thisDuration - lastDuration) * 10 / lastDuration)
		}
	}

	if thisDuration != 0 {
		if step != 0 {
			e.sourceTS.refFrameRate = float64(libvpxTimestampTicksPerSecond) / float64(thisDuration)
		} else {
			// Average this frame's rate into the last second's average frame
			// rate (vp9_encoder.c:5776-5785).
			interval := float64(end - e.sourceTS.firstTimestampEver)
			if interval > float64(libvpxTimestampTicksPerSecond) {
				interval = float64(libvpxTimestampTicksPerSecond)
			}
			avgDuration := float64(libvpxTimestampTicksPerSecond) / e.sourceTS.refFrameRate
			avgDuration *= interval - avgDuration + float64(thisDuration)
			avgDuration /= interval
			e.sourceTS.refFrameRate = float64(libvpxTimestampTicksPerSecond) / avgDuration
		}
		e.rc.vp9NewFramerate(e.sourceTS.refFrameRate)
	}

	e.sourceTS.lastTimestampSeen = start
	e.sourceTS.lastEndSeen = end
}

// vp9RefreshSourceTimestampRatio recomputes the source-timestamp tick ratio
// from the current configured timebase. libvpx recomputes
// oxcf->g_timebase_in_ts inside set_encoder_config (vp9/vp9_cx_iface.c:525) on
// every vpx_codec_enc_config_set, so a runtime fps change updates the
// pts->ticks conversion used by the *next* adjust_frame_rate. Only the ratio is
// refreshed; the running firstTimestampEver / lastEndSeen / refFrameRate
// bookkeeping is preserved so the cadence EMA continues across the change.
func (e *VP9Encoder) vp9RefreshSourceTimestampRatio(timing timingState) {
	fresh := newEncoderSourceTimestampState(timing)
	e.sourceTS.ratioNum = fresh.ratioNum
	e.sourceTS.ratioDen = fresh.ratioDen
}
