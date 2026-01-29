import React, { useState } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  IconButton,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  FormControlLabel,
  Switch,
  Chip,
  Alert,
  CircularProgress,
  Tooltip,
} from '@mui/material';
import {
  Add as AddIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  PlayArrow as TestIcon,
  Visibility as ViewIcon,
  VisibilityOff as HideIcon,
  FolderOpen as FolderIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useProfiles } from '../../hooks/useConfig';
import { SSHProfile, ProfileFormData } from '../../types';
import { TestConnection, OpenFileDialog } from '../../../wailsjs/go/main/App';

const initialFormData: ProfileFormData = {
  name: '',
  host: '',
  port: '22',
  user: '',
  password: '',
  keyFile: '',
  httpAddr: ':8080',
  socksAddr: '',
  systemProxy: true,
  ruleFile: '',
  enableJumpHost: false,
  jumpHosts: [],
};

const ProfilesPage: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const { profiles, loading, addProfile, updateProfile, deleteProfile } = useProfiles();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [editingProfile, setEditingProfile] = useState<SSHProfile | null>(null);
  const [deletingProfile, setDeletingProfile] = useState<SSHProfile | null>(null);
  const [formData, setFormData] = useState<ProfileFormData>(initialFormData);
  const [showPassword, setShowPassword] = useState(false);
  const [testLoading, setTestLoading] = useState<string | null>(null);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [saving, setSaving] = useState(false);

  const handleOpenDialog = (profile?: SSHProfile) => {
    if (profile) {
      setEditingProfile(profile);
      setFormData({
        name: profile.name,
        host: profile.host,
        port: profile.port,
        user: profile.user,
        password: profile.password || '',
        keyFile: profile.keyFile || '',
        httpAddr: profile.httpAddr,
        socksAddr: profile.socksAddr || '',
        systemProxy: profile.systemProxy,
        ruleFile: profile.ruleFile || '',
        enableJumpHost: (profile.jumpHostsList && profile.jumpHostsList.length > 0) || (profile.jumpHosts && profile.jumpHosts.length > 0) || false,
        jumpHosts: profile.jumpHostsList || [],
      });
    } else {
      setEditingProfile(null);
      setFormData(initialFormData);
    }
    setDialogOpen(true);
  };

  const handleCloseDialog = () => {
    setDialogOpen(false);
    setDialogError(null);
    setEditingProfile(null);
    setFormData(initialFormData);
  };

  const handleChange = (field: keyof ProfileFormData) => (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const value = event.target.type === 'checkbox' ? event.target.checked : event.target.value;
    setFormData(prev => ({ ...prev, [field]: value }));
  };

  const handleJumpHostChange = (index: number, field: keyof any, value: string) => {
    const newJumpHosts = [...formData.jumpHosts];
    newJumpHosts[index] = { ...newJumpHosts[index], [field]: value };
    setFormData(prev => ({ ...prev, jumpHosts: newJumpHosts }));
  };

  const handleAddJumpHost = () => {
    setFormData(prev => ({
      ...prev,
      jumpHosts: [...prev.jumpHosts, { host: '', port: '22', user: '', password: '' }]
    }));
  };

  const handleRemoveJumpHost = (index: number) => {
    const newJumpHosts = [...formData.jumpHosts];
    newJumpHosts.splice(index, 1);
    setFormData(prev => ({ ...prev, jumpHosts: newJumpHosts }));
  };

  const handleToggleJumpHost = (checked: boolean) => {
    setFormData(prev => ({
      ...prev,
      enableJumpHost: checked,
      jumpHosts: checked && prev.jumpHosts.length === 0
        ? [{ host: '', port: '22', user: '', password: '' }]
        : prev.jumpHosts
    }));
  };

  const handleSelectFile = async (field: 'keyFile' | 'ruleFile') => {
    try {
      const title = field === 'keyFile' ? '选择SSH私钥文件' : '选择规则文件';
      const path = await OpenFileDialog(title, []);
      if (path) {
        setFormData(prev => ({ ...prev, [field]: path }));
      }
    } catch (err) {
      console.error('选择文件失败:', err);
    }
  };

  const handleSave = async () => {
    if (!formData.name || !formData.host || !formData.user) {
      setMessage({ type: 'error', text: '请填写必填字段' });
      return;
    }

    if (formData.enableJumpHost) {
      for (let i = 0; i < formData.jumpHosts.length; i++) {
        const jh = formData.jumpHosts[i];
        if (!jh.host || !jh.user) {
          setMessage({ type: 'error', text: `跳板机 ${i + 1} 缺少主机或用户名` });
          return;
        }
      }
    }

    setSaving(true);
    setMessage(null);
    setDialogError(null);
    try {
      // 构造正确的数据结构
      // jumpHosts (legacy) 必须是字符串数组 (user@host:port 格式)
      // jumpHostsList (GUI) 是对象数组
      const jumpHostsLegacy: string[] = formData.enableJumpHost
        ? formData.jumpHosts.map(jh => {
          let str = jh.host;
          if (jh.port && jh.port !== '22') str += `:${jh.port}`;
          if (jh.user) str = `${jh.user}@${str}`;
          return str;
        })
        : [];
      const jumpHostsList = formData.enableJumpHost ? formData.jumpHosts : [];

      // 构造完整的 profile 对象，显式覆盖 jumpHosts
      const profilePayload = {
        name: formData.name,
        host: formData.host,
        port: formData.port,
        user: formData.user,
        password: formData.password,
        keyFile: formData.keyFile,
        httpAddr: formData.httpAddr,
        socksAddr: formData.socksAddr,
        systemProxy: formData.systemProxy,
        ruleFile: formData.ruleFile,
        jumpHosts: jumpHostsLegacy,  // 必须是 string[]
        jumpHostsList: jumpHostsList, // 对象数组
      };

      if (editingProfile) {
        await updateProfile({
          ...editingProfile,
          ...profilePayload,
        } as unknown as SSHProfile);
        setMessage({ type: 'success', text: '配置已更新' });
      } else {
        await addProfile(profilePayload as any);
        setMessage({ type: 'success', text: '配置已添加' });
      }
      handleCloseDialog();
    } catch (err) {
      setDialogError(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deletingProfile) return;

    try {
      await deleteProfile(deletingProfile.id);
      setMessage({ type: 'success', text: '配置已删除' });
      setDeleteDialogOpen(false);
      setDeletingProfile(null);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : '删除失败' });
    }
  };

  const handleTest = async (profile: SSHProfile) => {
    setTestLoading(profile.id);
    setMessage(null);
    try {
      await TestConnection(profile.id);
      setMessage({ type: 'success', text: `连接 ${profile.name} 测试成功` });
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : '连接测试失败' });
    } finally {
      setTestLoading(null);
    }
  };

  const cardStyle = {
    borderRadius: 2,
    backgroundColor: isDark ? '#282a36' : '#ffffff',
    boxShadow: isDark ? 'none' : '0 2px 8px rgba(0,0,0,0.1)',
  };

  return (
    <Box sx={{ maxWidth: 1200, margin: '0 auto' }}>
      {message && (
        <Alert severity={message.type} sx={{ mb: 2 }} onClose={() => setMessage(null)}>
          {message.text}
        </Alert>
      )}

      <Card sx={cardStyle}>
        <CardContent>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              SSH配置列表
            </Typography>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => handleOpenDialog()}
            >
              添加配置
            </Button>
          </Box>

          {loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
              <CircularProgress />
            </Box>
          ) : profiles.length === 0 ? (
            <Box sx={{ textAlign: 'center', py: 4 }}>
              <Typography variant="body1" color="textSecondary">
                暂无配置，点击"添加配置"创建新的SSH配置
              </Typography>
            </Box>
          ) : (
            <TableContainer component={Paper} sx={{ backgroundColor: 'transparent', boxShadow: 'none' }}>
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>名称</TableCell>
                    <TableCell>服务器</TableCell>
                    <TableCell>用户</TableCell>
                    <TableCell>HTTP代理</TableCell>
                    <TableCell>系统代理</TableCell>
                    <TableCell align="right">操作</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {profiles.map((profile) => (
                    <TableRow key={profile.id} hover>
                      <TableCell>
                        <Typography variant="body2" sx={{ fontWeight: 500 }}>
                          {profile.name}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        {profile.host}:{profile.port}
                      </TableCell>
                      <TableCell>{profile.user}</TableCell>
                      <TableCell>{profile.httpAddr}</TableCell>
                      <TableCell>
                        <Chip
                          label={profile.systemProxy ? '启用' : '禁用'}
                          size="small"
                          color={profile.systemProxy ? 'success' : 'default'}
                        />
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title="测试连接">
                          <IconButton
                            size="small"
                            onClick={() => handleTest(profile)}
                            disabled={testLoading === profile.id}
                          >
                            {testLoading === profile.id ? (
                              <CircularProgress size={20} />
                            ) : (
                              <TestIcon />
                            )}
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="编辑">
                          <IconButton
                            size="small"
                            onClick={() => handleOpenDialog(profile)}
                          >
                            <EditIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="删除">
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => {
                              setDeletingProfile(profile);
                              setDeleteDialogOpen(true);
                            }}
                          >
                            <DeleteIcon />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </CardContent>
      </Card>

      {/* 添加/编辑对话框 */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingProfile ? '编辑配置' : '添加配置'}
        </DialogTitle>
        <DialogContent>
          {dialogError && (
            <Alert severity="error" sx={{ mb: 2, mt: 1 }} onClose={() => setDialogError(null)}>
              {dialogError}
            </Alert>
          )}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 2 }}>
            <TextField
              label="配置名称"
              fullWidth
              required
              value={formData.name}
              onChange={handleChange('name')}
              placeholder="例如: 我的服务器"
              inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
            />

            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label="SSH主机"
                fullWidth
                required
                value={formData.host}
                onChange={handleChange('host')}
                placeholder="例如: example.com"
                sx={{ flex: 2 }}
                inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
              />
              <TextField
                label="端口"
                value={formData.port}
                onChange={handleChange('port')}
                placeholder="22"
                sx={{ flex: 1 }}
                inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
              />
            </Box>

            <TextField
              label="用户名"
              fullWidth
              required
              value={formData.user}
              onChange={handleChange('user')}
              placeholder="例如: root"
              inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
            />

            <TextField
              label="密码"
              fullWidth
              type={showPassword ? 'text' : 'password'}
              value={formData.password}
              onChange={handleChange('password')}
              placeholder="留空则使用密钥或交互式认证"
              InputProps={{
                endAdornment: (
                  <IconButton onClick={() => setShowPassword(!showPassword)} edge="end">
                    {showPassword ? <HideIcon /> : <ViewIcon />}
                  </IconButton>
                ),
              }}
            />

            <TextField
              label="私钥文件路径"
              fullWidth
              value={formData.keyFile}
              onChange={handleChange('keyFile')}
              placeholder="例如: ~/.ssh/id_rsa"
              InputProps={{
                endAdornment: (
                  <IconButton onClick={() => handleSelectFile('keyFile')} edge="end">
                    <FolderIcon />
                  </IconButton>
                ),
              }}
            />

            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label="HTTP代理地址"
                fullWidth
                value={formData.httpAddr}
                onChange={handleChange('httpAddr')}
                placeholder=":8080"
              />
              <TextField
                label="SOCKS5代理地址"
                fullWidth
                value={formData.socksAddr}
                onChange={handleChange('socksAddr')}
                placeholder="留空则不启用"
              />
            </Box>

            <FormControlLabel
              control={
                <Switch
                  checked={formData.systemProxy}
                  onChange={handleChange('systemProxy')}
                />
              }
              label="自动设置系统代理"
            />

            <TextField
              label="规则文件路径"
              fullWidth
              value={formData.ruleFile}
              onChange={handleChange('ruleFile')}
              placeholder="可选，用于自定义路由规则"
              InputProps={{
                endAdornment: (
                  <IconButton onClick={() => handleSelectFile('ruleFile')} edge="end">
                    <FolderIcon />
                  </IconButton>
                ),
              }}
            />

            <Box sx={{ my: 1 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={formData.enableJumpHost}
                      onChange={(e) => handleToggleJumpHost(e.target.checked)}
                    />
                  }
                  label="启用跳板机"
                />
              </Box>

              {formData.enableJumpHost && (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pl: 2, borderLeft: '2px solid', borderColor: 'divider' }}>
                  {formData.jumpHosts.map((jh, index) => (
                    <Box key={index} sx={{ mt: 1 }}>
                      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
                        <Typography variant="subtitle2" color="primary">跳板机 {index + 1}</Typography>
                        <IconButton size="small" color="error" onClick={() => handleRemoveJumpHost(index)}>
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Box>
                      <Box sx={{ display: 'flex', gap: 2, mb: 1 }}>
                        <TextField
                          label="主机地址"
                          size="small"
                          fullWidth
                          value={jh.host}
                          onChange={(e) => handleJumpHostChange(index, 'host', e.target.value)}
                          sx={{ flex: 2 }}
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                        <TextField
                          label="端口"
                          size="small"
                          value={jh.port}
                          onChange={(e) => handleJumpHostChange(index, 'port', e.target.value)}
                          sx={{ flex: 1 }}
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                      </Box>
                      <Box sx={{ display: 'flex', gap: 2 }}>
                        <TextField
                          label="用户名"
                          size="small"
                          fullWidth
                          value={jh.user}
                          onChange={(e) => handleJumpHostChange(index, 'user', e.target.value)}
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                        <TextField
                          label="密码"
                          size="small"
                          fullWidth
                          type="password"
                          value={jh.password || ''}
                          onChange={(e) => handleJumpHostChange(index, 'password', e.target.value)}
                          placeholder="如果不填则使用私钥"
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                      </Box>
                    </Box>
                  ))}
                  <Button
                    variant="outlined"
                    size="small"
                    startIcon={<AddIcon />}
                    onClick={handleAddJumpHost}
                    sx={{ alignSelf: 'flex-start', mt: 1 }}
                  >
                    增加跳板机
                  </Button>
                </Box>
              )}
            </Box>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>取消</Button>
          <Button
            variant="contained"
            onClick={handleSave}
            disabled={saving}
          >
            {saving ? <CircularProgress size={24} /> : '保存'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 删除确认对话框 */}
      <Dialog open={deleteDialogOpen} onClose={() => setDeleteDialogOpen(false)}>
        <DialogTitle>确认删除</DialogTitle>
        <DialogContent>
          <Typography>
            确定要删除配置 "{deletingProfile?.name}" 吗？此操作无法撤销。
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>取消</Button>
          <Button variant="contained" color="error" onClick={handleDelete}>
            删除
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default ProfilesPage;
