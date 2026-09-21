// Package config reads the shop-alarm configuration from environment
// variables (Docker-friendly) with defaults that match the live setup.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the full shop-alarm configuration.
type Config struct {
	// MQTT
	BrokerURL string
	Username  string
	Password  string
	ClientID  string
	Verbose   bool
	TopicRoot string // all alarm topics are rooted at <TopicRoot>/alarm/...

	// Frigate topics
	FrigateProfileTopic         string
	FrigateReviewsTopic         string
	FrigateAvailableTopic       string
	FrigateDetectStateTopic     string
	FrigateRecordingsStateTopic string
	FrigateDetectStatusTopic    string
	FrigateAudioTopicPrefix     string

	// Shop sensor topics
	DoorTopic             string
	PIRTopics             []string
	BridgeStateTopic      string
	SensorAvailableTopics []string

	// Device supervision (Tasmota-style LWT topics, e.g. tele/+/LWT).
	// Empty disables werkstatt_device_up; the concrete pattern must be
	// confirmed against the live broker before enabling on opus.
	DeviceLWTPatterns []string

	// Physical actors (existing mqtt-router cmnd topics)
	LightTopic  string // room light (lightservo) command topic
	PowerTopic  string // power strip (gosund) command topic
	RadioTopic  string // radio / SwitchBot command topic
	Blink1Topic string // blink(1) RGB LED command topic

	// Timing
	ExitDelay              time.Duration // arm -> door-closed-or-arm deadline
	DoorEscalation         time.Duration // additional delay with the door still open -> triggered
	EntryDelay             time.Duration // trigger -> triggered
	SupervisionCameraDelay time.Duration // frigate loss while armed -> triggered
	SupervisionBridgeDelay time.Duration // bridge loss while armed -> triggered
	TriggerRepeat          time.Duration // notification repeat while triggered
	FlashInterval          time.Duration // blink cadence while triggered
	PreArmSweep            time.Duration // PIR trip window that blocks arming
	ReconcileDelay         time.Duration // wait for retained state before reconciling
	VerifyInitialDelay     time.Duration // profile publish -> first switch-state check
	VerifyRetryDelay       time.Duration // retry cadence before faulting
	MismatchNotifyInterval time.Duration // minimum spacing between profile_mismatch notifications

	// Persistence
	StateFile string

	// Notifications
	NTFYURL        string
	LiveStreamURL  string
	FrigateAPIURL  string
	FrigateAPIUser string
	FrigateAPIPass string

	// HA discovery
	DiscoveryPrefix string

	// Metrics (exporter folded into shop-alarm; see monitoring/README.md)
	HealthAddr        string        // serves /healthz and /metrics
	FrigateCameraName string        // camera whose /api/stats fields are exported
	StatsInterval     time.Duration // Frigate /api/stats poll cadence

	// StatsFailThreshold counts consecutive failed /api/stats polls before
	// the Frigate API is treated as unreachable (camera-loss rule input).
	StatsFailThreshold int
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	dur := func(name string, def time.Duration) (time.Duration, error) {
		v := os.Getenv(name)
		if v == "" {
			return def, nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return d, nil
	}

	root := env("TOPIC_PREFIX", "werkstatt")

	cfg := Config{
		BrokerURL: env("MQTT_URL", "tcp://localhost:1883"),
		Username:  os.Getenv("MQTT_USER"),
		Password:  os.Getenv("MQTT_PASSWORD"),
		ClientID:  env("MQTT_CLIENT_ID", "shop-alarm"),
		Verbose:   boolEnv("VERBOSE"),
		TopicRoot: root,

		FrigateProfileTopic:         env("FRIGATE_PROFILE_TOPIC", "frigate/profile/set"),
		FrigateReviewsTopic:         env("FRIGATE_REVIEWS_TOPIC", "frigate/reviews"),
		FrigateAvailableTopic:       env("FRIGATE_AVAILABLE_TOPIC", "frigate/available"),
		FrigateDetectStateTopic:     env("FRIGATE_DETECT_STATE_TOPIC", "frigate/werkstatt/detect/state"),
		FrigateRecordingsStateTopic: env("FRIGATE_RECORDINGS_STATE_TOPIC", "frigate/werkstatt/recordings/state"),
		FrigateDetectStatusTopic:    env("FRIGATE_DETECT_STATUS_TOPIC", "frigate/werkstatt/status/detect"),
		FrigateAudioTopicPrefix:     env("FRIGATE_AUDIO_TOPIC_PREFIX", "frigate/werkstatt/audio/#"),

		DoorTopic:             env("DOOR_TOPIC", root+"/door"),
		PIRTopics:             listEnvDefault("PIR_TOPICS"),
		BridgeStateTopic:      env("BRIDGE_STATE_TOPIC", "$SYS/broker/connection/shop.shop/state"),
		SensorAvailableTopics: listEnvDefault("SENSOR_AVAILABILITY_TOPICS", "mqtt-gpio-binary-sensor/mqtt-gpio-binary-sensor_shop/status"),
		DeviceLWTPatterns:     listEnvDefault("LWT_TOPICS"),

		LightTopic:  env("LIGHT_TOPIC", root+"/licht/hinten/cmnd"),
		PowerTopic:  env("POWER_TOPIC", root+"/strom/cmnd"),
		RadioTopic:  env("RADIO_TOPIC", root+"/radio/cmnd"),
		Blink1Topic: env("BLINK1_TOPIC", root+"/blink1/cmnd"),

		StateFile: env("STATE_FILE", "/var/lib/shop-alarm/state.json"),

		NTFYURL:        os.Getenv("NTFY_URL"),
		LiveStreamURL:  os.Getenv("LIVE_STREAM_URL"),
		FrigateAPIURL:  os.Getenv("FRIGATE_API_URL"),
		FrigateAPIUser: os.Getenv("FRIGATE_API_USER"),
		FrigateAPIPass: os.Getenv("FRIGATE_API_PASSWORD"),

		DiscoveryPrefix: env("DISCOVERY_PREFIX", "homeassistant"),
		HealthAddr:      env("HEALTH_ADDR", ":8080"),

		FrigateCameraName: env("FRIGATE_CAMERA", "werkstatt"),
	}

	var err error
	if cfg.ExitDelay, err = dur("EXIT_DELAY", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DoorEscalation, err = dur("DOOR_ESCALATION_DELAY", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.EntryDelay, err = dur("ENTRY_DELAY", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.SupervisionCameraDelay, err = dur("SUPERVISION_CAMERA_DELAY", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.SupervisionBridgeDelay, err = dur("SUPERVISION_BRIDGE_DELAY", 5*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.TriggerRepeat, err = dur("TRIGGER_REPEAT", 2*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.FlashInterval, err = dur("FLASH_INTERVAL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.PreArmSweep, err = dur("PRE_ARM_SWEEP", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReconcileDelay, err = dur("RECONCILE_DELAY", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.VerifyInitialDelay, err = dur("VERIFY_INITIAL_DELAY", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.VerifyRetryDelay, err = dur("VERIFY_RETRY_DELAY", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.MismatchNotifyInterval, err = dur("PROFILE_MISMATCH_NOTIFY_INTERVAL", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.StatsInterval, err = dur("STATS_INTERVAL", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.StatsFailThreshold, err = intEnv("STATS_FAIL_THRESHOLD", 3); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func boolEnv(name string) bool {
	v, _ := strconv.ParseBool(os.Getenv(name))
	return v
}

func intEnv(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}

func listEnvDefault(name string, def ...string) []string {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
