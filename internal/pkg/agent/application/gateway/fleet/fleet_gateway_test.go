// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package fleet

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/coordinator/state"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/agent/storage"
	"github.com/elastic/elastic-agent/internal/pkg/agent/storage/store"
	"github.com/elastic/elastic-agent/internal/pkg/fleetapi/acker/noop"
	"github.com/elastic/elastic-agent/internal/pkg/help"
	"github.com/elastic/elastic-agent/internal/pkg/scheduler"
	"github.com/elastic/elastic-agent/pkg/component/runtime"
	agentclient "github.com/elastic/elastic-agent/pkg/control/v2/client"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

// Refer to https://vektra.github.io/mockery/installation/ to check how to install mockery binary
//go:generate mockery --name StateFetcher
//go:generate mockery --name clock
//go:generate mockery --keeptree --output . --outpkg fleet --dir ../../coordinator/state --name StateUpdateSource

type clientCallbackFunc func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error)

type testingClientInvocation struct {
	headers http.Header
	body    []byte
	resp    *http.Response
	err     error
}

type testingClient struct {
	sync.Mutex
	t        *testing.T
	callback clientCallbackFunc
	received chan testingClientInvocation
}

func (t *testingClient) Send(
	ctx context.Context,
	_ string,
	_ string,
	_ url.Values,
	headers http.Header,
	body io.Reader,
) (*http.Response, error) {
	t.Lock()
	defer t.Unlock()

	var byteBuf bytes.Buffer

	if _, copyerr := io.Copy(&byteBuf, body); copyerr != nil {
		t.t.Errorf("Error reading request body: %v", copyerr)
	}
	bodyBytes := byteBuf.Bytes()

	resp, err := t.callback(ctx, headers, bytes.NewReader(bodyBytes))
	defer func() {
		t.received <- testingClientInvocation{
			headers: headers,
			body:    bodyBytes,
			resp:    resp,
			err:     err,
		}
	}()
	return resp, err
}

func (t *testingClient) URI() string {
	return "http://localhost"
}

func (t *testingClient) Answer(fn clientCallbackFunc) <-chan testingClientInvocation {
	t.Lock()
	defer t.Unlock()
	t.callback = fn
	return t.received
}

func newTestingClient(t *testing.T) *testingClient {
	return &testingClient{t: t, received: make(chan testingClientInvocation, 1)}
}

func emptyStateFetcher(t *testing.T) *MockStateFetcher {
	mockFetcher := NewMockStateFetcher(t)
	mockFetcher.EXPECT().StateSubscribe(mock.Anything).RunAndReturn(func(ctx context.Context) state.StateUpdateSource {
		ch := make(chan state.State)
		stateUpdateSrc := NewStateUpdateSource(t)
		stateUpdateSrc.EXPECT().Ch().Return(ch)
		// execute a goroutine to send the state ONCE on the channel
		go func() {
			ch <- state.State{}
		}()
		return stateUpdateSrc
	})
	return mockFetcher
}

type withGatewayFunc func(*testing.T, *fleetGateway, *testingClient, *scheduler.Stepper)

func withGateway(agentInfo agentInfo, settings FleetGatewaySettings, fn withGatewayFunc) func(t *testing.T) {
	return func(t *testing.T) {
		scheduler := scheduler.NewStepper()
		client := newTestingClient(t)

		log, _ := logger.New("fleet_gateway", false)

		stateStore := newStateStore(t, log)

		gateway, err := newFleetGatewayWithSchedulerAndClock(
			log,
			settings,
			agentInfo,
			client,
			scheduler,
			new(stdlibClock),
			noop.New(),
			emptyStateFetcher(t),
			stateStore,
		)

		require.NoError(t, err)

		fn(t, gateway, client, scheduler)
	}
}
func withGatewayAndLog(agentInfo agentInfo, logIF loggerIF, settings FleetGatewaySettings, fn withGatewayFunc) func(t *testing.T) {
	return func(t *testing.T) {
		scheduler := scheduler.NewStepper()
		client := newTestingClient(t)

		log, _ := logger.New("fleet_gateway", false)

		stateStore := newStateStore(t, log)

		gateway, err := newFleetGatewayWithSchedulerAndClock(
			logIF,
			settings,
			agentInfo,
			client,
			scheduler,
			new(stdlibClock),
			noop.New(),
			emptyStateFetcher(t),
			stateStore,
		)

		require.NoError(t, err)

		fn(t, gateway, client, scheduler)
	}
}

func ackSeq(channels ...<-chan testingClientInvocation) func() {
	return func() {
		for _, c := range channels {
			<-c
		}
	}
}

