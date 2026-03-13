// Package gui provides a web-based graphical interface for managing gotun.
//
// It starts a local HTTP server that serves a simple control panel, allowing
// users to configure the SSH tunnel, start/stop the proxy, and view
// connection status through a browser.
package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Sesame2/gotun/internal/config"
	"github.com/Sesame2/gotun/internal/logger"
	"github.com/Sesame2/gotun/internal/proxy"
	"github.com/Sesame2/gotun/internal/router"
	"github.com/Sesame2/gotun/internal/sysproxy"
)

// App is the GUI application that wraps the gotun proxy service.
type App struct {
	cfg        *config.Config
	log        *logger.Logger
	addr       string // web UI listen address, e.g. "127.0.0.1:8089"
	mu         sync.Mutex
	running    bool
	sshClient  *proxy.SSHClient
	httpProxy  *proxy.HTTPOverSSH
	socksProxy *proxy.SOCKS5OverSSH
	proxyMgr   *sysproxy.Manager
	server     *http.Server
	startedAt  time.Time
}

// StatusResponse is the JSON structure returned by the /api/status endpoint.
type StatusResponse struct {
	Running   bool   `json:"running"`
	HTTPAddr  string `json:"http_addr"`
	SocksAddr string `json:"socks_addr"`
	SSHServer string `json:"ssh_server"`
	StartedAt string `json:"started_at,omitempty"`
	GitConfig string `json:"git_config,omitempty"`
}

// NewApp creates a new GUI application.
func NewApp(cfg *config.Config, addr string) *App {
	return &App{
		cfg:  cfg,
		log:  logger.NewLogger(cfg.Verbose),
		addr: addr,
	}
}

// Run starts the web UI server and blocks until the server exits.
func (a *App) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/start", a.handleStart)
	mux.HandleFunc("/api/stop", a.handleStop)

	a.server = &http.Server{
		Addr:    a.addr,
		Handler: mux,
	}

	a.log.Infof("GoTun GUI 已启动，请在浏览器中打开 http://%s", a.addr)
	err := a.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the web UI server and any running proxy.
