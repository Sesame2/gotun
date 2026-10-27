package tun

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

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

type sshConnection interface {
	Dial(string, string) (net.Conn, error)
	RemoteAddr() net.Addr
}

type routeUndo struct {
	command string
	args    []string
}

// TunService 管理 TUN 设备和用户态协议栈
type TunService struct {
	cfg       *config.Config
	logger    *logger.Logger
	ssh       sshConnection
	dev       tun.Device
	stack     *stack.Stack
	endpoint  *channel.Endpoint
	tunIP     string
	tunMask   string
	tunPrefix int
	peerIP    string
	routes    []string
	global    bool

	ifIndex int // [新增] 用于存储 Wintun 网卡的接口索引

	platform        string
	run             func(string, ...string) ([]byte, error)
	createDevice    func(string, int) (tun.Device, error)
	setupDriver     func() error
	interfaceByName func(string) (*net.Interface, error)
	undoRoutes      []routeUndo
	cleaning        bool
	started         bool
	lifecycleMu     sync.Mutex
	deviceClosed    bool
	stackClosed     bool
}

// NewTunService 创建 TUN 服务
func NewTunService(cfg *config.Config, log *logger.Logger, sshClient *proxy.SSHClient) (*TunService, error) {
	if cfg == nil {
		return nil, fmt.Errorf("TUN 配置不能为空")
	}
	if log == nil {
		log = logger.NewLogger(false)
	}
	// 解析 CIDR
	ip, ipNet, err := net.ParseCIDR(cfg.TunCIDR)
	if err != nil {
		return nil, fmt.Errorf("无效的 TUN CIDR: %s (%v)", cfg.TunCIDR, err)
	}
	tunIP := ip.To4()
	_, bits := ipNet.Mask.Size()
	if tunIP == nil || bits != 32 {
		return nil, fmt.Errorf("只支持 IPv4 TUN 地址: %s", cfg.TunCIDR)
	}

	// 计算 Mask
	mask := net.IP(ipNet.Mask).String()
	prefix, _ := ipNet.Mask.Size()

	// 计算 Peer IP (简单起见，IP+1)
	peerIP := ipAdd(tunIP, 1)
	for _, alias := range cfg.SubnetAliases {
		if alias.Src == nil || alias.Dst == nil {
			return nil, fmt.Errorf("TUN 子网映射不能为空")
		}
		if _, err := config.ParseSubnetAlias(alias.Src.String() + ":" + alias.Dst.String()); err != nil {
			return nil, err
		}
	}

	service := &TunService{
		cfg:             cfg,
		logger:          log,
		platform:        runtime.GOOS,
		createDevice:    tun.CreateTUN,
		setupDriver:     assets.SetupWintun,
		interfaceByName: net.InterfaceByName,
		tunIP:           tunIP.String(),
		tunMask:         mask, // 内部仍使用 mask 字符串
		tunPrefix:       prefix,
		peerIP:          peerIP.String(),
		routes:          cfg.TunRoute,
		global:          cfg.TunGlobal,
	}
	service.run = service.runCommand
	if sshClient != nil {
		service.ssh = sshClient
	}
	return service, nil
}

// Start 启动 TUN 设备和协议栈
func (t *TunService) Start() (err error) {
	t.lifecycleMu.Lock()
	defer t.lifecycleMu.Unlock()
	if t.started || t.dev != nil || t.stackClosed {
		return fmt.Errorf("TUN 服务已启动或关闭，请创建新实例")
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, t.closeLocked())
		}
	}()
	if err := t.contextError(); err != nil {
		return err
	}
	if err := t.setupDriver(); err != nil {
		return fmt.Errorf("准备 Wintun 驱动失败: %w", err)
	}
	devName := "gotun"
	if t.platform == "darwin" {
		devName = "utun"
	}
	dev, err := t.createDevice(devName, 1500)
	if err != nil {
		return fmt.Errorf("创建 TUN 设备失败: %w", err)
	}
	t.dev = dev
	realName, err := dev.Name()
	if err != nil {
		return fmt.Errorf("获取 TUN 设备名称失败: %w", err)
	}
	if t.platform == "windows" {
		iface, err := t.interfaceByName(realName)
		if err != nil || iface.Index <= 0 {
			return fmt.Errorf("获取 TUN 接口索引失败: %v", err)
		}
		t.ifIndex = iface.Index
	}
	if err := t.setupTunIP(realName); err != nil {
		return err
	}
	if err := t.contextError(); err != nil {
		return err
	}
	if err := t.checkRouteConflicts(); err != nil {
		return err
	}
	if t.global {
		if err := t.setupGlobalRoutes(realName); err != nil {
			return err
		}
	} else if err := t.setupRoutes(realName); err != nil {
		return err
	}
	for _, alias := range t.cfg.SubnetAliases {
		if err := t.addRoute(alias.Src.String(), t.tunIP, realName, t.ifIndex); err != nil {
			return err
		}
	}
	if err := t.contextError(); err != nil {
		return err
	}
	if err := t.initNetstack(); err != nil {
		return err
	}
	go t.pumpTunToStack()
	go t.pumpStackToTun()
	t.started = true
	t.logger.Infof("[TUN] 模式启动成功! IP: %s Peer: %s", t.tunIP, t.peerIP)
	return nil
}

