package sysproxy

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/Sesame2/gotun/internal/logger"
)

// Manager 管理系统代理设置
type Manager struct {
	logger       *logger.Logger
	proxyAddress string
	enabled      bool
	origSettings map[string]string // 保存原始设置
}

// NewManager 创建新的系统代理管理器
func NewManager(log *logger.Logger, listenAddr string) *Manager {
	// 解析地址，确保格式正确
	host, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		log.Warnf("无法解析监听地址 %s: %v, 使用原始地址", listenAddr, err)
		return &Manager{
			logger:       log,
			proxyAddress: listenAddr,
			origSettings: make(map[string]string),
		}
	}

	// 如果主机为空，使用localhost
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}

	return &Manager{
		logger:       log,
		proxyAddress: fmt.Sprintf("%s:%s", host, portStr),
		origSettings: make(map[string]string),
	}
}

// Enable 启用系统代理
func (m *Manager) Enable() error {
	if m.enabled {
		return nil // 已经启用，无需重复操作
	}

	// 先保存当前设置
	if err := m.saveCurrentSettings(); err != nil {
		return fmt.Errorf("保存当前代理设置失败: %v", err)
	}

	// 根据操作系统设置代理
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = m.enableMacOS()
	case "windows":
		err = m.enableWindows()
	case "linux":
		err = m.enableLinux()
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	if err != nil {
		return err
	}

	m.enabled = true
	m.logger.Infof("系统代理已设置为 %s", m.proxyAddress)
	return nil
}

// Disable 禁用系统代理
func (m *Manager) Disable() error {
	if !m.enabled {
		return nil // 已经禁用，无需操作
	}

	// 根据操作系统恢复设置
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = m.disableMacOS()
	case "windows":
		err = m.disableWindows()
	case "linux":
		err = m.disableLinux()
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	if err != nil {
		return err
	}

	m.enabled = false
	m.logger.Info("系统代理已恢复")
	return nil
}

// IsEnabled 返回代理是否已启用
func (m *Manager) IsEnabled() bool {
	return m.enabled
}

// saveCurrentSettings 保存当前的代理设置
func (m *Manager) saveCurrentSettings() error {
	switch runtime.GOOS {
	case "darwin":
		return m.saveSettingsMacOS()
	case "windows":
		return m.saveSettingsWindows()
	case "linux":
		return m.saveSettingsLinux()
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
}

// ===== MacOS 实现 =====

func (m *Manager) saveSettingsMacOS() error {
	m.logger.Debug("保存MacOS代理设置...")

	// 获取网络服务列表
	services, err := getMacOSNetworkServices()
	if err != nil {
		return fmt.Errorf("获取网络服务列表失败: %v", err)
	}

	for _, service := range services {
		// 获取HTTP代理设置
		cmd := exec.Command("networksetup", "-getwebproxy", service)
		output, err := cmd.CombinedOutput()
		if err == nil {
			m.origSettings["http_"+service] = string(output)
		}

		// 获取HTTPS代理设置
		cmd = exec.Command("networksetup", "-getsecurewebproxy", service)
		output, err = cmd.CombinedOutput()
		if err == nil {
			m.origSettings["https_"+service] = string(output)
		}

		// 获取代理例外列表
		cmd = exec.Command("networksetup", "-getproxybypassdomains", service)
		output, err = cmd.CombinedOutput()
		if err == nil {
			m.origSettings["bypass_"+service] = string(output)
		}
	}

	return nil
}

func (m *Manager) enableMacOS() error {
	m.logger.Debug("设置MacOS系统代理...")

	host, portStr, _ := net.SplitHostPort(m.proxyAddress)
	port, _ := strconv.Atoi(portStr)

	// 获取网络服务列表
	services, err := getMacOSNetworkServices()
	if err != nil {
		return fmt.Errorf("获取网络服务列表失败: %v", err)
	}

	for _, service := range services {
		m.logger.Debugf("配置网络服务: %s", service)

		// 清空代理例外列表 (使用"Empty"关键字)
		cmd := exec.Command("networksetup", "-setproxybypassdomains", service, "Empty")
		if err := cmd.Run(); err != nil {
			m.logger.Warnf("清空代理例外列表失败: %v", err)
		}

		// 设置HTTP代理
		cmd = exec.Command("networksetup", "-setwebproxy", service, host, portStr)
		if err := cmd.Run(); err != nil {
			m.logger.Warnf("为服务 %s 设置HTTP代理失败: %v", service, err)
			continue
		}

		// 设置HTTPS代理
		cmd = exec.Command("networksetup", "-setsecurewebproxy", service, host, fmt.Sprintf("%d", port))
		if err := cmd.Run(); err != nil {
			m.logger.Warnf("为服务 %s 设置HTTPS代理失败: %v", service, err)
			continue
		}

		// 启用代理
		cmd = exec.Command("networksetup", "-setwebproxystate", service, "on")
		cmd.Run()

		cmd = exec.Command("networksetup", "-setsecurewebproxystate", service, "on")
		cmd.Run()
	}

	return nil
}

func (m *Manager) disableMacOS() error {
	m.logger.Debug("恢复MacOS系统代理设置...")

	// 获取网络服务列表
	services, err := getMacOSNetworkServices()
	if err != nil {
		return fmt.Errorf("获取网络服务列表失败: %v", err)
	}

	for _, service := range services {
		// 禁用HTTP代理
		cmd := exec.Command("networksetup", "-setwebproxystate", service, "off")
		cmd.Run()

		// 禁用HTTPS代理
		cmd = exec.Command("networksetup", "-setsecurewebproxystate", service, "off")
		cmd.Run()
	}

	return nil
}

// 获取MacOS网络服务列表
func getMacOSNetworkServices() ([]string, error) {
	cmd := exec.Command("networksetup", "-listallnetworkservices")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("获取网络服务列表失败: %v", err)
	}

	lines := strings.Split(string(output), "\n")
	var services []string

	for i, line := range lines {
		// 跳过第一行，它是标题
		if i == 0 || line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}

	return services, nil
}

