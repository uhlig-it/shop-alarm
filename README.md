# Workshop Alarm — shop-alarm

State machine and supervision for the workshop alarm: arming preconditions, exit/entry delays, trigger fusion (door, person detection, PIR, fire/smoke audio), Frigate profile switching, supervision of the shop bridge and camera, audit events, and the monitoring exporter for the werkstatt dashboard.

Everything is MQTT-only: the service subscribes to the door/PIR/Frigate/bridge topics on the opus broker and publishes the alarm state, fault, attributes, audit events and HA discovery. The shop Pi and its broker are untrusted; opus is the trust boundary.

Status: implemented and covered by `go test ./...` (state machine, engine wiring via a fake MQTT client, metrics). Deployment to opus (ansible role) and the Home Assistant migration are in progress — the normative design and the dated verification history are maintained in the private workspace, not in this repository.

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
| `SHOP_PROBE_ADDR` | `shop:1883` | TCP liveness probe; empty disables |
| `PROBE_INTERVAL` | `30s` | probe cadence |
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
* `<prefix>/lock/status` — retained compat alias (`armed`/`disarmed`) for the replaced mqtt-router consumers
* `frigate/profile/set` — `armed`/`disarmed` (reconciliation + switching)
* supervision inputs: `frigate/available`, `frigate/<cam>/detect/state`, `/recordings/state`, `/status/detect`, `frigate/reviews`, `frigate/profile/state`, `$SYS/broker/connection/<remote>/state`

## Monitoring

`/metrics` exposes the `werkstatt-*` gauges consumed by the monitoring dashboard (alarm state, door, bridge, Frigate switches/profile, device LWTs, Frigate stats, shop probe). Metric contract, dashboard and alerting rules: `monitoring/README.md`, dashboard `monitoring/werkstatt-dashboard.json` (Grafana, VictoriaMetrics datasource).

## License

Unlicensed (private) for now.