// Close retains failed cleanup so the same instance can retry it.
func (t *TunService) Close() error {
	t.lifecycleMu.Lock()
	defer t.lifecycleMu.Unlock()
	return t.closeLocked()
}

func (t *TunService) closeLocked() error {
	t.cleaning = true
	defer func() { t.cleaning = false }()
	var failures []error
	remaining := make([]routeUndo, 0, len(t.undoRoutes))
	for i := len(t.undoRoutes) - 1; i >= 0; i-- {
		undo := t.undoRoutes[i]
		if output, err := t.runRoute(undo.command, undo.args...); err != nil {
			message := strings.ToLower(string(output))
			if strings.Contains(message, "not in table") || strings.Contains(message, "no such process") || strings.Contains(message, "element not found") || strings.Contains(message, "找不到元素") {
				continue // The OS may remove an interface's routes before explicit cleanup.
			}
			failures = append(failures, fmt.Errorf("撤销 TUN 路由失败: %s: %w", output, err))
			remaining = append([]routeUndo{undo}, remaining...)
		}
	}
	t.undoRoutes = remaining
	if err := errors.Join(failures...); err != nil {
		return err
	}
	if t.dev != nil && !t.deviceClosed {
		if err := t.dev.Close(); err != nil {
			return fmt.Errorf("关闭 TUN 设备失败: %w", err)
		}
		t.deviceClosed = true
	}
	if !t.stackClosed {
		if t.endpoint != nil {
			t.endpoint.Close()
		}
		if t.stack != nil {
			t.stack.Close()
		}
		t.stackClosed = true
	}
	return nil
}

// initNetstack 初始化 gVisor 协议栈
func (t *TunService) initNetstack() error {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	t.stack = s
	e := channel.New(256, 1500, "")
	t.endpoint = e

	if err := s.CreateNIC(1, e); err != nil {
		return fmt.Errorf("创建 TUN NIC 失败: %v", err)
	}

	parsedIP := net.ParseIP(t.tunIP)
	addr := tcpip.AddrFromSlice(parsedIP.To4())
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   addr,
			PrefixLen: t.tunPrefix,
		},
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		return fmt.Errorf("添加 TUN 协议地址失败: %v", err)
	}

	if err := s.SetPromiscuousMode(1, true); err != nil {
		return fmt.Errorf("设置 TUN 混杂模式失败: %v", err)
	}
	if err := s.SetSpoofing(1, true); err != nil {
		return fmt.Errorf("设置 TUN Spoofing 失败: %v", err)
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

	return nil
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
			packetBuf.DecRef()
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
		pkt := t.endpoint.ReadContext(context.Background())
		if pkt == nil {
			return
		}
		view := pkt.ToView()
		pkt.DecRef()

		// WireGuard Write 也是 batch 接口
		// 我们需要为 offset 预留空间
		buf := make([]byte, offset+view.Size())
		copy(buf[offset:], view.AsSlice())
		view.Release()

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
	var name string
	var args []string
	switch t.platform {
	case "darwin":
		name, args = "ifconfig", []string{devName, t.tunIP, t.peerIP, "up"}
	case "linux":
		if output, err := t.run("ip", "addr", "add", fmt.Sprintf("%s/%d", t.tunIP, t.tunPrefix), "dev", devName); err != nil {
			return fmt.Errorf("配置 TUN IP 失败: %s: %w", output, err)
		}
		name, args = "ip", []string{"link", "set", devName, "up"}
	case "windows":
		name, args = "netsh", []string{"interface", "ip", "set", "address", "name=" + devName, "source=static", "addr=" + t.tunIP, "mask=" + t.tunMask}
	default:
		return fmt.Errorf("不支持的操作系统: %s", t.platform)
	}
	if output, err := t.run(name, args...); err != nil {
		return fmt.Errorf("配置 TUN IP 失败: %s: %w", output, err)
	}
	return nil
}