func (a *App) Shutdown() {
	a.stopProxy()
	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.server.Shutdown(ctx)
	}
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	resp := StatusResponse{
		Running:   a.running,
		HTTPAddr:  a.cfg.ListenAddr,
		SocksAddr: a.cfg.SocksAddr,
		SSHServer: a.cfg.SSHServer,
	}
	if a.running {
		resp.StartedAt = a.startedAt.Format(time.DateTime)
		if a.cfg.SocksAddr != "" {
			resp.GitConfig = fmt.Sprintf(
				"# Add to ~/.ssh/config to route git SSH through the SOCKS5 proxy:\n"+
					"Host github.com\n"+
					"    ProxyCommand nc -x %s %%h %%p\n"+
					"    ServerAliveInterval 30\n"+
					"    ServerAliveCountMax 3\n\n"+
					"# Alternative (if nc without -x support): use ncat or connect-proxy:\n"+
					"#   ProxyCommand ncat --proxy %s --proxy-type socks5 %%h %%p\n"+
					"#   ProxyCommand connect-proxy -S %s %%h %%p",
				a.cfg.SocksAddr, a.cfg.SocksAddr, a.cfg.SocksAddr,
			)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		http.Error(w, "代理已在运行", http.StatusConflict)
		return
	}
	a.mu.Unlock()

	if err := a.cfg.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("配置错误: %v", err), http.StatusBadRequest)
		return
	}

	if err := a.startProxy(); err != nil {
		http.Error(w, fmt.Sprintf("启动失败: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	a.stopProxy()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func (a *App) startProxy() error {
	sshClient, err := proxy.NewSSHClient(a.cfg, a.log)
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}

	var r *router.Router
	if a.cfg.RuleFile != "" {
		r, err = router.NewRouter(a.cfg.RuleFile)
		if err != nil {
			a.log.Warnf("加载规则文件失败: %v", err)
		}
	}

	httpProxy, err := proxy.NewHTTPOverSSH(a.cfg, a.log, sshClient, r)
	if err != nil {
		sshClient.Close()
		return fmt.Errorf("HTTP 代理初始化失败: %w", err)
	}

	var socksProxy *proxy.SOCKS5OverSSH
	if a.cfg.SocksAddr != "" {
		socksProxy, err = proxy.NewSOCKS5OverSSH(a.cfg, a.log, sshClient, r)
		if err != nil {
			sshClient.Close()
			return fmt.Errorf("SOCKS5 代理初始化失败: %w", err)
		}
	}

	a.mu.Lock()
	a.sshClient = sshClient
	a.httpProxy = httpProxy
	a.socksProxy = socksProxy
	a.running = true
	a.startedAt = time.Now()
	a.mu.Unlock()

	var proxyMgr *sysproxy.Manager
	if a.cfg.SystemProxy {
		proxyMgr = sysproxy.NewManager(a.log, a.cfg.ListenAddr, a.cfg.SocksAddr)
		a.mu.Lock()
		a.proxyMgr = proxyMgr
		a.mu.Unlock()
	}

	go func() {
		if proxyMgr != nil {
			if err := proxyMgr.Enable(); err != nil {
				a.log.Errorf("设置系统代理失败: %v", err)
			}
		}
		if err := httpProxy.Start(); err != nil {
			a.log.Errorf("HTTP 代理服务启动失败: %v", err)
		}
	}()

	if socksProxy != nil {
		go func() {
			if err := socksProxy.Start(); err != nil {
				a.log.Errorf("SOCKS5 代理服务启动失败: %v", err)
			}
		}()
	}

	return nil
}

func (a *App) stopProxy() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return
	}

	a.running = false

	if a.proxyMgr != nil {
		if err := a.proxyMgr.Disable(); err != nil {
			a.log.Errorf("恢复系统代理设置失败: %v", err)
		}
		a.proxyMgr = nil
	}

	if a.httpProxy != nil {
		if err := a.httpProxy.Close(); err != nil {
			a.log.Errorf("关闭 HTTP 代理失败: %v", err)
		}
		a.httpProxy = nil
	}

	if a.socksProxy != nil {
		if err := a.socksProxy.Close(); err != nil {
			a.log.Errorf("关闭 SOCKS5 代理失败: %v", err)
		}
		a.socksProxy = nil
	}

	if a.sshClient != nil {
		if err := a.sshClient.Close(); err != nil {
			a.log.Errorf("关闭 SSH 连接失败: %v", err)
		}
		a.sshClient = nil
	}
}

