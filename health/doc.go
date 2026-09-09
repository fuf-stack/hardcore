// Package health provides HTTP liveness and readiness probes for services.
//
// A Probes value starts unready. Applications explicitly open its readiness
// gate after startup and close it before shutdown. Optional readiness checks
// run concurrently within one bounded timeout.
package health