func (t *TunService) setupRoutes(devName string) error {
	for _, cidr := range t.routes {
		if err := t.addRoute(cidr, t.tunIP, devName, t.ifIndex); err != nil {
			return err
		}
	}
	return nil
}

func (t *TunService) setupGlobalRoutes(devName string) error {
	if t.ssh == nil || t.ssh.RemoteAddr() == nil {
		return fmt.Errorf("无法获取 SSH 底层连接地址")
	}
	host, _, err := net.SplitHostPort(t.ssh.RemoteAddr().String())
	if err != nil {
		return fmt.Errorf("无效的 SSH 底层连接地址: %w", err)
	}
	host, _, _ = strings.Cut(host, "%")
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("无效的 SSH 底层连接 IP: %s", host)
	}
	// IPv4 coverage routes cannot capture loopback or an IPv6 SSH transport.
	if ip.To4() != nil && !ip.IsLoopback() {
		gateway, iface, index, err := t.getSSHRoute(ip.String())
		if err != nil {
			return err
		}
		if err := t.addRoute(ip.String(), gateway, iface, index); err != nil {
			return err
		}
	}
	if err := t.addRoute("0.0.0.0/1", t.tunIP, devName, t.ifIndex); err != nil {
		return err
	}
	return t.addRoute("128.0.0.0/1", t.tunIP, devName, t.ifIndex)
}

func (t *TunService) addRoute(target, gateway, devName string, ifIndex int) error {
	if err := t.contextError(); err != nil {
		return err
	}
	if _, network, err := net.ParseCIDR(target); err == nil {
		target = network.String()
	} else if net.ParseIP(target) == nil {
		return fmt.Errorf("无效的路由目标: %s", target)
	} else if t.platform == "darwin" {
		target += "/32" // Prevent BSD route from inferring a network for a host ending in .0.
	}
	var name string
	var args, undo []string
	switch t.platform {
	case "darwin":
		name, args, undo = "route", []string{"add", target}, []string{"delete", target}
		if devName != "" && (gateway == "" || gateway == t.tunIP || gateway == t.peerIP) {
			args = append(args, "-interface", devName)
			undo = append(undo, "-interface", devName)
		} else {
			args = append(args, gateway)
			undo = append(undo, gateway)
			if devName != "" {
				args = append(args, "-ifp", devName)
				undo = append(undo, "-ifp", devName)
			}
		}
	case "linux":
		name, args, undo = "ip", []string{"route", "add", target}, []string{"route", "del", target}
		if gateway != "" && gateway != t.tunIP && gateway != t.peerIP {
			args = append(args, "via", gateway)
			undo = append(undo, "via", gateway)
		}
		if devName != "" {
			args = append(args, "dev", devName)
			undo = append(undo, "dev", devName)
		}
	case "windows":
		ip, network, err := net.ParseCIDR(target)
		mask := "255.255.255.255"
		if err == nil {
			target, mask = ip.String(), net.IP(network.Mask).String()
		}
		if gateway == t.tunIP || gateway == t.peerIP {
			gateway = "0.0.0.0"
		}
		name, args, undo = "route", []string{"add", target, "mask", mask, gateway, "METRIC", "1"}, []string{"delete", target, "mask", mask, gateway}
		if ifIndex > 0 {
			args = append(args, "IF", strconv.Itoa(ifIndex))
			undo = append(undo, "IF", strconv.Itoa(ifIndex))
		}
	default:
		return fmt.Errorf("不支持的操作系统: %s", t.platform)
	}
	for _, owned := range t.undoRoutes {
		if owned.command == name && strings.Join(owned.args, "\x00") == strings.Join(undo, "\x00") {
			return nil
		}
	}
	// Never replace an existing route: only a successful add belongs to this instance.
	if output, err := t.runRoute(name, args...); err != nil {
		return fmt.Errorf("添加路由 %s 失败: %s: %w", target, output, err)
	}
	t.undoRoutes = append(t.undoRoutes, routeUndo{command: name, args: undo})
	return nil
}

