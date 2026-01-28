import React, { useState, useEffect, useRef, useCallback } from 'react';
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
} from '@mui/material';
import {
  Delete as ClearIcon,
  Download as DownloadIcon,
  Pause as PauseIcon,
  PlayArrow as ResumeIcon,
  Search as SearchIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { GetLogs, ClearLogs, SaveFileDialog, SaveLogsToFile } from '../../../wailsjs/go/main/App';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';
import { main } from '../../../wailsjs/go/models';

type LogEntry = main.LogEntry;

const LogsPage: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [searchText, setSearchText] = useState('');
  const [filter, setFilter] = useState<string>('all');
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({ open: false, message: '', severity: 'success' });
  const logsEndRef = useRef<HTMLDivElement>(null);

  // 加载初始日志
  useEffect(() => {
    const loadLogs = async () => {
      try {
        const data = await GetLogs();
        setLogs(data || []);
      } catch (err) {
        console.error('加载日志失败:', err);
      }
    };
    loadLogs();
  }, []);

  // 监听实时日志事件
  useEffect(() => {
    if (paused) return;

    const handleNewLog = (entry: LogEntry) => {
      setLogs(prev => {
        const newLogs = [...prev, entry];
        // 限制前端日志数量
        if (newLogs.length > 1000) {
          return newLogs.slice(-1000);
        }
        return newLogs;
      });
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
    try {
      await ClearLogs();
      setLogs([]);
    } catch (err) {
      console.error('清空日志失败:', err);
    }
  };

  const handleExport = async () => {
    try {
      const defaultName = `gotun-logs-${new Date().toISOString().split('T')[0]}.txt`;
      const filePath = await SaveFileDialog('保存日志文件', defaultName);
      if (filePath) {
        await SaveLogsToFile(filePath);
        setSnackbar({ open: true, message: '日志已保存', severity: 'success' });
      }
    } catch (err) {
      console.error('保存日志失败:', err);
      setSnackbar({ open: true, message: '保存日志失败', severity: 'error' });
    }
  };

  const getLogColor = (level: string) => {
    switch (level) {
      case 'ERROR':
        return '#f44336';
      case 'WARN':
        return '#ff9800';
      case 'INFO':
        return '#2196f3';
      case 'DEBUG':
        return '#9e9e9e';
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
    <Box sx={{ maxWidth: 1200, margin: '0 auto', height: 'calc(100vh - 150px)', display: 'flex', flexDirection: 'column' }}>
      <Card sx={{ ...cardStyle, flex: 1, display: 'flex', flexDirection: 'column' }}>
        <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {/* 工具栏 */}
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 1 }}>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
              <TextField
                size="small"
                placeholder="搜索日志..."
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
                    label={level === 'all' ? '全部' : level}
                    size="small"
                    variant={filter === level ? 'filled' : 'outlined'}
                    color={filter === level ? 'primary' : 'default'}
                    onClick={() => setFilter(level)}
                  />
                ))}
              </Box>
            </Box>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
              <FormControlLabel
                control={
                  <Switch
                    size="small"
                    checked={autoScroll}
                    onChange={(e) => setAutoScroll(e.target.checked)}
                  />
                }
                label="自动滚动"
              />
              <IconButton
                size="small"
                onClick={() => setPaused(!paused)}
                title={paused ? '继续' : '暂停'}
              >
                {paused ? <ResumeIcon /> : <PauseIcon />}
              </IconButton>
              <IconButton size="small" onClick={handleExport} title="导出日志">
                <DownloadIcon />
              </IconButton>
              <IconButton size="small" onClick={handleClear} title="清空日志">
                <ClearIcon />
              </IconButton>
            </Box>
          </Box>

          {/* 日志内容 */}
          <Box
            className="log-content"
            sx={{
              flex: 1,
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
                暂无日志
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
                  <span style={{ color: '#888', whiteSpace: 'nowrap', userSelect: 'text' }}>
                    {new Date(log.timestamp).toLocaleTimeString()}
                  </span>
                  <span
                    style={{
                      color: getLogColor(log.level),
                      fontWeight: 600,
                      minWidth: 50,
                      userSelect: 'text',
                    }}
                  >
                    [{log.level}]
                  </span>
                  <span style={{ color: isDark ? '#e0e0e0' : '#333', wordBreak: 'break-all', userSelect: 'text' }}>
                    {log.message}
                  </span>
                </Box>
              ))
            )}
            <div ref={logsEndRef} />
          </Box>
        </CardContent>
      </Card>

      <Snackbar 
        open={snackbar.open} 
        autoHideDuration={3000} 
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Box>
  );
};

export default LogsPage;
