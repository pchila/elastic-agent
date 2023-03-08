// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package coordinator

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/info"
	"github.com/elastic/elastic-agent/internal/pkg/agent/control/v2/cproto"
	"github.com/elastic/elastic-agent/internal/pkg/agent/transpiler"
	"github.com/elastic/elastic-agent/internal/pkg/config"
	"github.com/elastic/elastic-agent/internal/pkg/diagnostics"
	"github.com/elastic/elastic-agent/pkg/component"
	"github.com/elastic/elastic-agent/pkg/component/runtime"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

//go:generate mockgen -source=coordinator.go -package coordinator -destination mocks_test.go
//go:generate mockgen -source ../../../capabilities/capabilities.go -package coordinator -destination mocks_cap_test.go
//go:generate mockgen -source ../../../core/composable/providers.go -package coordinator -destination mocks_var_providers_test.go

var expectedDiagnosticHooks map[string]string = map[string]string{
	"pre-config":      "pre-config.yaml",
	"variables":       "variables.yaml",
	"computed-config": "computed-config.yaml",
	"components":      "components.yaml",
	"state":           "state.yaml",
}

func TestCoordinatorDiagnosticHooks(t *testing.T) {

	helper := newCoordinatorTestHelper(t, &info.AgentInfo{}, component.RuntimeSpecs{}, false)

	//FIXME I cannot customize what is returned in SubscriptionAll object since it's defined as a struct and all the
	//constructors and fields are private -> the RuntimeManager interface definition should define an interface as
	//return type on the coordinator side or the returned struct should be plain data object with all the fields
	//accessible by anyone
	subscriptionAll := new(runtime.SubscriptionAll)
	helper.runtimeManager.EXPECT().SubscribeAll(gomock.Any()).Return(subscriptionAll).AnyTimes()
	helper.runtimeManager.EXPECT().Update(gomock.Any()).Return(nil)
	helper.runtimeManager.EXPECT().State().Return([]runtime.ComponentComponentState{
		{
			Component: component.Component{
				ID: "mock_component_1",
				Units: []component.Unit{
					{
						ID:       "mock_input_unit",
						Type:     client.UnitTypeInput,
						LogLevel: client.UnitLogLevelInfo,
						Config:   &proto.UnitExpectedConfig{
							//TODO
						},
					},
				},
			},
			State: runtime.ComponentState{
				State: client.UnitStateHealthy,
				VersionInfo: runtime.ComponentVersionInfo{
					Name:    "shiny mock component",
					Version: "latest, obvs :D",
				},
			},
		},
	}).AnyTimes()

	sut := helper.coordinator

	ctx, cancelFunc := context.WithCancel(context.Background())

	wg := new(sync.WaitGroup)
	defer func() {
		cancelFunc()
		wg.Wait()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		coordErr := sut.Run(ctx)
		assert.ErrorIs(t, coordErr, context.Canceled, "Coordinator exited with unexpected error")
	}()

	//Provide vars
	processors := transpiler.Processors{
		{
			"add_fields": map[string]interface{}{
				"dynamic": "added",
			},
		},
	}
	fetchContextProvider := NewMockFetchContextProvider(helper.mockCtrl)
	fetchContextProviders := mapstr.M{
		"kubernetes_secrets": fetchContextProvider,
	}
	vars, err := transpiler.NewVarsWithProcessors(
		"id",
		map[string]interface{}{
			"host": map[string]interface{}{"platform": "linux"},
			"dynamic": map[string]interface{}{
				"key1": "dynamic1",
				"list": []string{
					"array1",
					"array2",
				},
				"dict": map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
		"dynamic",
		processors,
		fetchContextProviders)
	require.NoError(t, err)
	select {
	case helper.varsChannel <- []*transpiler.Vars{vars}:
		t.Log("Vars injected")
	case <-time.NewTimer(100 * time.Millisecond).C:
		t.Fatalf("Timeout writing vars")
	}

	// Inject initial configuration - after starting coordinator
	configBytes, err := os.ReadFile("./testdata/simple_config/elastic-agent.yml")
	require.NoError(t, err)

	initialConf := config.MustNewConfigFrom(configBytes)

	initialConfChange := NewMockConfigChange(helper.mockCtrl)
	initialConfChange.EXPECT().Config().Return(initialConf).AnyTimes()
	initialConfChange.EXPECT().Ack().Times(1)

	select {
	case helper.configChangeChannel <- initialConfChange:
		t.Log("Initial config injected")
	case <-time.NewTimer(100 * time.Millisecond).C:
		t.Fatalf("Timeout writing initial config")
	}

	assert.Eventually(t, func() bool { return sut.State(true).State == cproto.State_HEALTHY }, 1*time.Second, 50*time.Millisecond)

	t.Logf("Agent state: %s", sut.State(true).State)

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
			stringHookResult := sanitizeHookResult(t, hook.Filename, hook.ContentType, hookResult)
			// The output of hooks is VERY verbose even for simple configs but useful for debugging
			t.Logf("Hook %s result: 👇\n--- #--- START ---#\n%s\n--- #--- END ---#", hook.Name, stringHookResult)
			expectedbytes, err := os.ReadFile(fmt.Sprintf("./testdata/simple_config/expected/%s", hook.Filename))
			if assert.NoError(t, err) {
				assert.YAMLEqf(t, string(expectedbytes), stringHookResult, "Unexpected YAML content for file %s", hook.Filename)
			}
		}
	}
}

