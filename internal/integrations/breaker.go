// Package integrations holds thin, circuit-broken adapters for the other Omni services.
// Every adapter fails soft: when its service is disabled or its breaker is open, calls
// return a sentinel error the workers log and skip, so the tracker degrades gracefully.
package integrations

import (
	"time"

	"github.com/sony/gobreaker"
)

// newBreaker builds a circuit breaker that trips after sustained failures.
func newBreaker(name string) *gobreaker.CircuitBreaker {
	return gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 5
		},
	})
}
