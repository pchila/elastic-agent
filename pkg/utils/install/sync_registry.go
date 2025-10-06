// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package install

import (
	"sync"

	v1 "github.com/elastic/elastic-agent/pkg/api/v1"
)

type SyncInstallRegistry struct {
	mx       *sync.RWMutex
	registry Registry
}

func NewSyncInstallRegistry(inner Registry) *SyncInstallRegistry {
	return &SyncInstallRegistry{
		mx:       new(sync.RWMutex),
		registry: inner,
	}
}

func (s *SyncInstallRegistry) GetInstallDesc() (*v1.InstallDescriptor, error) {
	s.mx.RLock()
	defer s.mx.RUnlock()
	return s.registry.GetInstallDesc()
}

func (s *SyncInstallRegistry) AddInstallDesc(desc v1.AgentInstallDesc) (*v1.InstallDescriptor, error) {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.registry.AddInstallDesc(desc)
}

func (s *SyncInstallRegistry) ModifyInstallDesc(modifierFunc func(desc *v1.AgentInstallDesc) error) (*v1.InstallDescriptor, error) {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.registry.ModifyInstallDesc(modifierFunc)
}

func (s *SyncInstallRegistry) RemoveAgentInstallDesc(versionedHomes ...string) (*v1.InstallDescriptor, error) {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.registry.RemoveAgentInstallDesc(versionedHomes...)
}

// Ensure that SyncInstallRegistry implements the Registry interface
var _ Registry = &SyncInstallRegistry{}