func (t *TunService) runRoute(name string, args ...string) ([]byte, error) {
	output, err := t.run(name, args...)
	if err == nil && t.platform == "darwin" {
		// BSD route can report a routing-socket failure but still exit zero.
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "route:") {
				return output, fmt.Errorf("%s", strings.TrimSpace(line))
			}
		}
	}
	return output, err
}

func (t *TunService) contextError() error {
	if t.cfg != nil && t.cfg.Context != nil {
		return t.cfg.Context.Err()
	}
	return nil
}

func (t *TunService) runCommand(name string, args ...string) ([]byte, error) {
	ctx := context.Background()
	if !t.cleaning && t.cfg != nil && t.cfg.Context != nil {
		ctx = t.cfg.Context
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	if t.platform != "windows" {
		command.Env = append(os.Environ(), "LC_ALL=C") // Keep route diagnostics stable for cleanup.
	}
	output, err := command.CombinedOutput()
	if err != nil {
		err = errors.Join(err, ctx.Err())
	}
	return output, err
}

// getSSHRoute preserves the transport's current VPN or physical interface.
func (t *TunService) getSSHRoute(ip string) (gateway, iface string, index int, err error) {
	var output []byte
	switch t.platform {
	case "darwin":
		output, err = t.runRoute("route", "-n", "get", ip)
		if err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				switch fields[0] {
				case "gateway:":
					gateway = fields[1]
				case "interface:":
					iface = fields[1]
				}
			}
			if net.ParseIP(gateway) == nil {
				gateway = ""
			}
		}
	case "linux":
		output, err = t.run("ip", "-4", "route", "get", ip)
		if err == nil {
			fields := strings.Fields(string(output))
			for i := 0; i+1 < len(fields); i++ {
				switch fields[i] {
				case "via":
					gateway = fields[i+1]
				case "dev":
					iface = fields[i+1]
				}
			}
		}
	case "windows":
		script := "$ErrorActionPreference = 'Stop'; $route = Find-NetRoute -RemoteIPAddress '" + ip + "' | Where-Object { $_.PSObject.Properties.Name -contains 'NextHop' } | Select-Object -First 1; if ($null -eq $route) { throw 'No SSH route' }; $route | Select-Object NextHop, InterfaceIndex | ConvertTo-Json -Compress"
		output, err = t.run("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
		if err == nil {
			var route struct {
				NextHop        string
				InterfaceIndex int
			}
			if err = json.Unmarshal(output, &route); err == nil {
				gateway, index = route.NextHop, route.InterfaceIndex
			}
			if index <= 0 {
				err = fmt.Errorf("无效的 SSH 路由接口索引")
			}
		}
	default:
		err = fmt.Errorf("不支持的操作系统: %s", t.platform)
	}
	if err != nil {
		return "", "", 0, fmt.Errorf("查询 SSH 绕过路由失败: %s: %w", output, err)
	}
	if gateway == "" && iface == "" {
		return "", "", 0, fmt.Errorf("未找到 SSH 底层连接的路由")
	}
	return gateway, iface, index, nil
}

func (t *TunService) checkRouteConflicts() error {
	var sshIP net.IP
	if t.ssh != nil && t.ssh.RemoteAddr() != nil {
		host, _, err := net.SplitHostPort(t.ssh.RemoteAddr().String())
		if err != nil {
			return fmt.Errorf("无效的 SSH 底层连接地址: %w", err)
		}
		host, _, _ = strings.Cut(host, "%")
		sshIP = net.ParseIP(host)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.logger.Warnf("[TUN] 无法检查物理网卡冲突: %v", err)
	}
	check := func(cidr string) error {
		ip, network, err := net.ParseCIDR(cidr)
		if err != nil || ip.To4() == nil {
			return fmt.Errorf("无效的 IPv4 TUN 路由: %s", cidr)
		}
		if sshIP != nil && network.Contains(sshIP) {
			return fmt.Errorf("SSH 底层连接 IP %s 包含在 TUN 路由 %s 中，请调整路由或别名设置", sshIP, cidr)
		}
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err == nil && network.Contains(ip) {
					t.logger.Warnf("[TUN] 路由 %s 包含物理网卡 %s 的 IP %s", cidr, iface.Name, ip)
				}
			}
		}
		return nil
	}
	if !t.global {
		for _, cidr := range t.routes {
			if err := check(cidr); err != nil {
				return err
			}
		}
	}
	for _, alias := range t.cfg.SubnetAliases {
		if err := check(alias.Src.String()); err != nil {
			return err
		}
	}
	return nil
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
