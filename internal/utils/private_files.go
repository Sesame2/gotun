package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrFileLocked = errors.New("file is locked by another instance")

// OwnerReference finds an existing file or directory whose owner should be retained.
func OwnerReference(path string) (string, error) {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return "", fmt.Errorf("unsafe owner reference: %s", path)
			}
			return path, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

// boundedPrivateRoot is used on Windows, where os.Root also confines reparse points.
func boundedPrivateRoot(path, reference string) (*os.Root, string, error) {
	info, err := os.Lstat(reference)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return nil, "", fmt.Errorf("unsafe owner reference: %s", reference)
	}
	base := reference
	if !info.IsDir() {
		base = filepath.Dir(base)
	}
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("private file is outside owner directory: %s", path)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	var current string
	for _, name := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, name)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			root.Close()
			return nil, "", fmt.Errorf("unsafe private path: %s: %v", current, err)
		}
	}
	return root, relative, nil
}
