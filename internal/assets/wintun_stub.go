//go:build !windows

package assets

// SetupWintun 非 Windows 平台无需操作
func SetupWintun() error {
	return nil
}