func wrapStrToResp(code int, body string) *http.Response {
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", code, http.StatusText(code)),
		StatusCode:    code,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          io.NopCloser(bytes.NewBufferString(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
	}
}

func mustWriteToChannelBeforeTimeout[V any](t *testing.T, value V, channel chan V, timeout time.Duration) {
	timer := time.NewTimer(timeout)

	select {
	case channel <- value:
		if !timer.Stop() {
			<-timer.C
		}
		t.Logf("%T written", value)
	case <-timer.C:
		t.Errorf("Timeout writing %T", value)
	}
}

func TestFleetGateway(t *testing.T) {
	agentInfo := &testAgentInfo{}
	settings := FleetGatewaySettings{
		Duration: 5 * time.Second,
		Backoff:  backoffSettings{Init: 1 * time.Second, Max: 5 * time.Second},
	}

	t.Run("send no event and receive no action", withGateway(agentInfo, settings, func(
		t *testing.T,
		gateway *fleetGateway,
		client *testingClient,
		scheduler *scheduler.Stepper,
	) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		waitFn := ackSeq(
			client.Answer(func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error) {
				resp := wrapStrToResp(http.StatusOK, `{ "actions": [] }`)
				return resp, nil
			}),
		)

		errCh := runFleetGateway(ctx, gateway)

		// Synchronize scheduler and acking of calls from the worker go routine.
		scheduler.Next()
		waitFn()

		cancel()
		err := <-errCh
		require.NoError(t, err)
		select {
		case actions := <-gateway.Actions():
			t.Errorf("Expected no actions, got %v", actions)
		default:
		}
	}))

	t.Run("Successfully connects and receives a series of actions", withGateway(agentInfo, settings, func(
		t *testing.T,
		gateway *fleetGateway,
		client *testingClient,
		scheduler *scheduler.Stepper,
	) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		waitFn := ackSeq(
			client.Answer(func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error) {
				// TODO: assert no events
				resp := wrapStrToResp(http.StatusOK, `
	{
		"actions": [
			{
				"type": "POLICY_CHANGE",
				"id": "id1",
				"data": {
					"policy": {
						"id": "policy-id"
					}
				}
			},
			{
				"type": "ANOTHER_ACTION",
				"id": "id2"
			}
		]
	}
	`)
				return resp, nil
			}),
		)

		errCh := runFleetGateway(ctx, gateway)

		scheduler.Next()
		waitFn()
		time.Sleep(50 * time.Millisecond)
		cancel()
		err := <-errCh
		require.NoError(t, err)
		select {
		case actions := <-gateway.Actions():
			require.Len(t, actions, 2)
		default:
			t.Errorf("Expected to receive actions")
		}
	}))

	// Test the normal time based execution.
	t.Run("Periodically communicates with Fleet", func(t *testing.T) {
		scheduler := scheduler.NewPeriodic(150 * time.Millisecond)
		client := newTestingClient(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log, _ := logger.New("tst", false)
		stateStore := newStateStore(t, log)

		gateway, err := newFleetGatewayWithSchedulerAndClock(
			log,
			settings,
			agentInfo,
			client,
			scheduler,
			new(stdlibClock),
			noop.New(),
			emptyStateFetcher(t),
			stateStore,
		)
		require.NoError(t, err)

		waitFn := ackSeq(
			client.Answer(func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error) {
				resp := wrapStrToResp(http.StatusOK, `{ "actions": [] }`)
				return resp, nil
			}),
		)

		errCh := runFleetGateway(ctx, gateway)

		func() {
			var count int
			for {
				waitFn()
				count++
				if count == 4 {
					return
				}
			}
		}()

		cancel()
		err = <-errCh
		require.NoError(t, err)
	})

	t.Run("Test the wait loop is interruptible", func(t *testing.T) {
		// 20mins is the double of the base timeout values for golang test suites.
		// If we cannot interrupt we will timeout.
		d := 20 * time.Minute
		scheduler := scheduler.NewPeriodic(d)
		client := newTestingClient(t)

		ctx, cancel := context.WithCancel(context.Background())

		log, _ := logger.New("tst", false)
		stateStore := newStateStore(t, log)

		gateway, err := newFleetGatewayWithSchedulerAndClock(
			log,
			FleetGatewaySettings{
				Duration: d,
				Backoff:  backoffSettings{Init: 1 * time.Second, Max: 30 * time.Second},
			},
			agentInfo,
			client,
			scheduler,
			new(stdlibClock),
			noop.New(),
			emptyStateFetcher(t),
			stateStore,
		)
		require.NoError(t, err)

		ch2 := client.Answer(func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error) {
			resp := wrapStrToResp(http.StatusOK, `{ "actions": [] }`)
			return resp, nil
		})

		errCh := runFleetGateway(ctx, gateway)

		// Make sure that all API calls to the checkin API are successful, the following will happen:
		// block on the first call.
		<-ch2

		go func() {
			// drain the channel
			for range ch2 {
			}
		}()

		// 1. Gateway will check the API on boot.
		// 2. WaitTick() will block for 20 minutes.
		// 3. Stop will should unblock the wait.
		cancel()
		err = <-errCh
		require.NoError(t, err)
	})

	t.Run("Test the long poll checkin still consumes states", func(t *testing.T) {
		longPollClient := newTestingClient(t)
		answerCh := longPollClient.Answer(func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error) {
			time.Sleep(500 * time.Millisecond)
			resp := wrapStrToResp(http.StatusOK, `{ "actions": [] }`)
			return resp, nil
		})
		ctx, cancel := context.WithCancel(context.Background())

		log, _ := logger.New("tst", false)
		stateStore := newStateStore(t, log)

		scheduler := scheduler.NewStepper()
		mockLogger := newTestLogger(t)

		mockFetcher := NewMockStateFetcher(t)

		var statesSent int

		mockFetcher.EXPECT().StateSubscribe(mock.Anything).RunAndReturn(func(ctx context.Context) state.StateUpdateSource {
			ch := make(chan state.State)
			stateUpdateSrc := NewStateUpdateSource(t)
			stateUpdateSrc.EXPECT().Ch().Return(ch)

			// execute a goroutine to send the state a few times
			statesSent = 0
			go func() {
				defer close(ch)
				for {
					select {
					case ch <- state.State{}:
						statesSent++
					case <-ctx.Done():
						return
					}
				}
			}()

			return stateUpdateSrc
		})

		gateway, err := newFleetGatewayWithSchedulerAndClock(
			mockLogger,
			DefaultFleetGatewaySettings(),
			agentInfo,
			longPollClient,
			scheduler,
			new(stdlibClock),
			noop.New(),
			mockFetcher,
			stateStore,
		)
		require.NoError(t, err)

		errCh := runFleetGateway(ctx, gateway)

		scheduler.Next()

		// Make sure that all API calls to the checkin API are successful, the following will happen:
		// block on the first call.
		<-answerCh

		cancel()

		go func() {
			// drain the channel
			for range answerCh {
			}
		}()

		err = <-errCh
		require.NoError(t, err)

		assert.Greater(t, statesSent, 1) // at this point we have sent waaaay more than one, let's keep it easy for slower systems
	})

	t.Run("Long poll checkin drops state updates coming earlier than configured debounce", func(t *testing.T) {

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		longPollClient := newTestingClient(t)

		// completeCheckinCh := make(chan struct{}, 1)

		clientInvocationChannel := longPollClient.Answer(func(ctx context.Context, headers http.Header, body io.Reader) (*http.Response, error) {
			select {
			// case <-completeCheckinCh:
			case <-time.After(1 * time.Second):
				resp := wrapStrToResp(http.StatusOK, `{ "actions": [] }`)
				return resp, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

		reportedAgentStatuses := make([]testingClientInvocation, 0, 2)

		// fleet test client invocation consumer
		go func() {
			for {
				select {
				case tci := <-clientInvocationChannel:
					reportedAgentStatuses = append(reportedAgentStatuses, tci)
				case <-ctx.Done():
					return
				}
			}
		}()

		log, _ := logger.New("tst", false)
		stateStore := newStateStore(t, log)

		scheduler := scheduler.NewStepper()
		mockLogger := newTestLogger(t)

		mockClock := newMockClock(t)
		now := time.Now()
		t.Logf("Setting the clock to %v", now)
		mockClock.EXPECT().Now().Return(now).Once()

		mockFetcher := NewMockStateFetcher(t)

		stateCh := make(chan state.State)
		mockFetcher.EXPECT().StateSubscribe(mock.Anything).RunAndReturn(func(ctx context.Context) state.StateUpdateSource {
			stateUpdateSrc := NewStateUpdateSource(t)
			stateUpdateSrc.EXPECT().Ch().Return(stateCh)
			go func() {
				<-ctx.Done()
			}()
			return stateUpdateSrc
		})

		// pass the initial status

		agentStartingState := state.State{
			State:        agentclient.Starting,
			Message:      "Agent is starting",
			FleetState:   agentclient.Healthy,
			FleetMessage: "Fleet is healthy",
			Components:   []runtime.ComponentComponentState{},
			LogLevel:     logp.InfoLevel,
		}
		go mustWriteToChannelBeforeTimeout(t, agentStartingState, stateCh, 50*time.Millisecond)

		settings := DefaultFleetGatewaySettings()
		settings.Debounce = 5 * time.Second

		gateway, err := newFleetGatewayWithSchedulerAndClock(
			mockLogger,
			settings,
			agentInfo,
			longPollClient,
			scheduler,
			mockClock,
			noop.New(),
			mockFetcher,
			stateStore,
		)
		require.NoError(t, err)

		errCh := runFleetGateway(ctx, gateway)

		scheduler.Next()

		// move the clock forward 10ms
		now = now.Add(10 * time.Millisecond)
		t.Logf("Setting the clock to %v", now)
		mockClock.EXPECT().Now().Return(now).Once()

		// inject new state
		agentRunningState := state.State{
			State:        agentclient.Healthy,
			Message:      "Agent is healthy",
			FleetState:   agentclient.Healthy,
			FleetMessage: "Fleet is healthy",
			Components:   []runtime.ComponentComponentState{},
			LogLevel:     logp.InfoLevel,
		}
		go mustWriteToChannelBeforeTimeout(t, agentStartingState, stateCh, 50*time.Millisecond)

		assert.Eventually(t, func() bool { return len(reportedAgentStatuses) > 0 }, 30*time.Second, 50*time.Millisecond)
		assert.Len(t, reportedAgentStatuses, 1) // we should have only 1 reported state

		// move the clock forward 5 seconds (we move beyond the debounce)
		now = now.Add(5 * time.Second)
		t.Logf("Setting the clock to %v", now)
		mockClock.EXPECT().Now().Return(now).Twice()

		//inject another new state
		agentUnhealthyState := state.State{
			State:        agentclient.Degraded,
			Message:      "Agent is degraded",
			FleetState:   agentclient.Healthy,
			FleetMessage: "Fleet is healthy",
			Components:   []runtime.ComponentComponentState{},
			LogLevel:     logp.InfoLevel,
		}
		go mustWriteToChannelBeforeTimeout(t, agentUnhealthyState, stateCh, 50*time.Millisecond)

		// Make sure that all API calls to the checkin API are successful, the following will happen:
		// block on the first (canceled) and block on second call.
		// completeCheckinCh <- struct{}{}
		// <-clientInvocationChannel

		// completeCheckinCh <- struct{}{}
		// <-clientInvocationChannel

		assert.Eventually(t, func() bool { return len(reportedAgentStatuses) > 1 }, 30*time.Second, 50*time.Millisecond)
		assert.Len(t, reportedAgentStatuses, 2) // we should have only 2 reported states (the canceled one and the completed one with the new state)
		// assert.Contains(t, reportedAgentStatuses, agentStartingState)
		assert.NotContains(t, reportedAgentStatuses, agentRunningState)

		cancel()

		err = <-errCh
		require.NoError(t, err)
	})
}

func TestRetriesOnFailures(t *testing.T) {
	agentInfo := &testAgentInfo{}
	settings := FleetGatewaySettings{
		Duration: 5 * time.Second,
		Backoff:  backoffSettings{Init: 100 * time.Millisecond, Max: 5 * time.Second},
	}

	t.Run("When the gateway fails to communicate with the checkin API we will retry",
		withGateway(agentInfo, settings, func(
			t *testing.T,
			gateway *fleetGateway,
			client *testingClient,
			scheduler *scheduler.Stepper,
		) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fail := func(_ context.Context, _ http.Header, _ io.Reader) (*http.Response, error) {
				return wrapStrToResp(http.StatusInternalServerError, "something is bad"), nil
			}
			clientWaitFn := client.Answer(fail)

			errCh := runFleetGateway(ctx, gateway)

			// Initial tick is done out of bound so we can block on channels.
			scheduler.Next()

			// Simulate a 500 errors for the next 3 calls.
			<-clientWaitFn
			<-clientWaitFn
			<-clientWaitFn

			// API recover
			waitFn := ackSeq(
				client.Answer(func(ctx context.Context, _ http.Header, body io.Reader) (*http.Response, error) {
					resp := wrapStrToResp(http.StatusOK, `{ "actions": [] }`)
					return resp, nil
				}),
			)

			waitFn()

			cancel()
			err := <-errCh
			require.NoError(t, err)
			select {
			case actions := <-gateway.Actions():
				t.Errorf("Expected no actions, got %v", actions)
			default:
			}
		}))

	t.Run("The retry loop is interruptible",
		withGateway(agentInfo, FleetGatewaySettings{
			Duration: 0 * time.Second,
			Backoff:  backoffSettings{Init: 10 * time.Minute, Max: 20 * time.Minute},
		}, func(
			t *testing.T,
			gateway *fleetGateway,
			client *testingClient,
			scheduler *scheduler.Stepper,
		) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fail := func(_ context.Context, _ http.Header, _ io.Reader) (*http.Response, error) {
				return wrapStrToResp(http.StatusInternalServerError, "something is bad"), nil
			}
			waitChan := client.Answer(fail)

			errCh := runFleetGateway(ctx, gateway)

			// Initial tick is done out of bound so we can block on channels.
			scheduler.Next()

			// Fail to enter retry loop, all other calls will fails and will force to wait on big initial
			// delay.
			<-waitChan

			cancel()
			err := <-errCh
			require.NoError(t, err)
		}))

	t.Run("The error logs contain link to troubleshooting",
		withGatewayAndLog(
			agentInfo,
			newTestLogger(t),
			FleetGatewaySettings{
				Duration: 0 * time.Second,
				Backoff: backoffSettings{
					Init: 1 * time.Millisecond,
					Max:  1 * time.Millisecond,
				},
			},
			func(
				t *testing.T,
				fg *fleetGateway,
				tc *testingClient,
				s *scheduler.Stepper,
			) {
				ctx, cancel := context.WithCancel(context.Background())
				var errCh <-chan error
				func() {
					defer cancel()

					fail := func(_ context.Context, _ http.Header, _ io.Reader) (*http.Response, error) {
						return wrapStrToResp(http.StatusInternalServerError, "Fleet checkin went wrong"), nil
					}
					waitChan := tc.Answer(fail)

					errCh = runFleetGateway(ctx, fg)

					// Initial tick is done out of bound so we can block on channels.
					s.Next()

					// we wait until we fail at least 3 times to write an error log (the first 2 times it's just a warning)
					for i := 0; i < 3; i++ {
						<-waitChan
					}
				}()

				assert.Eventually(t, func() bool {
					for _, lm := range fg.log.(*testlogger).logs {
						if lm.lvl == ERROR && strings.Contains(lm.msg, help.GetTroubleshootMessage()) {
							return true
						}
					}
					return false
				}, 100*time.Millisecond, 10*time.Millisecond)
				err := <-errCh
				require.NoError(t, err)
			}))
}

