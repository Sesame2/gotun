package sysproxy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Sesame2/gotun/internal/logger"
)

func TestTwoInstancesCannotReplaceEachOthersProxy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	f := newMacProxyFixture()
	first := NewManager(logger.NewLogger(false), ":8080", "")
	second := NewManager(logger.NewLogger(false), ":8081", "")
	first.platform, first.run = "darwin", f.run
	second.platform, second.run = "darwin", f.run
	if err := first.Enable(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Disable(); _ = second.Disable() })
	writes := f.writes
	if err := second.Enable(); err == nil {
		t.Error("another instance replaced the current proxy snapshot")
	}
	if err := second.Clear(); err == nil {
		t.Error("Clear stole the active instance's system proxy")
	}
	if f.writes != writes {
		t.Error("rejected instance wrote native proxy settings")
	}
	if err := first.Disable(); err != nil {
		t.Fatal(err)
	}
	if err := second.Enable(); err != nil {
		t.Fatalf("clean stop did not release proxy ownership: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, ".gotun", "system-proxy.lock"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("ownership lock is not private: %v, %v", info, err)
	}
}

func TestProxyOwnershipSurvivesFailedCleanupUntilRetry(t *testing.T) {
	for _, operation := range []string{"enable rollback", "disable", "clear"} {
		t.Run(operation, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			f := newMacProxyFixture()
			first := NewManager(logger.NewLogger(false), ":8080", "")
			second := NewManager(logger.NewLogger(false), ":8081", "")
			first.platform, first.run = "darwin", f.run
			second.platform, second.run = "darwin", f.run
			t.Cleanup(func() { f.fail = nil; _ = first.Disable(); _ = second.Disable() })
			if operation != "enable rollback" {
				if err := first.Enable(); err != nil {
					t.Fatal(err)
				}
			}
			f.fail = func([]string) bool { return true }
			var err error
			switch operation {
			case "enable rollback":
				err = first.Enable()
			case "disable":
				err = first.Disable()
			case "clear":
				err = first.Clear()
			}
			if err == nil || !first.NeedsCleanup() {
				t.Fatalf("lost pending cleanup: %v", err)
			}
			writes := f.writes
			if err := second.Enable(); err == nil {
				t.Error("pending cleanup lost ownership")
			}
			if err := second.Clear(); err == nil {
				t.Error("Clear stole pending cleanup ownership")
			}
			if f.writes != writes {
				t.Error("blocked manager wrote native settings")
			}
			f.fail = nil
			if err := first.Disable(); err != nil {
				t.Fatal(err)
			}
			if err := second.Enable(); err != nil {
				t.Fatalf("retry did not release ownership: %v", err)
			}
		})
	}
}

func TestFailedProxyStartupReleasesOwnershipAfterFullRollback(t *testing.T) {
	for _, stage := range []string{"snapshot", "native write", "explicit clear"} {
		t.Run(stage, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			f := newMacProxyFixture()
			first := NewManager(logger.NewLogger(false), ":8080", "")
			first.platform = "darwin"
			calls := 0
			first.run = func(name string, args ...string) ([]byte, error) {
				calls++
				if stage == "snapshot" && calls == 1 {
					return nil, errors.New("snapshot denied")
				}
				return f.run(name, args...)
			}
			second := NewManager(logger.NewLogger(false), ":8081", "")
			second.platform, second.run = "darwin", f.run
			t.Cleanup(func() { f.fail = nil; _ = first.Disable(); _ = second.Disable() })
			if stage == "native write" {
				f.fail = func([]string) bool { return f.writes == 1 }
			}
			var err error
			if stage == "explicit clear" {
				err = first.Clear()
			} else {
				err = first.Enable()
				if err == nil {
					t.Fatal("fake startup failure was ignored")
				}
			}
			if stage == "explicit clear" && err != nil {
				t.Fatal(err)
			}
			if first.NeedsCleanup() {
				t.Fatal("completed rollback still needs cleanup")
			}
			if err := second.Enable(); err != nil {
				t.Fatalf("completed cleanup retained ownership: %v", err)
			}
		})
	}
}
