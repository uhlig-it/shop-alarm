# Runbook — operating the workshop alarm

What to do once the alarm has triggered, and where to look when something seems off. The normative design is `DESIGN.md` (beside this checkout) and the dated history is `docs/verification-log.md`; this file is the operator's view.

## Review the event

* Frigate UI — Videos → Review (and the Events/History view): `https://eye.tailnet-204f.ts.net`. Reachable from the phone over the tailnet; this is the link the HA push carries.
* The ntfy backup message carries `LIVE_STREAM_URL` instead (`http://shop:8888/cam/`, the shop-side mediamtx URL — LAN/tailnet only).
* API forensics: `curl 'http://opus:5000/api/events?limit=20'` or `curl 'http://opus:5000/api/events?label=person&limit=20'` (label, camera, `start_time`/`end_time`, `data.max_severity`). The `/api/reviews` endpoint 404s on the deployed 0.18 (`Camera not found`) — use `/api/events` and the UI.
* The audit stream `werkstatt/alarm/events` (not retained) carries every command, transition, trigger, fault and ACK as JSON. Subscribe to it **before** reproducing a problem: `mosquitto_sub -t 'werkstatt/alarm/events' -v`.

## Clear the alarm state

The state stays `triggered` until someone **disarms** it, and the 2-minute repeats (HA push + shop-alarm's ntfy backup) keep coming until then. Two different actions:

* **Silence (`ACK`)** — stops the notifications and the strobe (blink1 and the room lights); the state stays `triggered` and the panel stays red. Use it while you review the footage. It also turns HA's `input_boolean.werkstatt_alarm_muted` on, which is what suppresses the HA repeats.
* **Disarm** — resolves the episode: state → `disarmed`, everything stops, and Frigate switches to the `disarmed` profile (detect, motion and recording off — the privacy floor). The alarm is inactive until you arm it again.

### Silence without disarming (`ACK`)

* **In the Home Assistant app: the button `button.werkstatt_alarm_ack`, labelled "Werkstatt silence (ACK)".** It is available only while the alarm is `triggered` (greyed out otherwise), so it cannot be pressed by accident. Put it on a dashboard, or into an iOS widget/shortcut, for one-tap access.
* From a shell: `mosquitto_pub -h <broker> -u <user> -P <pass> -t werkstatt/alarm/cmnd -m ACK`.
* Toggling `input_boolean.werkstatt_alarm_muted` by hand only silences the HA pushes — it does **not** stop the strobe or shop-alarm's ntfy repeats. Use `ACK` for both.
* `ACK` while the alarm is not triggered does nothing (the mute is guarded on the `triggered` state), so it cannot silence a future alarm.

### Disarm

If you just want everything to stop, **Disarm does that too** — the panel's Disarm button silences the notifications and the strobe *and* resolves the episode. The `ACK` button above is only for "quiet, but leave it triggered while I review".

1. Home Assistant app → the `Werkstatt` alarm panel card → **Disarm** (`alarm_control_panel.werkstatt`).
2. NFC tag at either workshop door (the tag automations call the panel's disarm).
3. iOS shortcut **"Disarm Shop"**.
4. Home Assistant → Developer Tools → Actions → `alarm_control_panel.disarm`, target `entity_id: alarm_control_panel.werkstatt`.
5. From a shell with broker access: `mosquitto_pub -h <broker> -u <user> -P <pass> -t werkstatt/alarm/cmnd -m DISARM`. This topic is **never retained** — a retained command would replay on every consumer restart.

After disarming: review, fix whatever caused it, and re-arm when you leave (arm from the panel/NFC/iOS, or publish `ARM_AWAY`). Arming runs the 60 s exit delay and switches Frigate to the `armed` profile.

## The door/exit gotcha (cause of the 2026-09-24 false alarm)

Arming assumes "the door closed ⇒ you have left". If the door was **already open** when you armed:

1. `ARM_AWAY` → `arming` (the open door is treated as part of the exit).
2. you close the door → `armed_away` (that exit rule fires immediately).
3. you open the door again to actually step out → `pending`, the 30 s entry delay starts.
4. if you do not disarm within those 30 s → `triggered` (source `entry_delay`).

So arm **after** the door is closed, or arm and leave in one motion (arm → open → close, without re-opening). The state machine is intentionally unchanged here (user decision 2026-09-24); this is a usage case, not a bug.

## Which trigger was it?

The audit event and the ntfy text carry the source:

* `entry_delay` — a `pending` ran out; the `pending` came from the door opening (or a person review, once person detection is armed) while `armed_away`.
* `door_open` — arming with the door still open past the exit delay **and** the extra escalation grace (120 s total).
* `camera_loss` — `frigate/<cam>/status/detect` offline, or `frigate/available` offline **and** the Frigate API unreachable, for ≥ 30 s while armed.
* `bridge_loss` — the shop↔opus MQTT bridge down for ≥ 5 min while armed.
* `fire`, `person`, `pir` — audio fire/smoke, a person review, or a PIR.

Live state: the retained `werkstatt/alarm/attributes` has `state`, `since`, `door`, `pirs`, `audio`, active `faults` and `review_id`; the retained `werkstatt/alarm/fault` is the oldest active fault (empty when none).

## One-place health

* vmui dashboard "Workshop alarm": `https://metrics.tailnet-204f.ts.net/vmui` (alarm state, door, bridge, camera, detect/recordings, devices).
* shop-alarm directly: `curl http://opus:9101/metrics`.
* Log: `ssh opus 'docker logs --since 1h shop-alarm'`.

## Known gotchas

* `frigate/available` and `werkstatt/alarm/available` are retained LWTs that go stale after a broker restart; an opus cron guard (`roles/mosquitto` in the opus repo) republishes `online` every 2 min while the owner answers its health probe. A stale `offline` is therefore not evidence of a dead service.
* That same 2-minute guard republish makes shop-alarm reconcile — one `frigate/profile/set` plus a retained state republish every 2 min. Benign, but it is visible in the log.
* While `disarmed` there is no detection, motion or recording, so the camera sees nothing and no person/camera trigger can fire.
* `/api/reviews` returns 404 on Frigate 0.18 (`Camera not found`); the review data lives in the Frigate UI and in `/api/events`.
