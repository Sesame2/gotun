import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import LogsPage from './index';
import { deferred, installBridge, logEntry, settings } from '../../test/bridge';
import { I18nProvider } from '../../providers/I18nProvider';
import { ThemeProvider } from '../../providers/ThemeProvider';
import { getContrastRatio } from '@mui/material/styles';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); });

function renderLogs() {
  return render(<I18nProvider><LogsPage /></I18nProvider>);
}

async function confirmClear() {
  if (!screen.queryByRole('dialog')) await userEvent.click(screen.getByTitle('清空日志'));
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '清空日志' }));
}

it('combines case-insensitive search with level filtering', async () => {
  bridge.app.GetLogs.mockResolvedValue([
    logEntry(1, 'INFO', 'Tunnel connected'), logEntry(2, 'ERROR', 'Tunnel failed'),
    logEntry(3, 'ERROR', 'disk failed'),
  ]);
  renderLogs();
  await screen.findByText('Tunnel connected');
  await userEvent.type(screen.getByPlaceholderText('搜索日志...'), 'TUNNEL');
  expect(screen.queryByText('disk failed')).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: 'ERROR' }));
  expect(screen.getByText('Tunnel failed')).toBeInTheDocument();
  expect(screen.queryByText('Tunnel connected')).not.toBeInTheDocument();
  await userEvent.clear(screen.getByPlaceholderText('搜索日志...'));
  expect(screen.getByText('disk failed')).toBeInTheDocument();
});

it('subscribes to real-time logs, pauses, resumes, and cleans up on unmount', async () => {
  const { unmount } = renderLogs();
  await waitFor(() => expect(bridge.app.GetLogs).toHaveBeenCalled());
  act(() => bridge.emit('log:new', logEntry(1, 'INFO', 'first log')));
  expect(screen.getByText('first log')).toBeInTheDocument();
  await userEvent.click(screen.getByTitle('暂停'));
  expect(bridge.hasListener('log:new')).toBe(false);
  act(() => bridge.emit('log:new', logEntry(2, 'INFO', 'paused log')));
  expect(screen.queryByText('paused log')).not.toBeInTheDocument();
  await userEvent.click(screen.getByTitle('继续'));
  act(() => bridge.emit('log:new', logEntry(3, 'INFO', 'resumed log')));
  expect(screen.getByText('resumed log')).toBeInTheDocument();
  unmount();
  expect(bridge.hasListener('log:new')).toBe(false);
});

it('limits live log history to the newest 1000 entries', async () => {
  renderLogs();
  await waitFor(() => expect(bridge.app.GetLogs).toHaveBeenCalled());
  act(() => { for (let id = 1; id <= 1001; id++) bridge.emit('log:new', logEntry(id, 'INFO', `entry ${id}`)); });
  expect(screen.queryByText('entry 1', { exact: true })).not.toBeInTheDocument();
  expect(screen.getByText('entry 2', { exact: true })).toBeInTheDocument();
  expect(screen.getByText('entry 1001', { exact: true })).toBeInTheDocument();
});

it('clears displayed logs only after the native clear succeeds', async () => {
  const errorLog = vi.spyOn(console, 'error').mockImplementation(() => {});
  bridge.app.GetLogs.mockResolvedValue([logEntry(1, 'INFO', 'keep until clear')]);
  renderLogs();
  await screen.findByText('keep until clear');
  bridge.app.ClearLogs.mockRejectedValueOnce(new Error('native failed'));
  await confirmClear();
  await waitFor(() => expect(errorLog).toHaveBeenCalled());
  expect(screen.getByText('keep until clear')).toBeInTheDocument();
  await confirmClear();
  await screen.findByText('暂无日志');
});