func sanitizeHookResult(t *testing.T, fileName string, contentType string, rawBytes []byte) (retVal string) {
	const agentPathPlaceholder string = "<AgentRunDir>"
	const hostIDPlaceholder string = "<HostID>"
	const hostKey = "host"
	const pathKey = "path"

	if contentType == "application/yaml" {
		yamlContent := map[string]any{}
		err := yaml.Unmarshal(rawBytes, yamlContent)
		assert.NoErrorf(t, err, "file %s is invalid YAML", fileName)

		if fileName == "pre-config.yaml" || fileName == "computed-config.yaml" {
			// get rid of runtime informations, since those depend on the machine where the test is executed, just assert that they exist
			assert.Containsf(t, yamlContent, "runtime", "No runtime information found in YAML")
			delete(yamlContent, "runtime")

			//fix id and directories
			if assert.Containsf(t, yamlContent, hostKey, "config yaml does not contain %s key", hostKey) {
				hostValue := yamlContent[hostKey]
				if assert.IsType(t, map[interface{}]interface{}{}, hostValue) {
					hostMap := hostValue.(map[interface{}]interface{})
					if assert.Contains(t, hostMap, "id", "host map does not contain id") {
						t.Logf("Substituting host id %q with %q", hostMap["id"], hostIDPlaceholder)
						hostMap["id"] = hostIDPlaceholder
					}
				}
			}

			if assert.Containsf(t, yamlContent, pathKey, "config yaml does not contain agent path map") {
				pathValue := yamlContent[pathKey]
				if assert.IsType(t, map[interface{}]interface{}{}, pathValue) {
					pathMap := pathValue.(map[interface{}]interface{})
					currentDir := pathMap["config"].(string)
					for _, key := range []string{"config", "data", "home", "logs"} {
						if assert.Containsf(t, pathMap, key, "path map is missing expected key %q", key) {
							value := pathMap[key]
							if assert.IsType(t, "", value) {
								valueString := value.(string)
								pathMap[key] = strings.Replace(valueString, currentDir, agentPathPlaceholder, 1)
							}
						}
					}
				}
			}

		}
		sanitizedBytes, err := yaml.Marshal(yamlContent)
		assert.NoError(t, err)
		return string(sanitizedBytes)
	}

	//substitute current running dir with a placeholder
	testDir := path.Dir(os.Args[0])
	t.Logf("Replacing test dir %s with %s", testDir, agentPathPlaceholder)
	return strings.ReplaceAll(string(rawBytes), testDir, agentPathPlaceholder)
}

type coordinatorTestHelper struct {
	coordinator *Coordinator

	runtimeManager      *MockRuntimeManager
	runtimeErrorChannel chan error

	configManager       *MockConfigManager
	configChangeChannel chan ConfigChange
	configErrorChannel  chan error
	actionErrorChannel  chan error

	varsManager      *MockVarsManager
	varsChannel      chan []*transpiler.Vars
	varsErrorChannel chan error

	capability     *MockCapability
	upgradeManager *MockUpgradeManager
	reExecManager  *MockReExecManager
	monitorManager *MockMonitorManager
	mockCtrl       *gomock.Controller
}

