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
import PlayIcon from '@mui/icons-material/PlayArrow';
import StopIcon from '@mui/icons-material/Stop';
import RefreshIcon from '@mui/icons-material/Refresh';
import SuccessIcon from '@mui/icons-material/CheckCircle';
import ErrorIcon from '@mui/icons-material/Error';
import ClockIcon from '@mui/icons-material/Schedule';
import RouterIcon from '@mui/icons-material/Router';
import VpnIcon from '@mui/icons-material/VpnKey';
import AdminIcon from '@mui/icons-material/AdminPanelSettings';
import TunIcon from '@mui/icons-material/Lan';
import GlobalIcon from '@mui/icons-material/Public';
import { useTheme } from '@mui/material/styles';
import { useI18n } from '../../providers/I18nProvider';
import { useProxyStatus } from '../../hooks/useProxyStatus';
import { useProfiles } from '../../hooks/useConfig';
import { useTunStatus, isTunRunning, isTunStarting, isTunError } from '../../hooks/useTunStatus';
import {
  StartProxy,
  StopProxy,
  TestConnection,
} from '../../../wailsjs/go/main/App';

const getErrorMessage = (error: unknown, fallback: string) =>
  error instanceof Error ? error.message : typeof error === 'string' ? error : fallback;

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
  const { t } = useI18n();
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const { status, refresh: refreshStatus } = useProxyStatus(1000);
  const { profiles, loading: profilesLoading } = useProfiles();
  const {
    tunStatus,
    loading: tunLoading,
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
      setMessage({ type: 'error', text: t.home.selectProfileRequired });
      return;
    }

    setActionLoading(true);
    setMessage(null);
    try {
      await StartProxy(selectedProfileId);
      setMessage({ type: 'success', text: t.home.started });
      refreshStatus();
    } catch (err) {
      setMessage({ type: 'error', text: getErrorMessage(err, t.home.startFailed) });
    } finally {
      setActionLoading(false);
    }
  };

  const handleStop = async () => {
    setActionLoading(true);
    setMessage(null);
    try {
      await StopProxy();
      setMessage({ type: 'success', text: t.home.stoppedMessage });
      refreshStatus();
    } catch (err) {
      setMessage({ type: 'error', text: getErrorMessage(err, t.home.stopFailed) });
    } finally {
      setActionLoading(false);
    }
  };

  const handleTest = async () => {
    if (!selectedProfileId) {
      setMessage({ type: 'error', text: t.home.selectProfileRequired });
      return;
    }

    setTestLoading(true);
    setMessage(null);
    try {
      await TestConnection(selectedProfileId);
      setMessage({ type: 'success', text: t.home.connectionSuccess });
    } catch (err) {
      setMessage({ type: 'error', text: getErrorMessage(err, t.home.connectionFailed) });
    } finally {
      setTestLoading(false);
    }
  };

  // TUN 模式启动
  const handleStartTun = async () => {
    if (!selectedProfileId) {
      setMessage({ type: 'error', text: t.home.selectProfileRequired });
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
      setMessage({ type: 'success', text: t.home.tunStarted });
      refreshTunStatus();
    } catch (err) {
      setMessage({ type: 'error', text: getErrorMessage(err, t.home.tunStartFailed) });
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
      setMessage({ type: 'success', text: t.home.tunStopped });
      refreshTunStatus();
    } catch (err) {
      setMessage({ type: 'error', text: getErrorMessage(err, t.home.tunStopFailed) });
    } finally {
      setActionLoading(false);
    }
  };

  // 处理权限提升
  const handleElevationConfirm = async () => {
    setElevationDialogOpen(false);
    try {
      setMessage({ type: 'info', text: t.home.elevationPending });
      await requestElevation();
    } catch (err) {
      setMessage({ type: 'error', text: getErrorMessage(err, t.home.elevationFailed) });
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
  const tunHasError = isTunError(tunStatus?.status);
  const isTunStopping = tunStatus?.status === 'stopping';
  const httpBusy = isStarting || isStopping;
  const tunBusy = tunStarting || isTunStopping;

  // 检查当前配置是否启用了TUN模式
  const selectedProfileHasTun = selectedProfile?.tunMode ?? false;
  const canControlTun = selectedProfileHasTun || tunRunning || tunBusy;

  useEffect(() => {
    if (!canControlTun) setProxyTab(0);
  }, [canControlTun]);

  const cardStyle = {
    borderRadius: 2,
    backgroundColor: isDark ? '#282a36' : '#ffffff',
    boxShadow: isDark ? 'none' : '0 2px 8px rgba(0,0,0,0.1)',
  };

  return (
    <Box sx={{ maxWidth: 900, minWidth: 0, margin: '0 auto', overflowWrap: 'anywhere' }}>
      {message && (
        <Alert
          severity={message.type}
          sx={{ mb: 2 }}
          onClose={() => setMessage(null)}
          closeText={t.common.close}
          aria-live="polite"
        >
          {message.text}
        </Alert>
      )}

      {/* 控制面板 */}
      <Card sx={{ ...cardStyle, mb: 3 }}>
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            {t.home.controlPanel}
          </Typography>

          {/* 配置选择 */}
          <Box sx={{ mb: 2 }}>
            <FormControl fullWidth size="small">
              <InputLabel id="home-profile-label">{t.home.selectProfile}</InputLabel>
              <Select
                id="home-profile"
                name="profileId"
                labelId="home-profile-label"
                value={selectedProfile ? selectedProfileId : ''}
                label={t.home.selectProfile}
                onChange={(e) => setSelectedProfileId(e.target.value)}
                disabled={isRunning || httpBusy || tunRunning || tunBusy || actionLoading || profilesLoading}
              >
                {profiles.map((profile) => (
                  <MenuItem key={profile.id} value={profile.id}>
                    <Box sx={{ display: 'flex', minWidth: 0, whiteSpace: 'normal', overflowWrap: 'anywhere', alignItems: 'center', gap: 1 }}>
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
            aria-label={t.home.proxyModes}
            variant="scrollable"
            scrollButtons="auto"
            value={proxyTab}
            onChange={handleTabChange}
            sx={{ borderBottom: 1, borderColor: 'divider' }}
          >
            <Tab id="proxy-tab-0" aria-controls="proxy-tabpanel-0" icon={<RouterIcon />} label="HTTP/SOCKS5" iconPosition="start" />
            <Tab
              id="proxy-tab-1"
              aria-controls="proxy-tabpanel-1"
              icon={<TunIcon />}
              label={t.home.tunMode}
              iconPosition="start"
              disabled={!canControlTun}
            />
          </Tabs>

          {/* HTTP/SOCKS5 代理控制 */}
          <TabPanel value={proxyTab} index={0}>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'center' }}>
              {!isRunning && !isStopping ? (
                <Button
                  variant="contained"
                  color="primary"
                  startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <PlayIcon />}
                  aria-busy={actionLoading}
                  onClick={handleStart}
                  disabled={actionLoading || httpBusy || !selectedProfile || profilesLoading || tunRunning || tunBusy}
                  sx={{ minWidth: 100 }}
                >
                  {isStarting ? t.home.starting : t.home.start}
                </Button>
              ) : (
                <Button
                  variant="contained"
                  color="error"
                  startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <StopIcon />}
                  aria-busy={actionLoading}
                  onClick={handleStop}
                  disabled={actionLoading || httpBusy}
                  sx={{ minWidth: 100 }}
                >
                  {isStopping ? t.home.stopping : t.home.stop}
                </Button>
              )}
              <Tooltip title={t.home.testConnection}>
                <span>
                <IconButton
                  aria-label={t.home.testConnection}
                  color="primary"
                  aria-busy={testLoading}
                  onClick={handleTest}
                  disabled={testLoading || actionLoading || isRunning || httpBusy || tunRunning || tunBusy || !selectedProfile}
                >
                  {testLoading ? <CircularProgress size={20} /> : <RefreshIcon />}
                </IconButton>
                </span>
              </Tooltip>
              {tunRunning && (
                <Chip
                  label={t.home.tunActiveWarning}
                  color="warning"
                  size="small"
                />
              )}
            </Box>
          </TabPanel>

          {/* TUN 模式控制 */}
          <TabPanel value={proxyTab} index={1}>
            {!canControlTun ? (
              <Alert severity="info">
                {t.home.tunNotEnabled}
              </Alert>
            ) : (
              <Box>
                {needsElevation && !isAdmin && (
                  <Alert severity="warning" sx={{ mb: 2 }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <AdminIcon />
                      {t.home.tunNeedsElevation}
                    </Box>
                  </Alert>
                )}
                <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'center' }}>
                  {!tunRunning && !isTunStopping ? (
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
                      aria-busy={actionLoading}
                  onClick={handleStartTun}
                      disabled={actionLoading || tunLoading || tunBusy || !selectedProfile || profilesLoading || isRunning || httpBusy}
                      sx={{ minWidth: 120 }}
                    >
                      {tunStarting ? t.home.starting : needsElevation && !isAdmin ? t.home.startAsAdmin : t.home.startTun}
                    </Button>
                  ) : (
                    <Button
                      variant="contained"
                      color="error"
                      startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <StopIcon />}
                      aria-busy={actionLoading}
                  onClick={handleStopTun}
                      disabled={actionLoading || tunLoading || tunBusy}
                      sx={{ minWidth: 120 }}
                    >
                      {isTunStopping ? t.home.stopping : t.home.stopTun}
                    </Button>
                  )}
                  <Tooltip title={t.home.testConnection}>
                    <span>
                    <IconButton
                      aria-label={t.home.testConnection}
                      color="primary"
                      aria-busy={testLoading}
                  onClick={handleTest}
                      disabled={testLoading || actionLoading || tunLoading || tunRunning || tunBusy || isRunning || httpBusy || !selectedProfile}
                    >
                      {testLoading ? <CircularProgress size={20} /> : <RefreshIcon />}
                    </IconButton>
                    </span>
                  </Tooltip>
                  {isRunning && (
                    <Chip
                      label={t.home.httpActiveWarning}
                      color="warning"
                      size="small"
                    />
                  )}
                  {isAdmin && (
                    <Chip
                      icon={<AdminIcon />}
                      label={t.home.administratorMode}
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
                {t.home.httpStatus}
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {isRunning ? t.home.running : isStarting ? t.home.starting : isStopping ? t.home.stopping : isError ? t.home.error : t.home.stopped}
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
                {t.home.tunStatus}
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {tunRunning ? t.home.running : tunStarting ? t.home.starting : isTunStopping ? t.home.stopping : tunHasError ? t.home.error : t.home.stopped}
            </Typography>
            {tunHasError && tunStatus?.errorMessage && (
              <Typography variant="caption" color="error">
                {tunStatus.errorMessage}
              </Typography>
            )}
            {tunRunning && tunStatus?.isGlobal && (
              <Chip
                icon={<GlobalIcon />}
                label={t.home.global}
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
                {t.home.httpProxy}
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
                {t.home.uptime}
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
              {t.home.tunDeviceInfo}
            </Typography>
            <Box sx={{
              display: 'grid',
              gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
              gap: 2
            }}>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.deviceName}</Typography>
                <Typography variant="body1">{tunStatus.tunDevice || '-'}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">TUN IP</Typography>
                <Typography variant="body1">{tunStatus.tunIP || '-'}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.mode}</Typography>
                <Chip
                  label={tunStatus.isGlobal ? t.home.globalProxy : t.home.routeMode}
                  size="small"
                  color={tunStatus.isGlobal ? 'secondary' : 'primary'}
                />
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.uptime}</Typography>
                <Typography variant="body1">{tunStatus.uptime || '-'}</Typography>
              </Box>
            </Box>

            {/* 路由列表 */}
            {tunStatus.routes && tunStatus.routes.length > 0 && (
              <Box sx={{ mt: 2 }}>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  {t.home.routeRules} ({tunStatus.routes.length})
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
                        primary={t.home.moreRoutes.replace('{count}', String(tunStatus.routes.length - 5))}
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
                  {t.home.natRules} ({tunStatus.natRules.length})
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
              {t.home.configDetails}: {selectedProfile.name}
            </Typography>
            <Box sx={{
              display: 'grid',
              gridTemplateColumns: { xs: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
              gap: 2
            }}>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.host}</Typography>
                <Typography variant="body1">{selectedProfile.host}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.port}</Typography>
                <Typography variant="body1">{selectedProfile.port}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.user}</Typography>
                <Typography variant="body1">{selectedProfile.user}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.httpListen}</Typography>
                <Typography variant="body1">{selectedProfile.httpAddr}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">SOCKS5</Typography>
                <Typography variant="body1">{selectedProfile.socksAddr || '-'}</Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.systemProxy}</Typography>
                <Chip
                  label={selectedProfile.systemProxy ? t.home.enabled : t.home.disabled}
                  size="small"
                  color={selectedProfile.systemProxy ? 'success' : 'default'}
                />
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.authType}</Typography>
                <Typography variant="body1">
                  {selectedProfile.keyFile ? t.home.keyAuth : selectedProfile.password ? t.home.password : t.home.interactive}
                </Typography>
              </Box>
              <Box>
                <Typography variant="body2" color="textSecondary">{t.home.jumpHost}</Typography>
                <Typography variant="body1">
                  {selectedProfile.jumpHosts?.length ? t.home.jumpHostCount.replace('{count}', String(selectedProfile.jumpHosts.length)) : t.home.none}
                </Typography>
              </Box>
            </Box>

            {/* TUN 模式配置信息 */}
            {selectedProfile.tunMode && (
              <>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" sx={{ mb: 1, display: 'flex', alignItems: 'center', gap: 1 }}>
                  <TunIcon fontSize="small" color="secondary" />
                  {t.home.tunConfig}
                </Typography>
                <Box sx={{
                  display: 'grid',
                  gridTemplateColumns: { xs: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
                  gap: 2
                }}>
                  <Box>
                    <Typography variant="body2" color="textSecondary">{t.home.tunMode}</Typography>
                    <Chip label={t.home.enabled} size="small" color="secondary" />
                  </Box>
                  <Box>
                    <Typography variant="body2" color="textSecondary">{t.home.proxyScope}</Typography>
                    <Chip
                      icon={selectedProfile.tunGlobal ? <GlobalIcon /> : undefined}
                      label={selectedProfile.tunGlobal ? t.home.global : t.home.staticRoutes}
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
                    <Typography variant="body2" color="textSecondary">{t.home.routeCount}</Typography>
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
              {t.home.noProfiles}
            </Typography>
            <Typography variant="body2" color="textSecondary" sx={{ mb: 2 }}>
              {t.home.addProfileHint}
            </Typography>
            <Button variant="contained" href="#/profiles">
              {t.home.addProfile}
            </Button>
          </CardContent>
        </Card>
      )}

      {/* 权限提升确认对话框 */}
      <Dialog
        aria-labelledby="tun-elevation-title"
        aria-describedby="tun-elevation-description"
        open={elevationDialogOpen}
        onClose={() => setElevationDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="tun-elevation-title" sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <AdminIcon color="warning" />
          {t.home.elevationTitle}
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="tun-elevation-description">
            {t.home.elevationDescription}
          </DialogContentText>
          <Alert severity="info" sx={{ mt: 2 }}>
            {t.home.elevationHint}
          </Alert>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setElevationDialogOpen(false)}>
            {t.common.cancel}
          </Button>
          <Button
            variant="contained"
            color="primary"
            startIcon={<AdminIcon />}
            onClick={handleElevationConfirm}
          >
            {t.home.confirmElevation}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default HomePage;
