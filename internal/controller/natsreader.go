package controller

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// Portal is a controller's view of one portal it drives: its code (the {thing}
// subject segment) and its type (the {type} segment). The controller resolves
// these from the PolicyStore after the initial sync, so it can build exact
// {app}.{location}.{type}.{thing}.tap subjects.
type Portal struct {
	Code string
	Type string
}

// NATSReader is the v1 "reader": instead of OSDP/RS485 hardware, it turns NATS
// messages into taps so presentations can be simulated with `nats pub`. It
// subscribes per configured portal to {app}.{location}.{type}.{thing}.tap; the
// message body is {"cred":"..."} (or a bare credential string).
type NATSReader struct {
	log  *logger.Logger
	ch   chan drivers.Tap
	subs []*nats.Subscription
}

// NewNATSReader subscribes to the tap subject for each portal.
func NewNATSReader(nc *nats.Conn, location string, portals []Portal, subs subjects.Subjects, log *logger.Logger) (*NATSReader, error) {
	r := &NATSReader{
		log: log.With("component", "nats-reader"),
		ch:  make(chan drivers.Tap, 64),
	}
	for _, portal := range portals {
		subject := subs.Tap(location, portal.Type, portal.Code)
		code := portal.Code // capture
		sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
			r.ch <- drivers.Tap{Portal: code, Credential: parseCred(msg.Data), At: time.Now().UTC()}
		})
		if err != nil {
			r.stopSubs()
			return nil, fmt.Errorf("subscribe %q: %w", subject, err)
		}
		r.subs = append(r.subs, sub)
		r.log.Info("reader subscribed", "subject", subject)
	}
	return r, nil
}

// Taps implements drivers.ReaderDriver.
func (r *NATSReader) Taps() <-chan drivers.Tap { return r.ch }

// Stop unsubscribes and closes the taps channel.
func (r *NATSReader) Stop() {
	r.stopSubs()
	close(r.ch)
}

func (r *NATSReader) stopSubs() {
	for _, s := range r.subs {
		_ = s.Unsubscribe()
	}
	r.subs = nil
}

// parseCred accepts {"cred":"..."} JSON or a bare credential string.
func parseCred(data []byte) string {
	var body struct {
		Cred string `json:"cred"`
	}
	if err := json.Unmarshal(data, &body); err == nil && body.Cred != "" {
		return body.Cred
	}
	return string(data)
}
