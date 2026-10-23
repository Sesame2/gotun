//go:build !windows

package utils

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type ownerFile interface {
	Chown(int, int) error
}

// PreserveOwner uses the descriptor, never a path that could have become a symlink.
func PreserveOwner(file *os.File, reference string) error {
	return preserveOwner(file, reference, os.Geteuid(), os.Lstat)
}

func preserveOwner(file ownerFile, reference string, euid int, stat func(string) (os.FileInfo, error)) error {
	if euid != 0 {
		return nil
	}
	info, err := stat(reference)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return fmt.Errorf("unsafe owner reference: %s", reference)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine owner of %s", reference)
	}
	return file.Chown(int(owner.Uid), int(owner.Gid))
}

// privateParent opens every descendant relative to an already opened directory.
// The owner reference is the trusted HOME or an explicitly selected existing path.
func privateParent(path, reference string, create bool) (*os.File, string, error) {
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
	fd, err := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	parent := os.NewFile(uintptr(fd), base)
	for _, name := range strings.Split(filepath.Dir(relative), string(os.PathSeparator)) {
		if name == "." || name == "" {
			continue
		}
		next, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		created := false
		if err == unix.ENOENT && create {
			err = unix.Mkdirat(int(parent.Fd()), name, 0700)
			if err == nil {
				created = true
			} else if err != unix.EEXIST {
				parent.Close()
				return nil, "", err
			}
			next, err = unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			parent.Close()
			return nil, "", fmt.Errorf("open private directory %s: %w", name, err)
		}
		child := os.NewFile(uintptr(next), filepath.Join(parent.Name(), name))
		if created {
			err := PreserveOwner(child, reference)
			if err == nil {
				err = child.Chmod(0700)
			}
			if err != nil {
				child.Close()
				_ = unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
				parent.Close()
				return nil, "", err
			}
		}
		parent.Close()
		parent = child
	}
	return parent, filepath.Base(relative), nil
}

func EnsurePrivateDir(path, reference string) error {
	parent, _, err := privateParent(filepath.Join(path, ".private-directory"), reference, true)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := PreserveOwner(parent, reference); err != nil {
		return err
	}
	return parent.Chmod(0700)
}

func OpenPrivateFile(path string, flags int, reference string) (*os.File, error) {
	parent, name, err := privateParent(path, reference, false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("private file is not a regular file: %s", path)
	}
	if err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
			err = fmt.Errorf("private file has an unsafe hard link: %s", path)
		}
	}
	if err == nil {
		err = PreserveOwner(file, reference)
	}
	if err == nil {
		err = file.Chmod(0600)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func LockFile(path string, nonblocking bool, reference string) (func(), error) {
	file, err := OpenPrivateFile(path, os.O_CREATE|os.O_RDWR, reference)
	if err != nil {
		return nil, err
	}
	flags := unix.LOCK_EX
	if nonblocking {
		flags |= unix.LOCK_NB
	}
	if err := unix.Flock(int(file.Fd()), flags); err != nil {
		file.Close()
		if err == unix.EWOULDBLOCK || err == unix.EAGAIN {
			return nil, ErrFileLocked
		}
		return nil, err
	}
	return func() { _ = file.Close() }, nil
}

// AtomicWritePrivateFile keeps creation and replacement on the same directory descriptor.
func AtomicWritePrivateFile(path string, data []byte, reference string) error {
	parent, name, err := privateParent(path, reference, false)
	if err != nil {
		return err
	}
	defer parent.Close()
	temporary := ".config-" + rand.Text() + ".tmp"
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	defer unix.Unlinkat(int(parent.Fd()), temporary, 0)
	if err := PreserveOwner(file, reference); err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return unix.Renameat(int(parent.Fd()), temporary, int(parent.Fd()), name)
}