it('exports to the chosen path, cancels without writing, and displays write failures', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  renderLogs();
  await waitFor(() => expect(bridge.app.GetLogs).toHaveBeenCalled());
  await userEvent.click(screen.getByTitle('导出日志'));
  expect(bridge.app.SaveLogsToFile).not.toHaveBeenCalled();
  bridge.app.SaveFileDialog.mockResolvedValue('/tmp/gotun-test.log');
  await userEvent.click(screen.getByTitle('导出日志'));
  await screen.findByText('日志已保存');
  expect(bridge.app.SaveLogsToFile).toHaveBeenCalledWith('/tmp/gotun-test.log');
  bridge.app.SaveLogsToFile.mockRejectedValueOnce(new Error('read-only'));
  await userEvent.click(screen.getByTitle('导出日志'));
  await screen.findByText('保存日志失败');
});

it('labels search and exposes the active log level to assistive technology', async () => {
  renderLogs();
  expect(screen.getByRole('textbox', { name: '搜索日志...' })).toBeEnabled();
  expect(screen.getByRole('button', { name: '全部' })).toHaveAttribute('aria-pressed', 'true');
  for (const level of ['DEBUG', 'INFO', 'WARN', 'ERROR']) {
    await userEvent.click(screen.getByRole('button', { name: level }));
    expect(screen.getByRole('button', { name: level })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: '全部' })).toHaveAttribute('aria-pressed', 'false');
  }
});

it('shows native clear failures and keeps the displayed logs', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  bridge.app.GetLogs.mockResolvedValue([logEntry(1, 'INFO', 'preserved log')]);
  bridge.app.ClearLogs.mockRejectedValueOnce(new Error('clear unavailable'));
  renderLogs();
  await screen.findByText('preserved log');
  await confirmClear();
  expect(await screen.findByRole('alert')).toHaveTextContent('clear unavailable');
  expect(screen.getByText('preserved log')).toBeInTheDocument();
});

