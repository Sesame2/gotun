import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  Chip,
  CircularProgress,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Alert,
  IconButton,
  Tooltip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  Divider,
  List,
  ListItem,
  ListItemText,
  Tabs,
  Tab,
} from '@mui/material';
import {
  PlayArrow as PlayIcon,
  Stop as StopIcon,
  Refresh as RefreshIcon,
  CheckCircle as SuccessIcon,
  Error as ErrorIcon,
  Schedule as ClockIcon,
  Router as RouterIcon,
  Dns as DnsIcon,
  VpnKey as VpnIcon,
  AdminPanelSettings as AdminIcon,
  Lan as TunIcon,
  Public as GlobalIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useProxyStatus } from '../../hooks/useProxyStatus';
import { useProfiles } from '../../hooks/useConfig';
import { useTunStatus, isTunRunning, isTunStarting, isTunStopped, isTunError } from '../../hooks/useTunStatus';
import {
  StartProxy,
  StopProxy,
  TestConnection,
} from '../../../wailsjs/go/main/App';

// Tab面板组件
interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

function TabPanel(props: TabPanelProps) {
  const { children, value, index, ...other } = props;
  return (
    <div
      role="tabpanel"
      hidden={value !== index}
      id={`proxy-tabpanel-${index}`}
      aria-labelledby={`proxy-tab-${index}`}
      {...other}
    >
      {value === index && <Box sx={{ pt: 2 }}>{children}</Box>}
    </div>
  );
}

