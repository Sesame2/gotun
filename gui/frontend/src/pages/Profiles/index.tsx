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
  Divider,
} from '@mui/material';
import {
  Add as AddIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  PlayArrow as TestIcon,
  Visibility as ViewIcon,
  VisibilityOff as HideIcon,
  FolderOpen as FolderIcon,
  Router as TunIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useProfiles } from '../../hooks/useConfig';
import { SSHProfile, ProfileFormData, TunNATRuleFormData } from '../../types';
import { TestConnection, OpenFileDialog } from '../../../wailsjs/go/main/App';
import { useI18n } from '../../providers/I18nProvider';

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
  // TUN 模式配置
  tunMode: false,
  tunGlobal: false,
  tunCIDR: '10.0.0.1/24',
  tunRoutes: [],
  tunNAT: [],
};

const ProfilesPage: React.FC = () => {
  const { t } = useI18n();
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const { profiles, loading, error, addProfile, updateProfile, deleteProfile } = useProfiles();

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
  const [deleting, setDeleting] = useState(false);

  const handleOpenDialog = (profile?: SSHProfile) => {
    setDialogError(null);
    setShowPassword(false);
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
        // TUN 模式配置
        tunMode: profile.tunMode || false,
        tunGlobal: profile.tunGlobal || false,
        tunCIDR: profile.tunCIDR || '10.0.0.1/24',
        tunRoutes: profile.tunRoutes || [],
        tunNAT: (profile.tunNAT || []).map(nat => ({
          srcCIDR: nat.srcCIDR,
          dstCIDR: nat.dstCIDR,
        })),
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
    setShowPassword(false);
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

  // TUN 模式相关处理函数
  const handleToggleTunMode = (checked: boolean) => {
    setFormData(prev => ({
      ...prev,
      tunMode: checked,
    }));
  };

  const handleTunRouteChange = (index: number, value: string) => {
    const newRoutes = [...formData.tunRoutes];
    newRoutes[index] = value;
    setFormData(prev => ({ ...prev, tunRoutes: newRoutes }));
  };

  const handleAddTunRoute = () => {
    setFormData(prev => ({
      ...prev,
      tunRoutes: [...prev.tunRoutes, '']
    }));
  };

  const handleRemoveTunRoute = (index: number) => {
    const newRoutes = [...formData.tunRoutes];
    newRoutes.splice(index, 1);
    setFormData(prev => ({ ...prev, tunRoutes: newRoutes }));
  };

  const handleTunNATChange = (index: number, field: keyof TunNATRuleFormData, value: string) => {
    const newNAT = [...formData.tunNAT];
    newNAT[index] = { ...newNAT[index], [field]: value };
    setFormData(prev => ({ ...prev, tunNAT: newNAT }));
  };

  const handleAddTunNAT = () => {
    setFormData(prev => ({
      ...prev,
      tunNAT: [...prev.tunNAT, { srcCIDR: '', dstCIDR: '' }]
    }));
  };

  const handleRemoveTunNAT = (index: number) => {
    const newNAT = [...formData.tunNAT];
    newNAT.splice(index, 1);
    setFormData(prev => ({ ...prev, tunNAT: newNAT }));
  };

  const handleSelectFile = async (field: 'keyFile' | 'ruleFile') => {
    try {
      const title = field === 'keyFile' ? t.profiles.selectKeyFile : t.profiles.selectRuleFile;
      const path = await OpenFileDialog(title, []);
      if (path) {
        setFormData(prev => ({ ...prev, [field]: path }));
      }
    } catch (err) {
      console.error('选择文件失败:', err);
      setDialogError(err instanceof Error ? err.message : typeof err === 'string' ? err : t.profiles.fileFailed);
    }
  };

  const handleSave = async () => {
    if (!formData.name.trim() || !formData.host.trim() || !formData.user.trim()) {
      setDialogError(t.profiles.requiredFields);
      return;
    }

    if (formData.enableJumpHost) {
      for (let i = 0; i < formData.jumpHosts.length; i++) {
        const jh = formData.jumpHosts[i];
        if (!jh.host.trim() || !jh.user.trim()) {
          setDialogError(t.profiles.jumpRequired.replace('{index}', String(i + 1)));
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
      let jumpHostsLegacy: string[] = formData.enableJumpHost
        ? formData.jumpHosts.map(jh => {
          let str = jh.host;
          if (jh.port && jh.port !== '22') str += `:${jh.port}`;
          if (jh.user) str = `${jh.user}@${str}`;
          return str;
        })
        : [];
      // 旧版配置没有结构化表单数据，修改其他字段时保留原有跳板链。
      if (formData.enableJumpHost && formData.jumpHosts.length === 0 && !editingProfile?.jumpHostsList?.length) {
        jumpHostsLegacy = editingProfile?.jumpHosts || [];
      }
      const jumpHostsList = formData.enableJumpHost ? formData.jumpHosts : [];

      // 过滤空的 TUN 路由
      const tunRoutes = formData.tunRoutes.filter(r => r.trim() !== '');
      // 过滤空的 TUN NAT 规则
      const tunNAT = formData.tunNAT.filter(nat => nat.srcCIDR.trim() !== '' && nat.dstCIDR.trim() !== '');

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
        // TUN 模式配置
        tunMode: formData.tunMode,
        tunGlobal: formData.tunGlobal,
        tunCIDR: formData.tunCIDR,
        tunRoutes: tunRoutes,
        tunNAT: tunNAT,
      };

      if (editingProfile) {
        await updateProfile({
          ...editingProfile,
          ...profilePayload,
        } as unknown as SSHProfile);
        setMessage({ type: 'success', text: t.profiles.updated });
      } else {
        await addProfile(profilePayload as any);
        setMessage({ type: 'success', text: t.profiles.added });
      }
      handleCloseDialog();
    } catch (err) {
      setDialogError(err instanceof Error ? err.message : typeof err === 'string' ? err : t.profiles.saveFailed);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deletingProfile || deleting) return;

    setDeleting(true);
    try {
      await deleteProfile(deletingProfile.id);
      setMessage({ type: 'success', text: t.profiles.deleted });
      setDeleteDialogOpen(false);
      setDeletingProfile(null);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : typeof err === 'string' ? err : t.profiles.deleteFailed });
    } finally {
      setDeleting(false);
    }
  };

  const handleTest = async (profile: SSHProfile) => {
    setTestLoading(profile.id);
    setMessage(null);
    try {
      await TestConnection(profile.id);
      setMessage({ type: 'success', text: t.profiles.testSuccess.replace('{name}', profile.name) });
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : typeof err === 'string' ? err : t.profiles.testFailed });
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
    <Box sx={{ maxWidth: 1200, width: '100%', minWidth: 0, margin: '0 auto' }}>
      {error && <Alert severity="error" sx={{ mb: 2 }}>{t.profiles.loadFailed}: {error}</Alert>}
      {message && (
        <Alert severity={message.type} closeText={t.common.close} sx={{ mb: 2 }} onClose={() => setMessage(null)}>
          {message.text}
        </Alert>
      )}

      <Card sx={cardStyle}>
        <CardContent>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 2 }}>
            <Typography variant="h6" sx={{ fontWeight: 600 }}>
              {t.profiles.title}
            </Typography>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => handleOpenDialog()}
            >
              {t.profiles.addProfile}
            </Button>
          </Box>

          {loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
              <CircularProgress />
            </Box>
          ) : profiles.length === 0 ? (
            <Box sx={{ textAlign: 'center', py: 4 }}>
              <Typography variant="body1" color="textSecondary">
                {t.profiles.noProfiles}
              </Typography>
            </Box>
          ) : (
            <TableContainer component={Paper} role="region" aria-label={t.profiles.title} tabIndex={0} sx={{ maxWidth: '100%', overflowX: 'auto', backgroundColor: 'transparent', boxShadow: 'none' }}>
              <Table sx={{ minWidth: 760, '& th': { whiteSpace: 'nowrap' }, '& td': { overflowWrap: 'anywhere' } }}>
                <TableHead>
                  <TableRow>
                    <TableCell>{t.profiles.name}</TableCell>
                    <TableCell>{t.profiles.host}</TableCell>
                    <TableCell>{t.profiles.user}</TableCell>
                    <TableCell>{t.profiles.httpProxy}</TableCell>
                    <TableCell>{t.profiles.mode}</TableCell>
                    <TableCell align="right">{t.profiles.actions}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {profiles.map((profile) => (
                    <TableRow key={profile.id} hover>
                      <TableCell sx={{ minWidth: 120 }}>
                        <Typography variant="body2" sx={{ fontWeight: 500, maxWidth: 240, overflowWrap: 'anywhere' }}>
                          {profile.name}
                        </Typography>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: 'nowrap' }}>
                        {profile.host}:{profile.port}
                      </TableCell>
                      <TableCell sx={{ whiteSpace: 'nowrap' }}>{profile.user}</TableCell>
                      <TableCell sx={{ whiteSpace: 'nowrap' }}>{profile.httpAddr}</TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                          {profile.tunMode ? (
                            <Chip
                              icon={<TunIcon sx={{ fontSize: '1rem !important' }} />}
                              label={profile.tunGlobal ? t.profiles.tunGlobalMode : t.profiles.tunRouteMode}
                              size="small"
                              color="primary"
                              variant="outlined"
                            />
                          ) : (
                            <Chip
                              label={t.profiles.httpProxy}
                              size="small"
                              color="default"
                            />
                          )}
                          {profile.systemProxy && (
                            <Chip
                              label={t.profiles.systemProxy}
                              size="small"
                              color="success"
                            />
                          )}
                        </Box>
                      </TableCell>
                      <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                        <Tooltip title={t.home.testConnection}>
                          <span style={{ display: 'inline-flex', verticalAlign: 'middle' }}>
                            <IconButton
                              size="small"
                              aria-label={t.home.testConnection}
                              onClick={() => handleTest(profile)}
                              disabled={testLoading === profile.id}
                            >
                              {testLoading === profile.id ? (
                                <CircularProgress size={20} />
                              ) : (
                                <TestIcon />
                              )}
                            </IconButton>
                          </span>
                        </Tooltip>
                        <Tooltip title={t.profiles.edit}>
                          <IconButton
                            size="small"
                            aria-label={t.profiles.edit}
                            onClick={() => handleOpenDialog(profile)}
                          >
                            <EditIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title={t.profiles.delete}>
                          <IconButton
                            size="small"
                            color="error"
                            aria-label={t.profiles.delete}
                            onClick={() => {
                              setMessage(null);
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
      <Dialog open={dialogOpen} onClose={saving ? undefined : handleCloseDialog} maxWidth="sm" fullWidth PaperProps={{ sx: { m: 2, maxHeight: 'calc(100vh - 32px)', width: 'calc(100% - 32px)' } }}>
        <DialogTitle>
          {editingProfile ? t.profiles.editProfile : t.profiles.addProfile}
        </DialogTitle>
        <DialogContent>
          {dialogError && (
            <Alert severity="error" closeText={t.common.close} sx={{ mb: 2, mt: 1 }} onClose={() => setDialogError(null)}>
              {dialogError}
            </Alert>
          )}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 2 }}>
            <TextField
              label={t.profiles.name}
              fullWidth
              required
              value={formData.name}
              onChange={handleChange('name')}
              placeholder={t.profiles.namePlaceholder}
              inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
            />

            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label={t.profiles.sshHost}
                fullWidth
                required
                value={formData.host}
                onChange={handleChange('host')}
                placeholder={t.profiles.hostPlaceholder}
                sx={{ flex: 2, minWidth: 0 }}
                inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
              />
              <TextField
                label={t.profiles.port}
                value={formData.port}
                onChange={handleChange('port')}
                placeholder="22"
                sx={{ flex: 1, minWidth: 0 }}
                inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
              />
            </Box>

            <TextField
              label={t.profiles.user}
              fullWidth
              required
              value={formData.user}
              onChange={handleChange('user')}
              placeholder={t.profiles.userPlaceholder}
              inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
            />

            <TextField
              label={t.profiles.password}
              fullWidth
              type={showPassword ? 'text' : 'password'}
              value={formData.password}
              onChange={handleChange('password')}
              placeholder={t.profiles.authHint}
              InputProps={{
                endAdornment: (
                  <IconButton aria-label={showPassword ? t.profiles.hidePassword : t.profiles.showPassword} aria-pressed={showPassword} onClick={() => setShowPassword(!showPassword)} edge="end">
                    {showPassword ? <HideIcon /> : <ViewIcon />}
                  </IconButton>
                ),
              }}
            />

            <TextField
              label={t.profiles.keyFilePath}
              fullWidth
              value={formData.keyFile}
              onChange={handleChange('keyFile')}
              placeholder="~/.ssh/id_rsa"
              InputProps={{
                endAdornment: (
                  <IconButton aria-label={t.profiles.selectKeyFile} onClick={() => handleSelectFile('keyFile')} edge="end">
                    <FolderIcon />
                  </IconButton>
                ),
              }}
            />

            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label={t.profiles.httpAddr}
                fullWidth
                value={formData.httpAddr}
                onChange={handleChange('httpAddr')}
                placeholder=":8080"
              />
              <TextField
                label={t.profiles.socksAddr}
                fullWidth
                value={formData.socksAddr}
                onChange={handleChange('socksAddr')}
                placeholder={t.profiles.socksHint}
              />
            </Box>

            <FormControlLabel
              control={
                <Switch
                  checked={formData.systemProxy}
                  onChange={handleChange('systemProxy')}
                />
              }
              label={t.profiles.autoSystemProxy}
            />

            <TextField
              label={t.profiles.ruleFilePath}
              fullWidth
              value={formData.ruleFile}
              onChange={handleChange('ruleFile')}
              placeholder={t.profiles.ruleHint}
              InputProps={{
                endAdornment: (
                  <IconButton aria-label={t.profiles.selectRuleFile} onClick={() => handleSelectFile('ruleFile')} edge="end">
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
                  label={t.profiles.enableJumpHost}
                />
              </Box>

              {formData.enableJumpHost && (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pl: 2, borderLeft: '2px solid', borderColor: 'divider' }}>
                  {formData.jumpHosts.map((jh, index) => (
                    <Box key={index} sx={{ mt: 1 }}>
                      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
                        <Typography variant="subtitle2" color="primary">{t.profiles.jumpHostNumber.replace('{index}', String(index + 1))}</Typography>
                        <IconButton size="small" color="error" aria-label={t.profiles.removeJumpHost.replace('{index}', String(index + 1))} onClick={() => handleRemoveJumpHost(index)}>
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Box>
                      <Box sx={{ display: 'flex', gap: 2, mb: 1 }}>
                        <TextField
                          label={t.profiles.host}
                          size="small"
                          fullWidth
                          value={jh.host}
                          onChange={(e) => handleJumpHostChange(index, 'host', e.target.value)}
                          sx={{ flex: 2, minWidth: 0 }}
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                        <TextField
                          label={t.profiles.port}
                          size="small"
                          value={jh.port}
                          onChange={(e) => handleJumpHostChange(index, 'port', e.target.value)}
                          sx={{ flex: 1, minWidth: 0 }}
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                      </Box>
                      <Box sx={{ display: 'flex', gap: 2 }}>
                        <TextField
                          label={t.profiles.user}
                          size="small"
                          fullWidth
                          value={jh.user}
                          onChange={(e) => handleJumpHostChange(index, 'user', e.target.value)}
                          inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                        <TextField
                          label={t.profiles.password}
                          size="small"
                          fullWidth
                          type="password"
                          value={jh.password || ''}
                          onChange={(e) => handleJumpHostChange(index, 'password', e.target.value)}
                          placeholder={t.profiles.jumpPasswordHint}
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
                    {t.profiles.addJumpHost}
                  </Button>
                </Box>
              )}
            </Box>

            {/* TUN 模式配置 */}
            <Divider sx={{ my: 2 }} />
            <Box sx={{ my: 1 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={formData.tunMode}
                      onChange={(e) => handleToggleTunMode(e.target.checked)}
                    />
                  }
                  label={
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <TunIcon fontSize="small" />
                      <span>{t.profiles.enableTun}</span>
                    </Box>
                  }
                />
              </Box>

              {formData.tunMode && (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pl: 2, borderLeft: '2px solid', borderColor: 'primary.main' }}>
                  <Alert severity="info" sx={{ mb: 1 }}>
                    {t.profiles.tunPrivilegeHint}
                  </Alert>

                  <FormControlLabel
                    control={
                      <Switch
                        checked={formData.tunGlobal}
                        onChange={handleChange('tunGlobal')}
                      />
                    }
                    label={t.profiles.tunGlobal}
                  />

                  <TextField
                    label={t.profiles.tunCIDR}
                    fullWidth
                    size="small"
                    value={formData.tunCIDR}
                    onChange={handleChange('tunCIDR')}
                    placeholder="10.0.0.1/24"
                    helperText={t.profiles.tunCIDRHint}
                    inputProps={{ autoCapitalize: 'off', autoCorrect: 'off' }}
                  />

                  {/* 静态路由配置 */}
                  {!formData.tunGlobal && (
                    <Box>
                      <Typography variant="subtitle2" sx={{ mb: 1 }}>
                        {t.profiles.tunRoutes}
                      </Typography>
                      {formData.tunRoutes.map((route, index) => (
                        <Box key={index} sx={{ display: 'flex', gap: 1, mb: 1 }}>
                          <TextField
                            size="small"
                            fullWidth
                            value={route}
                            onChange={(e) => handleTunRouteChange(index, e.target.value)}
                            placeholder={t.profiles.routePlaceholder}
                            inputProps={{ 'aria-label': t.profiles.routeLabel.replace('{index}', String(index + 1)), autoCapitalize: 'off', autoCorrect: 'off' }}
                          />
                          <IconButton size="small" color="error" aria-label={t.profiles.removeRoute.replace('{index}', String(index + 1))} onClick={() => handleRemoveTunRoute(index)}>
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Box>
                      ))}
                      <Button
                        variant="outlined"
                        size="small"
                        startIcon={<AddIcon />}
                        onClick={handleAddTunRoute}
                        sx={{ mt: 1 }}
                      >
                        {t.profiles.addRoute}
                      </Button>
                    </Box>
                  )}

                  {/* NAT 映射配置 */}
                  <Box>
                    <Typography variant="subtitle2" sx={{ mb: 1 }}>
                      {t.profiles.tunNAT}
                    </Typography>
                    {formData.tunNAT.map((nat, index) => (
                      <Box key={index} sx={{ display: 'flex', gap: 1, mb: 1, alignItems: 'center' }}>
                        <TextField
                          size="small"
                          sx={{ flex: 1, minWidth: 0 }}
                          value={nat.srcCIDR}
                          onChange={(e) => handleTunNATChange(index, 'srcCIDR', e.target.value)}
                          placeholder={t.profiles.natLocal}
                          inputProps={{ 'aria-label': `${t.profiles.natLocal} ${index + 1}`, autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                        <Typography variant="body2" sx={{ mx: 1 }}>→</Typography>
                        <TextField
                          size="small"
                          sx={{ flex: 1, minWidth: 0 }}
                          value={nat.dstCIDR}
                          onChange={(e) => handleTunNATChange(index, 'dstCIDR', e.target.value)}
                          placeholder={t.profiles.natRemote}
                          inputProps={{ 'aria-label': `${t.profiles.natRemote} ${index + 1}`, autoCapitalize: 'off', autoCorrect: 'off' }}
                        />
                        <IconButton size="small" color="error" aria-label={t.profiles.removeNAT.replace('{index}', String(index + 1))} onClick={() => handleRemoveTunNAT(index)}>
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Box>
                    ))}
                    <Button
                      variant="outlined"
                      size="small"
                      startIcon={<AddIcon />}
                      onClick={handleAddTunNAT}
                      sx={{ mt: 1 }}
                    >
                      {t.profiles.addNAT}
                    </Button>
                  </Box>
                </Box>
              )}
            </Box>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog} disabled={saving}>{t.common.cancel}</Button>
          <Button
            variant="contained"
            onClick={handleSave}
            disabled={saving}
            aria-label={t.common.save}
            aria-busy={saving}
          >
            {saving ? <CircularProgress size={24} /> : t.common.save}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 删除确认对话框 */}
      <Dialog open={deleteDialogOpen} onClose={deleting ? undefined : () => setDeleteDialogOpen(false)}>
        <DialogTitle>{t.profiles.deleteProfile}</DialogTitle>
        <DialogContent>
          {message?.type === 'error' && <Alert severity="error" sx={{ mb: 2 }}>{message.text}</Alert>}
          <Typography>
            {t.profiles.deleteConfirm.replace('{name}', deletingProfile?.name || '')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)} disabled={deleting}>{t.common.cancel}</Button>
          <Button variant="contained" color="error" onClick={handleDelete} disabled={deleting}>
            {t.common.delete}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default ProfilesPage;
