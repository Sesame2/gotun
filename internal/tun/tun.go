package tun

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/songgao/water"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// TunService 管理 TUN 设备和用户态协议栈
type TunService struct {
	cfg       *config.Config
	logger    *logger.Logger
	ssh       *proxy.SSHClient
	ifce      *water.Interface
	stack     *stack.Stack
	endpoint  *channel.Endpoint
	tunIP     string // 本地 TUN IP，例如 10.0.0.1
	tunMask   string // 子网掩码，例如 255.255.255.0
	peerIP    string // 对端 IP (macOS 需要)
	routes    []string
	global    bool // 是否开启全局模式
	closeOnce sync.Once
}

// NewTunService 创建 TUN 服务
func NewTunService(cfg *config.Config, log *logger.Logger, sshClient *proxy.SSHClient) (*TunService, error) {
	// 计算 Peer IP (简单起见，IP+1)
	tunIP := net.ParseIP(cfg.TunAddr).To4()
	if tunIP == nil {
		return nil, fmt.Errorf("无效的 TUN IP: %s", cfg.TunAddr)
	}
	peerIP := make(net.IP, len(tunIP))
	copy(peerIP, tunIP)
	peerIP[3]++ // +1

	return &TunService{
		cfg:     cfg,
		logger:  log,
		ssh:     sshClient,
		tunIP:   cfg.TunAddr,
		tunMask: cfg.TunMask,
		peerIP:  peerIP.String(),
		routes:  cfg.TunRoutes,
		global:  cfg.TunGlobal,
	}, nil
}

// Start 启动 TUN 设备和协议栈
func (t *TunService) Start() error {
	// 1. 创建 TUN 设备
	config := water.Config{
		DeviceType: water.TUN,
	}
	ifce, err := water.New(config)
	if err != nil {
		return fmt.Errorf("创建 TUN 设备失败: %v", err)
	}
	t.ifce = ifce
	t.logger.Infof("TUN 设备已创建: %s", ifce.Name())

	// 2. 配置 TUN 网卡 IP (需调用系统命令)
	if err := t.setupTunIP(ifce.Name()); err != nil {
		ifce.Close()
		return fmt.Errorf("配置 TUN IP 失败: %v", err)
	}

	// 2.5 配置路由
	if t.global {
		if err := t.setupGlobalRoutes(ifce.Name()); err != nil {
			t.logger.Warnf("配置全局路由失败: %v", err)
		}
	} else if len(t.routes) > 0 {
		if err := t.setupRoutes(ifce.Name()); err != nil {
			t.logger.Warnf("配置路由部分失败: %v", err)
		}
	}

	// 3. 初始化 gVisor 用户态协议栈
	t.initNetstack()

	// 4. 启动数据泵 (Pump): TUN <-> Netstack
	go t.pumpTunToStack()
	go t.pumpStackToTun()

	t.logger.Infof("TUN 模式启动成功! IP: %s", t.tunIP)
	t.logger.Infof("提示: 请手动添加路由，例如: route add 192.168.x.x mask 255.255.255.0 %s", t.tunIP)

	return nil
}

// Close 关闭服务
func (t *TunService) Close() error {
	t.closeOnce.Do(func() {
		if t.ifce != nil {
			t.ifce.Close()
		}
		if t.stack != nil {
			t.stack.Close()
		}
	})
	return nil
}

// initNetstack 初始化 gVisor 协议栈
func (t *TunService) initNetstack() {
	// 创建一个新的网络栈，支持 IPv4, TCP, UDP
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	// 创建一个链路层端点 (Channel)，连接 TUN 和 Stack
	e := channel.New(256, 1500, "")
	t.endpoint = e

	// 将链路端点绑定到协议栈的 NIC 1
	if err := s.CreateNIC(1, e); err != nil {
		t.logger.Fatalf("创建 NIC 失败: %v", err)
	}

	// 在协议栈上添加地址 (就是 TUN 的 IP)
	parsedIP := net.ParseIP(t.tunIP)
	addr := tcpip.AddrFromSlice(parsedIP.To4())
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   addr,
			PrefixLen: 24, // 对应 255.255.255.0
		},
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		t.logger.Fatalf("添加协议地址失败: %v", err)
	}

	// 设置默认路由表
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         1,
		},
	})

	// 关键：设置 TCP 转发处理器
	tcpHandler := tcp.NewForwarder(s, 0, 10, func(r *tcp.ForwarderRequest) {
		id := r.ID()
		targetAddr := fmt.Sprintf("%s:%d", id.LocalAddress.String(), id.LocalPort)
		t.logger.Infof("[TUN] 收到 TCP 连接请求 -> %s", targetAddr)
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			t.logger.Errorf("创建 TCP Endpoint 失败: %v", err)
			r.Complete(true)
			return
		}
		r.Complete(false)
		localConn := gonet.NewTCPConn(&wq, ep)
		go t.handleTCPForward(localConn, targetAddr)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpHandler.HandlePacket)

	// 设置 UDP 转发处理器 (主要用于 DNS)
	udpHandler := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		id := r.ID()
		// 只处理 DNS (53)
		if id.LocalPort != 53 {
			return false
		}

		// 创建 UDP 端点
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			t.logger.Errorf("创建 UDP Endpoint 失败: %v", err)
			return true
		}

		// 这里的 localConn 代表发来 UDP 包的“本地程序”
		localConn := gonet.NewUDPConn(&wq, ep)

		// 异步处理
		go t.handleUDPForward(localConn, id.LocalAddress.String(), id.LocalPort)
		return true
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)

	t.stack = s
}

