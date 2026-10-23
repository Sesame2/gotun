package service

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Sesame2/gotun/internal/config"
)

func newTestConfigService(t *testing.T) *ConfigService {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	svc, err := NewConfigService()
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func testProfile() SSHProfile {
	return SSHProfile{
		ID: "profile", Name: "Production", Host: "ssh.example.com", Port: "2222",
		User: "alice", Password: "secret", KeyFile: "/keys/id_ed25519", HTTPAddr: "127.0.0.1:8080",
		SocksAddr: "127.0.0.1:1080", SystemProxy: true, RuleFile: "/rules.yaml",
		JumpHosts:     []string{"jump@legacy:22"},
		JumpHostsList: []config.JumpHost{{Host: "jump.example.com", Port: "2200", User: "jump", Password: "jump-secret"}},
		TunMode:       true, TunGlobal: true, TunCIDR: "10.12.0.1/24", TunRoutes: []string{"192.168.0.0/16"},
		TunNAT: []TunNATRule{{SrcCIDR: "10.10.0.0/24", DstCIDR: "10.20.0.0/24"}},
	}
}

func requireProfile(t *testing.T, svc *ConfigService, id string) *SSHProfile {
	t.Helper()
	profile, err := svc.GetProfile(id)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func reloadConfigService(t *testing.T) *ConfigService {
	t.Helper()
	svc, err := NewConfigService()
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestConfigDefaults(t *testing.T) {
	svc := newTestConfigService(t)
	settings := svc.GetSettings()
	if settings.Theme != "system" || settings.Language != "zh-CN" || !settings.MinimizeToTray {
		t.Fatalf("unexpected settings defaults: %+v", settings)
	}
	if settings.AutoStart || settings.AutoConnect || settings.Verbose || settings.DefaultProfile != "" {
		t.Fatalf("unexpected enabled settings: %+v", settings)
	}
	if profiles := svc.GetProfiles(); profiles == nil || len(profiles) != 0 {
		t.Fatalf("new profiles = %#v, want empty non-nil list", profiles)
	}
	if svc.GetDefaultProfile() != nil {
		t.Fatal("empty configuration must not have a default profile")
	}
	if err := svc.AddProfile(SSHProfile{Name: "Minimal", Host: "localhost", User: "user"}); err != nil {
		t.Fatal(err)
	}
	profile := svc.GetProfiles()[0]
	if profile.ID == "" || profile.Port != "22" || profile.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("unexpected profile defaults: %+v", profile)
	}
	if profile.CreatedAt.IsZero() || !profile.CreatedAt.Equal(profile.UpdatedAt) || !profile.LastUsedAt.IsZero() {
		t.Fatalf("unexpected new profile timestamps: %+v", profile)
	}
}

func TestConfigRejectsStaleInstanceWithoutLosingProfiles(t *testing.T) {
	first := newTestConfigService(t)
	second := reloadConfigService(t)
	if err := first.AddProfile(SSHProfile{ID: "new", Name: "Keep this profile"}); err != nil {
		t.Fatal(err)
	}
	previous := second.GetSettings()
	next := previous
	next.Theme = "light"
	if err := second.UpdateSettings(next); err == nil {
		t.Fatal("a stale instance overwrote a newer configuration")
	}
	if second.GetSettings() != previous {
		t.Fatal("rejected save changed in-memory settings")
	}
	reloaded := reloadConfigService(t)
	if _, err := reloaded.GetProfile("new"); err != nil {
		t.Fatalf("stale save lost the other instance's profile: %v", err)
	}
	if err := second.Load(); err != nil {
		t.Fatal(err)
	}
	if err := second.UpdateSettings(next); err != nil {
		t.Fatalf("save after explicitly reloading should work: %v", err)
	}
}

func TestFailedProfileMutationsNeverLeakIntoLaterSave(t *testing.T) {
	for _, operation := range []string{"add", "update", "delete", "last used"} {
		t.Run(operation, func(t *testing.T) {
			svc := newTestConfigService(t)
			if err := svc.AddProfile(testProfile()); err != nil {
				t.Fatal(err)
			}
			before := svc.GetProfiles()
			persistedBefore := reloadConfigService(t).GetProfiles()
			path := svc.configPath
			svc.configPath = t.TempDir()
			var err error
			switch operation {
			case "add":
				err = svc.AddProfile(SSHProfile{ID: "failed"})
			case "update":
				changed := before[0]
				changed.Name = "Must not persist"
				err = svc.UpdateProfile(changed)
			case "delete":
				err = svc.DeleteProfile(before[0].ID)
			case "last used":
				err = svc.SetLastUsed(before[0].ID)
			}
			if err == nil {
				t.Fatal("save over a directory unexpectedly succeeded")
			}
			if !reflect.DeepEqual(svc.GetProfiles(), before) {
				t.Fatal("failed save changed observable profiles")
			}
			svc.configPath = path
			settings := svc.GetSettings()
			settings.Language = "en-US"
			if err := svc.UpdateSettings(settings); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reloadConfigService(t).GetProfiles(), persistedBefore) {
				t.Fatal("later save persisted a rejected profile change")
			}
		})
	}
}

func TestConfigSaveAtomicallyReplacesPrivateFile(t *testing.T) {
	svc := newTestConfigService(t)
	if err := svc.AddProfile(testProfile()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(svc.configPath)
	if err != nil {
		t.Fatal(err)
	}
	settings := svc.GetSettings()
	settings.Theme = "light"
	if err := svc.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(svc.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("save modified the original file instead of atomically replacing it")
	}
	if got := reloadConfigService(t).GetSettings(); got != settings {
		t.Fatalf("replacement did not persist settings: %+v", got)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(svc.configPath), ".config-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary credential files left behind: %v, %v", leftovers, err)
	}
}

func TestConcurrentConfigInstancesCannotOverwriteEachOther(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			first := newTestConfigService(t)
			second := reloadConfigService(t)
			start := make(chan struct{})
			results := make(chan error, 2)
			for i, svc := range []*ConfigService{first, second} {
				go func(i int, svc *ConfigService) {
					<-start
					results <- svc.AddProfile(SSHProfile{ID: fmt.Sprint(i)})
				}(i, svc)
			}
			close(start)
			successes := 0
			for i := 0; i < 2; i++ {
				if <-results == nil {
					successes++
				}
			}
			if successes != 1 || len(reloadConfigService(t).GetProfiles()) != 1 {
				t.Fatalf("concurrent stale saves must preserve exactly one successful change; successes=%d", successes)
			}
		})
	}
}

