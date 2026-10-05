package actions

import "github.com/atomicobject/rhizome/pkg/vault/obsidian"

type stubVault struct {
	path string
}

func (s stubVault) DefaultName() (string, error) { return "", nil }
func (s stubVault) SetDefaultName(string) error  { return nil }
func (s stubVault) Path() (string, error)        { return s.path, nil }
func (s stubVault) Definition() (obsidian.VaultDefinition, error) {
	return obsidian.VaultDefinition{Name: "", Path: s.path}, nil
}
