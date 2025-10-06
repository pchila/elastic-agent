// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package install

import v1 "github.com/elastic/elastic-agent/pkg/api/v1"

type Registry interface {
	GetInstallDesc() (*v1.InstallDescriptor, error)
	AddInstallDesc(desc v1.AgentInstallDesc) (*v1.InstallDescriptor, error)
	ModifyInstallDesc(modifierFunc func(desc *v1.AgentInstallDesc) error) (*v1.InstallDescriptor, error)
	RemoveAgentInstallDesc(versionedHomes ...string) (*v1.InstallDescriptor, error)
}
