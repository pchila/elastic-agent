// Code generated by mockery v2.20.0. DO NOT EDIT.

package coordinator

import (
	config "github.com/elastic/elastic-agent/internal/pkg/config"
	mock "github.com/stretchr/testify/mock"
)

// MockConfigChange is an autogenerated mock type for the ConfigChange type
type MockConfigChange struct {
	mock.Mock
}

type MockConfigChange_Expecter struct {
	mock *mock.Mock
}

func (_m *MockConfigChange) EXPECT() *MockConfigChange_Expecter {
	return &MockConfigChange_Expecter{mock: &_m.Mock}
}

// Ack provides a mock function with given fields:
func (_m *MockConfigChange) Ack() error {
	ret := _m.Called()

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockConfigChange_Ack_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Ack'
type MockConfigChange_Ack_Call struct {
	*mock.Call
}

// Ack is a helper method to define mock.On call
func (_e *MockConfigChange_Expecter) Ack() *MockConfigChange_Ack_Call {
	return &MockConfigChange_Ack_Call{Call: _e.mock.On("Ack")}
}

func (_c *MockConfigChange_Ack_Call) Run(run func()) *MockConfigChange_Ack_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockConfigChange_Ack_Call) Return(_a0 error) *MockConfigChange_Ack_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockConfigChange_Ack_Call) RunAndReturn(run func() error) *MockConfigChange_Ack_Call {
	_c.Call.Return(run)
	return _c
}

// Config provides a mock function with given fields:
func (_m *MockConfigChange) Config() *config.Config {
	ret := _m.Called()

	var r0 *config.Config
	if rf, ok := ret.Get(0).(func() *config.Config); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*config.Config)
		}
	}

	return r0
}

// MockConfigChange_Config_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Config'
type MockConfigChange_Config_Call struct {
	*mock.Call
}

// Config is a helper method to define mock.On call
func (_e *MockConfigChange_Expecter) Config() *MockConfigChange_Config_Call {
	return &MockConfigChange_Config_Call{Call: _e.mock.On("Config")}
}

func (_c *MockConfigChange_Config_Call) Run(run func()) *MockConfigChange_Config_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockConfigChange_Config_Call) Return(_a0 *config.Config) *MockConfigChange_Config_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockConfigChange_Config_Call) RunAndReturn(run func() *config.Config) *MockConfigChange_Config_Call {
	_c.Call.Return(run)
	return _c
}

// Fail provides a mock function with given fields: err
func (_m *MockConfigChange) Fail(err error) {
	_m.Called(err)
}

// MockConfigChange_Fail_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Fail'
type MockConfigChange_Fail_Call struct {
	*mock.Call
}

// Fail is a helper method to define mock.On call
//   - err error
func (_e *MockConfigChange_Expecter) Fail(err interface{}) *MockConfigChange_Fail_Call {
	return &MockConfigChange_Fail_Call{Call: _e.mock.On("Fail", err)}
}

func (_c *MockConfigChange_Fail_Call) Run(run func(err error)) *MockConfigChange_Fail_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(error))
	})
	return _c
}

func (_c *MockConfigChange_Fail_Call) Return() *MockConfigChange_Fail_Call {
	_c.Call.Return()
	return _c
}

func (_c *MockConfigChange_Fail_Call) RunAndReturn(run func(error)) *MockConfigChange_Fail_Call {
	_c.Call.Return(run)
	return _c
}

type mockConstructorTestingTNewMockConfigChange interface {
	mock.TestingT
	Cleanup(func())
}

// NewMockConfigChange creates a new instance of MockConfigChange. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewMockConfigChange(t mockConstructorTestingTNewMockConfigChange) *MockConfigChange {
	mock := &MockConfigChange{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