const HomePage: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const { status, refresh: refreshStatus } = useProxyStatus(1000);
  const { profiles, loading: profilesLoading } = useProfiles();
  const {
    tunStatus,
    loading: tunLoading,
    error: tunError,
    isAdmin,
    needsElevation,
    startTun,
    stopTun,
    requestElevation,
    refresh: refreshTunStatus,
  } = useTunStatus(2000);
  
  const [selectedProfileId, setSelectedProfileId] = useState<string>(() => {
    return localStorage.getItem('gotun_last_profile_id') || '';
  });
  const [proxyTab, setProxyTab] = useState(0);
  const [actionLoading, setActionLoading] = useState(false);
  const [testLoading, setTestLoading] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error' | 'info' | 'warning'; text: string } | null>(null);
  const [elevationDialogOpen, setElevationDialogOpen] = useState(false);

  // 监听配置变化，如果当前选中的配置不存在了，或者没有选中配置，则默认选择第一个
  useEffect(() => {
    if (profiles.length > 0) {
      if (!selectedProfileId) {
        setSelectedProfileId(profiles[0].id);
      } else {
        const exists = profiles.some(p => p.id === selectedProfileId);
        if (!exists) {
          setSelectedProfileId(profiles[0].id);
        }
      }
    }
  }, [profiles, selectedProfileId]);

  // 保存选中状态
  useEffect(() => {
    if (selectedProfileId) {
      localStorage.setItem('gotun_last_profile_id', selectedProfileId);
    }
  }, [selectedProfileId]);

  const handleStart = async () => {
    if (!selectedProfileId) {
      setMessage({ type: 'error', text: '请选择一个配置文件' });
      return;
    }

    setActionLoading(true);
    setMessage(null);
    try {
      await StartProxy(selectedProfileId);
      setMessage({ type: 'success', text: '代理服务已启动' });
      refreshStatus();
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : '启动失败' });
    } finally {
      setActionLoading(false);
    }
  };

  const handleStop = async () => {
    setActionLoading(true);
    setMessage(null);
    try {
      await StopProxy();
      setMessage({ type: 'success', text: '代理服务已停止' });
      refreshStatus();
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : '停止失败' });
    } finally {
      setActionLoading(false);
    }
  };

  const handleTest = async () => {
    if (!selectedProfileId) {
      setMessage({ type: 'error', text: '请选择一个配置文件' });
      return;
    }

    setTestLoading(true);
    setMessage(null);
    try {
      await TestConnection(selectedProfileId);
      setMessage({ type: 'success', text: 'SSH连接测试成功' });
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : '连接测试失败' });
    } finally {
      setTestLoading(false);
    }
  };

  // TUN 模式启动
  const handleStartTun = async () => {
    if (!selectedProfileId) {
      setMessage({ type: 'error', text: '请选择一个配置文件' });
      return;
    }

    // 检查是否需要提权
    if (needsElevation && !isAdmin) {
      setElevationDialogOpen(true);
      return;
    }

    setActionLoading(true);
    setMessage(null);
    try {
      await startTun(selectedProfileId);
      setMessage({ type: 'success', text: 'TUN代理服务已启动' });
      refreshTunStatus();
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : 'TUN启动失败' });
    } finally {
      setActionLoading(false);
    }
  };

  // TUN 模式停止
  const handleStopTun = async () => {
    setActionLoading(true);
    setMessage(null);
    try {
      await stopTun();
      setMessage({ type: 'success', text: 'TUN代理服务已停止' });
      refreshTunStatus();
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : 'TUN停止失败' });
    } finally {
      setActionLoading(false);
    }
  };

  // 处理权限提升
  const handleElevationConfirm = async () => {
    setElevationDialogOpen(false);
    try {
      setMessage({ type: 'info', text: '正在请求管理员权限，应用将重新启动...' });
      await requestElevation();
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : '权限提升失败' });
    }
  };

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setProxyTab(newValue);
  };

  const selectedProfile = profiles.find(p => p.id === selectedProfileId);
  const isRunning = status.status === 'running';
  const isStarting = status.status === 'starting';
  const isStopping = status.status === 'stopping';
  const isError = status.status === 'error';

  // TUN 状态
  const tunRunning = isTunRunning(tunStatus?.status);
  const tunStarting = isTunStarting(tunStatus?.status);
  const tunStopped = isTunStopped(tunStatus?.status);
  const tunHasError = isTunError(tunStatus?.status);
  const isTunStopping = tunStatus?.status === 'stopping';

  // 检查当前配置是否启用了TUN模式
  const selectedProfileHasTun = selectedProfile?.tunMode ?? false;

  const cardStyle = {
    borderRadius: 2,
    backgroundColor: isDark ? '#282a36' : '#ffffff',
    boxShadow: isDark ? 'none' : '0 2px 8px rgba(0,0,0,0.1)',
  };

  return (
    <Box sx={{ maxWidth: 900, margin: '0 auto' }}>
      {message && (
        <Alert
          severity={message.type}
          sx={{ mb: 2 }}
          onClose={() => setMessage(null)}
        >
          {message.text}
        </Alert>
      )}

      {/* 控制面板 */}
      <Card sx={{ ...cardStyle, mb: 3 }}>
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            控制面板
          </Typography>

          {/* 配置选择 */}
          <Box sx={{ mb: 2 }}>
            <FormControl fullWidth size="small">
              <InputLabel>选择配置</InputLabel>
              <Select
                value={selectedProfileId}
                label="选择配置"
                onChange={(e) => setSelectedProfileId(e.target.value)}
                disabled={isRunning || tunRunning || profilesLoading}
              >
                {profiles.map((profile) => (
                  <MenuItem key={profile.id} value={profile.id}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      {profile.name} ({profile.user}@{profile.host})
                      {profile.tunMode && (
                        <Chip label="TUN" size="small" color="secondary" sx={{ height: 20 }} />
                      )}
                    </Box>
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          {/* 代理模式 Tab */}
          <Tabs
            value={proxyTab}
            onChange={handleTabChange}
            sx={{ borderBottom: 1, borderColor: 'divider' }}
          >
            <Tab icon={<RouterIcon />} label="HTTP/SOCKS5" iconPosition="start" />
            <Tab
              icon={<TunIcon />}
              label="TUN模式"
              iconPosition="start"
              disabled={!selectedProfileHasTun}
            />
          </Tabs>

          {/* HTTP/SOCKS5 代理控制 */}
          <TabPanel value={proxyTab} index={0}>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
              {!isRunning ? (
                <Button
                  variant="contained"
                  color="primary"
                  startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <PlayIcon />}
                  onClick={handleStart}
                  disabled={actionLoading || !selectedProfileId || profilesLoading || tunRunning}
                  sx={{ minWidth: 100 }}
                >
                  {isStarting ? '启动中...' : '启动'}
                </Button>
              ) : (
                <Button
                  variant="contained"
                  color="error"
                  startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <StopIcon />}
                  onClick={handleStop}
                  disabled={actionLoading}
                  sx={{ minWidth: 100 }}
                >
                  {isStopping ? '停止中...' : '停止'}
                </Button>
              )}
              <Tooltip title="测试连接">
                <IconButton
                  color="primary"
                  onClick={handleTest}
                  disabled={testLoading || isRunning || !selectedProfileId}
                >
                  {testLoading ? <CircularProgress size={20} /> : <RefreshIcon />}
                </IconButton>
              </Tooltip>
              {tunRunning && (
                <Chip
                  label="TUN模式运行中，请先停止TUN再启动HTTP代理"
                  color="warning"
                  size="small"
                />
              )}
            </Box>
          </TabPanel>

          {/* TUN 模式控制 */}
          <TabPanel value={proxyTab} index={1}>
            {!selectedProfileHasTun ? (
              <Alert severity="info">
                当前配置未启用TUN模式，请在配置管理中开启TUN模式
              </Alert>
            ) : (
              <Box>
                {needsElevation && !isAdmin && (
                  <Alert severity="warning" sx={{ mb: 2 }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <AdminIcon />
                      TUN模式需要管理员权限才能运行
                    </Box>
                  </Alert>
                )}
                <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
                  {!tunRunning ? (
                    <Button
                      variant="contained"
                      color="secondary"
                      startIcon={
                        actionLoading ? (
                          <CircularProgress size={20} color="inherit" />
                        ) : needsElevation && !isAdmin ? (
                          <AdminIcon />
                        ) : (
                          <VpnIcon />
                        )
                      }
                      onClick={handleStartTun}
                      disabled={actionLoading || !selectedProfileId || profilesLoading || isRunning}
                      sx={{ minWidth: 120 }}
                    >
                      {tunStarting ? '启动中...' : needsElevation && !isAdmin ? '以管理员启动' : '启动TUN'}
                    </Button>
                  ) : (
                    <Button
                      variant="contained"
                      color="error"
                      startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <StopIcon />}
                      onClick={handleStopTun}
                      disabled={actionLoading}
                      sx={{ minWidth: 120 }}
                    >
                      {isTunStopping ? '停止中...' : '停止TUN'}
                    </Button>
                  )}
                  <Tooltip title="测试连接">
                    <IconButton
                      color="primary"
                      onClick={handleTest}
                      disabled={testLoading || tunRunning || !selectedProfileId}
                    >
                      {testLoading ? <CircularProgress size={20} /> : <RefreshIcon />}
                    </IconButton>
                  </Tooltip>
                  {isRunning && (
                    <Chip
                      label="HTTP代理运行中，请先停止HTTP代理再启动TUN"
                      color="warning"
                      size="small"
                    />
                  )}
                  {isAdmin && (
                    <Chip
                      icon={<AdminIcon />}
                      label="管理员模式"
                      color="success"
                      size="small"
                      variant="outlined"
                    />
                  )}
                </Box>
              </Box>
            )}
          </TabPanel>
        </CardContent>
      </Card>

      {/* 状态卡片 */}
      <Box sx={{
        display: 'grid',
        gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
        gap: 2,
        mb: 3
      }}>
        {/* HTTP代理状态 */}
        <Card sx={cardStyle}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
              {isRunning ? (
                <SuccessIcon sx={{ color: '#4caf50', mr: 1 }} />
              ) : isError ? (
                <ErrorIcon sx={{ color: '#f44336', mr: 1 }} />
              ) : (
                <Box sx={{ width: 24, height: 24, mr: 1, borderRadius: '50%', bgcolor: '#9e9e9e' }} />
              )}
              <Typography variant="body2" color="textSecondary">
                HTTP状态
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {isRunning ? '运行中' : isStarting ? '启动中' : isStopping ? '停止中' : isError ? '错误' : '已停止'}
            </Typography>
            {isError && status.errorMessage && (
              <Typography variant="caption" color="error">
                {status.errorMessage}
              </Typography>
            )}
          </CardContent>
        </Card>

        {/* TUN状态 */}
        <Card sx={cardStyle}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
              {tunRunning ? (
                <SuccessIcon sx={{ color: '#9c27b0', mr: 1 }} />
              ) : tunHasError ? (
                <ErrorIcon sx={{ color: '#f44336', mr: 1 }} />
              ) : (
                <TunIcon sx={{ color: '#9e9e9e', mr: 1 }} />
              )}
              <Typography variant="body2" color="textSecondary">
                TUN状态
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {tunRunning ? '运行中' : tunStarting ? '启动中' : isTunStopping ? '停止中' : tunHasError ? '错误' : '已停止'}
            </Typography>
            {tunHasError && tunStatus?.errorMessage && (
              <Typography variant="caption" color="error">
                {tunStatus.errorMessage}
              </Typography>
            )}
            {tunRunning && tunStatus?.isGlobal && (
              <Chip
                icon={<GlobalIcon />}
                label="全局"
                size="small"
                color="secondary"
                sx={{ mt: 0.5 }}
              />
            )}
          </CardContent>
        </Card>

        <Card sx={cardStyle}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
              <RouterIcon sx={{ color: '#5b5c9d', mr: 1 }} />
              <Typography variant="body2" color="textSecondary">
                HTTP代理
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {status.httpAddr || '-'}
            </Typography>
          </CardContent>
        </Card>

        <Card sx={cardStyle}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
              <ClockIcon sx={{ color: '#5b5c9d', mr: 1 }} />
              <Typography variant="body2" color="textSecondary">
                运行时间
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {isRunning ? status.uptime : tunRunning ? tunStatus?.uptime : '-'}
            </Typography>
          </CardContent>
        </Card>
      </Box>

      {/* TUN 详细信息 */}
      {tunRunning && tunStatus && (
        <Card sx={{ ...cardStyle, mb: 3 }}>
          <CardContent>
            <Typography variant="h6" sx={{ mb: 2, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 1 }}>
              <TunIcon color="secondary" />
              TUN 设备信息
            </Typography>
            <Box sx={{
              display: 'grid',
              gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
              gap: 2
            }}>
              <Box>
                <Typography variant="body2" color="textSecondary">设备名称</Typography>
                <Typography variant="body1">{tunStatus.tunDevice || '-'}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">TUN IP</Typography>
                <Typography variant="body1">{tunStatus.tunIP || '-'}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">模式</Typography>
                <Chip
                  label={tunStatus.isGlobal ? '全局代理' : '路由模式'}
                  size="small"
                  color={tunStatus.isGlobal ? 'secondary' : 'primary'}
                />
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">运行时间</Typography>
                <Typography variant="body1">{tunStatus.uptime || '-'}</Typography>
              </Box>
            </Box>

            {/* 路由列表 */}
            {tunStatus.routes && tunStatus.routes.length > 0 && (
              <Box sx={{ mt: 2 }}>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  路由规则 ({tunStatus.routes.length})
                </Typography>
                <List dense sx={{ bgcolor: isDark ? '#1e1f29' : '#f5f5f5', borderRadius: 1 }}>
                  {tunStatus.routes.slice(0, 5).map((route, index) => (
                    <ListItem key={index}>
                      <ListItemText primary={route} primaryTypographyProps={{ fontFamily: 'monospace', fontSize: 13 }} />
                    </ListItem>
                  ))}
                  {tunStatus.routes.length > 5 && (
                    <ListItem>
                      <ListItemText
                        primary={`... 还有 ${tunStatus.routes.length - 5} 条路由`}
                        primaryTypographyProps={{ color: 'textSecondary', fontSize: 13 }}
                      />
                    </ListItem>
                  )}
                </List>
              </Box>
            )}

            {/* NAT规则 */}
            {tunStatus.natRules && tunStatus.natRules.length > 0 && (
              <Box sx={{ mt: 2 }}>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  NAT映射规则 ({tunStatus.natRules.length})
                </Typography>
                <List dense sx={{ bgcolor: isDark ? '#1e1f29' : '#f5f5f5', borderRadius: 1 }}>
                  {tunStatus.natRules.map((rule, index) => (
                    <ListItem key={index}>
                      <ListItemText primary={rule} primaryTypographyProps={{ fontFamily: 'monospace', fontSize: 13 }} />
                    </ListItem>
                  ))}
                </List>
              </Box>
            )}
          </CardContent>
        </Card>
      )}

      {/* 当前配置详情 */}
      {selectedProfile && (
        <Card sx={cardStyle}>
          <CardContent>
            <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
              配置详情: {selectedProfile.name}
            </Typography>
            <Box sx={{
              display: 'grid',
              gridTemplateColumns: { xs: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
              gap: 2
            }}>
              <Box>
                <Typography variant="body2" color="textSecondary">主机</Typography>
                <Typography variant="body1">{selectedProfile.host}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">端口</Typography>
                <Typography variant="body1">{selectedProfile.port}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">用户</Typography>
                <Typography variant="body1">{selectedProfile.user}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">HTTP监听</Typography>
                <Typography variant="body1">{selectedProfile.httpAddr}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">SOCKS5</Typography>
                <Typography variant="body1">{selectedProfile.socksAddr || '-'}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">系统代理</Typography>
                <Chip
                  label={selectedProfile.systemProxy ? '已启用' : '已禁用'}
                  size="small"
                  color={selectedProfile.systemProxy ? 'success' : 'default'}
                />
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">认证方式</Typography>
                <Typography variant="body1">
                  {selectedProfile.keyFile ? '密钥' : selectedProfile.password ? '密码' : '交互式'}
                </Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">跳板机</Typography>
                <Typography variant="body1">
                  {selectedProfile.jumpHosts?.length ? selectedProfile.jumpHosts.length + ' 个' : '无'}
                </Typography>
              </Box>
            </Box>

            {/* TUN 模式配置信息 */}
            {selectedProfile.tunMode && (
              <>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" sx={{ mb: 1, display: 'flex', alignItems: 'center', gap: 1 }}>
                  <TunIcon fontSize="small" color="secondary" />
                  TUN 模式配置
                </Typography>
                <Box sx={{
                  display: 'grid',
                  gridTemplateColumns: { xs: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
                  gap: 2
                }}>
                  <Box>
                    <Typography variant="body2" color="textSecondary">TUN模式</Typography>
                    <Chip label="已启用" size="small" color="secondary" />
                  </Box>
                  <Box>
                    <Typography variant="body2" color="textSecondary">代理范围</Typography>
                    <Chip
                      icon={selectedProfile.tunGlobal ? <GlobalIcon /> : undefined}
                      label={selectedProfile.tunGlobal ? '全局' : '静态路由'}
                      size="small"
                      color={selectedProfile.tunGlobal ? 'warning' : 'info'}
                    />
                  </Box>
                  <Box>
                    <Typography variant="body2" color="textSecondary">TUN CIDR</Typography>
                    <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                      {selectedProfile.tunCIDR || '10.0.85.1/24'}
                    </Typography>
                  </Box>
                  <Box>
                    <Typography variant="body2" color="textSecondary">静态路由数</Typography>
                    <Typography variant="body1">
                      {selectedProfile.tunRoutes?.length || 0}
                    </Typography>
                  </Box>
                </Box>
              </>
            )}
          </CardContent>
        </Card>
      )}

      {/* 无配置提示 */}
      {!profilesLoading && profiles.length === 0 && (
        <Card sx={cardStyle}>
          <CardContent sx={{ textAlign: 'center', py: 4 }}>
            <Typography variant="h6" color="textSecondary" sx={{ mb: 2 }}>
              暂无配置文件
            </Typography>
            <Typography variant="body2" color="textSecondary" sx={{ mb: 2 }}>
              请先在"配置管理"页面添加SSH配置
            </Typography>
            <Button variant="contained" href="#/profiles">
              添加配置
            </Button>
          </CardContent>
        </Card>
      )}

      {/* 权限提升确认对话框 */}
      <Dialog
        open={elevationDialogOpen}
        onClose={() => setElevationDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <AdminIcon color="warning" />
          需要管理员权限
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            TUN模式需要创建虚拟网络设备和修改系统路由表，这需要管理员权限才能完成。
          </DialogContentText>
          <Alert severity="info" sx={{ mt: 2 }}>
            点击"确认"后，应用将以管理员权限重新启动。请在系统弹出的权限请求中授权。
          </Alert>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setElevationDialogOpen(false)}>
            取消
          </Button>
          <Button
            variant="contained"
            color="primary"
            startIcon={<AdminIcon />}
            onClick={handleElevationConfirm}
          >
            确认提权
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default HomePage;
