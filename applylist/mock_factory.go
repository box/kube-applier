// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/box/kube-applier/applylist (interfaces: FactoryInterface)

package applylist

import (
	gomock "github.com/golang/mock/gomock"
)

// MockFactoryInterface is a mock of FactoryInterface interface
type MockFactoryInterface struct {
	ctrl     *gomock.Controller
	recorder *MockFactoryInterfaceMockRecorder
}

// MockFactoryInterfaceMockRecorder is the mock recorder for MockFactoryInterface
type MockFactoryInterfaceMockRecorder struct {
	mock *MockFactoryInterface
}

// NewMockFactoryInterface creates a new mock instance
func NewMockFactoryInterface(ctrl *gomock.Controller) *MockFactoryInterface {
	mock := &MockFactoryInterface{ctrl: ctrl}
	mock.recorder = &MockFactoryInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (_m *MockFactoryInterface) EXPECT() *MockFactoryInterfaceMockRecorder {
	return _m.recorder
}

// Create mocks base method
func (_m *MockFactoryInterface) Create(_param0 []string) ([]string, []string, []string, error) {
	ret := _m.ctrl.Call(_m, "Create", _param0)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].([]string)
	ret2, _ := ret[2].([]string)
	ret3, _ := ret[3].(error)
	return ret0, ret1, ret2, ret3
}

// Create indicates an expected call of Create
func (_mr *MockFactoryInterfaceMockRecorder) Create(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Create", arg0)
}