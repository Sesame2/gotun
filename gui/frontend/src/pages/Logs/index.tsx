import React, { useState, useEffect, useRef } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  IconButton,
  TextField,
  FormControlLabel,
  Switch,
  Chip,
  Snackbar,
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
} from '@mui/material';
import ClearIcon from '@mui/icons-material/Delete';
import DownloadIcon from '@mui/icons-material/Download';
import PauseIcon from '@mui/icons-material/Pause';
import ResumeIcon from '@mui/icons-material/PlayArrow';
import SearchIcon from '@mui/icons-material/Search';
import { useTheme } from '@mui/material/styles';
import { GetLogs, ClearLogs, SaveFileDialog, SaveLogsToFile } from '../../../wailsjs/go/main/App';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';
import { main } from '../../../wailsjs/go/models';
import { useI18n } from '../../providers/I18nProvider';

type LogEntry = main.LogEntry;

const mergeLogs = (history: LogEntry[], incoming: LogEntry[]): LogEntry[] => {
  const byID = new Map(history.map(entry => [entry.id, entry]));
  incoming.forEach(entry => byID.set(entry.id, entry));
  return Array.from(byID.values()).sort((a, b) => a.id - b.id).slice(-1000);
};

const LogsPage: React.FC = () => {
  const { t } = useI18n();
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [searchText, setSearchText] = useState('');
  const [filter, setFilter] = useState<string>('all');
  const [clearDialogOpen, setClearDialogOpen] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [clearError, setClearError] = useState<string | null>(null);
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({ open: false, message: '', severity: 'success' });
  const logsEndRef = useRef<HTMLDivElement>(null);
  const ignoreInitialLogs = useRef(false);

  // 加载初始日志
  useEffect(() => {
    let active = true;
    const loadLogs = async () => {
      try {
        const data = await GetLogs();
        if (active && !ignoreInitialLogs.current) {
          setLogs(current => mergeLogs(data || [], current));
        }
      } catch (err) {
        if (!active) return;
        console.error('加载日志失败:', err);
        setSnackbar({ open: true, message: `${t.logs.loadFailed}: ${err instanceof Error ? err.message : String(err)}`, severity: 'error' });
      }
    };
    loadLogs();
    return () => { active = false; };
  }, []);

  // 监听实时日志事件
  useEffect(() => {
    if (paused) return;

    const handleNewLog = (entry: LogEntry) => {
      setLogs(prev => mergeLogs(prev, [entry]));
    };

    EventsOn('log:new', handleNewLog);

    return () => {
      EventsOff('log:new');
    };
  }, [paused]);

  // 自动滚动
  useEffect(() => {
    if (autoScroll && !paused && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs, autoScroll, paused]);

  const handleClear = async () => {
    if (clearing) return;
    setClearing(true);
    setClearError(null);
    try {
      await ClearLogs();
      ignoreInitialLogs.current = true;
      setLogs([]);
      setClearDialogOpen(false);
    } catch (err) {
      console.error('清空日志失败:', err);
      setClearError(`${t.logs.clearFailed}: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setClearing(false);
    }
  };

  const handleExport = async () => {
    try {
      const now = new Date();
      const date = [now.getFullYear(), String(now.getMonth() + 1).padStart(2, '0'), String(now.getDate()).padStart(2, '0')].join('-');
      const defaultName = `gotun-logs-${date}.txt`;
      const filePath = await SaveFileDialog(t.logs.saveDialogTitle, defaultName);
      if (filePath) {
        await SaveLogsToFile(filePath);
        setSnackbar({ open: true, message: t.logs.saveSuccess, severity: 'success' });
      }
    } catch (err) {
      console.error('保存日志失败:', err);
      setSnackbar({ open: true, message: t.logs.saveFailed, severity: 'error' });
    }
  };

  const getLogColor = (level: string) => {
    switch (level) {
      case 'ERROR':
        return isDark ? '#f44336' : '#c62828';
      case 'WARN':
        return isDark ? '#ff9800' : '#9a4d00';
      case 'INFO':
        return isDark ? '#2196f3' : '#1565c0';
      case 'DEBUG':
        return isDark ? '#9e9e9e' : '#616161';
      default:
        return isDark ? '#fff' : '#000';
    }
  };

  const filteredLogs = logs.filter(log => {
    if (filter !== 'all' && log.level !== filter) return false;
    if (searchText && !log.message.toLowerCase().includes(searchText.toLowerCase())) return false;
    return true;
  });

  const cardStyle = {
    borderRadius: 2,
    backgroundColor: isDark ? '#282a36' : '#ffffff',
    boxShadow: isDark ? 'none' : '0 2px 8px rgba(0,0,0,0.1)',
  };

  return (
    <Box sx={{ maxWidth: 1200, minWidth: 0, margin: '0 auto', height: 'calc(100vh - 150px)', display: 'flex', flexDirection: 'column' }}>
      <Card sx={{ ...cardStyle, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        <CardContent sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {/* 工具栏 */}
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 1 }}>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap', minWidth: 0 }}>
              <TextField
                size="small"
                placeholder={t.logs.search}
                inputProps={{ 'aria-label': t.logs.search }}
                value={searchText}
                onChange={(e) => setSearchText(e.target.value)}
                InputProps={{
                  startAdornment: <SearchIcon sx={{ mr: 1, color: 'text.secondary' }} />,
                }}
                sx={{ width: 200 }}
              />
              <Box sx={{ display: 'flex', gap: 0.5 }}>
                {['all', 'DEBUG', 'INFO', 'WARN', 'ERROR'].map((level) => (
                  <Chip
                    key={level}
                    label={level === 'all' ? t.logs.all : level}
                    aria-pressed={filter === level}
                    size="small"
                    variant={filter === level ? 'filled' : 'outlined'}
                    color={filter === level ? 'primary' : 'default'}
                    onClick={() => setFilter(level)}
                  />
                ))}
              </Box>
            </Box>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap' }}>
              <FormControlLabel
                control={
                  <Switch
                    size="small"
                    checked={autoScroll}
                    onChange={(e) => setAutoScroll(e.target.checked)}
                  />
                }
                label={t.logs.autoScroll}
              />
              <IconButton
                size="small"
                onClick={() => setPaused(!paused)}
                title={paused ? t.logs.resume : t.logs.pause}
                aria-label={paused ? t.logs.resume : t.logs.pause}
                aria-pressed={paused}
              >
                {paused ? <ResumeIcon /> : <PauseIcon />}
              </IconButton>
              <IconButton size="small" onClick={handleExport} title={t.logs.export} aria-label={t.logs.export}>
                <DownloadIcon />
              </IconButton>
              <IconButton size="small" onClick={() => { setClearError(null); setClearDialogOpen(true); }} title={t.logs.clear} aria-label={t.logs.clear}>
                <ClearIcon />
              </IconButton>
            </Box>
          </Box>

          {/* 日志内容 */}
          <Box
            className="log-content"
            sx={{
              flex: 1,
              minHeight: 0,
              overflow: 'auto',
              backgroundColor: isDark ? '#1a1b26' : '#f8f9fa',
              borderRadius: 1,
              padding: 2,
              fontFamily: 'Monaco, Menlo, "Ubuntu Mono", Consolas, monospace',
              fontSize: 13,
              lineHeight: 1.6,
              userSelect: 'text !important',
              WebkitUserSelect: 'text !important',
              cursor: 'text',
            }}
          >
            {filteredLogs.length === 0 ? (
              <Typography color="textSecondary" sx={{ textAlign: 'center', py: 4, userSelect: 'none' }}>
                {t.logs.noLogs}
              </Typography>
            ) : (
              filteredLogs.map((log) => (
                <Box
                  key={log.id}
                  sx={{
                    display: 'flex',
                    gap: 1,
                    mb: 0.5,
                    '&:hover': { backgroundColor: isDark ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.02)' },
                    padding: '2px 4px',
                    borderRadius: 1,
                    userSelect: 'text',
                    WebkitUserSelect: 'text',
                    cursor: 'text',
                  }}
                >
                  <span style={{ color: isDark ? '#888' : '#666', flexShrink: 0, whiteSpace: 'nowrap', userSelect: 'text' }}>
                    {new Date(log.timestamp).toLocaleTimeString()}
                  </span>
                  <span
                    style={{
                      color: getLogColor(log.level),
                      fontWeight: 600,
                      minWidth: 50,
                      flexShrink: 0,
                      userSelect: 'text',
                    }}
                  >
                    [{log.level}]
                  </span>
                  <span style={{ color: isDark ? '#e0e0e0' : '#333', minWidth: 0, overflowWrap: 'anywhere', wordBreak: 'break-word', userSelect: 'text' }}>
                    {log.message}
                  </span>
                </Box>
              ))
            )}
            <div ref={logsEndRef} />
          </Box>
        </CardContent>
      </Card>

      <Dialog open={clearDialogOpen} onClose={clearing ? undefined : () => setClearDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>{t.logs.clearConfirmTitle}</DialogTitle>
        <DialogContent>
          {clearError && <Alert severity="error" sx={{ mb: 2 }}>{clearError}</Alert>}
          <Typography>{t.logs.clearConfirm}</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setClearDialogOpen(false)} disabled={clearing}>{t.common.cancel}</Button>
          <Button onClick={handleClear} disabled={clearing} color="error" variant="contained" aria-busy={clearing}>{t.logs.clear}</Button>
        </DialogActions>
      </Dialog>

      <Snackbar 
        open={snackbar.open} 
        autoHideDuration={3000} 
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} closeText={t.common.close} onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Box>
  );
};

export default LogsPage;