// ===== Windows 实现 =====

func (m *Manager) saveSettingsWindows() error {
	m.logger.Debug("保存Windows代理设置...")

	// 获取当前的代理设置
	cmd := exec.Command("reg", "query", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable")
	output, err := cmd.CombinedOutput()
	if err == nil {
		m.origSettings["enable"] = string(output)
	}

	cmd = exec.Command("reg", "query", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["server"] = string(output)
	}

	// 获取代理例外列表
	cmd = exec.Command("reg", "query", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyOverride")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["override"] = string(output)
	}

	return nil
}

func (m *Manager) enableWindows() error {
	m.logger.Debug("设置Windows系统代理...")

	// 设置代理服务器
	cmd := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer", "/t", "REG_SZ", "/d", m.proxyAddress, "/f")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("设置代理服务器失败: %v", err)
	}

	// 清空代理例外列表 (ProxyOverride)
	cmd = exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyOverride", "/t", "REG_SZ", "/d", "", "/f")
	if err := cmd.Run(); err != nil {
		m.logger.Warnf("清空代理例外列表失败: %v", err)
	}

	// 启用代理
	cmd = exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("启用代理失败: %v", err)
	}

	// 通知系统代理设置已更改
	notifyProxyChangedWindows()

	return nil
}

func (m *Manager) disableWindows() error {
	m.logger.Debug("恢复Windows系统代理设置...")

	// 禁用代理
	cmd := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("禁用代理失败: %v", err)
	}

	// 通知系统代理设置已更改
	notifyProxyChangedWindows()

	return nil
}

// 通知Windows系统代理设置已更改
func notifyProxyChangedWindows() {
	// 使用PowerShell脚本通知Internet Explorer代理设置已更改
	ps := `$signature = @'
[DllImport("wininet.dll", SetLastError = true, CharSet=CharSet.Auto)]
public static extern bool InternetSetOption(IntPtr hInternet, int dwOption, IntPtr lpBuffer, int dwBufferLength);
'@
$type = Add-Type -MemberDefinition $signature -Name WinINet -Namespace PInvoke -PassThru
$INTERNET_OPTION_SETTINGS_CHANGED = 39
$INTERNET_OPTION_REFRESH = 37
$type::InternetSetOption(0, $INTERNET_OPTION_SETTINGS_CHANGED, 0, 0) | Out-Null
$type::InternetSetOption(0, $INTERNET_OPTION_REFRESH, 0, 0) | Out-Null
`
	cmd := exec.Command("powershell", "-Command", ps)
	cmd.Run()
}

// ===== Linux 实现 =====

func (m *Manager) saveSettingsLinux() error {
	m.logger.Debug("保存Linux代理设置...")

	// 获取GNOME设置
	cmd := exec.Command("gsettings", "get", "org.gnome.system.proxy", "mode")
	output, err := cmd.CombinedOutput()
	if err == nil {
		m.origSettings["mode"] = strings.TrimSpace(string(output))
	}

	cmd = exec.Command("gsettings", "get", "org.gnome.system.proxy.http", "host")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["http_host"] = strings.TrimSpace(string(output))
	}

	cmd = exec.Command("gsettings", "get", "org.gnome.system.proxy.http", "port")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["http_port"] = strings.TrimSpace(string(output))
	}

	cmd = exec.Command("gsettings", "get", "org.gnome.system.proxy.https", "host")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["https_host"] = strings.TrimSpace(string(output))
	}

	cmd = exec.Command("gsettings", "get", "org.gnome.system.proxy.https", "port")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["https_port"] = strings.TrimSpace(string(output))
	}

	// 保存忽略主机列表
	cmd = exec.Command("gsettings", "get", "org.gnome.system.proxy", "ignore-hosts")
	output, err = cmd.CombinedOutput()
	if err == nil {
		m.origSettings["ignore_hosts"] = strings.TrimSpace(string(output))
	}

	return nil
}

func (m *Manager) enableLinux() error {
	m.logger.Debug("设置Linux系统代理...")

	// 尝试GNOME设置
	host, portStr, _ := net.SplitHostPort(m.proxyAddress)

	// 设置HTTP代理
	cmd := exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "host", host)
	cmd.Run()

	cmd = exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "port", portStr)
	cmd.Run()

	// 设置HTTPS代理
	cmd = exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "host", host)
	cmd.Run()

	cmd = exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "port", portStr)
	cmd.Run()

	// 清空忽略主机列表
	cmd = exec.Command("gsettings", "set", "org.gnome.system.proxy", "ignore-hosts", "[]")
	cmd.Run()

	// 启用代理
	cmd = exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "manual")
	cmd.Run()

	// 设置环境变量
	os.Setenv("http_proxy", "http://"+m.proxyAddress)
	os.Setenv("https_proxy", "http://"+m.proxyAddress)
	os.Setenv("no_proxy", "")

	return nil
}

func (m *Manager) disableLinux() error {
	m.logger.Debug("恢复Linux系统代理设置...")

	// 禁用GNOME代理
	cmd := exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "none")
	cmd.Run()

	// 清除环境变量
	os.Unsetenv("http_proxy")
	os.Unsetenv("https_proxy")
	os.Unsetenv("no_proxy")

	return nil
}
