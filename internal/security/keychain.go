package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"starling/internal/model"
	"sync"
)

var errKeychainMissing = errors.New("keychain item not found")

type keychainBackend interface {
	Available() bool
	Read(string) ([]byte, error)
	Write(string, []byte) error
	Delete(string) error
}
type keychainVault struct {
	mu      sync.Mutex
	account string
	backend keychainBackend
}

func newKeychainVault(path string, b keychainBackend) *keychainVault {
	absolute, e := filepath.Abs(path)
	if e != nil {
		absolute = filepath.Clean(path)
	}
	sum := sha256.Sum256([]byte(absolute))
	return &keychainVault{account: hex.EncodeToString(sum[:]), backend: b}
}
func (v *keychainVault) Available() bool { return v.backend.Available() }
func (v *keychainVault) Save(s model.SavedSession) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, e := json.Marshal(s)
	if e != nil {
		return model.Err("SECURE_STORAGE", "会话编码失败。")
	}
	defer clear(b)
	if len(b) > 65536 || s.Credentials.Access == "" || s.Credentials.Refresh == "" || s.Identity.ID == "" {
		return model.Err("SECURE_STORAGE", "会话内容无效，未保存。")
	}
	return v.backend.Write(v.account, b)
}
func (v *keychainVault) Load() (model.SavedSession, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	var s model.SavedSession
	b, e := v.backend.Read(v.account)
	defer clear(b)
	if errors.Is(e, errKeychainMissing) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if len(b) > 65536 || json.Unmarshal(b, &s) != nil || s.Credentials.Access == "" || s.Credentials.Refresh == "" || s.Identity.ID == "" {
		return model.SavedSession{}, model.Err("SECURE_STORAGE", "钥匙串中的会话内容无效，请重新登录。")
	}
	return s, nil
}
func (v *keychainVault) Clear() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	e := v.backend.Delete(v.account)
	if errors.Is(e, errKeychainMissing) {
		return nil
	}
	return e
}
