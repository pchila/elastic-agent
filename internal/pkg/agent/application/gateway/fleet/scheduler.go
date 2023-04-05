// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package fleet

import "time"

// Scheduler simple interface that encapsulate the scheduling logic.
// The interface closely follow time.Timer, since we'll have to start,
// stop and reset duration
type Scheduler interface {
	Reset(time.Duration)
	WaitTick() <-chan time.Time
	Stop() bool
}
