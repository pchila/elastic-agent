// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/elastic/elastic-agent-libs/transport/httpcommon"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/actions"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/coordinator"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/info"
	"github.com/elastic/elastic-agent/internal/pkg/agent/configuration"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/agent/storage"
	"github.com/elastic/elastic-agent/internal/pkg/config"
	"github.com/elastic/elastic-agent/internal/pkg/fleetapi"
	"github.com/elastic/elastic-agent/internal/pkg/fleetapi/acker"
	"github.com/elastic/elastic-agent/internal/pkg/fleetapi/client"
	"github.com/elastic/elastic-agent/internal/pkg/remote"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

const (
	apiStatusTimeout = 15 * time.Second
)

// PolicyChangeHandler is a handler for POLICY_CHANGE action.
type PolicyChangeHandler struct {
	log       *logger.Logger
	agentInfo info.Agent
	config    *configuration.Configuration
	store     storage.Store
	ch        chan coordinator.ConfigChange
	setters   []actions.ClientSetter

	// Disabled for 8.8.0 release in order to limit the surface
	// https://github.com/elastic/security-team/issues/6501
	// // Last known valid signature validation key
	// signatureValidationKey []byte
}

// NewPolicyChangeHandler creates a new PolicyChange handler.
func NewPolicyChangeHandler(
	log *logger.Logger,
	agentInfo info.Agent,
	config *configuration.Configuration,
	store storage.Store,
	ch chan coordinator.ConfigChange,
	setters ...actions.ClientSetter,
) *PolicyChangeHandler {
	return &PolicyChangeHandler{
		log:       log,
		agentInfo: agentInfo,
		config:    config,
		store:     store,
		ch:        ch,
		setters:   setters,
	}
}

// AddSetter adds a setter into a collection of client setters.
func (h *PolicyChangeHandler) AddSetter(cs actions.ClientSetter) {
	if h.setters == nil {
		h.setters = make([]actions.ClientSetter, 0)
	}

	h.setters = append(h.setters, cs)
}

// Handle handles policy change action.
func (h *PolicyChangeHandler) Handle(ctx context.Context, a fleetapi.Action, acker acker.Acker) error {
	h.log.Debugf("handlerPolicyChange: action '%+v' received", a)
	action, ok := a.(*fleetapi.ActionPolicyChange)
	if !ok {
		return fmt.Errorf("invalid type, expected ActionPolicyChange and received %T", a)
	}

	// Disabled for 8.8.0 release in order to limit the surface
	// https://github.com/elastic/security-team/issues/6501

	// // Validate policy signature and overlay signed configuration
	// policy, signatureValidationKey, err := protection.ValidatePolicySignature(h.log, action.Policy, h.signatureValidationKey)
	// if err != nil {
	// 	return errors.New(err, "could not validate the policy signed configuration", errors.TypeConfig)
	// }
	// h.log.Debugf("handlerPolicyChange: policy validation result: signature validation key length: %v, err: %v", len(signatureValidationKey), err)

	// // Cache signature validation key for the next policy handling
	// h.signatureValidationKey = signatureValidationKey

	c, err := config.NewConfigFrom(action.Policy)
	if err != nil {
		return errors.New(err, "could not parse the configuration from the policy", errors.TypeConfig)
	}

	h.log.Debugf("handlerPolicyChange: emit configuration for action %+v", a)
	err = h.handleFleetServerHosts(ctx, c)
	if err != nil {
		return err
	}

	// persist, apply
	h.ch <- newPolicyChange(ctx, c, a, acker, false)
	return nil
}

// Watch returns the channel for configuration change notifications.
func (h *PolicyChangeHandler) Watch() <-chan coordinator.ConfigChange {
	return h.ch
}

