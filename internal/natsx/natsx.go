// file: internal/natsx/natsx.go

// Package natsx is the stone-access binaries' NATS layer. The connection itself
// comes from stone-age-io/kit's natsx, which reconnects forever and keeps trying
// through auth errors, so a creds file creds-keeper rewrites after a Revoke is
// picked up. What is here is what only access control needs: its auth and TLS
// options, its metrics hooks, and helpers to ensure the policy KV buckets and
// the audit stream exist.
package natsx

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	kitnatsx "github.com/stone-age-io/kit/natsx"

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
// configured auth and TLS settings. name is the client name the server reports
// (the binary's discovery name). The metrics argument may be nil.
//
// onReconnect, if non-nil, is invoked on every reconnect (and, when
// retryOnFailedConnect is set, on the first connect too): the controller uses it
// to re-arm its KV watcher and re-publish its status shadow, which go stale
// across a connection loss.
//
// retryOnFailedConnect makes a cold boot with no reachable server non-fatal: the
// controller (an always-on edge device) returns a connection in reconnecting
// state and keeps trying in the background, so it can come up on its cached config
// and converge when NATS returns. accessd leaves it false and fails fast: the hub
// has nothing useful to do without its own NATS.
func Connect(cfg *config.NATSConfig, name string, log *logger.Logger, m *metrics.Metrics, onReconnect func(), retryOnFailedConnect bool) (*Conn, error) {
	log = log.With("component", "natsx")

	extra, err := authOptions(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to build NATS options: %w", err)
	}
	extra = append(extra,
		nats.ReconnectWait(cfg.ReconnectWait),
		// nats.maxReconnects defaults to -1, the kit's own setting. An explicit
		// value still wins, as the configuration reference documents.
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectJitter(reconnectJitterMin, reconnectJitterMax),
		nats.DrainTimeout(drainTimeout),
	)

	resync := func() {
		if onReconnect != nil {
			onReconnect()
		}
	}
	nc, err := kitnatsx.Connect(kitnatsx.Options{
		URLs:  cfg.URLs,
		Creds: cfg.CredsFile,
		Name:  name,
		Log:   log.Logger,
		Hooks: kitnatsx.Hooks{
			Connected: func() {
				m.SetNATSConnectionStatus(true)
				resync()
			},
			Disconnected: func() { m.SetNATSConnectionStatus(false) },
			Reconnected: func() {
				m.SetNATSConnectionStatus(true)
				m.IncNATSReconnects()
				resync()
			},
			Closed: func() { m.SetNATSConnectionStatus(false) },
		},
		RetryOnFailedConnect: retryOnFailedConnect,
		Extra:                extra,
	})
	if err != nil {
		return nil, err
	}
	m.SetNATSConnectionStatus(nc.IsConnected())

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	return &Conn{NC: nc, JS: js, log: log}, nil
}

// KVBinder returns a closure that binds the named KV bucket read-only on demand.
// The controller hands this to its PolicyStore/StatusWriter so the bind is retried
// lazily (inside the watcher/drain loop) rather than done fatally at boot: the
// key to surviving a reboot with NATS unreachable.
func (c *Conn) KVBinder(bucket string) func(context.Context) (jetstream.KeyValue, error) {
	return func(ctx context.Context) (jetstream.KeyValue, error) {
		return c.KVBucket(ctx, bucket)
	}
}

// EnsureKVBucket returns the named KV bucket, creating it if it does not exist.
// Only accessd (the policy writer/owner) should call this: it needs stream
// create/update rights on the bucket. Read-only consumers use KVBucket.
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

// KVBucket binds to an existing KV bucket read-only (no create/update), the
// least-privilege path for a controller: it only watches policy, so its NATS
// identity needn't carry stream-management rights. accessd is the sole creator
// of the bucket, so a not-found error here means accessd has not yet served.
func (c *Conn) KVBucket(ctx context.Context, bucket string) (jetstream.KeyValue, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	kv, err := c.JS.KeyValue(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to open KV bucket %q (accessd creates it on first serve — ensure accessd has run): %w", bucket, err)
	}
	c.log.Info("KV bucket opened (read-only)", "bucket", bucket)
	return kv, nil
}

// EnsureStream returns the named JetStream stream, creating it if it does not
// exist (file storage; subjs are the event subjects to capture). CreateOrUpdate,
// so a change of subjects reaches an existing deployment at startup.
func (c *Conn) EnsureStream(ctx context.Context, name string, subjs []string) (jetstream.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	stream, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        name,
		Description: "stone-access audit events",
		Subjects:    subjs,
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure stream %q: %w", name, err)
	}
	c.log.Info("JetStream stream ready", "stream", name, "subjects", subjs)
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

// authOptions turns the configured auth and TLS settings into client options.
// A creds file is not here: the kit's Connect takes it directly.
func authOptions(cfg *config.NATSConfig, log *logger.Logger) ([]nats.Option, error) {
	var opts []nats.Option

	switch {
	case cfg.CredsFile != "":
		log.Info("using NATS JWT authentication", "credsFile", cfg.CredsFile)
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
