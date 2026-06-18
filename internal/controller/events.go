package controller

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// Emitter publishes access events. The controller emits to the {app}.evt subtree
// over core NATS; the ACC_EVENTS JetStream stream captures them for audit.
type Emitter interface {
	Emit(subject string, payload any) error
}

type natsEmitter struct{ nc *nats.Conn }

// NewNATSEmitter returns an Emitter backed by a core NATS connection.
func NewNATSEmitter(nc *nats.Conn) Emitter { return &natsEmitter{nc: nc} }

func (e *natsEmitter) Emit(subject string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return e.nc.Publish(subject, b)
}

// TapEvent is emitted once per credential presentation, carrying the outcome.
type TapEvent struct {
	Cred   string `json:"cred"`
	User   string `json:"user"`
	Allow  bool   `json:"allow"`
	Reason string `json:"reason"`
	TS     string `json:"ts"`
}

// StateEvent is emitted when a portal's effective posture changes.
type StateEvent struct {
	Posture string `json:"posture"`
	Actor   string `json:"actor,omitempty"`
	Reason  string `json:"reason,omitempty"`
	TS      string `json:"ts"`
}
