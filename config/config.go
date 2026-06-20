// file: config/config.go

// Package config holds the unified configuration for both stone-access
// binaries (accessd and access-controller). It is loaded via Viper with
// environment-variable overrides (prefix SA_) and sensible defaults, mirroring
// the conventions used in rule-router but trimmed to what this app needs.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// Defaults.
const (
	DefaultNATSURL       = "nats://localhost:4222"
	DefaultReconnectWait = 250 * time.Millisecond

	DefaultLogLevel    = "info"
	DefaultLogEncoding = "json"
	DefaultLogOutput   = "stdout"

	DefaultMetricsAddress        = ":2113"
	DefaultMetricsPath           = "/metrics"
	DefaultMetricsUpdateInterval = "15s"

	// DefaultPolicyBucket is the KV bucket that mirrors the policy graph,
	// one key per record (cred.{value}, user.{id}, role.{code}, ...).
	DefaultPolicyBucket = "ACC_POLICY"

	// DefaultEventsStream names the JetStream audit log. Its subject set is
	// derived from the subjects root (see internal/subjects), not configured
	// here, so the stream can never drift from what the controllers publish.
	DefaultEventsStream = "ACC_EVENTS"

	// DefaultStatusBucket is the KV bucket carrying the upward "device shadow":
	// live edge state (door open/closed, effective posture, aux I/O), one key per
	// point. Controllers write it; accessd watches it into the point_status
	// projection. The mirror image of ACC_POLICY (which flows the other way).
	DefaultStatusBucket = "ACC_STATUS"

	// DefaultDataDir is where accessd's embedded PocketBase stores its data.
	DefaultDataDir = "./pb_data"
)

// Config is the unified configuration. Each binary reads the sections it needs:
// both read NATS/Logging/Metrics/Policy/Subjects; accessd also reads
// Accessd/Events; access-controller also reads Controller.
type Config struct {
	NATS       NATSConfig       `json:"nats" yaml:"nats" mapstructure:"nats"`
	Logging    LogConfig        `json:"logging" yaml:"logging" mapstructure:"logging"`
	Metrics    MetricsConfig    `json:"metrics" yaml:"metrics" mapstructure:"metrics"`
	Policy     PolicyConfig     `json:"policy" yaml:"policy" mapstructure:"policy"`
	Events     EventsConfig     `json:"events" yaml:"events" mapstructure:"events"`
	Status     StatusConfig     `json:"status" yaml:"status" mapstructure:"status"`
	Subjects   SubjectsConfig   `json:"subjects" yaml:"subjects" mapstructure:"subjects"`
	Accessd    AccessdConfig    `json:"accessd" yaml:"accessd" mapstructure:"accessd"`
	Controller ControllerConfig `json:"controller" yaml:"controller" mapstructure:"controller"`
}

// NATSConfig contains NATS connection settings. Exactly one auth method (or
// none) may be set.
type NATSConfig struct {
	URLs         []string `json:"urls" yaml:"urls" mapstructure:"urls"`
	Username     string   `json:"username" yaml:"username" mapstructure:"username"`
	Password     string   `json:"password" yaml:"password" mapstructure:"password"`
	Token        string   `json:"token" yaml:"token" mapstructure:"token"`
	NKeySeedFile string   `json:"nkeySeedFile" yaml:"nkeySeedFile" mapstructure:"nkeySeedFile"`
	CredsFile    string   `json:"credsFile" yaml:"credsFile" mapstructure:"credsFile"`

	TLS struct {
		Enable   bool   `json:"enable" yaml:"enable" mapstructure:"enable"`
		CertFile string `json:"certFile" yaml:"certFile" mapstructure:"certFile"`
		KeyFile  string `json:"keyFile" yaml:"keyFile" mapstructure:"keyFile"`
		CAFile   string `json:"caFile" yaml:"caFile" mapstructure:"caFile"`
		Insecure bool   `json:"insecure" yaml:"insecure" mapstructure:"insecure"`
	} `json:"tls" yaml:"tls" mapstructure:"tls"`

	MaxReconnects int           `json:"maxReconnects" yaml:"maxReconnects" mapstructure:"maxReconnects"`
	ReconnectWait time.Duration `json:"reconnectWait" yaml:"reconnectWait" mapstructure:"reconnectWait"`
}

// LogConfig contains logging configuration.
type LogConfig struct {
	Level      string `json:"level" yaml:"level" mapstructure:"level"`
	Encoding   string `json:"encoding" yaml:"encoding" mapstructure:"encoding"`
	OutputPath string `json:"outputPath" yaml:"outputPath" mapstructure:"outputPath"`
}

