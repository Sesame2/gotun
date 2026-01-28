import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  TextField,
  Switch,
  Select,
  MenuItem,
  FormControl,
  IconButton,
  Snackbar,
  Alert,
} from '@mui/material';
import {
  FolderOpen as FolderIcon,
  Info as InfoIcon,
  GitHub as GitHubIcon,
  BugReport as BugIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useThemeMode } from '../../providers/ThemeProvider';
import { useI18n } from '../../providers/I18nProvider';
import { LocaleKey } from '../../locales';
import {
  GetSettings,
  UpdateSettings,
  OpenFileDialog,
  GetVersion,
  OpenURL,
} from '../../../wailsjs/go/main/App';

type ThemeMode = 'light' | 'dark' | 'system';

const SettingsPage: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const { mode, setMode } = useThemeMode();
  const { locale, setLocale, t } = useI18n();

  const [verbose, setVerbose] = useState(false);
  const [logFile, setLogFile] = useState('');
  const [version, setVersion] = useState('');
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({
    open: false,
    message: '',
    severity: 'success',
  });

  // 加载设置
  useEffect(() => {
    const loadSettings = async () => {
      try {
        const settings = await GetSettings();
        setVerbose(settings.verbose || false);
        setLogFile(settings.logFile || '');
      } catch (err) {
        console.error('加载设置失败:', err);
      }
    };
    loadSettings();
  }, []);

  // 加载版本号
  useEffect(() => {
    GetVersion().then(setVersion).catch(console.error);
  }, []);

  // 保存设置的通用函数
  const saveSettings = async (updates: Record<string, any>) => {
    try {
      const settings = await GetSettings();
      await UpdateSettings({ ...settings, ...updates });
    } catch (err) {
      console.error('保存设置失败:', err);
      setSnackbar({ open: true, message: '保存设置失败', severity: 'error' });
    }
  };

  // 主题切换
  const handleThemeChange = (newMode: ThemeMode) => {
    setMode(newMode);
  };

  // 语言切换
  const handleLanguageChange = (newLocale: LocaleKey) => {
    setLocale(newLocale);
  };

  // 详细日志
  const handleVerboseChange = async (enabled: boolean) => {
    setVerbose(enabled);
    await saveSettings({ verbose: enabled });
  };

  // 日志文件路径
  const handleLogFileChange = async (path: string) => {
    setLogFile(path);
    await saveSettings({ logFile: path });
  };

  // 选择日志文件
  const handleSelectLogFile = async () => {
    try {
      const path = await OpenFileDialog('选择日志文件', []);
      if (path) {
        handleLogFileChange(path);
      }
    } catch (err) {
      console.error('选择文件失败:', err);
    }
  };

  // 打开 GitHub
  const handleOpenGitHub = () => {
    OpenURL('https://github.com/Sesame2/gotun');
  };

  // 打开 Issues
  const handleOpenIssues = () => {
    OpenURL('https://github.com/Sesame2/gotun/issues');
  };

  const cardStyle = {
    borderRadius: 2,
    backgroundColor: isDark ? '#282a36' : '#ffffff',
    boxShadow: isDark ? 'none' : '0 2px 8px rgba(0,0,0,0.1)',
    mb: 2,
  };

  const settingRowStyle = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    py: 1.5,
    borderBottom: `1px solid ${isDark ? '#3a3a4a' : '#e0e0e0'}`,
    '&:last-child': {
      borderBottom: 'none',
    },
  };

  return (
    <Box sx={{ maxWidth: 800, margin: '0 auto' }}>
      {/* 基本设置 */}
      <Card sx={cardStyle}>
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            {t.settings.basic}
          </Typography>

          {/* 主题 */}
          <Box sx={settingRowStyle}>
            <Typography>{t.settings.theme}</Typography>
            <FormControl size="small" sx={{ minWidth: 150 }}>
              <Select
                value={mode}
                onChange={(e) => handleThemeChange(e.target.value as ThemeMode)}
              >
                <MenuItem value="light">{t.settings.themeLight}</MenuItem>
                <MenuItem value="dark">{t.settings.themeDark}</MenuItem>
                <MenuItem value="system">{t.settings.themeSystem}</MenuItem>
              </Select>
            </FormControl>
          </Box>

          {/* 语言 */}
          <Box sx={settingRowStyle}>
            <Typography>{t.settings.language}</Typography>
            <FormControl size="small" sx={{ minWidth: 150 }}>
              <Select
                value={locale}
                onChange={(e) => handleLanguageChange(e.target.value as LocaleKey)}
              >
                <MenuItem value="zh-CN">简体中文</MenuItem>
                <MenuItem value="en-US">English</MenuItem>
              </Select>
            </FormControl>
          </Box>
        </CardContent>
      </Card>

      {/* 日志设置 */}
      <Card sx={cardStyle}>
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            {t.settings.log}
          </Typography>

          {/* 详细日志模式 */}
          <Box sx={settingRowStyle}>
            <Typography>{t.settings.verboseLog}</Typography>
            <Switch
              checked={verbose}
              onChange={(e) => handleVerboseChange(e.target.checked)}
            />
          </Box>

          {/* 日志文件路径 */}
          <Box sx={{ ...settingRowStyle, flexDirection: 'column', alignItems: 'stretch', gap: 1 }}>
            <Typography>{t.settings.logFilePath}</Typography>
            <TextField
              fullWidth
              size="small"
              value={logFile}
              onChange={(e) => handleLogFileChange(e.target.value)}
              placeholder={t.settings.logFileHint}
              InputProps={{
                endAdornment: (
                  <IconButton onClick={handleSelectLogFile} edge="end" size="small">
                    <FolderIcon />
                  </IconButton>
                ),
              }}
            />
          </Box>
        </CardContent>
      </Card>

      {/* 关于 */}
      <Card sx={cardStyle}>
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            {t.settings.about}
          </Typography>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <InfoIcon color="primary" />
            <Typography variant="body1">GoTun</Typography>
          </Box>
          <Typography variant="body2" color="textSecondary" sx={{ mb: 1 }}>
            {t.settings.version}: {version || 'dev'}
          </Typography>
          <Typography variant="body2" color="textSecondary" sx={{ mb: 2 }}>
            {t.settings.description}
          </Typography>
          <Box sx={{ display: 'flex', gap: 1 }}>
            <Button
              size="small"
              variant="outlined"
              startIcon={<GitHubIcon />}
              onClick={handleOpenGitHub}
            >
              {t.settings.github}
            </Button>
            <Button
              size="small"
              variant="outlined"
              startIcon={<BugIcon />}
              onClick={handleOpenIssues}
            >
              {t.settings.feedback}
            </Button>
          </Box>
        </CardContent>
      </Card>

      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Box>
  );
};

export default SettingsPage;
