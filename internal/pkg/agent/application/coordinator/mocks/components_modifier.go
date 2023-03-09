// Code generated by mockery v2.20.0. DO NOT EDIT.

// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package mocks

import (
	component "github.com/elastic/elastic-agent/pkg/component"

	mock "github.com/stretchr/testify/mock"
)

// ComponentsModifier is an autogenerated mock type for the ComponentsModifier type
type ComponentsModifier struct {
	mock.Mock
}

type ComponentsModifier_Expecter struct {
	mock *mock.Mock
}

func (_m *ComponentsModifier) EXPECT() *ComponentsModifier_Expecter {
	return &ComponentsModifier_Expecter{mock: &_m.Mock}
}

// Execute provides a mock function with given fields: comps, cfg
func (_m *ComponentsModifier) Execute(comps []component.Component, cfg map[string]interface{}) ([]component.Component, error) {
	ret := _m.Called(comps, cfg)

	var r0 []component.Component
	var r1 error
	if rf, ok := ret.Get(0).(func([]component.Component, map[string]interface{}) ([]component.Component, error)); ok {
		return rf(comps, cfg)
	}
	if rf, ok := ret.Get(0).(func([]component.Component, map[string]interface{}) []component.Component); ok {
		r0 = rf(comps, cfg)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]component.Component)
		}
	}

	if rf, ok := ret.Get(1).(func([]component.Component, map[string]interface{}) error); ok {
		r1 = rf(comps, cfg)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ComponentsModifier_Execute_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Execute'
type ComponentsModifier_Execute_Call struct {
	*mock.Call
}

// Execute is a helper method to define mock.On call
//   - comps []component.Component
//   - cfg map[string]interface{}
func (_e *ComponentsModifier_Expecter) Execute(comps interface{}, cfg interface{}) *ComponentsModifier_Execute_Call {
	return &ComponentsModifier_Execute_Call{Call: _e.mock.On("Execute", comps, cfg)}
}

func (_c *ComponentsModifier_Execute_Call) Run(run func(comps []component.Component, cfg map[string]interface{})) *ComponentsModifier_Execute_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].([]component.Component), args[1].(map[string]interface{}))
	})
	return _c
}

func (_c *ComponentsModifier_Execute_Call) Return(_a0 []component.Component, _a1 error) *ComponentsModifier_Execute_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *ComponentsModifier_Execute_Call) RunAndReturn(run func([]component.Component, map[string]interface{}) ([]component.Component, error)) *ComponentsModifier_Execute_Call {
	_c.Call.Return(run)
	return _c
}

type mockConstructorTestingTNewComponentsModifier interface {
	mock.TestingT
	Cleanup(func())
}

// NewComponentsModifier creates a new instance of ComponentsModifier. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewComponentsModifier(t mockConstructorTestingTNewComponentsModifier) *ComponentsModifier {
	mock := &ComponentsModifier{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
