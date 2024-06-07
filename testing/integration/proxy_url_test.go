// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/elastic/elastic-agent-libs/testing/certutil"
	integrationtest "github.com/elastic/elastic-agent/pkg/testing"
	"github.com/elastic/elastic-agent/pkg/testing/define"
	"github.com/elastic/elastic-agent/pkg/testing/tools/check"
	"github.com/elastic/elastic-agent/pkg/testing/tools/testcontext"
	"github.com/elastic/elastic-agent/testing/fleetservertest"
	"github.com/elastic/elastic-agent/testing/proxytest"
)

func TestProxyURL(t *testing.T) {
	_ = define.Require(t, define.Requirements{
		Group: Fleet,
		Local: false,
		Sudo:  true,
	})

	t.Skip("Currently flaky, see https://github.com/elastic/elastic-agent/issues/4861")

	// Setup proxies and fake fleet server host we are gonna rewrite
	unreachableFleetHost := "fleet.elastic.co"
	unreachableFleetHttpURL := "http://" + unreachableFleetHost

	// mockFleetComponents is a struct that holds all the mock fleet stuff in a nice single unit (easier to pass around).
	// all the fields are initialized in the main test loop
	type mockFleetComponents struct {
		fleetServer      *fleetservertest.Server
		policyData       *fleetservertest.TmplPolicy
		checkinWithAcker *fleetservertest.CheckinActionsWithAcker
	}

	// setupFunc is a hook used by testcases to set up proxies and add data/behaviors to fleet policy and checkinAcker
	// the test will use one of the returned proxies if a proxy key is set in args.enrollProxyName
	type setupFunc func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy)

	// enrollmentURLFunc is a getter for the enrollmentURL so that each testcase can control what is used at install time
	type enrollmentURLFunc func(fleet *fleetservertest.Server) string

	// assertFunc is the hook the main test loop calls for performing assertions after the agent has been installed and is healthy
	type assertFunc func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents)

	type args struct {
		enrollProxyName string
	}

	type testcase struct {
		name          string
		args          args
		setup         setupFunc
		enrollmentURL enrollmentURLFunc
		wantErr       assert.ErrorAssertionFunc
		assertFunc    assertFunc
	}

	testcases := []testcase{
		{
			name: "EnrollWithProxy-NoProxyInPolicy",
			args: args{enrollProxyName: "proxy"},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {

				// Create and start fake proxy
				proxy := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy", t.Logf),
					proxytest.WithVerboseLog())
				err := proxy.Start()
				require.NoError(t, err, "error starting proxy")
				t.Cleanup(proxy.Close)

				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestNoProxyInThePolicyActionID", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				// Create checkin action with respective ack token
				ackToken := "ackToken-AckTokenTestNoProxyInThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)

				return map[string]*proxytest.Proxy{"proxy": proxy}
			},
			enrollmentURL: func(_ *fleetservertest.Server) string {
				// returning a non-existing URL we make sure Fleet is only reachable through proxy
				return unreachableFleetHttpURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, _ map[string]*proxytest.Proxy, _ *mockFleetComponents) {
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)
			},
		},
		{
			name: "EnrollWithProxy-EmptyProxyInPolicy",
			args: args{enrollProxyName: "proxy"},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {

				// set FleetProxyURL to empty string in the policy
				mockFleet.policyData.FleetProxyURL = new(string)
				// FIXME: this reassignment is pointless ?
				*mockFleet.policyData.FleetProxyURL = ""

				// Create and start fake proxy
				proxy := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy", t.Logf),
					proxytest.WithVerboseLog())
				err := proxy.Start()
				require.NoError(t, err, "error starting proxy")
				t.Cleanup(proxy.Close)

				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				ackToken := "ackToken-AckTokenTestNoProxyInThePolicy"
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestNoProxyInThePolicyActionID", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)

				return map[string]*proxytest.Proxy{"proxy": proxy}
			},
			enrollmentURL: func(_ *fleetservertest.Server) string {
				// returning a non-existing URL we make sure Fleet is only reachable through proxy
				return unreachableFleetHttpURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents) {
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)
			},
		},
		{
			name: "EnrollWithProxy-PolicyProxyTakesPrecedence",
			args: args{enrollProxyName: "enroll"},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {

				// We need 2 proxies: one for the initial enroll and another to specify in the policy
				proxyEnroll := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy-enroll", t.Logf),
					proxytest.WithVerboseLog())
				proxyEnroll.Start()
				t.Cleanup(proxyEnroll.Close)
				proxyFleetPolicy := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy-fleet-policy", t.Logf),
					proxytest.WithVerboseLog())
				proxyFleetPolicy.Start()
				t.Cleanup(proxyFleetPolicy.Close)

				// set the proxy URL in policy to proxyFleetPolicy
				mockFleet.policyData.FleetProxyURL = new(string)
				*mockFleet.policyData.FleetProxyURL = proxyFleetPolicy.LocalhostURL

				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestValidProxyInThePolicy", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				ackToken := "AckToken-TestValidProxyInThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)

				return map[string]*proxytest.Proxy{"enroll": proxyEnroll, "policy": proxyFleetPolicy}
			},
			enrollmentURL: func(_ *fleetservertest.Server) string {
				// returning a non-existing URL we make sure Fleet is only reachable through proxy
				return unreachableFleetHttpURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents) {
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)

				// ensure the agent is communicating through the proxy set in the policy
				want := fleetservertest.NewPathCheckin(mockFleet.policyData.AgentID)
				assert.Eventually(t, func() bool {
					for _, r := range proxies["policy"].ProxiedRequests() {
						if strings.Contains(r, want) {
							return true
						}
					}

					return false
				}, 5*time.Minute, 5*time.Second,
					"did not find requests to the proxy defined in the policy. Want [%s] on %v",
					proxies["policy"].LocalhostURL, proxies["policy"].ProxiedRequests())
			},
		},
		{
			name: "NoEnrollProxy-ProxyInThePolicy",
			args: args{
				// no proxy at enroll
				enrollProxyName: "",
			},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {
				// Create a fake proxy to be used in fleet policy
				proxyFleetPolicy := proxytest.New(t,
					proxytest.WithRequestLog("proxy-fleet-policy", t.Logf),
					proxytest.WithVerboseLog())
				proxyFleetPolicy.Start()
				t.Cleanup(proxyFleetPolicy.Close)

				mockFleet.policyData.FleetProxyURL = new(string)
				*mockFleet.policyData.FleetProxyURL = proxyFleetPolicy.LocalhostURL
				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestValidProxyInThePolicy", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				ackToken := "AckToken-TestValidProxyInThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)
				return map[string]*proxytest.Proxy{"proxyFleetPolicy": proxyFleetPolicy}
			},
			enrollmentURL: func(fleet *fleetservertest.Server) string {
				return fleet.LocalhostURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents) {
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)

				// ensure the agent is communicating through the new proxy
				if !assert.Eventually(t, func() bool {
					proxy := proxies["proxyFleetPolicy"]
					for _, r := range proxy.ProxiedRequests() {
						if strings.Contains(
							r,
							fleetservertest.NewPathCheckin(mockFleet.policyData.AgentID)) {
							return true
						}
					}

					return false
				}, 5*time.Minute, 5*time.Second) {
					t.Errorf("did not find requests to the proxy defined in the policy")
				}
			},
		},
		{
			name: "NoEnrollProxy-RemoveProxyFromThePolicy",
			args: args{
				// no proxy at enroll
				enrollProxyName: "",
			},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {
				// Create a fake proxy to use in initial fleet policy
				proxyFleetPolicy := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy-fleet-policy", t.Logf),
					proxytest.WithVerboseLog())
				proxyFleetPolicy.Start()
				t.Cleanup(proxyFleetPolicy.Close)

				mockFleet.policyData.FleetProxyURL = new(string)
				*mockFleet.policyData.FleetProxyURL = proxyFleetPolicy.LocalhostURL

				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestRemoveProxyFromThePolicy", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				ackToken := "AckToken-TestRemoveProxyFromThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)

				return map[string]*proxytest.Proxy{"fleetProxy": proxyFleetPolicy}
			},
			enrollmentURL: func(fleet *fleetservertest.Server) string {
				return fleet.LocalhostURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents) {
				// assert the agent is actually connected to fleet.
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)

				// ensure the agent is communicating through the proxy set in the policy
				if !assert.Eventually(t, func() bool {
					for _, r := range proxies["fleetProxy"].ProxiedRequests() {
						if strings.Contains(
							r,
							fleetservertest.NewPathCheckin(mockFleet.policyData.AgentID)) {
							return true
						}
					}

					return false
				}, 5*time.Minute, 5*time.Second) {
					t.Errorf("did not find requests to the proxy defined in the policy")
				}

				// Assert the proxy is set on the agent
				inspect, err := fixture.ExecInspect(ctx)
				require.NoError(t, err)
				assert.Equal(t, *mockFleet.policyData.FleetProxyURL, inspect.Fleet.ProxyURL)

				// remove proxy from the policy
				want := *mockFleet.policyData.FleetProxyURL
				mockFleet.policyData.FleetProxyURL = nil
				actionIDRemoveProxyFromPolicy := "actionIDRemoveProxyFromPolicy-actionID-TestRemoveProxyFromThePolicy"
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					actionIDRemoveProxyFromPolicy, *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				ackToken := "AckToken-TestRemovedProxyFromThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)

				// ensures the agent acked the action sending a policy without proxy
				require.Eventually(t, func() bool {
					return mockFleet.checkinWithAcker.Acked(actionIDRemoveProxyFromPolicy)
				},
					30*time.Second, 5*time.Second)
				inspect, err = fixture.ExecInspect(ctx)
				require.NoError(t, err)
				assert.Equal(t, inspect.Fleet.ProxyURL, want)

				// assert, again, the agent is actually connected to fleet.
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)
			},
		},
		{
			name: "NoEnrollProxy-TLSProxyWithCAInThePolicy",
			args: args{
				// no proxy at enroll
				enrollProxyName: "",
			},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {

				// use os.MkdirTemp since we are installing agent unprivileged and t.TempDir() does not guarantee that the elastic-agent user has access
				baseDir := ""
				if runtime.GOOS == "windows" {
					baseDir = "C:\\"
				}
				tmpDir, err := os.MkdirTemp(baseDir, "TLSProxyWithCAInThePolicy*")
				require.NoError(t, err, "error creating temp dir for TLS files")
				t.Cleanup(func() {
					cleanupErr := os.RemoveAll(tmpDir)
					assert.NoErrorf(t, cleanupErr, "error cleaning up directory %q", tmpDir)
				})

				// fix permissions on temp dir
				err = os.Chmod(tmpDir, 0o755&os.ModePerm)
				require.NoError(t, err, "error setting temporary dir %q as world-readable", tmpDir)

				caKey, caCert, pair, err := certutil.NewRootCA()
				require.NoError(t, err, "failed creating CA root")

				caCertFile := filepath.Join(tmpDir, "ca.cert")
				err = os.WriteFile(caCertFile, pair.Cert, 0o644&os.ModePerm)
				require.NoError(t, err, "failed writing CA cert file %q", caCertFile)

				caCertPool := x509.NewCertPool()
				caCertPool.AddCert(caCert)

				proxyCert, _, err := certutil.GenerateChildCert("localhost", []net.IP{net.IPv6loopback, net.IPv6zero, net.ParseIP("127.0.0.1")}, caKey, caCert)

				// Create a fake proxy with TLS config to be used in fleet policy
				proxyFleetPolicy := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy-fleet-policy", t.Logf),
					proxytest.WithVerboseLog(),
					proxytest.WithServerTLSConfig(&tls.Config{
						RootCAs: caCertPool,
						Certificates: []tls.Certificate{
							{
								Certificate: proxyCert.Certificate,
								PrivateKey:  proxyCert.PrivateKey,
								Leaf:        proxyCert.Leaf,
							},
						},
					}))
				err = proxyFleetPolicy.StartTLS()
				require.NoError(t, err, "error starting TLS-enabled proxy")
				t.Cleanup(proxyFleetPolicy.Close)

				mockFleet.policyData.FleetProxyURL = &proxyFleetPolicy.LocalhostURL
				mockFleet.policyData.SSL = &fleetservertest.SSL{
					CertificateAuthorities: []string{caCertFile},
					Renegotiation:          "never",
				}
				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestValidProxyInThePolicy", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				ackToken := "AckToken-TestValidProxyInThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)
				return map[string]*proxytest.Proxy{"proxyFleetPolicy": proxyFleetPolicy}
			},
			enrollmentURL: func(fleet *fleetservertest.Server) string {
				return fleet.LocalhostURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents) {
				// assert the agent is actually connected to fleet.
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)

				// ensure the agent is communicating through the proxy set in the policy
				if !assert.Eventually(t, func() bool {
					for _, r := range proxies["proxyFleetPolicy"].ProxiedRequests() {
						if strings.Contains(
							r,
							fleetservertest.NewPathCheckin(mockFleet.policyData.AgentID)) {
							return true
						}
					}

					return false
				}, 5*time.Minute, 5*time.Second) {
					t.Errorf("did not find requests to the proxy defined in the policy")
				}

				inspectOutput, err := fixture.Exec(ctx, []string{"inspect"})
				assert.NoError(t, err, "error running elastic-agent inspect")
				t.Logf("elastic-agent inspect output:\n%s\n", string(inspectOutput))
			},
		},
		{
			name: "NoEnrollProxy-mTLSProxyInThePolicy",
			args: args{
				// no proxy at enroll
				enrollProxyName: "",
			},
			setup: func(t *testing.T, mockFleet *mockFleetComponents) (proxies map[string]*proxytest.Proxy) {

				// use os.MkdirTemp since we are installing agent unprivileged and t.TempDir() does not guarantee that the elastic-agent user has access
				baseDir := ""
				if runtime.GOOS == "windows" {
					baseDir = "C:\\"
				}
				tmpDir, err := os.MkdirTemp(baseDir, "mTLSProxyInThePolicy*")
				require.NoError(t, err, "error creating temp dir for TLS files")

				t.Cleanup(func() {
					cleanupErr := os.RemoveAll(tmpDir)
					assert.NoErrorf(t, cleanupErr, "error cleaning up directory %q", tmpDir)
				})

				// fix permissions on temp dir
				err = os.Chmod(tmpDir, 0o755&os.ModePerm)
				require.NoError(t, err, "error setting temporary dir %q as world-readable", tmpDir)

				serverCAKey, serverCACert, serverPair, err := certutil.NewRootCA()
				require.NoError(t, err, "failed creating root CA")

				serverCACertFile := filepath.Join(tmpDir, "server_ca.cert")
				err = os.WriteFile(serverCACertFile, serverPair.Cert, 0o644&os.ModePerm)
				require.NoError(t, err, "failed writing Server CA cert file %q", serverCACertFile)

				clientCAKey, clientCACert, clientPair, err := certutil.NewRootCA()
				require.NoError(t, err, "failed creating root CA")

				clientCACertFile := filepath.Join(tmpDir, "client_ca.cert")
				err = os.WriteFile(clientCACertFile, clientPair.Cert, 0o644&os.ModePerm)
				require.NoError(t, err, "failed writing Client CA cert file %q", clientCACertFile)

				// server CA certpool
				serverCACertPool := x509.NewCertPool()
				serverCACertPool.AddCert(serverCACert)

				// the server must trust the client CA
				clientCACertPool := x509.NewCertPool()
				clientCACertPool.AddCert(clientCACert)

				proxyCert, _, err := certutil.GenerateChildCert("localhost", []net.IP{net.IPv6loopback, net.IPv6zero, net.ParseIP("127.0.0.1")}, serverCAKey, serverCACert)

				// Create a fake proxy with mTLS config to be used in fleet policy
				proxyFleetPolicy := proxytest.New(t,
					proxytest.WithRewrite(unreachableFleetHost, "localhost:"+mockFleet.fleetServer.Port),
					proxytest.WithRequestLog("proxy-fleet-policy", t.Logf),
					proxytest.WithVerboseLog(),
					proxytest.WithServerTLSConfig(&tls.Config{
						Certificates: []tls.Certificate{
							{
								Certificate: proxyCert.Certificate,
								PrivateKey:  proxyCert.PrivateKey,
								Leaf:        proxyCert.Leaf,
							},
						},
						// require client auth with a trusted Cert
						ClientAuth: tls.RequireAndVerifyClientCert,
						ClientCAs:  clientCACertPool,
						RootCAs:    serverCACertPool,
					}))
				err = proxyFleetPolicy.StartTLS()
				require.NoError(t, err, "error starting TLS-enabled proxy")
				t.Cleanup(proxyFleetPolicy.Close)

				// generate a certificate for elastic-agent from the client CA
				_, agentPair, err := certutil.GenerateChildCert("localhost", []net.IP{net.IPv6loopback, net.IPv6zero, net.ParseIP("127.0.0.1")}, clientCAKey, clientCACert)

				//// Write out certificate file
				//agentCertFile := filepath.Join(tmpDir, "elastic-agent.cert")
				//err = os.WriteFile(agentCertFile, agentPair.Cert, 0o644&os.ModePerm)
				//require.NoError(t, err, "failed writing elastic-agent cert file %q", agentCertFile)
				//
				//// Write out key file
				//agentKeyFile := filepath.Join(tmpDir, "elastic-agent.key")
				//err = os.WriteFile(agentKeyFile, agentPair.Key, 0o644&os.ModePerm)
				//require.NoError(t, err, "failed writing elastic-agent key file %q", agentCertFile)

				mockFleet.policyData.FleetProxyURL = &proxyFleetPolicy.LocalhostURL
				mockFleet.policyData.SSL = &fleetservertest.SSL{
					CertificateAuthorities: []string{string(serverPair.Cert)},
					Renegotiation:          "never",
					Certificate:            string(agentPair.Cert),
					Key:                    string(agentPair.Key),
				}
				// now that we have fleet and the proxy running, we can add actions which
				// depend on them.
				action, err := fleetservertest.NewActionWithEmptyPolicyChange(
					"actionID-TestValidProxyInThePolicy", *mockFleet.policyData)
				require.NoError(t, err, "could not generate action with policy")

				ackToken := "AckToken-TestValidProxyInThePolicy"
				mockFleet.checkinWithAcker.AddCheckin(
					ackToken,
					0,
					action,
				)
				return map[string]*proxytest.Proxy{"proxyFleetPolicy": proxyFleetPolicy}
			},
			enrollmentURL: func(fleet *fleetservertest.Server) string {
				return fleet.LocalhostURL
			},
			wantErr: assert.NoError,
			assertFunc: func(ctx context.Context, t *testing.T, fixture *integrationtest.Fixture, proxies map[string]*proxytest.Proxy, mockFleet *mockFleetComponents) {
				// assert the agent is actually connected to fleet.
				check.ConnectedToFleet(ctx, t, fixture, 5*time.Minute)

				// ensure the agent is communicating through the proxy set in the policy
				if !assert.Eventually(t, func() bool {
					for _, r := range proxies["proxyFleetPolicy"].ProxiedRequests() {
						if strings.Contains(
							r,
							fleetservertest.NewPathCheckin(mockFleet.policyData.AgentID)) {
							return true
						}
					}

					return false
				}, 5*time.Minute, 5*time.Second) {
					t.Errorf("did not find requests to the proxy defined in the policy")
				}

				inspectOutput, err := fixture.ExecInspect(ctx)
				if assert.NoError(t, err, "error running elastic-agent inspect") {
					inspectYaml, _ := yaml.Marshal(inspectOutput)
					t.Logf("elastic-agent inspect output:\n%s\n", string(inspectYaml))
				}

			},
		},
	}

	for _, tt := range testcases {

		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := testcontext.WithDeadline(t, context.Background(), time.Now().Add(10*time.Minute))
			defer cancel()

			// create API Key and basic Fleet Policy
			apiKey, policyData := createBasicFleetPolicyData(t, unreachableFleetHost)

			// Create a checkin and ack handler
			checkinWithAcker := fleetservertest.NewCheckinActionsWithAcker()

			// Start fake fleet server
			enrollmentToken := "enrollmentToken"
			fleet := fleetservertest.NewServerWithHandlers(
				apiKey,
				enrollmentToken,
				policyData.AgentID,
				policyData.PolicyID,
				checkinWithAcker.ActionsGenerator(),
				checkinWithAcker.Acker(),
				fleetservertest.WithRequestLog(t.Logf),
			)
			t.Cleanup(fleet.Close)

			mockFleet := &mockFleetComponents{
				fleetServer:      fleet,
				policyData:       &policyData,
				checkinWithAcker: &checkinWithAcker,
			}

			// Specific testcase setup and map of created proxies
			proxies := tt.setup(t, mockFleet)

			fixture, err := define.NewFixtureFromLocalBuild(t,
				define.Version(),
				integrationtest.WithAllowErrors(),
				integrationtest.WithLogOutput())
			require.NoError(t, err, "SetupTest: NewFixtureFromLocalBuild failed")

			err = fixture.EnsurePrepared(ctx)
			require.NoError(t, err, "SetupTest: fixture.Prepare failed")

			installProxyURL := ""
			if tt.args.enrollProxyName != "" {
				require.Containsf(t, proxies, tt.args.enrollProxyName, "Proxy %q to be used for enrolling is missing from the map of proxies", tt.args.enrollProxyName)
				installProxyURL = proxies[tt.args.enrollProxyName].LocalhostURL
			}

			require.NotNil(t, tt.enrollmentURL, "testcase must define an enrollmentURL getter")
			enrollmentURL := tt.enrollmentURL(fleet)

			out, err := fixture.Install(
				ctx,
				&integrationtest.InstallOpts{
					Force:          true,
					NonInteractive: true,
					Insecure:       true,
					ProxyURL:       installProxyURL,
					EnrollOpts: integrationtest.EnrollOpts{
						URL:             enrollmentURL,
						EnrollmentToken: "anythingWillDO",
					}})
			t.Logf("elastic-agent install output: \n%s\n", string(out))
			if tt.wantErr(t, err, "elastic-agent install returned an unexpected error") {
				tt.assertFunc(ctx, t, fixture, proxies, mockFleet)
			}
		})
	}

}

func createBasicFleetPolicyData(t *testing.T, fleetHost string) (fleetservertest.APIKey, fleetservertest.TmplPolicy) {
	apiKey := fleetservertest.APIKey{
		ID:  "apiKeyID",
		Key: "apiKeyKey",
	}

	agentID := strings.Replace(t.Name(), "/", "-", -1) + "-agent-id"
	policyUUID, err := uuid.NewUUID()
	require.NoError(t, err, "error generating UUID for policy")

	policyID := policyUUID.String()
	policyData := fleetservertest.TmplPolicy{
		AgentID:    agentID,
		PolicyID:   t.Name() + policyID,
		FleetHosts: []string{fleetHost},
		SourceURI:  "http://source.uri",
		CreatedAt:  time.Now().Format(time.RFC3339),
		Output: struct {
			APIKey string
			Hosts  string
			Type   string
		}{
			APIKey: apiKey.String(),
			Hosts:  `"https://my.clould.elstc.co:443"`,
			Type:   "elasticsearch"},
	}
	return apiKey, policyData
}
