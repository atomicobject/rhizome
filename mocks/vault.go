package mocks

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/mock"
)

type MockVaultOperator struct {
	DefaultNameErr error
	PathError      error
	Name           string
	VaultPath      string
}

func (m *MockVaultOperator) DefaultName() (string, error) {
	if m.DefaultNameErr != nil {
		return "", m.DefaultNameErr
	}
	return m.Name, nil
}

func (m *MockVaultOperator) SetDefaultName(_ string) error {
	if m.DefaultNameErr != nil {
		return m.DefaultNameErr
	}
	return nil
}

func (m *MockVaultOperator) Path() (string, error) {
	if m.PathError != nil {
		return "", m.PathError
	}
	if m.VaultPath != "" {
		return m.VaultPath, nil
	}
	return "path", nil
}

func (m *MockVaultOperator) Definition() (obsidian.VaultDefinition, error) {
	if m.PathError != nil {
		return obsidian.VaultDefinition{}, m.PathError
	}
	path := m.VaultPath
	if path == "" {
		path = "path"
	}
	return obsidian.VaultDefinition{Name: m.Name, Path: path}, nil
}

type VaultManager struct {
	mock.Mock
}

func (m *VaultManager) DefaultName() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *VaultManager) SetDefaultName(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *VaultManager) Path() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *VaultManager) Definition() (obsidian.VaultDefinition, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return obsidian.VaultDefinition{}, args.Error(1)
	}
	if def, ok := args.Get(0).(obsidian.VaultDefinition); ok {
		return def, args.Error(1)
	}
	return obsidian.VaultDefinition{}, args.Error(1)
}