// indexHTML is the single-page HTML/JS/CSS UI for the control panel.
const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GoTun 控制面板</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f0f2f5; color: #333; }
  .header { background: #1a1a2e; color: #fff; padding: 16px 24px; display: flex; align-items: center; gap: 12px; }
  .header h1 { font-size: 1.4rem; font-weight: 600; }
  .badge { padding: 3px 10px; border-radius: 12px; font-size: 0.75rem; font-weight: 600; }
  .badge-running { background: #52c41a; color: #fff; }
  .badge-stopped { background: #ff4d4f; color: #fff; }
  .container { max-width: 780px; margin: 24px auto; padding: 0 16px; }
  .card { background: #fff; border-radius: 8px; padding: 20px; margin-bottom: 16px; box-shadow: 0 1px 4px rgba(0,0,0,.08); }
  .card h2 { font-size: 1rem; font-weight: 600; margin-bottom: 14px; color: #555; text-transform: uppercase; letter-spacing: .5px; }
  .info-row { display: flex; justify-content: space-between; padding: 6px 0; border-bottom: 1px solid #f0f0f0; font-size: .9rem; }
  .info-row:last-child { border-bottom: none; }
  .info-label { color: #888; }
  .info-value { font-weight: 500; font-family: monospace; }
  .btn { display: inline-block; padding: 10px 28px; border-radius: 6px; border: none; cursor: pointer; font-size: .95rem; font-weight: 600; transition: opacity .15s; }
  .btn:hover { opacity: .85; }
  .btn:disabled { opacity: .4; cursor: not-allowed; }
  .btn-start { background: #1677ff; color: #fff; }
  .btn-stop  { background: #ff4d4f; color: #fff; }
  .btn-row { display: flex; gap: 10px; margin-top: 14px; }
  pre { background: #1e1e2e; color: #cdd6f4; padding: 14px 16px; border-radius: 6px; font-size: .82rem; overflow-x: auto; white-space: pre-wrap; word-break: break-all; }
  .git-hint { margin-top: 16px; }
  .git-hint h3 { font-size: .85rem; color: #888; margin-bottom: 8px; }
  #msg { margin-top: 10px; font-size: .85rem; min-height: 20px; color: #ff4d4f; }
</style>
</head>
<body>
<div class="header">
  <h1>🚀 GoTun 控制面板</h1>
  <span id="badge" class="badge badge-stopped">已停止</span>
</div>
<div class="container">
  <div class="card">
    <h2>代理状态</h2>
    <div class="info-row"><span class="info-label">HTTP 代理</span><span class="info-value" id="httpAddr">-</span></div>
    <div class="info-row"><span class="info-label">SOCKS5 代理</span><span class="info-value" id="socksAddr">-</span></div>
    <div class="info-row"><span class="info-label">SSH 服务器</span><span class="info-value" id="sshServer">-</span></div>
    <div class="info-row"><span class="info-label">启动时间</span><span class="info-value" id="startedAt">-</span></div>
    <div class="btn-row">
      <button class="btn btn-start" id="btnStart" onclick="doStart()">▶ 启动代理</button>
      <button class="btn btn-stop"  id="btnStop"  onclick="doStop()" disabled>■ 停止代理</button>
    </div>
    <div id="msg"></div>
  </div>
  <div class="card git-hint" id="gitCard" style="display:none">
    <h2>Git SSH 配置提示</h2>
    <p style="font-size:.85rem;color:#666;margin-bottom:8px;">
      当前 HTTP 代理不拦截 SSH 流量。若要让 <code>git@github.com</code> 等 SSH 远程仓库通过代理，
      请在 <code>~/.ssh/config</code> 中添加以下配置：
    </p>
    <pre id="gitConfig"></pre>
  </div>
</div>
<script>
async function refresh() {
  try {
    const r = await fetch('/api/status');
    const d = await r.json();
    const badge = document.getElementById('badge');
    badge.textContent = d.running ? '运行中' : '已停止';
    badge.className = 'badge ' + (d.running ? 'badge-running' : 'badge-stopped');
    document.getElementById('httpAddr').textContent  = d.http_addr  || '-';
    document.getElementById('socksAddr').textContent = d.socks_addr || '未启用';
    document.getElementById('sshServer').textContent = d.ssh_server || '-';
    document.getElementById('startedAt').textContent = d.started_at || '-';
    document.getElementById('btnStart').disabled = d.running;
    document.getElementById('btnStop').disabled  = !d.running;
    const gc = document.getElementById('gitCard');
    if (d.running && d.git_config) {
      gc.style.display = '';
      document.getElementById('gitConfig').textContent = d.git_config;
    } else {
      gc.style.display = 'none';
    }
  } catch(e) { console.error(e); }
}
async function doStart() {
  document.getElementById('msg').textContent = '';
  document.getElementById('btnStart').disabled = true;
  try {
    const r = await fetch('/api/start', { method: 'POST' });
    if (!r.ok) { const t = await r.text(); document.getElementById('msg').textContent = t; }
  } catch(e) { document.getElementById('msg').textContent = e.toString(); }
  refresh();
}
async function doStop() {
  document.getElementById('btnStop').disabled = true;
  await fetch('/api/stop', { method: 'POST' });
  refresh();
}
refresh();
setInterval(refresh, 3000);
</script>
</body>
</html>
`
