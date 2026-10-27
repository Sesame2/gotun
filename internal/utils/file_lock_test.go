package utils

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFileLockExcludesOtherProcesses(t *testing.T) {
	if path := os.Getenv("GOTUN_LOCK_FIXTURE"); path != "" {
		release, err := LockFile(path, true, filepath.Dir(path))
		if os.Getenv("GOTUN_LOCK_EXPECT_BLOCKED") == "1" {
			if !errors.Is(err, ErrFileLocked) {
				if release != nil {
					release()
				}
				t.Fatalf("another process acquired ownership: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			release()
		}
		return
	}
	home := t.TempDir()
	path := filepath.Join(home, "ownership.lock")
	release, err := LockFile(path, true, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	check := func(blocked string) {
		t.Helper()
		cmd := exec.Command(executable, "-test.run=^TestFileLockExcludesOtherProcesses$")
		cmd.Env = append(os.Environ(), "GOTUN_LOCK_FIXTURE="+path, "GOTUN_LOCK_EXPECT_BLOCKED="+blocked)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child ownership check: %v\n%s", err, output)
		}
	}
	check("1")
	release()
	check("0")
}
