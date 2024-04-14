package apm

import "io"

// Healthchecks is the interface for gitlab.com/etke.cc/go/healthchecks,
// to avoid direct dependecy on the package for project where it is not needed
type Healthchecks interface {
	Fail(optionalBody ...io.Reader)
}

// HealthcheckFail sends a healthcheck fail event (if healthchecks are configured)
func HealthcheckFail(optionalBody ...io.Reader) {
	if hc != nil {
		hc.Fail(optionalBody...)
	}
}
