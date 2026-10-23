//go:build !windows

package utils

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type ownerFixture struct {
	uid, gid, calls int
	failure         error
}

func (f *ownerFixture) Chown(uid, gid int) error {
	f.uid, f.gid, f.calls = uid, gid, f.calls+1
	return f.failure
}

type ownershipInfo struct {
	mode os.FileMode
	sys  any
}

func (i ownershipInfo) Name() string       { return "owner" }
func (i ownershipInfo) Size() int64        { return 0 }
func (i ownershipInfo) Mode() os.FileMode  { return i.mode }
func (i ownershipInfo) ModTime() time.Time { return time.Time{} }
func (i ownershipInfo) IsDir() bool        { return i.mode.IsDir() }
func (i ownershipInfo) Sys() any           { return i.sys }

func TestElevatedWritesPreserveOriginalUserOwner(t *testing.T) {
	failure := errors.New("ownership denied")
	for _, tc := range []struct {
		name              string
		euid              int
		info              ownershipInfo
		statErr, chownErr error
		wantCalls         int
		wantError         bool
	}{
		{name: "elevated original HOME", info: ownershipInfo{mode: os.ModeDir | 0700, sys: &syscall.Stat_t{Uid: 501, Gid: 20}}, wantCalls: 1},
		{name: "explicit known_hosts owner", info: ownershipInfo{mode: 0600, sys: &syscall.Stat_t{Uid: 1001, Gid: 1002}}, wantCalls: 1},
		{name: "ordinary user", euid: 501},
		{name: "failed owner lookup", statErr: failure, wantError: true},
		{name: "symbolic reference", info: ownershipInfo{mode: os.ModeSymlink, sys: &syscall.Stat_t{Uid: 501}}, wantError: true},
		{name: "missing native owner", info: ownershipInfo{mode: 0700}, wantError: true},
		{name: "chown failure", info: ownershipInfo{mode: 0600, sys: &syscall.Stat_t{Uid: 501, Gid: 20}}, chownErr: failure, wantCalls: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := &ownerFixture{failure: tc.chownErr}
			statCalls := 0
			err := preserveOwner(file, "/original-user", tc.euid, func(string) (os.FileInfo, error) { statCalls++; return tc.info, tc.statErr })
			if (err != nil) != tc.wantError || file.calls != tc.wantCalls {
				t.Fatalf("owner result=%v calls=%d, want error=%v calls=%d", err, file.calls, tc.wantError, tc.wantCalls)
			}
			if tc.euid != 0 && statCalls != 0 {
				t.Fatal("ordinary user attempted an ownership operation")
			}
			if file.calls > 0 {
				expected := tc.info.sys.(*syscall.Stat_t)
				if file.uid != int(expected.Uid) || file.gid != int(expected.Gid) {
					t.Fatalf("owner=%d:%d, want %d:%d", file.uid, file.gid, expected.Uid, expected.Gid)
				}
			}
			if tc.chownErr != nil && !errors.Is(err, tc.chownErr) {
				t.Fatalf("lost ownership error: %v", err)
			}
		})
	}
}

func TestPrivateFilesRejectSymlinksAndKeepOriginalContents(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".gotun", "nested")
	if err := EnsurePrivateDir(dir, home); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("directory mode %s: %v %v", path, info, err)
		}
	}
	path := filepath.Join(dir, "config.json")
	if err := AtomicWritePrivateFile(path, []byte("original"), home); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode=%v %v", info, err)
	}
	outside := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "lock")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if file, err := OpenPrivateFile(link, os.O_RDWR, home); err == nil {
		file.Close()
		t.Fatal("followed final symlink")
	}
	if _, err := OwnerReference(link); err == nil {
		t.Fatal("accepted symlink owner")
	}
	parentLink := filepath.Join(home, ".ssh")
	if err := os.Symlink(filepath.Dir(outside), parentLink); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir(parentLink, home); err == nil {
		t.Fatal("followed parent directory symlink")
	}
	if file, err := OpenPrivateFile(filepath.Join(parentLink, "unrelated"), os.O_RDWR, home); err == nil {
		file.Close()
		t.Fatal("opened through parent directory symlink")
	}
	if err := AtomicWritePrivateFile(path, []byte("changed"), link); err == nil {
		t.Fatal("accepted unsafe owner while replacing config")
	}
	for path, want := range map[string]string{path: "original", outside: "outside"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("path %s=%q %v, want %q", path, data, err, want)
		}
	}
}
