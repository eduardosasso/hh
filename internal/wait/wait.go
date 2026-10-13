// Package wait polls a condition with a deadline.
package wait

import (
	"errors"
	"time"
)

const poll = 100 * time.Millisecond

// ErrTimeout means the condition never held.
var ErrTimeout = errors.New("timed out")

// Until polls done until it reports true, returns an error, or d passes.
func Until(d time.Duration, done func() (bool, error)) error {
	deadline := time.Now().Add(d)
	for {
		ok, err := done()
		if ok || err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return ErrTimeout
		}
		time.Sleep(poll)
	}
}
