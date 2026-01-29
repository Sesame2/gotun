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
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useProxyStatus } from '../../hooks/useProxyStatus';
import { useProfiles } from '../../hooks/useConfig';
import {
  StartProxy,
  StopProxy,
  TestConnection,
} from '../../../wailsjs/go/main/App';

const HomePage: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const { status, refresh: refreshStatus } = useProxyStatus(1000);
  const { profiles, loading: profilesLoading } = useProfiles();
  const [selectedProfileId, setSelectedProfileId] = useState<string>(() => {
    return localStorage.getItem('gotun_last_profile_id') || '';
  });
  const [actionLoading, setActionLoading] = useState(false);
  const [testLoading, setTestLoading] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error' | 'info'; text: string } | null>(null);

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

  const selectedProfile = profiles.find(p => p.id === selectedProfileId);
  const isRunning = status.status === 'running';
  const isStarting = status.status === 'starting';
  const isStopping = status.status === 'stopping';
  const isError = status.status === 'error';

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

          <Box sx={{
            display: 'flex',
            gap: 2,
            alignItems: 'center',
            flexDirection: { xs: 'column', md: 'row' }
          }}>
            <Box sx={{ flex: 1, width: '100%' }}>
              <FormControl fullWidth size="small">
                <InputLabel>选择配置</InputLabel>
                <Select
                  value={selectedProfileId}
                  label="选择配置"
                  onChange={(e) => setSelectedProfileId(e.target.value)}
                  disabled={isRunning || profilesLoading}
                >
                  {profiles.map((profile) => (
                    <MenuItem key={profile.id} value={profile.id}>
                      {profile.name} ({profile.user}@{profile.host})
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Box>
            <Box sx={{ display: 'flex', gap: 1, width: { xs: '100%', md: 'auto' } }}>
              {!isRunning ? (
                <Button
                  variant="contained"
                  color="primary"
                  startIcon={actionLoading ? <CircularProgress size={20} color="inherit" /> : <PlayIcon />}
                  onClick={handleStart}
                  disabled={actionLoading || !selectedProfileId || profilesLoading}
                  sx={{ flex: { xs: 1, md: 'none' }, minWidth: 100 }}
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
                  sx={{ flex: { xs: 1, md: 'none' }, minWidth: 100 }}
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
            </Box>
          </Box>
        </CardContent>
      </Card>

      {/* 状态卡片 */}
      <Box sx={{
        display: 'grid',
        gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)', md: 'repeat(4, 1fr)' },
        gap: 2,
        mb: 3
      }}>
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
                状态
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
              <DnsIcon sx={{ color: '#5b5c9d', mr: 1 }} />
              <Typography variant="body2" color="textSecondary">
                SSH服务器
              </Typography>
            </Box>
            <Typography variant="h6" sx={{ fontWeight: 600, fontSize: 14 }}>
              {isRunning ? status.sshServer : (selectedProfile ? `${selectedProfile.host}:${selectedProfile.port}` : '-')}
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
              {status.uptime || '-'}
            </Typography>
          </CardContent>
        </Card>
      </Box>

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
    </Box>
  );
};

export default HomePage;
