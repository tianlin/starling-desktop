//go:build !darwin

package security

func NewVault(path string) Vault { return newVault(path, nativeProtector{}) }
