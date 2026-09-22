# shop-alarm — Workshop Alarm System

State machine and supervision for the workshop alarm: arming preconditions, exit/entry delays, trigger fusion (door, person detection, PIR, fire/smoke audio), Frigate profile switching, supervision of the shop bridge and camera, audit events, and the monitoring exporter for the werkstatt dashboard.

Everything is MQTT-only: the service subscribes to the door/PIR/Frigate/bridge topics on the opus broker and publishes the alarm state, fault, attributes, audit events and HA discovery. The shop Pi and its broker are untrusted; opus is the trust boundary.

Status: implemented, covered by `go test ./...` (state machine, engine wiring via a fake MQTT client, metrics) and deployed on opus as a Docker container (ansible role in the `opus` repo, image `ghcr.io/uhlig-it/shop-alarm`, auto-updated by watchtower). The Home Assistant migration is applied: the MQTT alarm control panel (discovery) is live and the old button automations are replaced. The normative design and the dated verification history are maintained beside this checkout (`DESIGN.md`, `docs/verification-log.md`).

## Run

```sh
MQTT_URL=tcp://localhost:1883 MQTT_USER=shop-alarm MQTT_PASSWORD=... \
STATE_FILE=/var/lib/shop-alarm/state.json HEALTH_ADDR=:9101 \
./shop-alarm
```

## Configuration (environment variables)

| Variable | Default | Meaning |
|---|---|---|
| `MQTT_URL`, `MQTT_USER`, `MQTT_PASSWORD` | `tcp://localhost:1883` | broker connection |
| `TOPIC_PREFIX` | `werkstatt` | root of the alarm topics |
| `STATE_FILE` | `/var/lib/shop-alarm/state.json` | persisted state and deadlines |
| `HEALTH_ADDR` | `:8080` | serves `/healthz` and `/metrics` (set `:9101` for the scrape target) |
| `EXIT_DELAY` | `60s` | arming → door-closed-or-arm deadline |
| `DOOR_ESCALATION_DELAY` | `60s` | extra delay with the door still open → triggered |
| `ENTRY_DELAY` | `30s` | pending → triggered |
| `SUPERVISION_CAMERA_DELAY` | `30s` | camera loss while armed → triggered |
| `SUPERVISION_BRIDGE_DELAY` | `5m` | bridge loss while armed → triggered |
| `RECONCILE_DELAY` | `2s` | retained-state settle before reconcile |
| `VERIFY_INITIAL_DELAY`, `VERIFY_RETRY_DELAY` | `2s`, `5s` | Frigate profile verification cadence |
| `PROFILE_MISMATCH_NOTIFY_INTERVAL` | `10m` | notification rate limit for switch-state mismatches |
| `LWT_TOPICS` | *(empty)* | device LWT subscription patterns for `werkstatt_device_up` (e.g. `tele/+/LWT`) |
| `FRIGATE_API_URL`, `FRIGATE_API_USER`, `FRIGATE_API_PASSWORD` | *(empty)* | Frigate API (snapshots, `/api/stats`) |
| `FRIGATE_CAMERA` | `werkstatt` | camera name for the stats fields |
| `STATS_INTERVAL` | `30s` | stats poll cadence |
| `NTFY_URL` | *(empty)* | ntfy backup channel |
| `LIVE_STREAM_URL` | *(empty)* | appended to notifications |
| `DISCOVERY_PREFIX` | `homeassistant` | HA discovery prefix |

## MQTT contract (topics under `<TOPIC_PREFIX>`)

* `<prefix>/alarm/cmnd` — `ARM_AWAY`, `DISARM`, `ACK` (never retained)
* `<prefix>/alarm/state` — retained, HA vocabulary: `disarmed`, `arming`, `armed_away`, `pending`, `triggered`
* `<prefix>/alarm/fault`, `<prefix>/alarm/attributes`, `<prefix>/alarm/available` (LWT), `<prefix>/alarm/events` (audit)
* `frigate/profile/set` — `armed`/`disarmed` (reconciliation + switching)
* supervision inputs: `frigate/available`, `frigate/<cam>/detect/state`, `/recordings/state`, `/status/detect`, `frigate/reviews`, `frigate/profile/state`, `$SYS/broker/connection/<remote>/state`

## Monitoring

`/metrics` exposes the `werkstatt-*` gauges consumed by the monitoring dashboard (alarm state, door, bridge, Frigate switches/profile, device LWTs, Frigate stats). Metric contract, dashboard and alerting rules: `monitoring/README.md`; Grafana dashboard `monitoring/werkstatt-dashboard.json` in this repo, live vmui dashboard deployed via `uhlig-it/metrics` to the VictoriaMetrics on soda.

## Home Assistant

The MQTT alarm control panel is registered via discovery by shop-alarm itself (no YAML needed). The HA-app notification automations (triggered push with snapshot + live-stream link, arming/supervision fault pushes, fire/smoke while disarmed, ACK mute) are drafted in `ha/notifications.yaml` — append to `/opt/homeassistant/automations.yaml` on opus; the file header lists the required helpers and placeholders. The panel was renamed to `alarm_control_panel.werkstatt` in the HA UI (2026-09-22).

## License

Licensed under the EUPL-1.2 (see `LICENSE`).
