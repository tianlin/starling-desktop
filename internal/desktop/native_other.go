//go:build !windows && !darwin

package desktop

import "errors"

type Integration struct{}

func Start(Hooks) *Integration { return &Integration{} }
func (*Integration) Info() Info {
	return Info{Message: "原生桌面集成只面向 Windows 11 x64。"}
}
func (*Integration) Close()                  {}
func AcquireInstance(string) (func(), error) { return func() {}, errors.New("Windows desktop only") }
func OpenURL(string) error                   { return errors.New("Windows desktop only") }
func Alert(string)                           {}