func (h *PolicyChangeHandler) validateFleetServerHosts(ctx context.Context, c *config.Config) (*remote.Config, error) {
	data, err := c.ToMapStr()
	if err != nil {
		return nil, errors.New(err, "could not convert the configuration from the policy", errors.TypeConfig)
	}
	if _, ok := data["fleet"]; !ok {
		// no fleet information in the configuration (skip checking client)
		return nil, nil
	}

	cfg, err := configuration.NewFromConfig(c)
	if err != nil {
		return nil, errors.New(err, "could not parse the configuration from the policy", errors.TypeConfig)
	}

	if clientEqual(h.config.Fleet.Client, cfg.Fleet.Client) {
		// already the same hosts
		return nil, nil
	}

	// only set protocol/hosts as that is all Fleet currently sends
	// copy the client config and apply the changes on this copy
	newFleetClientConfig := h.config.Fleet.Client
	updateFleetConfig(h.log, cfg.Fleet.Client, &newFleetClientConfig)

	// Test new config
	err = testFleetConfig(ctx, h.log, newFleetClientConfig, h.config.Fleet.AccessAPIKey)
	if err != nil {
		return nil, fmt.Errorf("validating fleet client config: %w", err)
	}

	return &newFleetClientConfig, nil
}

func testFleetConfig(ctx context.Context, log *logger.Logger, clientConfig remote.Config, apiKey string) error {
	fleetClient, err := client.NewAuthWithConfig(
		log, apiKey, clientConfig)
	if err != nil {
		return errors.New(
			err, "fail to create API client with updated config",
			errors.TypeConfig,
			errors.M("hosts", append(
				clientConfig.Hosts, clientConfig.Host)))
	}

	ctx, cancel := context.WithTimeout(ctx, apiStatusTimeout)
	defer cancel()

	// FIXME: a HEAD should be enough as we need to test only the connectivity part
	resp, err := fleetClient.Send(ctx, http.MethodGet, "/api/status", nil, nil, nil)
	if err != nil {
		return errors.New(
			err, "fail to communicate with Fleet Server API client hosts",
			errors.TypeNetwork, errors.M("hosts", clientConfig.Hosts))
	}

	// discard body for proper cancellation and connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	return nil
}

func updateFleetConfig(log *logger.Logger, src remote.Config, dst *remote.Config) {
	dst.Protocol = src.Protocol
	dst.Path = src.Path
	dst.Host = src.Host
	dst.Hosts = src.Hosts

	// Empty proxies from fleet are ignored. That way a proxy set by --proxy-url
	// it won't be overridden by an absent or empty proxy from fleet-server.
	// However, if there is a proxy sent by fleet-server, it'll take precedence.
	// Therefore, it's not possible to remove a proxy once it's set.

	// FIXME transport settings are passed by reference so is the proxy URL, properly copy or restore it if validation fails

	if src.Transport.Proxy.URL == nil ||
		src.Transport.Proxy.URL.String() == "" {
		log.Debug("proxy from fleet is empty or null, the proxy will not be changed")
	} else {
		dst.Transport.Proxy = src.Transport.Proxy
		log.Debug("received proxy from fleet, applying it")
	}
}

func (h *PolicyChangeHandler) handleFleetServerHosts(ctx context.Context, c *config.Config) (err error) {
	// do not update fleet-server host from policy; no setters provided with local Fleet Server
	if len(h.setters) == 0 {
		return nil
	}
	var validatedConfig *remote.Config
	validatedConfig, err = h.validateFleetServerHosts(ctx, c)
	if err != nil {
		return fmt.Errorf("error validating Fleet client config: %w", err)
	}

	err = saveFleetClientConfig(validatedConfig)
	if err != nil {
		return fmt.Errorf("saving FleetClientConfig: %w", err)
	}

	err = h.applyFleetClientConfig(validatedConfig)
	if err != nil {
		return fmt.Errorf("applying FleetClientConfig: %w", err)
	}

	previousConfig := h.config.Fleet.Client

	h.config.Fleet.Client = *validatedConfig
	// rollback on failure
	defer func() {
		if err != nil {
			h.config.Fleet.Client = previousConfig
		}
	}()

	fleetClient, err := client.NewAuthWithConfig(
		h.log, h.config.Fleet.AccessAPIKey, *validatedConfig)
	for _, setter := range h.setters {
		setter.SetClient(fleetClient)
	}
	return nil
}

