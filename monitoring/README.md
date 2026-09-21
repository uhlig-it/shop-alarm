# Workshop Monitoring

Goal: at a glance, know what is up and what is not — binary/state-first panels, green/red, not deep metrics. Covers: shop machine, MQTT brokers + bridge, Frigate (stream + switch states + performance), Tasmota devices, door, and the alarm state.

There is no standalone exporter: the werkstatt metrics are served by shop-alarm itself (decision 2026-09-21 — shop-alarm already subscribes to every topic behind these gauges). Implementation: `internal/metrics` in `github.com/uhlig-it/shop-alarm`.

## Data acquisition (scrape targets)

| Job | Target | Produces | Owner |
|---|---|---|---|
| `werkstatt` | `opus:9101` (shop-alarm's `/metrics`; set `HEALTH_ADDR=:9101` on opus) | all `werkstatt-*` gauges below | shop-alarm |
| `shop` | `shop:9103` (existing `env-sensors`, Prometheus format over Tailscale) | scrape `up` = shop machine liveness; environment values | existing |
| `mosquitto` | `opus:<exporter-port>` (`mosquitto-exporter`, user exists in vault) | broker clients/rates | existing, verify port + metric names |

VictoriaMetrics single-node on opus (Docker); scrape interval 30 s. Grafana datasource type `victoriametrics`.

## shop-alarm collector configuration

| Variable | Default | Meaning |
|---|---|---|
| `HEALTH_ADDR` | `:8080` | HTTP server for `/healthz` and `/metrics` (use `:9101` on opus) |
| `SHOP_PROBE_ADDR` | `shop:1883` | TCP liveness probe target for `werkstatt_shop_reachable`; empty disables (the probe does not rely on the bridge `$SYS` topic) |
| `PROBE_INTERVAL` | `30s` | probe cadence |
| `LWT_TOPICS` | *(empty)* | comma-separated MQTT subscription patterns for device LWTs (e.g. `tele/+/LWT`); empty disables `werkstatt_device_up`. The concrete pattern must be confirmed on the live broker before enabling |
| `FRIGATE_API_URL` | *(empty)* | enables the Frigate stats poller (and snapshot attachments on notifications) |
| `FRIGATE_CAMERA` | `werkstatt` | camera whose per-camera stats fields are exported |
| `STATS_INTERVAL` | `30s` | stats poll cadence |

## Metric contract

| Metric | Source | Meaning |
|---|---|---|
| `werkstatt_shop_reachable` 1/0 | TCP probe opus→`SHOP_PROBE_ADDR` | shop machine + broker reachable |
| `werkstatt_bridge_connected` 1/0 | `$SYS/broker/connection/shop.shop/state` | bridge up (as observed by opus) |
| `werkstatt_frigate_available` 1/0 | `frigate/available` = online | Frigate process up (LWT `offline` catches a dead Frigate) |
| `werkstatt_camera_online` 1/0 | `frigate/werkstatt/status/detect` = online | end-to-end shop stream (Pi→mediamtx→go2rtc→Frigate) |
| `werkstatt_detect_enabled`, `werkstatt_recordings_enabled` 1/0 | `…/detect/state`, `…/recordings/state` | the privacy floor / arming truth |
| `werkstatt_profile_active{profile=…}` 1/0 | `frigate/profile/state` | which profile Frigate thinks is active |
| `werkstatt_door_known` 1/0, `werkstatt_door_open` 1/0 | `werkstatt/door` | door state; `door_open` is omitted until the first message (unknown ≠ closed) |
| `werkstatt_device_up{device}` 1/0 | device LWT topics (`LWT_TOPICS`) | per-device liveness (expected labels: `gosund-0/1/2`, `sonoff-0/2`, `irlight`, `lightservo`) |
| `werkstatt_camera_fps`, `werkstatt_detection_fps` | Frigate `/api/stats` | stream/decode + inference rates |
| `werkstatt_connection_quality` 0–100 | Frigate `/api/stats` (optional field) | camera link quality percentile |
| `werkstatt_reconnects_total`, `werkstatt_stalls_total` | Frigate `/api/stats` (optional fields) | counters for trend/fault detection |
| `werkstatt_frigate_api_reachable` 1/0 | stats poller failure threshold | Frigate API reachable; input to the camera-loss rule |
| `werkstatt_alarm_state{state=…}` 1/0 | `werkstatt/alarm/state` | active state: `disarmed`, `arming`, `armed_away`, `pending`, `triggered` |

Unknown values are omitted rather than reported as zero, so dashboard panels show "no data" (gray) instead of a false red. The optional `/api/stats` fields (`connection_quality`, `reconnects`, `stalls`) are parsed defensively (numbers or numeric strings; a dead camera reporting `"N/A"` yields nil, never a parse failure) — `STATS_FAIL_THRESHOLD` (default 3) consecutive transport-level failures flip `werkstatt_frigate_api_reachable` and feed the camera-loss rule. The env-sensors metric names (`env_temperature_celsius{topic=…}`, `env_humidity_percent`, `env_illuminance_lux`) and mosquitto-exporter names (`mosquitto_clients_connected`, `mosquitto_messages_received_total`) are assumed and to be confirmed; adjust the dashboard queries once verified.

## Dashboard (importable: `werkstatt-dashboard.json`)

* **Row "Status at a glance"** — one stat panel per component, all green=OK, red=down, gray=pending: Shop machine (scrape `up`), MQTT bridge, Camera stream, Frigate, Detect, Recordings, Door, Devices online (count), Alarm state.
* **Row "Shop machine"** — temperature, humidity, illuminance time series (env-sensors).
* **Row "MQTT broker"** — connected clients, message rates (per second, 5 m window).
* **Row "Frigate detail"** — camera fps, detection fps, connection quality, reconnects/stalls per hour.
* **Row "Devices"** — per-device stat squares (gosund-0/1/2, sonoff-0/2, irlight, lightservo) colored by `werkstatt_device_up`.

Panel styling: thresholds `1`/`0` (green>red) for binary stats; `werkstatt_alarm_state` uses value mappings (disarmed=green, armed_away=blue, pending=yellow, triggered=red); time-series use `$__rate_interval`.

## Alerting (vmalert)

Ownership split (decision 2026-09-21): shop-alarm owns everything alarm-relevant — while armed it faults, notifies and escalates on camera loss (30 s), bridge loss (5 min), sensor LWT loss and profile mismatches. vmalert must not duplicate those pages. Uptime-Kuma is the liveness path while disarmed — **open item: confirm it actually watches the shop machine; if not, re-add `ShopUnreachable` here.**

vmalert rules (ntfy/webhook notification path):

* `RecordingWhileDisarmed` — `werkstatt_recordings_enabled == 1 and werkstatt_profile_active{profile="disarmed"} == 1` (privacy regression guard; the independent backstop for shop-alarm's own `profile_mismatch` fault)
* `CameraFPSLow` — `werkstatt_camera_fps < 1` for 10 m (stream degradation that never fully fails)
* `DetectionFPSLow` — `werkstatt_detection_fps < 1` for 10 m
* `ShopTemperatureHigh` — `env_temperature_celsius{topic="werkstatt"} > 35` for 30 m
