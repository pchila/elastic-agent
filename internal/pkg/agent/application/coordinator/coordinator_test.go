// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package coordinator

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/info"
	"github.com/elastic/elastic-agent/internal/pkg/agent/transpiler"
	"github.com/elastic/elastic-agent/internal/pkg/diagnostics"
	"github.com/elastic/elastic-agent/pkg/component"
	"github.com/elastic/elastic-agent/pkg/component/runtime"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

var expectedDiagnosticHooks map[string]string = map[string]string{
	"pre-config":      "pre-config.yaml",
	"variables":       "variables.yaml",
	"computed-config": "computed-config.yaml",
	"components":      "components.yaml",
	"state":           "state.yaml",
}

func TestCoordinatorDiagnosticHooks(t *testing.T) {

	log, err := logger.New("test-coordinator", false)
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockRuntimeMgr := NewMockRuntimeManager(mockCtrl)
	runtimeErrChan := make(chan error)
	mockRuntimeMgr.EXPECT().Errors().Return(runtimeErrChan).AnyTimes()
	mockRuntimeMgr.EXPECT().SubscribeAll(gomock.Any()).Return(new(runtime.SubscriptionAll)).AnyTimes()
	mockRuntimeMgr.EXPECT().Run(gomock.Any()).AnyTimes()
	mockRuntimeMgr.EXPECT().State().Return([]runtime.ComponentComponentState{}).Times(1)

	mockConfigMgr := NewMockConfigManager(mockCtrl)
	configErrChan := make(chan error)
	mockConfigMgr.EXPECT().Errors().Return(configErrChan).AnyTimes()
	actionErrorChan := make(chan error)
	mockConfigMgr.EXPECT().ActionErrors().Return(actionErrorChan).AnyTimes()
	configChangeChan := make(chan ConfigChange)
	mockConfigMgr.EXPECT().Watch().Return(configChangeChan).AnyTimes()
	mockConfigMgr.EXPECT().Run(gomock.Any()).AnyTimes()

	mockVarsMgr := NewMockVarsManager(mockCtrl)
	varsErrChan := make(chan error)
	mockVarsMgr.EXPECT().Errors().Return(varsErrChan).AnyTimes()
	varsChan := make(chan []*transpiler.Vars)
	mockVarsMgr.EXPECT().Watch().Return(varsChan).AnyTimes()
	mockVarsMgr.EXPECT().Run(gomock.Any()).AnyTimes()

	sut := New(
		log,
		logp.DebugLevel,
		&info.AgentInfo{},
		component.RuntimeSpecs{},
		NewMockReExecManager(mockCtrl),
		NewMockUpgradeManager(mockCtrl),
		mockRuntimeMgr,
		mockConfigMgr,
		mockVarsMgr,
		NewMockCapability(mockCtrl),
		NewMockMonitorManager(mockCtrl),
		false,
	)

	ctx, cancelFunc := context.WithCancel(context.Background())

	defer cancelFunc()

	go sut.Run(ctx)

	diagHooks := sut.DiagnosticHooks()
	t.Logf("Received diagnostics: %+v", diagHooks)
	assert.NotEmpty(t, diagHooks)

	hooksMap := map[string]diagnostics.Hook{}
	for i, h := range diagHooks {
		hooksMap[h.Name] = diagHooks[i]
	}

	for hookName, diagFileName := range expectedDiagnosticHooks {
		contained := assert.Contains(t, hooksMap, hookName)
		if contained {
			hook := hooksMap[hookName]
			assert.Equal(t, diagFileName, hook.Filename)
			hookResult := hook.Hook(ctx)
			t.Logf("Result of hook %v:\n%s\n", hook.Name, hookResult)
		}
	}
}