// handleUDPForward 处理 UDP 转发 (目前只支持 DNS)
func (t *TunService) handleUDPForward(conn *gonet.UDPConn, targetIP string, targetPort uint16) {
	defer conn.Close()

	// 读取 UDP 数据
	buf := make([]byte, 2048)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return
	}
	dnsQuery := buf[:n]

	// 转换为 TCP DNS (Length Prefixed)
	tcpQuery := make([]byte, 2+len(dnsQuery))
	binary.BigEndian.PutUint16(tcpQuery[0:2], uint16(len(dnsQuery)))
	copy(tcpQuery[2:], dnsQuery)

	// 通过 SSH 连接远程 DNS (使用 TCP)
	targetAddr := fmt.Sprintf("%s:%d", targetIP, targetPort)

	remoteConn, err := t.ssh.Dial("tcp", targetAddr)
	if err != nil {
		t.logger.Warnf("连接远程 DNS 失败 %s: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	// 发送 TCP DNS 查询
	if _, err := remoteConn.Write(tcpQuery); err != nil {
		return
	}

	// 读取响应
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(remoteConn, lenBuf); err != nil {
		return
	}
	respLen := binary.BigEndian.Uint16(lenBuf)

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(remoteConn, respBuf); err != nil {
		return
	}

	// 响应通过 UDP 发回给本地
	conn.Write(respBuf)
}

// handleTCPForward 处理 TCP 转发逻辑
func (t *TunService) handleTCPForward(localConn net.Conn, targetAddr string) {
	defer localConn.Close()

	remoteConn, err := t.ssh.Dial("tcp", targetAddr)
	if err != nil {
		t.logger.Warnf("[TUN] 连接目标失败 %s: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	t.logger.Infof("[TUN] 隧道建立: %s <-> %s", localConn.RemoteAddr(), targetAddr)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(remoteConn, localConn)
		if c, ok := remoteConn.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(localConn, remoteConn)
		if c, ok := localConn.(*net.TCPConn); ok {
			c.CloseWrite()
		}
	}()

	wg.Wait()
}

// pumpTunToStack 将 TUN 设备读取的数据写入 gVisor Stack
func (t *TunService) pumpTunToStack() {
	buf := make([]byte, 2048)
	for {
		n, err := t.ifce.Read(buf)
		if err != nil {
			t.logger.Errorf("读取 TUN 失败: %v", err)
			return
		}
		packetBuf := stack.NewPacketBuffer(stack.PacketBufferOptions{
			Payload: buffer.MakeWithData(buf[:n]),
		})
		t.endpoint.InjectInbound(header.IPv4ProtocolNumber, packetBuf)
	}
}

// pumpStackToTun 将 gVisor Stack 的输出写入 TUN 设备
func (t *TunService) pumpStackToTun() {
	for {
		pkt := t.endpoint.Read()
		if pkt == nil {
			continue
		}
		views := pkt.ToView().ToSlice()
		_, err := t.ifce.Write(views)
		pkt.DecRef()
		if err != nil {
			t.logger.Errorf("写入 TUN 失败: %v", err)
			return
		}
	}
}

// setupTunIP 配置网卡 IP
func (t *TunService) setupTunIP(devName string) error {
	t.logger.Infof("正在配置 %s IP: %s (Peer: %s)", devName, t.tunIP, t.peerIP)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("ifconfig", devName, t.tunIP, t.peerIP, "up")
	case "linux":
		err := exec.Command("ip", "addr", "add", fmt.Sprintf("%s/24", t.tunIP), "dev", devName).Run()
		if err != nil {
			return err
		}
		cmd = exec.Command("ip", "link", "set", devName, "up")
	case "windows":
		t.logger.Warn("Windows 下自动配置 IP 尚未完全实现，请手动配置网卡 IP")
		return nil
	default:
		return fmt.Errorf("不支持的操作系统")
	}

	if cmd != nil {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("执行命令失败: %s, %v", string(output), err)
		}
	}
	return nil
}

