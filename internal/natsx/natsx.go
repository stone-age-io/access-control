// file: internal/natsx/natsx.go

// Package natsx is a small NATS connection helper for the stone-access
// binaries. It is adapted (not imported) from rule-router's broker: connection
// lifecycle, auth, and TLS, plus convenience helpers to ensure the policy KV
// bucket and the audit JetStream stream exist. It deliberately knows nothing
// about consumers, subscriptions, or the rule engine.
package natsx

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/config"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
)

const (
	opTimeout          = 10 * time.Second
	reconnectJitterMin = 100 * time.Millisecond
	reconnectJitterMax = 1 * time.Second
	drainTimeout       = 10 * time.Second
)

// Conn bundles the core NATS connection and a JetStream context. Both binaries
// use core NATS (commands, events, KV watch) and JetStream (KV store, audit
// stream), so we hand back both.
type Conn struct {
	NC  *nats.Conn
	JS  jetstream.JetStream
	log *logger.Logger
}

// Connect establishes the NATS connection and JetStream context using the
// configured auth and TLS settings. The metrics argument may be nil.
//
// onReconnect, if non-nil, is invoked from the NATS reconnect handler — the
// controller uses it to re-arm its KV watcher, which can go stale across a
// reconnect.
func Connect(cfg *config.NATSConfig, log *logger.Logger, m *metrics.Metrics, onReconnect func()) (*Conn, error) {
	log = log.With("component", "natsx")

	opts, err := buildOptions(cfg, log, m, onReconnect)
	if err != nil {
		return nil, fmt.Errorf("failed to build NATS options: %w", err)
	}

	urlString := strings.Join(cfg.URLs, ",")
	log.Info("connecting to NATS", "urls", cfg.URLs)

	nc, err := nats.Connect(urlString, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS (urls=%v): %w", cfg.URLs, err)
	}
	m.SetNATSConnectionStatus(true)
	log.Info("NATS connection established", "connectedURL", nc.ConnectedUrl())

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	return &Conn{NC: nc, JS: js, log: log}, nil
}

// EnsureKVBucket returns the named KV bucket, creating it if it does not exist.
func (c *Conn) EnsureKVBucket(ctx context.Context, bucket string) (jetstream.KeyValue, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	kv, err := c.JS.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      bucket,
		Description: "stone-access policy mirror (one key per record)",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure KV bucket %q: %w", bucket, err)
	}
	c.log.Info("KV bucket ready", "bucket", bucket)
	return kv, nil
}

// EnsureStream returns the named JetStream stream, creating it if it does not
// exist (file storage; subjects are the configured event subjects).
func (c *Conn) EnsureStream(ctx context.Context, name, subjects string) (jetstream.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	stream, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        name,
		Description: "stone-access audit events",
		Subjects:    strings.Split(subjects, ","),
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure stream %q: %w", name, err)
	}
	c.log.Info("JetStream stream ready", "stream", name, "subjects", subjects)
	return stream, nil
}

// Close drains and closes the NATS connection.
func (c *Conn) Close() error {
	if c.NC == nil {
		return nil
	}
	c.log.Info("draining NATS connection")
	if err := c.NC.Drain(); err != nil {
		return fmt.Errorf("failed to drain NATS connection: %w", err)
	}
	return nil
}

func buildOptions(cfg *config.NATSConfig, log *logger.Logger, m *metrics.Metrics, onReconnect func()) ([]nats.Option, error) {
	opts := []nats.Option{
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectJitter(reconnectJitterMin, reconnectJitterMax),
		nats.DrainTimeout(drainTimeout),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn("NATS client disconnected", "error", err)
			m.SetNATSConnectionStatus(false)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info("NATS client reconnected", "url", nc.ConnectedUrl())
			m.SetNATSConnectionStatus(true)
			m.IncNATSReconnects()
			if onReconnect != nil {
				onReconnect()
			}
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Error("NATS connection permanently closed", "error", nc.LastError())
			m.SetNATSConnectionStatus(false)
		}),
	}

	switch {
	case cfg.CredsFile != "":
		log.Info("using NATS JWT authentication", "credsFile", cfg.CredsFile)
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	case cfg.NKeySeedFile != "":
		log.Info("using NATS NKey authentication", "seedFile", cfg.NKeySeedFile)
		nkeyOpt, err := nats.NkeyOptionFromSeed(cfg.NKeySeedFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load NKey seed file: %w", err)
		}
		opts = append(opts, nkeyOpt)
	case cfg.Token != "":
		log.Info("using NATS token authentication")
		opts = append(opts, nats.Token(cfg.Token))
	case cfg.Username != "":
		log.Info("using NATS username/password authentication", "username", cfg.Username)
		opts = append(opts, nats.UserInfo(cfg.Username, cfg.Password))
	}

	if cfg.TLS.Enable {
		log.Info("enabling TLS for NATS connection", "insecure", cfg.TLS.Insecure)
		tlsConfig := &tls.Config{InsecureSkipVerify: cfg.TLS.Insecure}

		if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load NATS TLS client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		if cfg.TLS.CAFile != "" {
			opts = append(opts, nats.RootCAs(cfg.TLS.CAFile))
		}
		opts = append(opts, nats.Secure(tlsConfig))
	}

	return opts, nil
}
