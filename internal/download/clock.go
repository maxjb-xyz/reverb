package download

import "time"

// Clock abstracts time so debounce tests don't wait real seconds.
type Clock interface {
	Now() time.Time
	// AfterFunc schedules f to run after d, returning a stop func that cancels it
	// (returns true if it stopped the timer before firing). Mirrors time.AfterFunc.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// RealClock is the production, time-based Clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
func (RealClock) AfterFunc(d time.Duration, f func()) func() bool {
	t := time.AfterFunc(d, f)
	return t.Stop
}
