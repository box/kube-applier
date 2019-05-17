// Code generated by MockGen. DO NOT EDIT.
// Source: metrics/prometheus.go

// Package metrics is a generated GoMock package.
package metrics

import (
	gomock "github.com/golang/mock/gomock"
	reflect "reflect"
)

// MockPrometheusInterface is a mock of PrometheusInterface interface
type MockPrometheusInterface struct {
	ctrl     *gomock.Controller
	recorder *MockPrometheusInterfaceMockRecorder
}

// MockPrometheusInterfaceMockRecorder is the mock recorder for MockPrometheusInterface
type MockPrometheusInterfaceMockRecorder struct {
	mock *MockPrometheusInterface
}

// NewMockPrometheusInterface creates a new mock instance
func NewMockPrometheusInterface(ctrl *gomock.Controller) *MockPrometheusInterface {
	mock := &MockPrometheusInterface{ctrl: ctrl}
	mock.recorder = &MockPrometheusInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockPrometheusInterface) EXPECT() *MockPrometheusInterfaceMockRecorder {
	return m.recorder
}

// UpdateKubectlSignalExitCount mocks base method
func (m *MockPrometheusInterface) UpdateKubectlSignalExitCount(arg0 string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "UpdateKubectlSignalExitCount", arg0)
}

// UpdateKubectlSignalExitCount indicates an expected call of UpdateKubectlSignalExitCount
func (mr *MockPrometheusInterfaceMockRecorder) UpdateKubectlSignalExitCount(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateKubectlSignalExitCount", reflect.TypeOf((*MockPrometheusInterface)(nil).UpdateKubectlSignalExitCount), arg0)
}

// UpdateNamespaceSuccess mocks base method
func (m *MockPrometheusInterface) UpdateNamespaceSuccess(arg0 string, arg1 bool) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "UpdateNamespaceSuccess", arg0, arg1)
}

// UpdateNamespaceSuccess indicates an expected call of UpdateNamespaceSuccess
func (mr *MockPrometheusInterfaceMockRecorder) UpdateNamespaceSuccess(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateNamespaceSuccess", reflect.TypeOf((*MockPrometheusInterface)(nil).UpdateNamespaceSuccess), arg0, arg1)
}

// UpdateRunLatency mocks base method
func (m *MockPrometheusInterface) UpdateRunLatency(arg0 float64, arg1 bool) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "UpdateRunLatency", arg0, arg1)
}

// UpdateRunLatency indicates an expected call of UpdateRunLatency
func (mr *MockPrometheusInterfaceMockRecorder) UpdateRunLatency(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateRunLatency", reflect.TypeOf((*MockPrometheusInterface)(nil).UpdateRunLatency), arg0, arg1)
}

// UpdateResultSummary mocks base method
func (m *MockPrometheusInterface) UpdateResultSummary(arg0 map[string]string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "UpdateResultSummary", arg0)
}

// UpdateResultSummary indicates an expected call of UpdateResultSummary
func (mr *MockPrometheusInterfaceMockRecorder) UpdateResultSummary(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateResultSummary", reflect.TypeOf((*MockPrometheusInterface)(nil).UpdateResultSummary), arg0)
}