it('reports load and save-dialog failures without writing a file', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  bridge.app.GetLogs.mockRejectedValueOnce(new Error('log store unavailable'));
  renderLogs();
  expect(await screen.findByRole('alert')).toHaveTextContent('log store unavailable');
  bridge.app.SaveFileDialog.mockRejectedValueOnce(new Error('save dialog unavailable'));
  await userEvent.click(screen.getByRole('button', { name: '导出日志' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('保存日志失败');
  expect(bridge.app.SaveLogsToFile).not.toHaveBeenCalled();
});

it('does not auto-scroll new logs while the labelled switch is off', async () => {
  renderLogs();
  await waitFor(() => expect(bridge.app.GetLogs).toHaveBeenCalled());
  const scrolling = vi.mocked(Element.prototype.scrollIntoView);
  const toggle = screen.getByRole('switch', { name: '自动滚动' });
  await userEvent.click(toggle);
  expect(toggle).not.toBeChecked();
  scrolling.mockClear();
  act(() => bridge.emit('log:new', logEntry(1, 'INFO', 'no scrolling')));
  expect(scrolling).not.toHaveBeenCalled();
  await userEvent.click(toggle);
  expect(toggle).toBeChecked();
  expect(scrolling).toHaveBeenCalled();
});

it('uses English for search, filters, and pause/resume when selected', async () => {
  bridge.setSettings(settings({ language: 'en-US' }));
  renderLogs();
  await screen.findByRole('textbox', { name: 'Search logs...' });
  expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true');
  await userEvent.click(screen.getByRole('button', { name: 'Pause' }));
  expect(screen.getByRole('button', { name: 'Resume' })).toHaveAttribute('aria-pressed', 'true');
});

it('asks before clearing logs and cancels without calling the native clear operation', async () => {
  bridge.app.GetLogs.mockResolvedValue([logEntry(1, 'INFO', 'do not discard')]);
  renderLogs();
  await screen.findByText('do not discard');
  await userEvent.click(screen.getByTitle('清空日志'));
  expect(bridge.app.ClearLogs).not.toHaveBeenCalled();
  const confirmation = within(screen.getByRole('dialog'));
  expect(confirmation.getByText('确定要清空所有日志吗？此操作无法撤销。')).toBeInTheDocument();
  await userEvent.click(confirmation.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(bridge.app.ClearLogs).not.toHaveBeenCalled();
  expect(screen.getByText('do not discard')).toBeInTheDocument();
});

it('keeps a pending clear confirmation open and prevents repeated native clears', async () => {
  const clearing = deferred<void>();
  bridge.app.ClearLogs.mockReturnValueOnce(clearing.promise);
  renderLogs();
  await confirmClear();
  const confirmation = within(screen.getByRole('dialog'));
  expect(confirmation.getByRole('button', { name: '清空日志' })).toBeDisabled();
  expect(confirmation.getByRole('button', { name: '取消' })).toBeDisabled();
  await userEvent.keyboard('{Escape}');
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  await act(async () => clearing.resolve());
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(bridge.app.ClearLogs).toHaveBeenCalledTimes(1);
});

it('cancels a failed clear confirmation without discarding displayed logs', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  bridge.app.GetLogs.mockResolvedValue([logEntry(1, 'INFO', 'keep after failure')]);
  bridge.app.ClearLogs.mockRejectedValueOnce(new Error('store is read-only'));
  renderLogs();
  await screen.findByText('keep after failure');
  await confirmClear();
  const confirmation = within(screen.getByRole('dialog'));
  expect(await confirmation.findByRole('alert')).toHaveTextContent('store is read-only');
  await userEvent.click(confirmation.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(screen.getByText('keep after failure')).toBeInTheDocument();
});

it('dismisses export feedback through its labelled close action', async () => {
  bridge.app.SaveFileDialog.mockResolvedValue('/tmp/gotun-test.log');
  renderLogs();
  await userEvent.click(screen.getByRole('button', { name: '导出日志' }));
  await screen.findByText('日志已保存');
  await userEvent.click(screen.getByRole('button', { name: '关闭' }));
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
});

it('uses the local calendar date for the suggested export filename after midnight', async () => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(2026, 9, 19, 0, 30));
  renderLogs();
  await userEvent.click(screen.getByRole('button', { name: '导出日志' }));
  expect(bridge.app.SaveFileDialog).toHaveBeenCalledWith('保存日志文件', 'gotun-logs-2026-10-19.txt');
});

it.each(['ERROR', 'WARN', 'INFO', 'DEBUG'])('keeps light-mode %s log labels at readable text contrast', async level => {
  bridge.setSettings(settings({ theme: 'light' }));
  bridge.app.GetLogs.mockResolvedValue([logEntry(1, level, 'contrast sample')]);
  render(<ThemeProvider><I18nProvider><LogsPage /></I18nProvider></ThemeProvider>);
  const label = await screen.findByText(`[${level}]`);
  const container = label.closest('.log-content')!;
  await waitFor(() => expect(container).toHaveStyle({ backgroundColor: '#f8f9fa' }));
  const foreground = getComputedStyle(label).color;
  const background = getComputedStyle(container).backgroundColor;
  expect(getContrastRatio(foreground, background)).toBeGreaterThanOrEqual(4.5);
});

it('keeps light-mode timestamps readable against the actual log background', async () => {
  bridge.setSettings(settings({ theme: 'light' }));
  bridge.app.GetLogs.mockResolvedValue([logEntry(1, 'INFO', 'timestamp contrast sample')]);
  render(<ThemeProvider><I18nProvider><LogsPage /></I18nProvider></ThemeProvider>);
  const label = await screen.findByText('[INFO]');
  const container = label.closest('.log-content')!;
  await waitFor(() => expect(container).toHaveStyle({ backgroundColor: '#f8f9fa' }));
  const timestamp = label.previousElementSibling!;
  expect(getContrastRatio(getComputedStyle(timestamp).color, getComputedStyle(container).backgroundColor)).toBeGreaterThanOrEqual(4.5);
});

it('preserves the existing dark log-level palette', async () => {
  bridge.setSettings(settings({ theme: 'dark' }));
  const colors = { ERROR: '#f44336', WARN: '#ff9800', INFO: '#2196f3', DEBUG: '#9e9e9e' };
  bridge.app.GetLogs.mockResolvedValue(Object.keys(colors).map((level, index) => logEntry(index + 1, level, `${level} sample`)));
  render(<ThemeProvider><I18nProvider><LogsPage /></I18nProvider></ThemeProvider>);
  for (const [level, color] of Object.entries(colors)) {
    expect(await screen.findByText(`[${level}]`)).toHaveStyle({ color });
  }
});

it('merges a late initial snapshot with already received live entries by ID', async () => {
  const snapshot = deferred<ReturnType<typeof logEntry>[]>();
  bridge.app.GetLogs.mockReturnValueOnce(snapshot.promise);
  renderLogs();
  act(() => bridge.emit('log:new', logEntry(2, 'INFO', 'live current version')));
  act(() => bridge.emit('log:new', logEntry(3, 'INFO', 'live following entry')));
  await act(async () => snapshot.resolve([
    logEntry(1, 'INFO', 'initial history'), logEntry(2, 'INFO', 'stale snapshot version'),
  ]));
  expect(screen.getByText('initial history')).toBeInTheDocument();
  expect(screen.getAllByText('live current version')).toHaveLength(1);
  expect(screen.getByText('live following entry')).toBeInTheDocument();
  expect(screen.queryByText('stale snapshot version')).not.toBeInTheDocument();
  const messages = Array.from(document.querySelectorAll('.log-content > div > span:last-child')).map(element => element.textContent);
  expect(messages).toEqual(['initial history', 'live current version', 'live following entry']);
});

it('deduplicates repeated live event IDs', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  renderLogs();
  await waitFor(() => expect(bridge.app.GetLogs).toHaveBeenCalled());
  act(() => {
    bridge.emit('log:new', logEntry(7, 'INFO', 'repeated log event'));
    bridge.emit('log:new', logEntry(7, 'INFO', 'repeated log event'));
  });
  expect(screen.getAllByText('repeated log event')).toHaveLength(1);
});

