package provider

import (
	"sync"
	"time"
)

type BreakerState int

const (
	StateClosed BreakerState = iota
	StateOpen 
	StateHalfOpen 
)

type CircuitBreaker struct {
	mu sync.RWMutex
	state BreakerState
	consecutiveFailures int
	lastStateChange time.Time
	failureThreshold int
	cooldown time.Duration
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: threshold,
		cooldown:         cooldown,
		lastStateChange:  time.Now(),
	}
}

func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateOpen {
		// If cooldown has passed, let's transition to Half-Open and allow 1 request
		if time.Since(cb.lastStateChange) > cb.cooldown {
			cb.state = StateHalfOpen
			cb.lastStateChange = time.Now()
			return true
		}
		// Still in cooldown period, block request
		return false
	}
	return true
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures = 0
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++
	if cb.state == StateHalfOpen || cb.consecutiveFailures >= cb.failureThreshold {
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
	}
}

// State returns the current breaker state name (for debugging/metrics)
func (cb *CircuitBreaker) State() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}
