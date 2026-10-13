package diag

import (
	"github.com/nats-io/nats.go/micro"
	"github.com/stone-age-io/access-control/internal/logger"
)

// HealthEndpoint is the endpoint name the controller's health request is
// listed under in $SRV.INFO and $SRV.STATS.
const HealthEndpoint = "health"

// Health is what the controller's NATS health endpoint answers with: the status
// page's report without its recent decisions.
//
// The status page stays on localhost because each decision carries the raw
// credential value that was presented. The health endpoint answers anyone the
// account lets publish on its subject, so those values never go in it. What a
// card did is already in the events stream, under its own permissions. Every
// other field is the box's own state: identity, build, NATS and policy health,
// its portals and aux points, fire suppression and recent door alarms.
func Health(r Report) Report {
	r.Decisions = nil
	return r
}

// RegisterHealth adds the read-only health endpoint to the controller's
// discovery service, on subject. Like the agent's commands, it has no queue
// group: two boxes misconfigured with one code both answer, where a queue group
// would hide one of them.
func RegisterHealth(svc micro.Service, subject string, src ReportSource, log *logger.Logger) error {
	return svc.AddEndpoint(HealthEndpoint, healthHandler(src, log.With("component", "health")),
		micro.WithEndpointSubject(subject),
		micro.WithEndpointQueueGroupDisabled(),
	)
}

// healthHandler answers every request with a fresh Health report. The request
// body is ignored.
func healthHandler(src ReportSource, log *logger.Logger) micro.HandlerFunc {
	return func(req micro.Request) {
		if err := req.RespondJSON(Health(src.Report())); err != nil {
			log.Warn("health reply failed", "error", err)
		}
	}
}
