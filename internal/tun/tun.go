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

	"github.com/Sesame2/gotun/internal/assets"
	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"

	"golang.zx2c4.com/wireguard/tun"

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
	cfg      *config.Config
	logger   *logger.Logger
	ssh      *proxy.SSHClient
	dev      tun.Device
	stack    *stack.Stack
	endpoint *channel.Endpoint
	tunIP    string
	tunMask  string
	peerIP   string
	routes   []string
	global   bool

	ifIndex int // [新增] 用于存储 Wintun 网卡的接口索引

	closeOnce sync.Once
}

// NewTunService 创建 TUN 服务
func NewTunService(cfg *config.Config, log *logger.Logger, sshClient *proxy.SSHClient) (*TunService, error) {
	// 解析 CIDR
	ip, ipNet, err := net.ParseCIDR(cfg.TunCIDR)
	if err != nil {
		return nil, fmt.Errorf("无效的 TUN CIDR: %s (%v)", cfg.TunCIDR, err)
	}
	tunIP := ip.To4()
	if tunIP == nil {
		return nil, fmt.Errorf("只支持 IPv4 TUN 地址: %s", cfg.TunCIDR)
	}

	// 计算 Mask
	mask := net.IP(ipNet.Mask).String()

	// 计算 Peer IP (简单起见，IP+1)
	peerIP := make(net.IP, len(tunIP))
	copy(peerIP, tunIP)
	peerIP[3]++ // +1

	return &TunService{
		cfg:     cfg,
		logger:  log,
		ssh:     sshClient,
		tunIP:   tunIP.String(),
		tunMask: mask, // 内部仍使用 mask 字符串
		peerIP:  peerIP.String(),
		routes:  cfg.TunRoute,
		global:  cfg.TunGlobal,
	}, nil
}

// Start 启动 TUN 设备和协议栈
func (t *TunService) Start() error {
	// 0. (Windows Only) 释放 Wintun DLL
	if err := assets.SetupWintun(); err != nil {
		return fmt.Errorf("准备 Wintun 驱动失败: %v", err)
	}

	// 1. 创建 TUN 设备 (使用 wireguard-go)
	// 在 Windows 上，这将使用 Wintun (L3)
	// 在 macOS 上，必须使用 utun[0-9]* 格式，通常传 "utun" 会自动分配
	devName := "gotun"
	if runtime.GOOS == "darwin" {
		devName = "utun"
	}

	dev, err := tun.CreateTUN(devName, 1500)
	if err != nil {
		return fmt.Errorf("创建 TUN 设备失败: %v", err)
	}
	t.dev = dev

	realName, err := dev.Name()
	if err == nil {
		t.logger.Infof("[TUN] 设备已创建: %s", realName)
	} else {
		realName = "gotun"
	}

	// 获取网卡索引 (Windows 特有)
	if runtime.GOOS == "windows" {
		iface, err := net.InterfaceByName(realName)
		if err == nil {
			t.ifIndex = iface.Index
		} else {
			// 如果 CreateTUN 返回的名字和系统里的不一致，尝试模糊匹配
			t.logger.Warnf("按名称 %s 查找接口失败，尝试遍历查找...", realName)
			ifaces, _ := net.Interfaces()
			for _, i := range ifaces {
				// Wintun 驱动显示的适配器描述通常包含 WireGuard 或 Tun
				// 但 InterfaceByName 通常匹配的是 Connection Name (如 'gotun')
				if i.Name == realName {
					t.ifIndex = i.Index
					break
				}
			}
		}

		if t.ifIndex > 0 {
			t.logger.Infof("[TUN] 获取到网卡索引 (IF): %d", t.ifIndex)
		} else {
			t.logger.Warn("[TUN] 警告: 未能获取网卡索引，路由配置可能会失败")
		}
	}

	// 2. 配置 TUN 网卡 IP (需调用系统命令)
	if err := t.setupTunIP(realName); err != nil {
		dev.Close()
		return fmt.Errorf("配置 TUN IP 失败: %v", err)
	}

	// 检测路由冲突
	t.checkRouteConflicts()

	// 2.5 配置路由
	if t.global {
		if err := t.setupGlobalRoutes(realName); err != nil {
			t.logger.Warnf("[TUN] 配置全局路由失败: %v", err)
		}
	} else if len(t.routes) > 0 {
		if err := t.setupRoutes(realName); err != nil {
			t.logger.Warnf("[TUN] 配置路由部分失败: %v", err)
		}
	}

	// 配置别名路由 (Subnet/IP Mapping)
	for _, sas := range t.cfg.SubnetAliases {
		cidr := sas.Src.String()
		t.logger.Infof("[TUN] 添加别名路由: %s -> TUN", cidr)
		if err := t.addRoute(cidr, t.tunIP, realName); err != nil {
			t.logger.Warnf("[TUN] 添加别名路由失败 %s: %v", cidr, err)
		}
	}

	// 3. 初始化 gVisor 用户态协议栈
	t.initNetstack()

	// 4. 启动数据泵
	go t.pumpTunToStack()
	go t.pumpStackToTun()

	t.logger.Infof("[TUN] 模式启动成功! IP: %s Peer: %s", t.tunIP, t.peerIP)

	return nil
}

