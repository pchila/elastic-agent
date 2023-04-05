// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type e struct {
	count int
	at    time.Time
}

type schedulertest interface {
	WaitTick() <-chan time.Time
}

type tickRecorder struct {
	scheduler schedulertest
	count     int
	done      chan struct{}
	recorder  chan e
}

func (m *tickRecorder) Start() {
	for {
		select {
		case t := <-m.scheduler.WaitTick():
			m.count = m.count + 1
			m.recorder <- e{count: m.count, at: t}
		case <-m.done:
			return
		}
	}
}

func (m *tickRecorder) Stop() {
	close(m.done)
}

func TestScheduler(t *testing.T) {
	t.Run("Step scheduler", testStepScheduler)
	t.Run("PeriodicJitter scheduler", testPeriodicJitter)
}

func newTickRecorder[S schedulertest](scheduler S) *tickRecorder {
	return &tickRecorder{
		scheduler: scheduler,
		done:      make(chan struct{}),
		recorder:  make(chan e),
	}
}

func testStepScheduler(t *testing.T) {
	t.Run("Trigger the Tick manually", func(t *testing.T) {
		scheduler := NewStepper()
		defer scheduler.Stop()

		recorder := newTickRecorder(scheduler)
		go recorder.Start()
		defer recorder.Stop()

		scheduler.Next()
		nE := <-recorder.recorder
		require.Equal(t, 1, nE.count)
		scheduler.Next()
		nE = <-recorder.recorder
		require.Equal(t, 2, nE.count)
		scheduler.Next()
		nE = <-recorder.recorder
		require.Equal(t, 3, nE.count)
	})
}

func testPeriodicJitter(t *testing.T) {
	t.Run("tick then wait", func(t *testing.T) {
		duration := 1 * time.Second
		variance := 2 * time.Second
		scheduler := NewPeriodicJitter(duration, variance)
		defer scheduler.Stop()

		startedAt := time.Now()
		recorder := newTickRecorder(scheduler)
		go recorder.Start()
		defer recorder.Stop()

		nE := <-recorder.recorder

		diff1 := nE.at.Sub(startedAt)
		require.Less(t, diff1, duration+variance)
		require.GreaterOrEqual(t, diff1, duration)

		scheduler.Reset(duration)
		startedAt = time.Now()
		nE = <-recorder.recorder
		diff2 := nE.at.Sub(startedAt)
		require.Less(t, diff2, duration+variance)
		require.GreaterOrEqual(t, diff2, duration)

		assert.NotEqual(t, diff1, diff2)
	})
}
