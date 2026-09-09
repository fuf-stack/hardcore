// Package service runs HTTP services with bounded graceful shutdown.
//
// It keeps process lifecycle explicit: callers provide the server, context,
// timeouts, and any shutdown hooks. Signal handling and product configuration
// remain application concerns.
package service
