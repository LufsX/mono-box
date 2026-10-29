package control

import (
	"fmt"
	"time"
)

// Separate retry state keeps one failing operation from delaying other events.
type watchRetry struct {
	key   string
	delay time.Duration
	next  time.Time
}

func (retry *watchRetry) ready(key string, now time.Time) bool {
	if retry.key != key {
		*retry = watchRetry{key: key}
	}
	return !now.Before(retry.next)
}

func (retry *watchRetry) failed(now time.Time, interval time.Duration) {
	if retry.delay == 0 {
		retry.delay = interval
	} else {
		retry.delay *= 2
	}
	if retry.delay > 30*interval {
		retry.delay = 30 * interval
	}
	retry.next = now.Add(retry.delay)
}

func (retry *watchRetry) reset() { *retry = watchRetry{} }

type watchError struct {
	message    string
	logged     time.Time
	suppressed int
}

// Log a changed error immediately and repeated errors at most every 30 seconds.
func (state *watchError) report(r *Runtime, err error, now time.Time) {
	if err == nil {
		*state = watchError{}
		return
	}
	message := err.Error()
	if message == state.message && now.Sub(state.logged) < 30*time.Second {
		state.suppressed++
		return
	}
	if message == state.message && state.suppressed > 0 {
		r.controllerError(fmt.Errorf("%w (%d repeated errors suppressed)", err, state.suppressed))
	} else {
		r.controllerError(err)
	}
	*state = watchError{message: message, logged: now}
}