func newCoordinatorTestHelper(t *testing.T, agentInfo *info.AgentInfo, specs component.RuntimeSpecs, isManaged bool) *coordinatorTestHelper {

	helper := new(coordinatorTestHelper)

	mockCtrl := gomock.NewController(t)
	helper.mockCtrl = mockCtrl

	// Runtime manager basic wiring
	mockRuntimeMgr := NewMockRuntimeManager(mockCtrl)
	runtimeErrChan := make(chan error)
	mockRuntimeMgr.EXPECT().Errors().Return(runtimeErrChan).AnyTimes()
	mockRuntimeMgr.EXPECT().Run(gomock.Any()).DoAndReturn(func(_ctx context.Context) error { <-_ctx.Done(); return _ctx.Err() }).Times(1)
	helper.runtimeManager = mockRuntimeMgr
	helper.runtimeErrorChannel = runtimeErrChan

	// Config manager basic wiring
	mockConfigMgr := NewMockConfigManager(mockCtrl)
	configErrChan := make(chan error)
	mockConfigMgr.EXPECT().Errors().Return(configErrChan).AnyTimes()
	actionErrorChan := make(chan error)
	mockConfigMgr.EXPECT().ActionErrors().Return(actionErrorChan).AnyTimes()
	configChangeChan := make(chan ConfigChange)
	mockConfigMgr.EXPECT().Watch().Return(configChangeChan).AnyTimes()
	mockConfigMgr.EXPECT().Run(gomock.Any()).DoAndReturn(func(_ctx context.Context) error { <-_ctx.Done(); return _ctx.Err() }).Times(1)
	helper.configManager = mockConfigMgr
	helper.configErrorChannel = configErrChan
	helper.actionErrorChannel = actionErrorChan
	helper.configChangeChannel = configChangeChan

	//Variables manager basic wiring
	mockVarsMgr := NewMockVarsManager(mockCtrl)
	varsErrChan := make(chan error)
	mockVarsMgr.EXPECT().Errors().Return(varsErrChan).AnyTimes()
	varsChan := make(chan []*transpiler.Vars)
	mockVarsMgr.EXPECT().Watch().Return(varsChan).AnyTimes()
	mockVarsMgr.EXPECT().Run(gomock.Any()).DoAndReturn(func(_ctx context.Context) error { <-_ctx.Done(); return _ctx.Err() }).Times(1)
	helper.varsManager = mockVarsMgr
	helper.varsChannel = varsChan
	helper.varsErrorChannel = varsErrChan

	//Capability basic wiring
	mockCapability := NewMockCapability(mockCtrl)
	mockCapability.EXPECT().Apply(gomock.Any()).DoAndReturn(func(in any) (interface{}, error) { return in, nil }).AnyTimes()
	helper.capability = mockCapability

	// Upgrade manager
	mockUpgradeMgr := NewMockUpgradeManager(mockCtrl)
	mockUpgradeMgr.EXPECT().Reload(gomock.Any()).Return(nil).AnyTimes()
	// mockUpgradeMgr.EXPECT().Upgradeable().Return(false)
	helper.upgradeManager = mockUpgradeMgr

	//ReExec manager
	helper.reExecManager = NewMockReExecManager(mockCtrl)

	//Monitor manager
	mockMonitorMgr := NewMockMonitorManager(mockCtrl)
	mockMonitorMgr.EXPECT().Reload(gomock.Any()).Return(nil).AnyTimes()
	mockMonitorMgr.EXPECT().Enabled().Return(false).AnyTimes()
	helper.monitorManager = mockMonitorMgr

	loggerCfg := logger.DefaultLoggingConfig()
	loggerCfg.ToStderr = true

	log, err := logger.NewFromConfig("coordinator-test", loggerCfg, false)
	require.NoError(t, err)

	helper.coordinator = New(
		log,
		logp.InfoLevel,
		agentInfo,
		specs,
		helper.reExecManager,
		helper.upgradeManager,
		helper.runtimeManager,
		helper.configManager,
		helper.varsManager,
		helper.capability,
		helper.monitorManager,
		isManaged,
	)

	return helper
}
