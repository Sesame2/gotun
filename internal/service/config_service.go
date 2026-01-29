package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
)

// SSHProfile SSH配置文件
type SSHProfile struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Host          string            `json:"host"`
	Port          string            `json:"port"`
	User          string            `json:"user"`
	Password      string            `json:"password,omitempty"`
	KeyFile       string            `json:"keyFile,omitempty"`
	JumpHosts     []string          `json:"jumpHosts,omitempty"`     // Legacy
	JumpHostsList []config.JumpHost `json:"jumpHostsList,omitempty"` // GUI Structured
	HTTPAddr      string            `json:"httpAddr"`
	SocksAddr     string            `json:"socksAddr,omitempty"`
	SystemProxy   bool              `json:"systemProxy"`
	RuleFile      string            `json:"ruleFile,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	LastUsedAt    time.Time         `json:"lastUsedAt,omitempty"`
}

// AppSettings 应用设置
type AppSettings struct {
	Theme          string `json:"theme"`          // "light" | "dark" | "system"
	Language       string `json:"language"`       // "zh-CN" | "en-US"
	AutoStart      bool   `json:"autoStart"`      // 开机自启
	MinimizeToTray bool   `json:"minimizeToTray"` // 最小化到托盘
	AutoConnect    bool   `json:"autoConnect"`    // 启动时自动连接
	DefaultProfile string `json:"defaultProfile"` // 默认配置文件ID
	Verbose        bool   `json:"verbose"`        // 详细日志
	LogFile        string `json:"logFile"`        // 日志文件路径
}

// ConfigData 配置数据
type ConfigData struct {
	Version  int          `json:"version"`
	Settings AppSettings  `json:"settings"`
	Profiles []SSHProfile `json:"profiles"`
}

// ConfigService 配置管理服务
type ConfigService struct {
	mu         sync.RWMutex
	configPath string
	data       *ConfigData
}

// NewConfigService 创建配置服务
func NewConfigService() (*ConfigService, error) {
	// 获取用户配置目录
	configDir, err := getConfigDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(configDir, "config.json")

	svc := &ConfigService{
		configPath: configPath,
		data: &ConfigData{
			Version: 1,
			Settings: AppSettings{
				Theme:          "system",
				Language:       "zh-CN",
				AutoStart:      false,
				MinimizeToTray: true,
				AutoConnect:    false,
				Verbose:        false,
			},
			Profiles: []SSHProfile{},
		},
	}

	// 加载现有配置
	if err := svc.Load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return svc, nil
}

// getConfigDir 获取配置目录
func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户目录: %v", err)
	}

	configDir := filepath.Join(homeDir, ".gotun")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("无法创建配置目录: %v", err)
	}

	return configDir, nil
}

// Load 加载配置
func (s *ConfigService) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	var cfg ConfigData
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	s.data = &cfg
	return nil
}

// Save 保存配置
func (s *ConfigService) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// GetSettings 获取应用设置
func (s *ConfigService) GetSettings() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Settings
}

// UpdateSettings 更新应用设置
func (s *ConfigService) UpdateSettings(settings AppSettings) error {
	s.mu.Lock()
	s.data.Settings = settings
	s.mu.Unlock()
	return s.Save()
}

// GetProfiles 获取所有配置文件
func (s *ConfigService) GetProfiles() []SSHProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Profiles
}

// GetProfile 获取指定配置文件
func (s *ConfigService) GetProfile(id string) (*SSHProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.data.Profiles {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("配置文件不存在: %s", id)
}

// AddProfile 添加配置文件
func (s *ConfigService) AddProfile(profile SSHProfile) error {
	s.mu.Lock()

	// 生成ID
	if profile.ID == "" {
		profile.ID = fmt.Sprintf("profile_%d", time.Now().UnixNano())
	}

	// 检查重复
	for _, p := range s.data.Profiles {
		if p.ID == profile.ID {
			s.mu.Unlock()
			return fmt.Errorf("配置文件ID已存在: %s", profile.ID)
		}
	}

	// 设置时间
	now := time.Now()
	profile.CreatedAt = now
	profile.UpdatedAt = now

	// 设置默认值
	if profile.Port == "" {
		profile.Port = "22"
	}
	if profile.HTTPAddr == "" {
		profile.HTTPAddr = ":8080"
	}

	s.data.Profiles = append(s.data.Profiles, profile)

	s.mu.Unlock()
	return s.Save()
}

// UpdateProfile 更新配置文件
func (s *ConfigService) UpdateProfile(profile SSHProfile) error {
	s.mu.Lock()

	for i, p := range s.data.Profiles {
		if p.ID == profile.ID {
			profile.CreatedAt = p.CreatedAt
			profile.UpdatedAt = time.Now()
			s.data.Profiles[i] = profile
			s.mu.Unlock()
			return s.Save()
		}
	}

	s.mu.Unlock()
	return fmt.Errorf("配置文件不存在: %s", profile.ID)
}

// DeleteProfile 删除配置文件
func (s *ConfigService) DeleteProfile(id string) error {
	s.mu.Lock()

	for i, p := range s.data.Profiles {
		if p.ID == id {
			s.data.Profiles = append(s.data.Profiles[:i], s.data.Profiles[i+1:]...)
			s.mu.Unlock()
			return s.Save()
		}
	}

	s.mu.Unlock()
	return fmt.Errorf("配置文件不存在: %s", id)
}

// SetLastUsed 设置最后使用时间
func (s *ConfigService) SetLastUsed(id string) error {
	s.mu.Lock()

	for i, p := range s.data.Profiles {
		if p.ID == id {
			s.data.Profiles[i].LastUsedAt = time.Now()
			s.mu.Unlock()
			return s.Save()
		}
	}

	s.mu.Unlock()
	return nil
}

// ProfileToConfig 将SSH配置文件转换为Config
func (s *ConfigService) ProfileToConfig(profile *SSHProfile) *config.Config {
	cfg := config.NewConfig()
	cfg.SSHServer = fmt.Sprintf("%s:%s", profile.Host, profile.Port)
	cfg.SSHUser = profile.User
	cfg.SSHPassword = profile.Password
	cfg.SSHKeyFile = profile.KeyFile
	cfg.SSHPort = profile.Port
	cfg.JumpHosts = profile.JumpHosts
	cfg.JumpHostsList = profile.JumpHostsList
	cfg.ListenAddr = profile.HTTPAddr
	cfg.SocksAddr = profile.SocksAddr
	cfg.SystemProxy = profile.SystemProxy
	cfg.RuleFile = profile.RuleFile
	return cfg
}

// GetDefaultProfile 获取默认配置文件
func (s *ConfigService) GetDefaultProfile() *SSHProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data.Settings.DefaultProfile != "" {
		for _, p := range s.data.Profiles {
			if p.ID == s.data.Settings.DefaultProfile {
				return &p
			}
		}
	}

	// 返回最近使用的
	var latest *SSHProfile
	for i := range s.data.Profiles {
		p := &s.data.Profiles[i]
		if latest == nil || p.LastUsedAt.After(latest.LastUsedAt) {
			latest = p
		}
	}

	return latest
}