func TestConfigCRUDPersistsAcrossReload(t *testing.T) {
	svc := newTestConfigService(t)
	profile := testProfile()
	if err := svc.AddProfile(profile); err != nil {
		t.Fatal(err)
	}
	created := requireProfile(t, reloadConfigService(t), profile.ID)
	if created.Name != profile.Name || created.Password != profile.Password || !reflect.DeepEqual(created.TunNAT, profile.TunNAT) || !reflect.DeepEqual(created.JumpHostsList, profile.JumpHostsList) {
		t.Fatalf("profile fields were not persisted: %+v", created)
	}
	created.Name = "Updated"
	createdAt := created.CreatedAt
	created.CreatedAt = time.Unix(1, 0)
	if err := svc.UpdateProfile(*created); err != nil {
		t.Fatal(err)
	}
	updated := requireProfile(t, reloadConfigService(t), profile.ID)
	if updated.Name != "Updated" || !updated.CreatedAt.Equal(createdAt) || updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Fatalf("update did not preserve creation time and persist changes: %+v", updated)
	}
	before := time.Now()
	if err := svc.SetLastUsed(profile.ID); err != nil {
		t.Fatal(err)
	}
	used := requireProfile(t, reloadConfigService(t), profile.ID)
	if used.LastUsedAt.Before(before) || used.LastUsedAt.After(time.Now()) {
		t.Fatalf("last used timestamp not persisted: %v", used.LastUsedAt)
	}
	settings := AppSettings{Theme: "dark", Language: "en-US", AutoStart: true, AutoConnect: true, DefaultProfile: profile.ID, Verbose: true, LogFile: "/tmp/gotun.log"}
	if err := svc.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if got := reloadConfigService(t).GetSettings(); got != settings {
		t.Fatalf("reloaded settings = %+v, want %+v", got, settings)
	}
	if err := svc.DeleteProfile(profile.ID); err != nil {
		t.Fatal(err)
	}
	if got := reloadConfigService(t).GetProfiles(); len(got) != 0 {
		t.Fatalf("delete did not persist: %+v", got)
	}
}

