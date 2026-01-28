package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewConfigService(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}
	if svc == nil {
		t.Fatal("ConfigService should not be nil")
	}
}

func TestAddProfile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name:     "Test Profile",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		Password: "testpass",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profiles := svc.GetProfiles()
	if len(profiles) != 1 {
		t.Errorf("Expected 1 profile, got %d", len(profiles))
	}

	if profiles[0].Name != "Test Profile" {
		t.Errorf("Expected name 'Test Profile', got '%s'", profiles[0].Name)
	}

	if profiles[0].ID == "" {
		t.Error("Profile ID should be generated")
	}
}

func TestGetProfile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name:     "Test Profile",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profiles := svc.GetProfiles()
	if len(profiles) == 0 {
		t.Fatal("No profiles found")
	}

	retrieved, err := svc.GetProfile(profiles[0].ID)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}

	if retrieved.Name != "Test Profile" {
		t.Errorf("Expected name 'Test Profile', got '%s'", retrieved.Name)
	}
}

func TestUpdateProfile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name:     "Original Name",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profiles := svc.GetProfiles()
	if len(profiles) == 0 {
		t.Fatal("No profiles found")
	}

	profiles[0].Name = "Updated Name"
	profiles[0].Host = "newhost.com"

	err = svc.UpdateProfile(profiles[0])
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	updated, err := svc.GetProfile(profiles[0].ID)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}
	if updated.Host != "newhost.com" {
		t.Errorf("Expected host 'newhost.com', got '%s'", updated.Host)
	}
}

func TestDeleteProfile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name:     "To Delete",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profiles := svc.GetProfiles()
	if len(profiles) != 1 {
		t.Fatal("Expected 1 profile")
	}

	err = svc.DeleteProfile(profiles[0].ID)
	if err != nil {
		t.Fatalf("DeleteProfile failed: %v", err)
	}

	profiles = svc.GetProfiles()
	if len(profiles) != 0 {
		t.Errorf("Expected 0 profiles after delete, got %d", len(profiles))
	}
}

func TestConfigPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc1, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name:     "Persistent Profile",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc1.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	svc2, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profiles := svc2.GetProfiles()
	if len(profiles) != 1 {
		t.Errorf("Expected 1 profile after reload, got %d", len(profiles))
	}

	if profiles[0].Name != "Persistent Profile" {
		t.Errorf("Expected name 'Persistent Profile', got '%s'", profiles[0].Name)
	}
}

func TestConfigDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	configDir := filepath.Join(tmpDir, ".gotun")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Error("Config directory should be created")
	}

	// 添加一个配置触发保存
	profile := SSHProfile{
		Name:     "Test",
		Host:     "example.com",
		User:     "test",
		HTTPAddr: ":8080",
	}
	svc.AddProfile(profile)

	configFile := filepath.Join(configDir, "config.json")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Error("Config file should be created after save")
	}
}

func TestProfileDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name: "Minimal Profile",
		Host: "example.com",
		User: "testuser",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profiles := svc.GetProfiles()
	if len(profiles) != 1 {
		t.Fatal("Expected 1 profile")
	}

	if profiles[0].Port != "22" {
		t.Errorf("Expected default port '22', got '%s'", profiles[0].Port)
	}
	if profiles[0].HTTPAddr != ":8080" {
		t.Errorf("Expected default HTTPAddr ':8080', got '%s'", profiles[0].HTTPAddr)
	}
}

func TestSetLastUsed(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		Name:     "Test Profile",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profiles := svc.GetProfiles()
	if len(profiles) == 0 {
		t.Fatal("No profiles found")
	}

	before := time.Now().Add(-time.Second)
	err = svc.SetLastUsed(profiles[0].ID)
	if err != nil {
		t.Fatalf("SetLastUsed failed: %v", err)
	}
	after := time.Now().Add(time.Second)

	updated, _ := svc.GetProfile(profiles[0].ID)
	if updated.LastUsedAt.Before(before) || updated.LastUsedAt.After(after) {
		t.Error("LastUsedAt should be updated to current time")
	}
}

func TestDuplicateProfileID(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	svc, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}

	profile := SSHProfile{
		ID:       "custom_id",
		Name:     "Profile 1",
		Host:     "example.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile)
	if err != nil {
		t.Fatalf("AddProfile failed: %v", err)
	}

	profile2 := SSHProfile{
		ID:       "custom_id",
		Name:     "Profile 2",
		Host:     "example2.com",
		Port:     "22",
		User:     "testuser",
		HTTPAddr: ":8080",
	}

	err = svc.AddProfile(profile2)
	if err == nil {
		t.Error("Expected error when adding duplicate profile ID")
	}
}