// Close 关闭服务
func (t *TunService) Close() error {
	t.closeOnce.Do(func() {
		if t.dev != nil {
			t.dev.Close()
		}
		if t.stack != nil {
			t.stack.Close()
		}
	})
	return nil
}

// initNetstack 初始化 gVisor 协议栈
func (t *TunService) initNetstack() {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	e := channel.New(256, 1500, "")
	t.endpoint = e

	if err := s.CreateNIC(1, e); err != nil {
		t.logger.Fatalf("[TUN] 创建 NIC 失败: %v", err)
	}

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
		t.logger.Fatalf("[TUN] 添加协议地址失败: %v", err)
	}

	if err := s.SetPromiscuousMode(1, true); err != nil {
		t.logger.Fatalf("设置混杂模式失败: %v", err)
	}
	if err := s.SetSpoofing(1, true); err != nil {
		t.logger.Fatalf("设置 Spoofing 失败: %v", err)
	}

	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         1,
		},
	})

	// TCP Handler
	tcpHandler := tcp.NewForwarder(s, 0, 10, func(r *tcp.ForwarderRequest) {
		id := r.ID()
		destIP := id.LocalAddress.String()
		destPort := id.LocalPort

		// --- 地址重写逻辑 (NAT) ---
		targetHost := destIP
		parsedDestIP := net.ParseIP(destIP)

		if parsedDestIP != nil {
			parsedDestIP = parsedDestIP.To4() // Ensure IPv4
			if parsedDestIP != nil {
				for _, rule := range t.cfg.SubnetAliases {
					if rule.Src.Contains(parsedDestIP) {
						// 计算偏移量: destIP - rule.Src.IP
						offset := ipSub(parsedDestIP, rule.Src.IP)
						// 计算新目标: rule.Dst.IP + offset
						realTargetIP := ipAdd(rule.Dst.IP, offset)

						targetHost = realTargetIP.String()
						t.logger.Infof("[TUN] 命中 NAT 规则: %s -> %s (Offset: %d)", destIP, targetHost, offset)
						break
					}
				}
			}
		}

		targetAddr := fmt.Sprintf("%s:%d", targetHost, destPort)
		// ------------------------

		t.logger.Infof("[TUN] 收到 TCP 连接请求 -> %s (原始目标: %s:%d)", targetAddr, destIP, destPort)
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

	// UDP Handler (DNS)
	udpHandler := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		id := r.ID()
		if id.LocalPort != 53 {
			return false
		}

		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			t.logger.Errorf("[TUN] 创建 UDP Endpoint 失败: %v", err)
			return true
		}

		localConn := gonet.NewUDPConn(&wq, ep)
		go t.handleUDPForward(localConn, id.LocalAddress.String(), id.LocalPort)
		return true
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)

	t.stack = s
}

// handleUDPForward (DNS)
func (t *TunService) handleUDPForward(conn *gonet.UDPConn, targetIP string, targetPort uint16) {
	defer conn.Close()
	buf := make([]byte, 2048)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return
	}
	dnsQuery := buf[:n]

	tcpQuery := make([]byte, 2+len(dnsQuery))
	binary.BigEndian.PutUint16(tcpQuery[0:2], uint16(len(dnsQuery)))
	copy(tcpQuery[2:], dnsQuery)

	targetAddr := fmt.Sprintf("%s:%d", targetIP, targetPort)
	remoteConn, err := t.ssh.Dial("tcp", targetAddr)
	if err != nil {
		t.logger.Warnf("[TUN] 连接远程 DNS 失败 %s: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	if _, err := remoteConn.Write(tcpQuery); err != nil {
		return
	}
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(remoteConn, lenBuf); err != nil {
		return
	}
	respLen := binary.BigEndian.Uint16(lenBuf)
	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(remoteConn, respBuf); err != nil {
		return
	}
	conn.Write(respBuf)
}