it('does not restore a stale initial snapshot after the user clears logs', async () => {
  const snapshot = deferred<ReturnType<typeof logEntry>[]>();
  bridge.app.GetLogs.mockReturnValueOnce(snapshot.promise);
  renderLogs();
  act(() => bridge.emit('log:new', logEntry(2, 'INFO', 'clear this live entry')));
  await confirmClear();
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await act(async () => snapshot.resolve([logEntry(1, 'INFO', 'stale cleared history')]));
  expect(screen.queryByText('stale cleared history')).not.toBeInTheDocument();
  expect(screen.queryByText('clear this live entry')).not.toBeInTheDocument();
  act(() => bridge.emit('log:new', logEntry(3, 'INFO', 'new after clear')));
  expect(screen.getByText('new after clear')).toBeInTheDocument();
});

it('keeps pause behavior when the initial snapshot arrives late', async () => {
  const snapshot = deferred<ReturnType<typeof logEntry>[]>();
  bridge.app.GetLogs.mockReturnValueOnce(snapshot.promise);
  renderLogs();
  act(() => bridge.emit('log:new', logEntry(2, 'INFO', 'before pause')));
  await userEvent.click(screen.getByRole('button', { name: '暂停' }));
  act(() => bridge.emit('log:new', logEntry(3, 'INFO', 'ignored while paused')));
  await act(async () => snapshot.resolve([logEntry(1, 'INFO', 'initial history')]));
  expect(screen.getByText('before pause')).toBeInTheDocument();
  expect(screen.queryByText('ignored while paused')).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: '继续' }));
  act(() => bridge.emit('log:new', logEntry(4, 'INFO', 'after resume')));
  expect(screen.getByText('after resume')).toBeInTheDocument();
});
