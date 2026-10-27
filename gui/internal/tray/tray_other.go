//go:build (!darwin && !windows) || (darwin && !cgo)

package tray

import "fmt"

type native struct{}

func New() Native { return &native{} }
func (*native) Start(func(string)) error {
	return fmt.Errorf("system tray is not available on this platform")
}
func (*native) Update([]Item) {}
func (*native) Close()        {}
