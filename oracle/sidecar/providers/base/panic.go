package base

import "fmt"

// RunRecovering runs fn and converts a panic in the current goroutine into an error.
func RunRecovering(name string, fn func() error) (err error) {
	defer func() {
		if recErr := recover(); recErr != nil {
			err = fmt.Errorf("%s panicked: %v", name, recErr)
		}
	}()

	return fn()
}
