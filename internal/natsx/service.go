// file: internal/natsx/service.go

package natsx

import (
	"github.com/nats-io/nats.go/micro"
	kitnatsx "github.com/stone-age-io/kit/natsx"
)

// The discovery name of each binary. Every copy of a binary shares its name,
// which is what lets one $SRV.PING.<name> find all of them.
const (
	ServiceController = "access-controller"
	ServiceAccessd    = "accessd"
)

// RegisterService announces this process on the NATS micro discovery protocol
// ($SRV.PING / $SRV.INFO / $SRV.STATS) through the kit, so `nats micro ls` shows
// what is running, at which version, and on which Thing: code is the process's
// Thing code, carried as `code` metadata (ADR 0008 rule 4), and the kit adds
// `version`. The service has no endpoints of its own; the controller adds its
// read-only health endpoint to the returned service.
//
// The $SRV subjects belong to the micro protocol and the micro library builds
// them. They are not app subjects, so they do not go through internal/subjects.
//
// Never fatal, never blocking: micro adds no timers and sends no requests, and its
// subscriptions wait in the client while a cold-booted controller is still
// retrying NATS. A failure, including an empty code, is logged and returns nil.
// Stop the returned service before closing the connection.
func (c *Conn) RegisterService(name, description, code string, meta map[string]string) micro.Service {
	svc, err := kitnatsx.RegisterService(c.NC, kitnatsx.Service{
		Name:        name,
		Description: description,
		Code:        code,
		Metadata:    meta,
	})
	if err != nil {
		c.log.Warn("NATS service discovery registration skipped; continuing without it", "service", name, "error", err)
		return nil
	}
	c.log.Info("registered for NATS service discovery", "service", name, "code", code, "version", svc.Info().Version)
	return svc
}
