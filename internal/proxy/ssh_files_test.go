//go:build !windows

package proxy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/testutil"
)

func TestSSHTrustFilesRemainPrivateAndRejectSymlinkWrites(t *testing.T) {
	for _, kind := range []string{"default", "explicit nested", "file symlink", "directory symlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			server := testutil.NewSSHServer(t)
			cfg := server.Config()
			cfg.HostKeyPrompt = func(string, string) bool { return true }
			path := filepath.Join(home, ".ssh", "known_hosts")
			cfg.KnownHostsFile = ""
			var outside string
			if kind == "explicit nested" {
				path = filepath.Join(home, "custom", "nested", "known_hosts")
				cfg.KnownHostsFile = path
			}
			if kind == "file symlink" || kind == "directory symlink" {
				outside = filepath.Join(t.TempDir(), "known_hosts")
				if err := os.WriteFile(outside, nil, 0644); err != nil {
					t.Fatal(err)
				}
				if kind == "file symlink" {
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(filepath.Dir(outside), filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
			}
			client, err := proxy.NewSSHClient(cfg, logger.NewLogger(false))
			if client != nil {
				client.Close()
			}
			if outside != "" {
				if err == nil {
					t.Fatal("accepted a symlink trust-file write")
				}
				data, readErr := os.ReadFile(outside)
				if readErr != nil || len(data) != 0 {
					t.Fatalf("symlink target changed: %q %v", data, readErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for path, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("trust path %s mode=%v %v, want %04o", path, info, err, mode)
				}
			}
		})
	}
}
