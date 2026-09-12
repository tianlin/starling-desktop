package security

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"starling/internal/model"
	"sync"
)

type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
	Available() bool
}
type Vault interface {
	Save(model.SavedSession) error
	Load() (model.SavedSession, error)
	Clear() error
	Available() bool
}
type FileVault struct {
	mu        sync.Mutex
	path      string
	protector Protector
}

func NewVault(path string) *FileVault              { return newVault(path, nativeProtector{}) }
func newVault(path string, p Protector) *FileVault { return &FileVault{path: path, protector: p} }
func (v *FileVault) Available() bool               { return v.protector.Available() }
func (v *FileVault) Save(s model.SavedSession) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.Available() {
		return model.Err("SECURE_STORAGE", "系统凭据保护不可用，请选择仅本次会话。")
	}
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	defer clear(b)
	enc, e := v.protector.Protect(b)
	if e != nil {
		return model.Err("SECURE_STORAGE", "无法安全保存凭据，没有写入明文文件。")
	}
	return atomicWrite(v.path, enc)
}
func (v *FileVault) Load() (model.SavedSession, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	var s model.SavedSession
	file, e := os.Open(v.path)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, model.Err("SECURE_STORAGE", "无法读取系统保护的凭据。")
	}
	defer file.Close()
	b, e := io.ReadAll(io.LimitReader(file, 65537))
	if e != nil {
		return s, model.Err("SECURE_STORAGE", "凭据读取失败。")
	}
	if len(b) > 65536 {
		return s, model.Err("SECURE_STORAGE", "凭据文件无效。")
	}
	if len(b) == 0 {
		return s, nil
	} // A logout tombstone is deliberately credential-free.
	raw, e := v.protector.Unprotect(b)
	if e != nil {
		return s, model.Err("SECURE_STORAGE", "凭据无法在当前 Windows 用户下解密，请重新登录。")
	}
	defer clear(raw)
	if json.Unmarshal(raw, &s) != nil {
		return model.SavedSession{}, model.Err("SECURE_STORAGE", "凭据内容无效，请重新登录。")
	}
	return s, nil
}
func (v *FileVault) Clear() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, e := os.Stat(v.path); os.IsNotExist(e) {
		return nil
	}
	// Replace with an empty tombstone before removing; a failed remove cannot resurrect the session.
	if e := atomicWrite(v.path, nil); e != nil {
		if er := os.Remove(v.path); er != nil && !os.IsNotExist(er) {
			return model.Err("SECURE_STORAGE", "未能删除本机凭据。请检查目录权限，不要把设备交给他人。")
		}
		return nil
	}
	if e := os.Remove(v.path); e != nil && !os.IsNotExist(e) {
		return nil
	}
	return nil
}
func atomicWrite(path string, data []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return model.Err("DISK", "无法创建数据目录。")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".starling-*")
	if e != nil {
		return model.Err("DISK", "无法写入数据目录。")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_ = f.Chmod(0600)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return model.Err("DISK", "磁盘写入失败。")
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return model.Err("DISK", "磁盘同步失败。")
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = replaceFile(tmp, path); e != nil {
		return model.Err("DISK", "无法原子更新凭据。")
	}
	return nil
}

var errNativeProtection = errors.New("native credential protection unavailable")
