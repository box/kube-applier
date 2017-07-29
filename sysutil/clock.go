package sysutil

import "time"

// ClockInterface allows for mocking out the functionality of the standard time library when testing.
type ClockInterface interface {
	Now() time.Time
	Sleep(time.Duration)
}

// Clock implements ClockInterface with the standard time library functions.
type Clock struct{}

func (c *Clock) Now() time.Time {
	return time.Now()
}

func (c *Clock) Sleep(d time.Duration) {
	time.Sleep(d)
}