type testAgentInfo struct{}

func (testAgentInfo) AgentID() string { return "agent-secret" }

func runFleetGateway(ctx context.Context, g *fleetGateway) <-chan error {
	done := make(chan bool)
	errCh := make(chan error, 1)
	go func() {
		err := g.Run(ctx)
		close(done)
		if err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-g.Errors():
				// ignore errors here
			}
		}
	}()
	return errCh
}

func newStateStore(t *testing.T, log *logger.Logger) *store.StateStore {
	dir, err := os.MkdirTemp("", "fleet-gateway-unit-test")
	require.NoError(t, err)

	filename := filepath.Join(dir, "state.enc")
	diskStore := storage.NewDiskStore(filename)
	stateStore, err := store.NewStateStore(log, diskStore)
	require.NoError(t, err)

	t.Cleanup(func() {
		os.RemoveAll(dir)
	})

	return stateStore
}

func TestAgentStateToString(t *testing.T) {
	testcases := []struct {
		agentState         agentclient.State
		expectedFleetState string
	}{
		{
			agentState:         agentclient.Healthy,
			expectedFleetState: fleetStateOnline,
		},
		{
			agentState:         agentclient.Failed,
			expectedFleetState: fleetStateError,
		},
		{
			agentState:         agentclient.Starting,
			expectedFleetState: fleetStateStarting,
		},
		// everything else maps to degraded
		{
			agentState:         agentclient.Configuring,
			expectedFleetState: fleetStateDegraded,
		},
		{
			agentState:         agentclient.Degraded,
			expectedFleetState: fleetStateDegraded,
		},
		{
			agentState:         agentclient.Stopping,
			expectedFleetState: fleetStateDegraded,
		},
		{
			agentState:         agentclient.Stopped,
			expectedFleetState: fleetStateDegraded,
		},
		{
			agentState:         agentclient.Upgrading,
			expectedFleetState: fleetStateDegraded,
		},
		{
			agentState:         agentclient.Rollback,
			expectedFleetState: fleetStateDegraded,
		},
	}

	for _, tc := range testcases {
		t.Run(fmt.Sprintf("%s -> %s", tc.agentState, tc.expectedFleetState), func(t *testing.T) {
			actualFleetState := agentStateToString(tc.agentState)
			assert.Equal(t, tc.expectedFleetState, actualFleetState)
		})
	}
}
