package utils

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func PreserveOwner(*os.File, string) error { return nil }

func EnsurePrivateDir(path, reference string) error {
	root, relative, err := boundedPrivateRoot(path, reference)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(relative, 0700)
}

func OpenPrivateFile(path string, flags int, reference string) (*os.File, error) {
	root, relative, err := boundedPrivateRoot(path, reference)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.OpenFile(relative, flags, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("private file is not a regular file: %s", path)
	}
	var native windows.ByHandleFileInformation
	if err == nil {
		err = windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &native)
		if err == nil && native.NumberOfLinks != 1 {
			err = fmt.Errorf("private file has an unsafe hard link: %s", path)
		}
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func AtomicWritePrivateFile(path string, data []byte, reference string) error {
	root, relative, err := boundedPrivateRoot(path, reference)
	if err != nil {
		return err
	}
	defer root.Close()
	temporary := filepath.Join(filepath.Dir(relative), ".config-"+rand.Text()+".tmp")
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	defer root.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return root.Rename(temporary, relative)
}

func LockFile(path string, nonblocking bool, reference string) (func(), error) {
	file, err := OpenPrivateFile(path, os.O_CREATE|os.O_RDWR, reference)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if nonblocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	overlapped := &windows.Overlapped{}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped); err != nil {
		file.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrFileLocked
		}
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
		_ = file.Close()
	}, nil
}
