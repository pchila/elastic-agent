// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package scheduler

import (
	"math/rand"
	"time"
)

// Stepper is a scheduler where each Tick is manually triggered, this is useful in scenario
// when you want to test the behavior of asynchronous code in a synchronous way.
type Stepper struct {
	C chan time.Time
}

// Next trigger the WaitTick unblock manually.
func (s *Stepper) Next() {
	s.C <- time.Now()
}

// WaitTick returns a channel to watch for ticks.
func (s *Stepper) WaitTick() <-chan time.Time {
	return s.C
}

// Stop is stopping the scheduler, in the case of the Stepper scheduler nothing is done.
func (s *Stepper) Stop() bool { return false }

// Reset is restarting the scheduler with the new duration, in the case of the Stepper scheduler nothing is done.
func (s *Stepper) Reset(time.Duration) {}

// NewStepper returns a new Stepper scheduler where the tick is manually controlled.
func NewStepper() *Stepper {
	return &Stepper{
		C: make(chan time.Time),
	}
}

// JitterTimer is as scheduler that will periodically create a timer
// and add some random variance to the duration when created or reset,
// to better distribute the load on the network and remote endpoint
type JitterTimer struct {
	t        *time.Timer
	duration time.Duration
	variance time.Duration
}

// NewJitterTimer creates a new JitterTimer initially waiting only for variance.
func NewJitterTimer(d, variance time.Duration) *JitterTimer {

	return &JitterTimer{
		variance: variance,
		duration: d,
		t:        time.NewTimer(randomizeDuration(d, variance)),
	}
}

// WaitTick returns the channel on which to block on read
// Note: you should not keep a reference to the channel.
func (p *JitterTimer) WaitTick() <-chan time.Time {
	return p.t.C
}

// Stop stops the PeriodicJitter scheduler.
func (p *JitterTimer) Stop() bool {
	return p.t.Stop()
}

// Reset set a new duration and restarts the PeriodicJitter scheduler.
// This must be called only on already stopped JitterTimers
func (p *JitterTimer) Restart() {
	p.t.Reset(randomizeDuration(p.duration, p.variance))
}

// Reset set a new duration and restarts the PeriodicJitter scheduler.
// This must be called only on already stopped JitterTimers
func (p *JitterTimer) Reset(d time.Duration) {
	p.t.Reset(randomizeDuration(d, p.variance))
}

func randomizeDuration(d time.Duration, v time.Duration) time.Duration {
	if v <= 0 {
		return d
	}
	return d + randomizeVariance(v)
}

func randomizeVariance(v time.Duration) time.Duration {
	t := int64(v)
	return time.Duration(rand.Int63n(t))
}
