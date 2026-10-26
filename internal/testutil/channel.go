package testutil

import (
	"testing"
	"time"
)

const channelWaitTimeout = 5 * time.Second

// Receive waits for one test signal without allowing a failed asynchronous path to hang the suite.
func Receive[T any](t testing.TB, channel <-chan T) T {
	t.Helper()
	timer := time.NewTimer(channelWaitTimeout)
	defer timer.Stop()

	select {
	case value := <-channel:
		return value
	case <-timer.C:
		t.Fatal("timed out waiting for test channel")
		var zero T
		return zero
	}
}