func TestConfigProfileErrors(t *testing.T) {
	svc := newTestConfigService(t)
	if err := svc.AddProfile(testProfile()); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddProfile(testProfile()); err == nil {
		t.Fatal("duplicate ID was accepted")
	}
	if _, err := svc.GetProfile("missing"); err == nil {
		t.Fatal("missing profile lookup succeeded")
	}
	if err := svc.UpdateProfile(SSHProfile{ID: "missing"}); err == nil {
		t.Fatal("missing profile update succeeded")
	}
	if err := svc.DeleteProfile("missing"); err == nil {
		t.Fatal("missing profile delete succeeded")
	}
	if got := reloadConfigService(t).GetProfiles(); len(got) != 1 || got[0].Name != "Production" {
		t.Fatalf("rejected mutations changed persisted profiles: %+v", got)
	}
}

func TestDefaultProfileSelection(t *testing.T) {
	svc := newTestConfigService(t)
	for _, id := range []string{"first", "recent"} {
		if err := svc.AddProfile(SSHProfile{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if got := svc.GetDefaultProfile(); got == nil || got.ID != "first" {
		t.Fatalf("with no usage, want first profile; got %+v", got)
	}
	if err := svc.SetLastUsed("recent"); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetDefaultProfile(); got == nil || got.ID != "recent" {
		t.Fatalf("want most recently used profile; got %+v", got)
	}
	settings := svc.GetSettings()
	settings.DefaultProfile = "first"
	if err := svc.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if got := reloadConfigService(t).GetDefaultProfile(); got == nil || got.ID != "first" {
		t.Fatalf("explicit default must override recency; got %+v", got)
	}
	if err := svc.DeleteProfile("first"); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetDefaultProfile(); got == nil || got.ID != "recent" {
		t.Fatalf("deleted default must fall back to most recently used; got %+v", got)
	}
}

func mutateProfile(profile *SSHProfile) {
	profile.Name = "mutated"
	profile.JumpHosts[0] = "mutated"
	profile.JumpHostsList[0].Password = "mutated"
	profile.TunRoutes[0] = "mutated"
	profile.TunNAT[0].DstCIDR = "mutated"
}

func TestConfigProfilesAreIsolatedSnapshots(t *testing.T) {
	for _, method := range []string{"add input", "update input", "list result", "single result", "default fallback", "explicit default"} {
		t.Run(method, func(t *testing.T) {
			svc := newTestConfigService(t)
			profile := testProfile()
			if err := svc.AddProfile(profile); err != nil {
				t.Fatal(err)
			}
			switch method {
			case "add input":
				mutateProfile(&profile)
			case "update input":
				profile = *requireProfile(t, svc, profile.ID)
				if err := svc.UpdateProfile(profile); err != nil {
					t.Fatal(err)
				}
				mutateProfile(&profile)
			case "list result":
				profiles := svc.GetProfiles()
				mutateProfile(&profiles[0])
			case "single result":
				mutateProfile(requireProfile(t, svc, profile.ID))
			case "default fallback":
				mutateProfile(svc.GetDefaultProfile())
			case "explicit default":
				settings := svc.GetSettings()
				settings.DefaultProfile = profile.ID
				if err := svc.UpdateSettings(settings); err != nil {
					t.Fatal(err)
				}
				mutateProfile(svc.GetDefaultProfile())
			}
			got := requireProfile(t, svc, "profile")
			want := testProfile()
			if got.Name != want.Name || !reflect.DeepEqual(got.JumpHosts, want.JumpHosts) || !reflect.DeepEqual(got.JumpHostsList, want.JumpHostsList) || !reflect.DeepEqual(got.TunRoutes, want.TunRoutes) || !reflect.DeepEqual(got.TunNAT, want.TunNAT) {
				t.Fatalf("caller mutation escaped into service: %+v", got)
			}
		})
	}
}

func TestConfigIOErrors(t *testing.T) {
	t.Run("missing home", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		if _, err := NewConfigService(); err == nil {
			t.Fatal("missing home directory was accepted")
		}
	})
	t.Run("blocked config directory", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := os.WriteFile(filepath.Join(home, ".gotun"), []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewConfigService(); err == nil {
			t.Fatal("config directory creation error was ignored")
		}
	})
	t.Run("invalid JSON preserves loaded state", func(t *testing.T) {
		svc := newTestConfigService(t)
		if err := svc.AddProfile(testProfile()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(svc.configPath, []byte("{broken"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := svc.Load(); err == nil {
			t.Fatal("invalid JSON was accepted")
		}
		if requireProfile(t, svc, "profile").Name != "Production" {
			t.Fatal("failed load discarded previous state")
		}
		if _, err := NewConfigService(); err == nil {
			t.Fatal("startup ignored invalid JSON")
		}
	})
	t.Run("write error", func(t *testing.T) {
		svc := newTestConfigService(t)
		svc.configPath = t.TempDir()
		if err := svc.Save(); err == nil {
			t.Fatal("writing over a directory succeeded")
		}
	})
}

func TestSettingsSaveFailurePreservesObservableSettings(t *testing.T) {
	svc := newTestConfigService(t)
	previous := svc.GetSettings()
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	path := svc.configPath
	svc.configPath = t.TempDir() // A directory is unwritable as a JSON file, including under root.
	failed := previous
	failed.Theme = "light"
	if err := svc.UpdateSettings(failed); err == nil {
		t.Fatal("writing settings over a directory succeeded")
	}
	if got := svc.GetSettings(); got != previous {
		t.Fatalf("failed save changed observable settings: got %+v, want %+v", got, previous)
	}
	svc.configPath = path
	next := svc.GetSettings()
	next.Language = "en-US"
	if err := svc.UpdateSettings(next); err != nil {
		t.Fatal(err)
	}
	want := previous
	want.Language = "en-US"
	if got := reloadConfigService(t).GetSettings(); got != want {
		t.Fatalf("later save persisted a failed change: got %+v, want %+v", got, want)
	}
}

func TestConfigCredentialsArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not describe Windows ACLs")
	}
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			svc := newTestConfigService(t)
			if existing {
				if err := os.WriteFile(svc.configPath, []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(svc.configPath, 0644); err != nil {
					t.Fatal(err)
				}
				if err := svc.Load(); err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.AddProfile(testProfile()); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(svc.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0600 {
				t.Fatalf("config containing SSH passwords has mode %04o, want 0600", got)
			}
		})
	}
}

func TestConfigDirectoryAndLockArePrivateAndRejectSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX filesystem permissions")
	}
	svc := newTestConfigService(t)
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{filepath.Dir(svc.configPath): 0700, svc.configPath + ".lock": 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("private path %s: info=%v error=%v, want %04o", path, info, err, want)
		}
	}
	before, err := os.ReadFile(svc.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(svc.configPath + ".lock"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(outside, []byte("preserve me"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, svc.configPath+".lock"); err != nil {
		t.Fatal(err)
	}
	settings := svc.GetSettings()
	settings.Theme = "light"
	if err := svc.UpdateSettings(settings); err == nil {
		t.Error("accepted a lock symlink to an unrelated file")
	}
	after, err := os.ReadFile(svc.configPath)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Errorf("failed private-file preparation changed saved config: %s, %v", after, err)
	}
}

func TestProfileToConfig(t *testing.T) {
	svc := newTestConfigService(t)
	profile := testProfile()
	profile.TunNAT = append(profile.TunNAT, TunNATRule{SrcCIDR: "invalid", DstCIDR: "invalid"})
	cfg := svc.ProfileToConfig(&profile)
	if cfg.SSHServer != "ssh.example.com:2222" || cfg.SSHUser != profile.User || cfg.SSHPassword != profile.Password || cfg.SSHKeyFile != profile.KeyFile || cfg.SSHPort != profile.Port || cfg.ListenAddr != profile.HTTPAddr || cfg.SocksAddr != profile.SocksAddr || cfg.SystemProxy != profile.SystemProxy || cfg.RuleFile != profile.RuleFile {
		t.Fatalf("SSH/proxy conversion lost fields: %+v", cfg)
	}
	if !cfg.TunMode || !cfg.TunGlobal || cfg.TunCIDR != profile.TunCIDR || !reflect.DeepEqual(cfg.TunRoute, profile.TunRoutes) || !reflect.DeepEqual(cfg.JumpHosts, profile.JumpHosts) || !reflect.DeepEqual(cfg.JumpHostsList, profile.JumpHostsList) {
		t.Fatalf("TUN/jump conversion lost fields: %+v", cfg)
	}
	if len(cfg.SubnetAliases) != 1 || cfg.SubnetAliases[0].Src.String() != "10.10.0.0/24" || cfg.SubnetAliases[0].Dst.String() != "10.20.0.0/24" {
		t.Fatalf("unexpected valid NAT mappings: %+v", cfg.SubnetAliases)
	}
	defaults := svc.ProfileToConfig(&SSHProfile{Host: "localhost", Port: "22"})
	if defaults.TunCIDR != "10.0.0.1/24" || defaults.Timeout != 10*time.Second {
		t.Fatalf("conversion discarded defaults: %+v", defaults)
	}
}

func TestProfileToConfigIPv6(t *testing.T) {
	svc := newTestConfigService(t)
	cfg := svc.ProfileToConfig(&SSHProfile{Host: "::1", Port: "2222"})
	if cfg.SSHServer != "[::1]:2222" {
		t.Fatalf("IPv6 SSH server = %q, want [::1]:2222", cfg.SSHServer)
	}
}

func TestProfileToConfigOwnsSlices(t *testing.T) {
	svc := newTestConfigService(t)
	profile := testProfile()
	cfg := svc.ProfileToConfig(&profile)
	cfg.JumpHosts[0] = "changed"
	cfg.JumpHostsList[0].Host = "changed"
	cfg.TunRoute[0] = "changed"
	want := testProfile()
	if !reflect.DeepEqual(profile.JumpHosts, want.JumpHosts) || !reflect.DeepEqual(profile.JumpHostsList, want.JumpHostsList) || !reflect.DeepEqual(profile.TunRoutes, want.TunRoutes) {
		t.Fatalf("converted config shares mutable slices with profile: %+v", profile)
	}
}

func TestConfigConcurrentReadersAndWriters(t *testing.T) {
	svc := newTestConfigService(t)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			id := fmt.Sprintf("concurrent-%d", i)
			if err := svc.AddProfile(SSHProfile{ID: id}); err != nil {
				t.Error(err)
				return
			}
			profiles := svc.GetProfiles()
			for j := range profiles {
				profiles[j].Name = "local edit"
			}
			if err := svc.SetLastUsed(id); err != nil {
				t.Error(err)
			}
			_ = svc.GetDefaultProfile()
		}(i)
	}
	workers.Wait()
	profiles := reloadConfigService(t).GetProfiles()
	if len(profiles) != 12 {
		t.Fatalf("lost concurrent writes: got %d profiles", len(profiles))
	}
	for _, profile := range profiles {
		if profile.Name != "" || profile.LastUsedAt.IsZero() {
			t.Fatalf("snapshot mutation or usage update lost: %+v", profile)
		}
	}
}

func TestSystemProxyModeValidationAndPersistence(t *testing.T) {
	svc := newTestConfigService(t)
	for _, mode := range []string{"", "follow-profile", "auto", "manual"} {
		settings := svc.GetSettings()
		settings.SystemProxyMode = mode
		if err := svc.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
		reloaded, err := NewConfigService()
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.GetSettings().SystemProxyMode != mode {
			t.Fatalf("mode %q not persisted", mode)
		}
	}
	previous := svc.GetSettings()
	invalid := previous
	invalid.SystemProxyMode = "invalid"
	if svc.UpdateSettings(invalid) == nil {
		t.Fatal("invalid mode accepted")
	}
	if svc.GetSettings() != previous {
		t.Fatal("invalid mode mutated settings")
	}
}