func (h *PolicyChangeHandler) applyFleetClientConfig(validatedConfig *remote.Config) error {
	if validatedConfig == nil {
		// nothing to do for fleet hosts
		return nil
	}

}

func saveFleetClientConfig(validatedConfig *remote.Config) error {
	if validatedConfig == nil {
		// nothing to do for fleet hosts
		return nil
	}
	reader, err := fleetToReader(h.agentInfo, h.config)
	if err != nil {
		return errors.New(
			err, "fail to persist new Fleet Server API client hosts",
			errors.TypeUnexpected, errors.M("hosts", h.config.Fleet.Client.Hosts))
	}

	err = h.store.Save(reader)
	if err != nil {
		return errors.New(
			err, "fail to persist new Fleet Server API client hosts",
			errors.TypeFilesystem, errors.M("hosts", h.config.Fleet.Client.Hosts))
	}
}

func clientEqual(k1 remote.Config, k2 remote.Config) bool {
	if k1.Protocol != k2.Protocol {
		return false
	}
	if k1.Path != k2.Path {
		return false
	}

	sort.Strings(k1.Hosts)
	sort.Strings(k2.Hosts)
	if len(k1.Hosts) != len(k2.Hosts) {
		return false
	}
	for i, v := range k1.Hosts {
		if v != k2.Hosts[i] {
			return false
		}
	}

	headersEqual := func(h1, h2 httpcommon.ProxyHeaders) bool {
		if len(h1) != len(h2) {
			return false
		}

		for k, v := range h1 {
			h2v, found := h2[k]
			if !found || v != h2v {
				return false
			}
		}

		return true
	}

	// different proxy
	if k1.Transport.Proxy.URL != k2.Transport.Proxy.URL ||
		k1.Transport.Proxy.Disable != k2.Transport.Proxy.Disable ||
		!headersEqual(k1.Transport.Proxy.Headers, k2.Transport.Proxy.Headers) {
		return false
	}

	return true
}

func fleetToReader(agentInfo info.Agent, cfg *configuration.Configuration) (io.Reader, error) {
	configToStore := map[string]interface{}{
		"fleet": cfg.Fleet,
		"agent": map[string]interface{}{
			"id":               agentInfo.AgentID(),
			"headers":          agentInfo.Headers(),
			"logging.level":    cfg.Settings.LoggingConfig.Level,
			"monitoring.http":  cfg.Settings.MonitoringConfig.HTTP,
			"monitoring.pprof": cfg.Settings.MonitoringConfig.Pprof,
		},
	}

	data, err := yaml.Marshal(configToStore)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

type policyChange struct {
	ctx        context.Context
	cfg        *config.Config
	action     fleetapi.Action
	acker      acker.Acker
	commit     bool
	ackWatcher chan struct{}
}

func newPolicyChange(
	ctx context.Context,
	config *config.Config,
	action fleetapi.Action,
	acker acker.Acker,
	commit bool) *policyChange {
	var ackWatcher chan struct{}
	if commit {
		// we don't need it otherwise
		ackWatcher = make(chan struct{})
	}
	return &policyChange{
		ctx:        ctx,
		cfg:        config,
		action:     action,
		acker:      acker,
		commit:     true,
		ackWatcher: ackWatcher,
	}
}

func (l *policyChange) Config() *config.Config {
	return l.cfg
}

func (l *policyChange) Ack() error {
	if l.action == nil {
		return nil
	}
	err := l.acker.Ack(l.ctx, l.action)
	if err != nil {
		return err
	}
	if l.commit {
		err := l.acker.Commit(l.ctx)
		if l.ackWatcher != nil && err == nil {
			close(l.ackWatcher)
		}
		return err
	}
	return nil
}

// WaitAck waits for policy change to be acked.
// Policy change ack is awaitable only in case commit flag was set.
// Caller is responsible to use any reasonable deadline otherwise
// function call can be endlessly blocking.
func (l *policyChange) WaitAck(ctx context.Context) {
	if !l.commit || l.ackWatcher == nil {
		return
	}

	select {
	case <-l.ackWatcher:
	case <-ctx.Done():
	}
}

func (l *policyChange) Fail(_ error) {
	// do nothing
}
