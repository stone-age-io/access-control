// file: internal/natsx/service.go

package natsx

import (
	"regexp"
	"strings"

	"github.com/nats-io/nats.go/micro"
	"github.com/stone-age-io/access-control/internal/version"
)

// The discovery name of each binary. Every copy of a binary shares its name,
// which is what lets one $SRV.PING.<name> find all of them.
const (
	ServiceController = "access-controller"
	ServiceAccessd    = "accessd"
)

// RegisterService announces this process on the NATS micro discovery protocol
// ($SRV.PING / $SRV.INFO / $SRV.STATS), so `nats micro ls` shows what is running,
// at which version, and where. It is identity, not an API: the service has NO
// endpoints, so all three verbs answer with name, version, metadata and start
// time, and there is nothing to call, read or control through it.
//
// The $SRV subjects belong to the micro protocol and the micro library builds
// them. They are not app subjects, so they do not go through internal/subjects.
//
// Never fatal, never blocking: micro adds no timers and sends no requests, and its
// subscriptions wait in the client while a cold-booted controller is still
// retrying NATS. A failure is logged and returns nil. A credential without $SRV.>
// is refused discovery only; nats.go reports that to stderr. Stop the returned
// service before closing the connection.
func (c *Conn) RegisterService(name, description string, meta map[string]string) micro.Service {
	ver := serviceVersion(version.Main())
	svc, err := micro.AddService(c.NC, micro.Config{
		Name:        name,
		Version:     ver,
		Description: description,
		Metadata:    meta,
	})
	if err != nil {
		c.log.Warn("NATS service discovery registration failed; continuing without it", "service", name, "error", err)
		return nil
	}
	c.log.Info("registered for NATS service discovery", "service", name, "version", ver)
	return svc
}

// semverPattern is the regular expression semver.org publishes, which is also
// the one micro validates a service version against.
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// serviceVersion is the build version in the form micro accepts: a leading
// "v" is dropped, and anything still not semver reports as 0.0.0-dev.
func serviceVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if semverPattern.MatchString(v) {
		return v
	}
	return "0.0.0-dev"
}
