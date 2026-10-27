package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/utils"
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
	// TUN 模式配置
	TunMode    bool         `json:"tunMode"`             // 是否启用TUN模式
	TunGlobal  bool         `json:"tunGlobal"`           // 全局模式
	TunCIDR    string       `json:"tunCIDR,omitempty"`   // TUN设备CIDR (默认 10.0.0.1/24)
	TunRoutes  []string     `json:"tunRoutes,omitempty"` // 静态路由列表
	TunNAT     []TunNATRule `json:"tunNAT,omitempty"`    // NAT映射规则
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`
	LastUsedAt time.Time    `json:"lastUsedAt,omitempty"`
}

// TunNATRule TUN NAT映射规则
type TunNATRule struct {
	SrcCIDR string `json:"srcCIDR"` // 本地CIDR (例如 10.0.0.0/24)
	DstCIDR string `json:"dstCIDR"` // 远程CIDR (例如 192.168.0.0/24)
}

// AppSettings 应用设置
type AppSettings struct {
	SystemProxyMode string `json:"systemProxyMode,omitempty"`
	Theme           string `json:"theme"`          // "light" | "dark" | "system"
	Language        string `json:"language"`       // "zh-CN" | "en-US"
	AutoStart       bool   `json:"autoStart"`      // 开机自启
	MinimizeToTray  bool   `json:"minimizeToTray"` // 最小化到托盘
	AutoConnect     bool   `json:"autoConnect"`    // 启动时自动连接
	DefaultProfile  string `json:"defaultProfile"` // 默认配置文件ID
	Verbose         bool   `json:"verbose"`        // 详细日志
	LogFile         string `json:"logFile"`        // 日志文件路径
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
	ownerPath  string
	data       *ConfigData
	revision   []byte
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
		ownerPath:  filepath.Dir(configDir),
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
	if err := utils.EnsurePrivateDir(configDir, homeDir); err != nil {
		return "", fmt.Errorf("无法创建配置目录: %v", err)
	}

	return configDir, nil
}

// Load 加载配置
func (s *ConfigService) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readConfigFile()
	if err != nil {
		return err
	}

	var cfg ConfigData
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	s.data = &cfg
	s.revision = slices.Clone(data)
	return nil
}

func (s *ConfigService) readConfigFile() ([]byte, error) {
	file, err := utils.OpenPrivateFile(s.configPath, os.O_RDONLY, s.ownerPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// Save 保存配置
func (s *ConfigService) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *ConfigService) saveLocked() error {
	unlock, err := lockConfigFile(s.configPath+".lock", s.ownerPath)
	if err != nil {
		return fmt.Errorf("锁定配置文件失败: %w", err)
	}
	defer unlock()
	current, err := s.readConfigFile()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取当前配置失败: %w", err)
	}
	if !bytes.Equal(current, s.revision) {
		return fmt.Errorf("配置已被其他实例修改，请重新加载后重试")
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	if err := utils.AtomicWritePrivateFile(s.configPath, data, s.ownerPath); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	s.revision = slices.Clone(data)
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
	switch settings.SystemProxyMode {
	case "", "follow-profile", "auto", "manual":
	default:
		return fmt.Errorf("无效的系统代理模式: %s", settings.SystemProxyMode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.Settings
	s.data.Settings = settings
	if err := s.saveLocked(); err != nil {
		s.data.Settings = previous
		return err
	}
	return nil
}

// GetProfiles 获取所有配置文件
func (s *ConfigService) GetProfiles() []SSHProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profiles := make([]SSHProfile, len(s.data.Profiles))
	for i, profile := range s.data.Profiles {
		profiles[i] = cloneProfile(profile)
	}
	return profiles
}

func cloneProfile(profile SSHProfile) SSHProfile {
	profile.JumpHosts = slices.Clone(profile.JumpHosts)
	profile.JumpHostsList = slices.Clone(profile.JumpHostsList)
	profile.TunRoutes = slices.Clone(profile.TunRoutes)
	profile.TunNAT = slices.Clone(profile.TunNAT)
	return profile
}

// GetProfile 获取指定配置文件
func (s *ConfigService) GetProfile(id string) (*SSHProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.data.Profiles {
		if p.ID == id {
			p = cloneProfile(p)
			return &p, nil
		}
	}
	return nil, fmt.Errorf("配置文件不存在: %s", id)
}

// AddProfile 添加配置文件
func (s *ConfigService) AddProfile(profile SSHProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 生成ID
	if profile.ID == "" {
		profile.ID = fmt.Sprintf("profile_%d", time.Now().UnixNano())
	}

	// 检查重复
	for _, p := range s.data.Profiles {
		if p.ID == profile.ID {
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
		profile.HTTPAddr = "127.0.0.1:8080"
	}

	previous := slices.Clone(s.data.Profiles)
	s.data.Profiles = append(s.data.Profiles, cloneProfile(profile))
	if err := s.saveLocked(); err != nil {
		s.data.Profiles = previous
		return err
	}
	return nil
}

// UpdateProfile 更新配置文件
func (s *ConfigService) UpdateProfile(profile SSHProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.data.Profiles {
		if p.ID == profile.ID {
			profile.CreatedAt = p.CreatedAt
			profile.UpdatedAt = time.Now()
			s.data.Profiles[i] = cloneProfile(profile)
			if err := s.saveLocked(); err != nil {
				s.data.Profiles[i] = p
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("配置文件不存在: %s", profile.ID)
}

// DeleteProfile 删除配置文件
func (s *ConfigService) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.data.Profiles {
		if p.ID == id {
			previous := slices.Clone(s.data.Profiles)
			s.data.Profiles = append(s.data.Profiles[:i], s.data.Profiles[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.data.Profiles = previous
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("配置文件不存在: %s", id)
}

// SetLastUsed 设置最后使用时间
func (s *ConfigService) SetLastUsed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.data.Profiles {
		if p.ID == id {
			s.data.Profiles[i].LastUsedAt = time.Now()
			if err := s.saveLocked(); err != nil {
				s.data.Profiles[i] = p
				return err
			}
			return nil
		}
	}

	return nil
}

// ProfileToConfig 将SSH配置文件转换为Config
func (s *ConfigService) ProfileToConfig(profile *SSHProfile) *config.Config {
	cfg := config.NewConfig()
	cfg.SSHServer = net.JoinHostPort(profile.Host, profile.Port)
	cfg.SSHUser = profile.User
	cfg.SSHPassword = profile.Password
	cfg.SSHKeyFile = profile.KeyFile
	cfg.SSHPort = profile.Port
	cfg.JumpHosts = slices.Clone(profile.JumpHosts)
	cfg.JumpHostsList = slices.Clone(profile.JumpHostsList)
	cfg.ListenAddr = profile.HTTPAddr
	cfg.SocksAddr = profile.SocksAddr
	cfg.SystemProxy = profile.SystemProxy
	cfg.RuleFile = profile.RuleFile

	// TUN 模式配置
	cfg.TunMode = profile.TunMode
	cfg.TunGlobal = profile.TunGlobal
	if profile.TunCIDR != "" {
		cfg.TunCIDR = profile.TunCIDR
	}
	cfg.TunRoute = slices.Clone(profile.TunRoutes)

	// 转换 NAT 规则
	for _, nat := range profile.TunNAT {
		alias, err := config.ParseSubnetAlias(nat.SrcCIDR + ":" + nat.DstCIDR)
		if err == nil {
			cfg.SubnetAliases = append(cfg.SubnetAliases, alias)
		}
	}

	return cfg
}

// GetDefaultProfile 获取默认配置文件
func (s *ConfigService) GetDefaultProfile() *SSHProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data.Settings.DefaultProfile != "" {
		for _, p := range s.data.Profiles {
			if p.ID == s.data.Settings.DefaultProfile {
				p = cloneProfile(p)
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

	if latest == nil {
		return nil
	}
	profile := cloneProfile(*latest)
	return &profile
}