// handleTCPForward (Traffic)
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
	// WireGuard tun Read 使用 Batch API
	const batchSize = 1
	bufs := make([][]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		bufs[i] = make([]byte, 1600)
	}
	sizes := make([]int, batchSize)

	// offset 在 Windows (Wintun) 上通常是 0
	// 在 Unix (macOS/Linux) 上，WireGuard 实现通常需要 4 字节 offset 用于处理 PI Header
	offset := 0
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		offset = 4
	}

	for {
		n, err := t.dev.Read(bufs, sizes, offset)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") || strings.Contains(err.Error(), "closed network connection") {
				return
			}
			t.logger.Errorf("[TUN] 读取设备失败: %v", err)
			return
		}

		for i := 0; i < n; i++ {
			size := sizes[i]
			data := bufs[i][offset : offset+size]

			packetBuf := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(data),
			})
			t.endpoint.InjectInbound(header.IPv4ProtocolNumber, packetBuf)
		}
	}
}

// pumpStackToTun 将 gVisor Stack 的输出写入 TUN 设备
func (t *TunService) pumpStackToTun() {
	offset := 0
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		offset = 4
	}

	for {
		pkt := t.endpoint.Read()
		if pkt == nil {
			continue
		}
		views := pkt.ToView().ToSlice()
		pkt.DecRef()

		// WireGuard Write 也是 batch 接口
		// 我们需要为 offset 预留空间
		buf := make([]byte, offset+len(views))
		copy(buf[offset:], views)

		_, err := t.dev.Write([][]byte{buf}, offset)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") || strings.Contains(err.Error(), "closed network connection") {
				return
			}
			t.logger.Errorf("[TUN] 写入设备失败: %v", err)
			return
		}
	}
}

// setupTunIP 配置网卡 IP
func (t *TunService) setupTunIP(devName string) error {
	t.logger.Infof("[TUN] 正在配置 %s IP: %s (Peer: %s)", devName, t.tunIP, t.peerIP)

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
		// Windows Wintun 配置
		cmd = exec.Command("netsh", "interface", "ip", "set", "address",
			fmt.Sprintf("name=%s", devName),
			"source=static",
			fmt.Sprintf("addr=%s", t.tunIP),
			fmt.Sprintf("mask=%s", t.tunMask),
		)
	default:
		return fmt.Errorf("不支持的操作系统")
	}

	if cmd != nil {
		output, err := cmd.CombinedOutput()
		if err != nil {
			outputStr := string(output)
			// Windows 下如果 IP 已存在，netsh 可能报错 "对象已存在" 或 "Object already exists"
			if runtime.GOOS == "windows" {
				if strings.Contains(outputStr, "Object already exists") || strings.Contains(outputStr, "对象已存在") {
					t.logger.Warnf("[TUN] Windows IP 配置提示: %s (视为成功)", strings.TrimSpace(outputStr))
					return nil
				}

				// 双重检查：尝试检查是否实际上已经配置成功
				checkCmd := exec.Command("netsh", "interface", "ip", "show", "address", fmt.Sprintf("name=%s", devName))
				checkOut, checkErr := checkCmd.CombinedOutput()
				if checkErr == nil && strings.Contains(string(checkOut), t.tunIP) {
					t.logger.Warnf("[TUN] 配置 IP 命令返回错误，但检测到 IP 已存在，忽略错误: %v", err)
					return nil
				}
			}
			return fmt.Errorf("执行命令失败: %s, %v", outputStr, err)
		}
	}
	return nil
}

// setupRoutes 配置路由
func (t *TunService) setupRoutes(devName string) error {
	t.logger.Infof("[TUN] 正在配置路由: %v", t.routes)
	for _, cidr := range t.routes {
		if err := t.addRoute(cidr, t.tunIP, devName); err != nil {
			t.logger.Errorf("[TUN] 添加路由失败 %s: %v", cidr, err)
		}
	}
	return nil
}

// setupGlobalRoutes 配置全局路由
func (t *TunService) setupGlobalRoutes(devName string) error {
	t.logger.Info("[TUN] 正在配置全局路由...")
	gateway, err := t.getDefaultGateway()
	if err != nil {
		return fmt.Errorf("无法获取默认网关: %v", err)
	}
	t.logger.Infof("[TUN] 检测到默认网关: %s", gateway)

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
	t.logger.Infof("[TUN] 为 SSH 服务器 %s (%s) 添加绕过路由 via %s", sshHost, targetSSH_IP, gateway)

	if err := t.addRoute(targetSSH_IP, gateway, ""); err != nil {
		return fmt.Errorf("添加 SSH 绕过路由失败: %v", err)
	}

	t.logger.Info("[TUN] 添加全局覆盖路由 (0.0.0.0/1, 128.0.0.0/1)...")
	if err := t.addRoute("0.0.0.0/1", t.tunIP, devName); err != nil {
		return fmt.Errorf("添加 0.0.0.0/1 路由失败: %v", err)
	}
	if err := t.addRoute("128.0.0.0/1", t.tunIP, devName); err != nil {
		return fmt.Errorf("添加 128.0.0.0/1 路由失败: %v", err)
	}
	return nil
}