// MetricsConfig contains the Prometheus metrics server configuration.
type MetricsConfig struct {
	Enabled        bool   `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Address        string `json:"address" yaml:"address" mapstructure:"address"`
	Path           string `json:"path" yaml:"path" mapstructure:"path"`
	UpdateInterval string `json:"updateInterval" yaml:"updateInterval" mapstructure:"updateInterval"`
}

// PolicyConfig names the KV bucket that mirrors the policy graph.
type PolicyConfig struct {
	Bucket string `json:"bucket" yaml:"bucket" mapstructure:"bucket"`
}

// EventsConfig names the JetStream audit stream. Its subjects are derived from
// the subjects root (the {root}.evt.> subtree), not configured here.
type EventsConfig struct {
	Stream string `json:"stream" yaml:"stream" mapstructure:"stream"`
}

// StatusConfig names the KV bucket for the upward device-shadow channel. accessd
// creates it and watches it into the point_status projection; controllers bind it
// read-write and write their live state. accessd and every controller MUST use
// the same bucket name.
type StatusConfig struct {
	Bucket string `json:"bucket" yaml:"bucket" mapstructure:"bucket"`
}

// SubjectsConfig sets the app-discriminator segment every NATS subject carries
// (see internal/subjects). accessd and every controller MUST use the same app
// token, since they publish and subscribe to each other's traffic. Defaults to
// "acc".
type SubjectsConfig struct {
	App string `json:"app" yaml:"app" mapstructure:"app"`
}

// AccessdConfig is the central app's configuration. The PocketBase HTTP
// address is controlled by PocketBase's own `serve --http` flag, not here.
type AccessdConfig struct {
	// DataDir is the embedded PocketBase data directory.
	DataDir string `json:"dataDir" yaml:"dataDir" mapstructure:"dataDir"`
	// ControllerOfflineAfter is how long accessd tolerates silence from a
	// controller before marking it offline. Should exceed a controller's
	// HeartbeatInterval by a few intervals. Defaults to 45s.
	ControllerOfflineAfter time.Duration `json:"controllerOfflineAfter" yaml:"controllerOfflineAfter" mapstructure:"controllerOfflineAfter"`
}

// ControllerConfig is the edge controller's configuration — just its identity.
// A controller holds the whole-org policy but only drives the portals assigned to
// it (the `controller` relation on portal records), so which doors it drives and
// their hardware bindings come from policy, not local config.
type ControllerConfig struct {
	// Code is this controller's stable code, matching a `controllers` record. The
	// controller arms the portals whose `controller` relation points at this code.
	Code string `json:"code" yaml:"code" mapstructure:"code"`
	// Location is the location code this controller belongs to (selects the
	// timezone and scopes the location-wide command/fire subscriptions).
	Location string `json:"location" yaml:"location" mapstructure:"location"`
	// HeartbeatInterval is how often this controller publishes a liveness
	// heartbeat to accessd. The publish ticker is a deliberate exception to the
	// controller's no-ticker rule, scoped to health reporting. Defaults to 15s.
	HeartbeatInterval time.Duration `json:"heartbeatInterval" yaml:"heartbeatInterval" mapstructure:"heartbeatInterval"`
	// Driver selects the portal hardware backend: "mock" (default — simulated, no
	// hardware) or "gpio" (real relays/inputs via the Linux GPIO character device).
	Driver string `json:"driver" yaml:"driver" mapstructure:"driver"`
	// Model is the controller hardware model, selecting the GPIO hardware profile
	// (logical relay/input index → physical line). Required when Driver is "gpio";
	// must match a model the hardware registry knows and the controllers record.
	Model string `json:"model" yaml:"model" mapstructure:"model"`
}

// Load reads configuration from the given file path, layering in env vars
// (prefix SA_) and applying defaults. A missing config file is not an error —
// the app can run on defaults plus env vars.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Only wire a config file if one actually exists. A missing file (including
	// the default path no one created) is fine — defaults + SA_ env vars apply.
	// Viper's ConfigFileNotFoundError only covers path search, not an explicit
	// SetConfigFile target, so we stat it ourselves.
	hasFile := false
	if path != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			v.SetConfigFile(path)
			ext := filepath.Ext(path)
			v.SetConfigType(strings.TrimPrefix(ext, "."))
			hasFile = true
		}
	}

	v.SetEnvPrefix("SA") // e.g. SA_NATS_URLS, SA_CONTROLLER_LOCATION
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// AutomaticEnv only resolves keys Viper already knows about; defaults are
	// applied by mutating the struct, so bind the env-overridable keys
	// explicitly so they work even when absent from the file.
	for _, key := range []string{
		"nats.urls", "nats.username", "nats.password", "nats.token",
		"nats.credsFile", "nats.nkeySeedFile",
		"nats.maxReconnects", "nats.reconnectWait",
		"nats.tls.enable", "nats.tls.certFile", "nats.tls.keyFile",
		"nats.tls.caFile", "nats.tls.insecure",
		"logging.level", "logging.encoding", "logging.outputPath",
		"metrics.enabled", "metrics.address", "metrics.path", "metrics.updateInterval",
		"policy.bucket", "events.stream", "status.bucket", "subjects.app",
		"accessd.dataDir", "accessd.controllerOfflineAfter",
		"controller.code", "controller.location", "controller.heartbeatInterval",
		"controller.driver", "controller.model",
	} {
		_ = v.BindEnv(key)
	}

	if hasFile {
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	setDefaults(&cfg)

	decodeHook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	)
	if err := v.Unmarshal(&cfg, viper.DecodeHook(decodeHook)); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if len(cfg.NATS.URLs) == 0 {
		cfg.NATS.URLs = []string{DefaultNATSURL}
	}
	if cfg.NATS.MaxReconnects == 0 {
		cfg.NATS.MaxReconnects = -1 // retry forever
	}
	if cfg.NATS.ReconnectWait == 0 {
		cfg.NATS.ReconnectWait = DefaultReconnectWait
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = DefaultLogLevel
	}
	if cfg.Logging.Encoding == "" {
		cfg.Logging.Encoding = DefaultLogEncoding
	}
	if cfg.Logging.OutputPath == "" {
		cfg.Logging.OutputPath = DefaultLogOutput
	}

	if cfg.Metrics.Address == "" {
		cfg.Metrics.Address = DefaultMetricsAddress
	}
	if cfg.Metrics.Path == "" {
		cfg.Metrics.Path = DefaultMetricsPath
	}
	if cfg.Metrics.UpdateInterval == "" {
		cfg.Metrics.UpdateInterval = DefaultMetricsUpdateInterval
	}

	if cfg.Policy.Bucket == "" {
		cfg.Policy.Bucket = DefaultPolicyBucket
	}
	if cfg.Events.Stream == "" {
		cfg.Events.Stream = DefaultEventsStream
	}
	if cfg.Status.Bucket == "" {
		cfg.Status.Bucket = DefaultStatusBucket
	}
	if cfg.Subjects.App == "" {
		cfg.Subjects.App = subjects.DefaultApp
	}
	if cfg.Accessd.DataDir == "" {
		cfg.Accessd.DataDir = DefaultDataDir
	}
	// Liveness cadence/threshold. The offline window is three heartbeat intervals
	// so a single dropped heartbeat never flaps a controller offline.
	if cfg.Controller.HeartbeatInterval == 0 {
		cfg.Controller.HeartbeatInterval = 15 * time.Second
	}
	if cfg.Accessd.ControllerOfflineAfter == 0 {
		cfg.Accessd.ControllerOfflineAfter = 45 * time.Second
	}
	if cfg.Controller.Driver == "" {
		cfg.Controller.Driver = "mock"
	}
}

func validate(cfg *Config) error {
	if len(cfg.NATS.URLs) == 0 {
		return fmt.Errorf("at least one NATS URL must be specified")
	}

	authCount := 0
	for _, set := range []bool{
		cfg.NATS.Username != "",
		cfg.NATS.Token != "",
		cfg.NATS.NKeySeedFile != "",
		cfg.NATS.CredsFile != "",
	} {
		if set {
			authCount++
		}
	}
	if authCount > 1 {
		return fmt.Errorf("only one NATS authentication method should be specified")
	}
	if cfg.NATS.CredsFile != "" {
		if _, err := os.Stat(cfg.NATS.CredsFile); os.IsNotExist(err) {
			return fmt.Errorf("NATS creds file does not exist: %s", cfg.NATS.CredsFile)
		}
	}
	if cfg.NATS.TLS.Enable {
		if (cfg.NATS.TLS.CertFile == "") != (cfg.NATS.TLS.KeyFile == "") {
			return fmt.Errorf("NATS TLS cert and key files must be provided together")
		}
	}

	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.Logging.Level] {
		return fmt.Errorf("invalid log level: %s", cfg.Logging.Level)
	}

	if cfg.Metrics.UpdateInterval != "" {
		if _, err := time.ParseDuration(cfg.Metrics.UpdateInterval); err != nil {
			return fmt.Errorf("invalid metrics update interval %q: %w", cfg.Metrics.UpdateInterval, err)
		}
	}

	if cfg.Policy.Bucket == "" {
		return fmt.Errorf("policy.bucket cannot be empty")
	}
	if cfg.Status.Bucket == "" {
		return fmt.Errorf("status.bucket cannot be empty")
	}

	// The app token must be a single NATS token: subject parsing compares it
	// against a fixed segment, so a "." (or a wildcard / whitespace) would
	// silently break command and event routing.
	if cfg.Subjects.App == "" || strings.ContainsAny(cfg.Subjects.App, ". \t*>") {
		return fmt.Errorf("subjects.app must be a single NATS token (no '.', '*', '>', or whitespace): %q", cfg.Subjects.App)
	}

	// Controller hardware driver. The default ("mock") is set in setDefaults; only
	// "gpio" needs a model, which must name a profile the hardware registry knows.
	switch cfg.Controller.Driver {
	case "", "mock":
	case "gpio":
		if cfg.Controller.Model == "" {
			return fmt.Errorf("controller.model is required when controller.driver is \"gpio\"")
		}
	default:
		return fmt.Errorf("invalid controller.driver %q (want \"mock\" or \"gpio\")", cfg.Controller.Driver)
	}
	return nil
}
