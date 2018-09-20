package sysutil

import "time"

// ClockInterface allows for mocking out the functionality of the standard time library when testing.
type ClockInterface interface {
	Now() time.Time
	Since(time.Time) time.Duration
	Sleep(time.Duration)
}

// Clock implements ClockInterface with the standard time library functions.
type Clock struct{}

// Now returns current time
func (c *Clock) Now() time.Time {
	return time.Now()
}

// Since returns time since t
func (c *Clock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// Sleep sleeps for d duration
func (c *Clock) Sleep(d time.Duration) {
	time.Sleep(d)
}
