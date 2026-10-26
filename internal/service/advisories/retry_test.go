package advisories

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetryDelayIsExponentialAndAlwaysCapped(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		base     time.Duration
		failures int
		want     time.Duration
	}{
		{name: "first", base: time.Second, failures: 1, want: time.Second},
		{name: "second", base: time.Second, failures: 2, want: 2 * time.Second},
		{name: "cap", base: time.Second, failures: 6, want: 30 * time.Second},
		{name: "large base", base: time.Minute, failures: 1, want: 30 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, retryDelay(test.base, test.failures))
		})
	}
}
