package desktopruntime

import "time"

const (
	tunnelRetryInitialDelay = 5 * time.Second
	tunnelRetryMaximumDelay = time.Minute
	tunnelRetryStableRun    = 2 * time.Minute
)

func nextTunnelRetryDelay(previous, runDuration time.Duration) time.Duration {
	if previous <= 0 || runDuration >= tunnelRetryStableRun {
		return tunnelRetryInitialDelay
	}
	next := previous * 2
	if next > tunnelRetryMaximumDelay {
		return tunnelRetryMaximumDelay
	}
	return next
}
