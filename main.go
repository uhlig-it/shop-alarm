package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/uhlig-it/shop-alarm/internal/alarm"
	"github.com/uhlig-it/shop-alarm/internal/config"
	"github.com/uhlig-it/shop-alarm/internal/metrics"
	"github.com/uhlig-it/shop-alarm/internal/notify"
)

var versionNumber = "0.3.0"

var (
	showVersion = flag.Bool("version", false, "Show the Application Version")
	help        = flag.Bool("help", false, "Displays this message")
	verbose     = flag.Bool("verbose", false, "Produce verbose output")
)

func main() {
	flag.Parse()

	if *help {
		flag.Usage()
		return
	}
	if *showVersion {
		fmt.Printf("v%s\n", versionNumber)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if *verbose || cfg.Verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	mqtt.CRITICAL = log.New(os.Stderr, "CRITICAL ", 0)
	mqtt.ERROR = log.New(os.Stderr, "ERROR ", 0)
	mqtt.WARN = log.New(os.Stderr, "WARN ", 0)
	if level == slog.LevelDebug {
		mqtt.DEBUG = log.New(os.Stderr, "DEBUG ", 0)
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetCleanSession(false).
		SetAutoReconnect(true).
		SetConnectRetryInterval(5*time.Second).
		SetWill(cfg.TopicRoot+"/alarm/available", "offline", 1, true)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	// The engine is referenced from the connect handler, so it must be
	// created before the handler is wired up (paho copies the options).
	// The handler itself only runs after a successful (re)connect.
	recorder := metrics.NewRecorder()
	core := alarm.NewCore(cfg, nil, nil)
	notifier := notify.New(notify.Config{
		NTFYURL:        cfg.NTFYURL,
		LiveStreamURL:  cfg.LiveStreamURL,
		FrigateAPIURL:  cfg.FrigateAPIURL,
		FrigateAPIUser: cfg.FrigateAPIUser,
		FrigateAPIPass: cfg.FrigateAPIPass,
	})
	var engine *alarm.Engine
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		slog.Info("connected to MQTT", "broker", cfg.BrokerURL)
		engine.Start()
	})

	client := mqtt.NewClient(opts)
	engine = alarm.NewEngine(cfg, client, core, notifier, recorder)
	core.SetSink(engine)

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		slog.Error("could not connect to MQTT", "error", token.Error())
		os.Exit(1)
	}

	// Monitoring collectors (exporter folded into alarm-core; the scrape
	// target is this process's /metrics endpoint).
	ctx := context.Background()
	recorder.StartProber(ctx, cfg.ShopProbeAddr, cfg.ProbeInterval)
	if cfg.FrigateAPIURL != "" {
		recorder.StartStatsPoller(ctx, metrics.StatsOptions{
			BaseURL: cfg.FrigateAPIURL,
			User:    cfg.FrigateAPIUser,
			Pass:    cfg.FrigateAPIPass,
			Camera:  cfg.FrigateCameraName,
		}, cfg.StatsInterval)
	}

	go serveHealth(cfg.HealthAddr, recorder)

	slog.Info("alarm-core started", "version", versionNumber, "topic_root", cfg.TopicRoot)
	select {}
}

func serveHealth(addr string, m *metrics.Recorder) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(m.Render()))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("health endpoint listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("health server failed", "error", err)
	}
}
