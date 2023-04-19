// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package fleet

import (
	"context"
	"time"
)

// type debouncerFunc[T any] func(context.Context, <-chan T, time.Duration) <-chan T

type accumulatorDebouncerFunc[T any, R any] func(context.Context, <-chan T, time.Duration, int) <-chan R

func Debounce[T any](ctx context.Context, in <-chan T, minDebounce time.Duration) <-chan T {

	debounceTimer := time.NewTimer(minDebounce)
	cancelDebounceTimer := func() {
		if !debounceTimer.Stop() {
			<-debounceTimer.C
		}
	}

	return debounceWithTimeSource(ctx, in, debounceTimer.C, cancelDebounceTimer)
}

func debounceWithTimeSource[T any](ctx context.Context, in <-chan T, timeSrc <-chan time.Time, stopTimeSrc func()) <-chan T {
	outCh := make(chan T, 1)
	go func() {
		defer close(outCh)
		var (
			value              T
			receivedNewValue   bool
			minDebounceElapsed bool
		)

		for {
			select {
			case <-ctx.Done():
				// TODO: Should we return value when context expires?
				if receivedNewValue {
					outCh <- value
				}
				stopTimeSrc()
				return
			case newValue, ok := <-in:
				if !ok {
					// input channel has closed, set it to nil  so we don't unblock here anymore
					in = nil
					continue
				}
				receivedNewValue = true
				value = newValue
				if minDebounceElapsed {
					outCh <- value
					return
				}
			case <-timeSrc:
				minDebounceElapsed = true
				if receivedNewValue {
					outCh <- value
					return
				}
			}
		}
	}()

	return outCh
}

type TimestampedValue[T any] struct {
	Time  time.Time
	Value T
}

type Clock interface {
	Now() time.Time
}

type stdlibClock struct{}

func (stdC stdlibClock) Now() time.Time {
	return time.Now()
}

func AccumulatorDebounce[T any, R []TimestampedValue[T]](ctx context.Context, in <-chan T, minDebounce time.Duration, maxItems int) <-chan R {

	debounceTimer := time.NewTimer(minDebounce)
	cancelDebounceTimer := func() {
		if !debounceTimer.Stop() {
			<-debounceTimer.C
		}
	}
	return accumulatorDebounceWithTimeSources[T, R](ctx, in, debounceTimer.C, cancelDebounceTimer, new(stdlibClock), maxItems)
}

func accumulatorDebounceWithTimeSources[T any, R []TimestampedValue[T]](ctx context.Context, in <-chan T, timeSrc <-chan time.Time, stopTimeSrc func(), clock Clock, maxItems int) <-chan R {
	outCh := make(chan R, 1)
	go func() {
		defer close(outCh)
		var (
			values             R
			receivedNewValue   bool
			minDebounceElapsed bool
		)

		for {
			select {
			case <-ctx.Done():
				// TODO: Should we return value when context expires?
				if receivedNewValue {
					outCh <- values
				}
				stopTimeSrc()
				return
			case newValue, ok := <-in:
				if !ok {
					// input channel has closed, set it to nil  so we don't unblock here anymore
					in = nil
					continue
				}
				receivedNewValue = true
				newTimestampedValue := TimestampedValue[T]{
					Value: newValue,
					Time:  clock.Now(),
				}
				if maxItems > 0 && len(values) == maxItems {
					values = append(values[1:], newTimestampedValue)
				} else {
					values = append(values, newTimestampedValue)
				}

				if minDebounceElapsed {
					outCh <- values
					return
				}
			case <-timeSrc:
				minDebounceElapsed = true
				if receivedNewValue {
					outCh <- values
					return
				}
			}
		}
	}()

	return outCh
}