// setupRoutes 配置路由
func (t *TunService) setupRoutes(devName string) error {
	t.logger.Infof("正在配置路由: %v", t.routes)

	for _, cidr := range t.routes {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("route", "add", cidr, t.tunIP)
		case "linux":
			cmd = exec.Command("ip", "route", "add", cidr, "via", t.tunIP)
		default:
			return fmt.Errorf("不支持的操作系统")
		}

		if output, err := cmd.CombinedOutput(); err != nil {
			t.logger.Errorf("添加路由失败 %s: %v, output: %s", cidr, err, string(output))
			return fmt.Errorf("添加路由失败: %s", cidr)
		}
	}
	return nil
}

// setupGlobalRoutes 配置全局路由
func (t *TunService) setupGlobalRoutes(devName string) error {
	t.logger.Info("正在配置全局路由...")

	// 1. 获取默认网关
	gateway, err := t.getDefaultGateway()
	if err != nil {
		return fmt.Errorf("无法获取默认网关: %v", err)
	}
	t.logger.Infof("检测到默认网关: %s", gateway)

	// 2. 绕过 SSH 服务器 IP (走物理网关)
	sshHost := t.cfg.SSHServer
	if host, _, err := net.SplitHostPort(sshHost); err == nil {
		sshHost = host
	}

	sshIPs, err := net.LookupIP(sshHost)
	if err != nil {
		return fmt.Errorf("无法解析 SSH 服务器 IP: %v", err)
	}
	if len(sshIPs) == 0 {
		return fmt.Errorf("SSH 服务器 IP 解析为空")
	}
	targetSSH_IP := sshIPs[0].String()
	t.logger.Infof("为 SSH 服务器 %s (%s) 添加绕过路由 via %s", sshHost, targetSSH_IP, gateway)

	if err := t.addRoute(targetSSH_IP, gateway, ""); err != nil {
		return fmt.Errorf("添加 SSH 绕过路由失败: %v", err)
	}

	// 3. 添加 0.0.0.0/1 和 128.0.0.0/1 指向 TUN (覆盖默认路由)
	t.logger.Info("添加全局覆盖路由 (0.0.0.0/1, 128.0.0.0/1)...")
	if err := t.addRoute("0.0.0.0/1", t.tunIP, devName); err != nil {
		return fmt.Errorf("添加 0.0.0.0/1 路由失败: %v", err)
	}
	if err := t.addRoute("128.0.0.0/1", t.tunIP, devName); err != nil {
		return fmt.Errorf("添加 128.0.0.0/1 路由失败: %v", err)
	}

	return nil
}

// addRoute 添加路由 (目标 -> 网关/设备)
func (t *TunService) addRoute(target, gateway, devName string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if (gateway == t.tunIP || gateway == t.peerIP) && devName != "" {
			cmd = exec.Command("route", "add", target, "-interface", devName)
		} else {
			cmd = exec.Command("route", "add", target, gateway)
		}
	case "linux":
		args := []string{"route", "add", target, "via", gateway}
		cmd = exec.Command("ip", args...)
	default:
		return fmt.Errorf("不支持的操作系统")
	}

	t.logger.Infof("执行路由命令: %s", cmd.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		outStr := string(output)
		if strings.Contains(outStr, "File exists") || strings.Contains(outStr, "exist") {
			return nil
		}
		return fmt.Errorf("cmd: %s, output: %s, err: %v", cmd.String(), outStr, err)
	}
	return nil
}

// getDefaultGateway 获取系统默认网关
func (t *TunService) getDefaultGateway() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("route", "-n", "get", "default").Output()
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "gateway:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					return parts[1], nil
				}
			}
		}
	case "linux":
		out, err := exec.Command("ip", "route", "show", "default").Output()
		if err != nil {
			return "", err
		}
		parts := strings.Fields(string(out))
		if len(parts) >= 3 && parts[0] == "default" && parts[1] == "via" {
			return parts[2], nil
		}
	}
	return "", fmt.Errorf("未找到默认网关")
}
