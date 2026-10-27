package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TrustKnownHosts installs fixture keys in an explicitly isolated user home.
func TrustKnownHosts(t testing.TB, home string, servers ...*SSHServer) {
	t.Helper()
	path := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(path, "known_hosts"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, server := range servers {
		key, err := os.ReadFile(server.KnownHostsFile)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(key); err != nil {
			t.Fatal(err)
		}
	}
}
