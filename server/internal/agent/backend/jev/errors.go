package jev

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrorClass classifies a failed call.
type ErrorClass string

// Error classes. The first group answers a request that was sent; the
// second refuses locally, without sending anything.
const (
	ClassAuth       ErrorClass = "auth"        // 401/403: the client disables itself
	ClassBadRequest ErrorClass = "bad_request" // 400/422: the question set is marked bad
	ClassRateLimit  ErrorClass = "rate_limit"  // 429: cooldown (Retry-After)
	ClassOverloaded ErrorClass = "overloaded"  // 529: cooldown (Retry-After)
	ClassServer     ErrorClass = "server"      // 408, 5xx: retried when allowed
	ClassTimeout    ErrorClass = "timeout"     // the attempt or the caller's deadline ran out
	ClassTransport  ErrorClass = "transport"   // no HTTP response
	ClassDecode     ErrorClass = "decode"      // a 2xx body the decoder cannot read
	ClassHTTP       ErrorClass = "http"        // any other status
	ClassCanceled   ErrorClass = "canceled"    // the caller canceled

	ClassDisabled  ErrorClass = "disabled"     // after an auth failure
	ClassBreaker   ErrorClass = "breaker_open" // too many consecutive failures
	ClassCooldown  ErrorClass = "cooldown"     // after a 429/529
	ClassBudget    ErrorClass = "budget"       // MaxCostUSD reached
	ClassThrottled ErrorClass = "throttled"    // the rate limiter refused
	ClassBadSet    ErrorClass = "bad_set"      // this question set was refused before
)

// Error is a failed call. It never contains the API key.
type Error struct {
	Class  ErrorClass
	Status int // HTTP status (0 without a response)
	// RetryAfter is the server's Retry-After (429/529).
	RetryAfter time.Duration
	Msg        string
}

func (e *Error) Error() string {
	s := "jev: " + string(e.Class)
	if e.Status != 0 {
		s += fmt.Sprintf(" (status %d)", e.Status)
	}
	if e.Msg != "" {
		s += ": " + e.Msg
	}
	return s
}

// HTTPStatus returns the HTTP status (for trace api_call events).
func (e *Error) HTTPStatus() int { return e.Status }

// Is makes errors.Is(err, context.DeadlineExceeded) hold for a timeout and
// errors.Is(err, context.Canceled) for a canceled call, so callers (the
// decide scheduler) see a timed-out attempt as a timeout.
func (e *Error) Is(target error) bool {
	switch target {
	case context.DeadlineExceeded:
		return e.Class == ClassTimeout
	case context.Canceled:
		return e.Class == ClassCanceled
	}
	return false
}

// Retryable reports a class worth another attempt.
func (c ErrorClass) Retryable() bool {
	return c == ClassServer || c == ClassTimeout || c == ClassTransport
}

// failure reports a class that counts towards the circuit breaker: every
// answer that is neither usable nor handled on its own (auth disables the
// client, 429/529 start a cooldown). A 400/422 counts too, so a schema the
// API rejects in every shape still backs off.
func (c ErrorClass) failure() bool {
	switch c {
	case ClassServer, ClassTimeout, ClassTransport, ClassDecode, ClassBadRequest, ClassHTTP:
		return true
	}
	return false
}

// ClassOf returns the class of an error returned by Client.Decide ("" for
// other errors).
func ClassOf(err error) ErrorClass {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return ""
}

// ErrorCounts counts failed calls by class.
type ErrorCounts struct {
	Auth, BadRequest, RateLimit, Overloaded, Server, Timeout, Transport, Decode, HTTP, Canceled int64
	Disabled, Breaker, Cooldown, Budget, Throttled, BadSet                                      int64
}

func (c *ErrorCounts) add(class ErrorClass) {
	switch class {
	case ClassAuth:
		c.Auth++
	case ClassBadRequest:
		c.BadRequest++
	case ClassRateLimit:
		c.RateLimit++
	case ClassOverloaded:
		c.Overloaded++
	case ClassServer:
		c.Server++
	case ClassTimeout:
		c.Timeout++
	case ClassTransport:
		c.Transport++
	case ClassDecode:
		c.Decode++
	case ClassHTTP:
		c.HTTP++
	case ClassCanceled:
		c.Canceled++
	case ClassDisabled:
		c.Disabled++
	case ClassBreaker:
		c.Breaker++
	case ClassCooldown:
		c.Cooldown++
	case ClassBudget:
		c.Budget++
	case ClassThrottled:
		c.Throttled++
	case ClassBadSet:
		c.BadSet++
	}
}

// Total is the number of failed calls.
func (c *ErrorCounts) Total() int64 {
	return c.Auth + c.BadRequest + c.RateLimit + c.Overloaded + c.Server + c.Timeout + c.Transport + c.Decode + c.HTTP +
		c.Canceled + c.Disabled + c.Breaker + c.Cooldown + c.Budget + c.Throttled + c.BadSet
}