// addRoute 添加路由
func (t *TunService) addRoute(target, gateway, devName string) error {
	var cmd *exec.Cmd

	// Windows 解析 CIDR
	var destIP, mask string
	if runtime.GOOS == "windows" {
		ip, network, err := net.ParseCIDR(target)
		if err == nil {
			destIP = ip.String()
			mask = net.IP(network.Mask).String()
		} else {
			destIP = target
			mask = "255.255.255.255"
		}
	}

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
	case "windows":
		// Windows: Wintun 是 L3
		// 1. 先删 (忽略错误)
		exec.Command("route", "delete", destIP).Run()

		// 2. 准备添加命令
		isTunRoute := false
		routeGw := gateway

		// 如果网关是 "0.0.0.0" 或 tunIP 或 peerIP，说明是要进 TUN
		if gateway == "0.0.0.0" || gateway == t.tunIP || gateway == t.peerIP {
			isTunRoute = true
			routeGw = "0.0.0.0" // Wintun 标准网关
		}

		// 强制 METRIC 1 以提高优先级
		args := []string{"add", destIP, "mask", mask, routeGw, "METRIC", "1"}

		// 【关键修复】如果是 TUN 路由，必须指定 IF 索引
		if isTunRoute && t.ifIndex > 0 {
			args = append(args, "IF", fmt.Sprintf("%d", t.ifIndex))
		}

		cmd = exec.Command("route", args...)
	default:
		return fmt.Errorf("不支持的操作系统")
	}

	t.logger.Infof("[TUN] 执行路由命令: %s", cmd.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		outStr := string(output)
		// 处理 "路由添加失败: 对象已存在"
		if strings.Contains(outStr, "File exists") || strings.Contains(outStr, "exist") || strings.Contains(outStr, "已存在") {
			t.logger.Warnf("[TUN] 路由已存在，忽略错误: %s", outStr)
			return nil
		}
		return fmt.Errorf("cmd: %s, output: %s, err: %v", cmd.String(), outStr, err)
	}
	return nil
}

// getDefaultGateway (同上)
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
	case "windows":
		out, err := exec.Command("route", "print", "0.0.0.0").Output()
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "0.0.0.0") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					return fields[2], nil
				}
			}
		}
	}
	return "", fmt.Errorf("未找到默认网关")
}

// checkRouteConflicts 检查请求的路由是否与本机物理网卡冲突
func (t *TunService) checkRouteConflicts() {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.logger.Warnf("[TUN] 无法获取本机网卡信息，跳过冲突检测: %v", err)
		return
	}

	sshHost := t.cfg.SSHServer
	if host, _, err := net.SplitHostPort(sshHost); err == nil {
		sshHost = host
	}
	sshIPs, _ := net.LookupIP(sshHost)

	// check conflict with t.routes & SubnetAliases
	checkConflict := func(targetCIDR string, targetName string) {
		_, network, err := net.ParseCIDR(targetCIDR)
		if err != nil {
			return
		}

		// 1. 检查 SSH Server 死循环
		for _, sshIP := range sshIPs {
			sshIPV4 := sshIP.To4()
			if sshIPV4 != nil && network.Contains(sshIPV4) {
				t.logger.Fatalf("[TUN] ❌ 致命错误: SSH 服务器 IP %s 包含在路由网段 %s 中！这将导致死循环 (SSH 流量被 TUN 拦截)。请调整路由或别名设置。", sshIPV4, targetCIDR)
			}
		}

		// 2. 检查本机网卡冲突
		for _, iface := range ifaces {
			// 跳过 Loopback 和 Down 的接口
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				ip = ip.To4()
				if ip == nil || ip.IsLoopback() {
					continue
				}

				if network.Contains(ip) {
					t.logger.Warnf("[TUN] ⚠️ 路由冲突警告: 请求的路由 %s 包含了本机网卡 %s 的 IP %s。这可能导致流量优先走物理网卡而跳过 TUN，导致代理不生效！", targetCIDR, iface.Name, ip.String())
				}
			}
		}
	}

	for _, route := range t.routes {
		checkConflict(route, "User Route")
	}
	for _, alias := range t.cfg.SubnetAliases {
		checkConflict(alias.Src.String(), "Alias Route")
	}
}

// Helper functions for IP arithmetic
func ipToUint32(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}

func ipAdd(ip net.IP, offset uint32) net.IP {
	val := ipToUint32(ip)
	return uint32ToIP(val + offset)
}

func ipSub(a, b net.IP) uint32 {
	return ipToUint32(a) - ipToUint32(b)
}
