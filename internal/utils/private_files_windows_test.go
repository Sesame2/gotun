package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPrivateFilesRejectWindowsJunction(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(home, ".gotun")
	path := filepath.Join(outside, "config.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Fatalf("create unprivileged NTFS junction: %v %s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if err := EnsurePrivateDir(link, home); err == nil {
		t.Error("accepted junction as a private directory")
	}
	if file, err := OpenPrivateFile(filepath.Join(link, "config.json"), os.O_RDWR, home); err == nil {
		file.Close()
		t.Error("opened a private file through a junction")
	}
	if err := AtomicWritePrivateFile(filepath.Join(link, "config.json"), []byte("changed"), home); err == nil {
		t.Error("replaced a private file through a junction")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("junction target changed: %s %v", data, err)
	}
}

func TestPrivateFilesRejectWindowsFileLinks(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			target := filepath.Join(t.TempDir(), "original")
			if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(home, "config.json")
			var err error
			if kind == "symlink" {
				err = os.Symlink(target, link)
			} else {
				err = os.Link(target, link)
			}
			if err != nil {
				t.Skipf("filesystem does not support test link: %v", err)
			}
			if file, err := OpenPrivateFile(link, os.O_RDWR, home); err == nil {
				file.Close()
				t.Fatal("accepted linked private file")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "original" {
				t.Fatalf("linked target changed: %s %v", data, err)
			}
		})
	}
}
