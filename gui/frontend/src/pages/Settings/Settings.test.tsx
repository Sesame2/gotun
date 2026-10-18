import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import SettingsPage from './index';
import { ThemeProvider } from '../../providers/ThemeProvider';
import { I18nProvider } from '../../providers/I18nProvider';
import { useTheme } from '@mui/material/styles';
import { deferred, installBridge, settings } from '../../test/bridge';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); });

function renderSettings() {
  return render(<I18nProvider><ThemeProvider><SettingsPage /><ThemeProbe /></ThemeProvider></I18nProvider>);
}

function ThemeProbe() {
  const theme = useTheme();
  return <output aria-label="primary color">{theme.palette.primary.main}</output>;
}

it('loads saved logging preferences and version, then persists a logging toggle', async () => {
  bridge.setSettings(settings({ logFile: '/tmp/current.log' }));
  renderSettings();
  expect(await screen.findByText(/v1.2.3/)).toBeInTheDocument();
  expect(screen.getByDisplayValue('/tmp/current.log')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('switch'));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledWith(settings({ verbose: true, logFile: '/tmp/current.log' })));
});

it('changes language and theme through their real providers', async () => {
  renderSettings();
  await screen.findByText(/v1.2.3/);
  await userEvent.click(screen.getAllByRole('combobox')[0]);
  await userEvent.click(screen.getByRole('option', { name: '浅色' }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledWith(expect.objectContaining({ theme: 'light' })));
  await userEvent.click(screen.getAllByRole('combobox')[1]);
  await userEvent.click(screen.getByRole('option', { name: 'English' }));
  await screen.findByText('Basic Settings');
  expect((await bridge.app.GetSettings()).language).toBe('en-US');
});

it('reports native persistence errors for logging settings', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  renderSettings();
  await screen.findByText(/v1.2.3/);
  bridge.app.UpdateSettings.mockRejectedValueOnce(new Error('disk full'));
  await userEvent.click(screen.getByRole('switch'));
  expect(await screen.findByText('保存设置失败')).toBeInTheDocument();
  expect((await bridge.app.GetSettings()).verbose).toBe(false);
});

it('opens the project and issue URLs through the native bridge', async () => {
  renderSettings();
  await screen.findByText(/v1.2.3/);
  await userEvent.click(screen.getByRole('button', { name: /GitHub/ }));
  await userEvent.click(screen.getByRole('button', { name: '反馈问题' }));
  expect(bridge.app.OpenURL.mock.calls).toEqual([
    ['https://github.com/Sesame2/gotun'], ['https://github.com/Sesame2/gotun/issues'],
  ]);
});

it('persists a selected log file and leaves it unchanged when the chooser is cancelled', async () => {
  bridge.app.OpenFileDialog.mockResolvedValueOnce('/tmp/selected.log');
  renderSettings();
  await screen.findByText(/v1.2.3/);
  const chooser = screen.getByRole('button', { name: '选择日志文件' });
  await userEvent.click(chooser);
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledWith(settings({ logFile: '/tmp/selected.log' })));
  expect(screen.getByRole('textbox')).toHaveValue('/tmp/selected.log');
  await userEvent.click(chooser);
  expect(bridge.app.UpdateSettings).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('textbox')).toHaveValue('/tmp/selected.log');
});

it('gives every form control and file chooser an accessible name and a clickable switch label', async () => {
  renderSettings();
  await screen.findByText(/v1.2.3/);
  expect(screen.getByRole('combobox', { name: '主题' })).toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: '语言' })).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: '日志文件路径' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: '选择日志文件' })).toBeInTheDocument();
  const verbose = screen.getByRole('switch', { name: '详细日志模式' });
  await userEvent.click(screen.getByText('详细日志模式'));
  expect(verbose).toBeChecked();
});

it.each(['浅色', '深色', '跟随系统'])('persists the %s theme option', async (name) => {
  if (name === '深色') bridge.setSettings(settings({ theme: 'light' }));
  renderSettings();
  await screen.findByText(/v1.2.3/);
  await userEvent.click(screen.getByRole('combobox', { name: '主题' }));
  await userEvent.click(screen.getByRole('option', { name }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledWith(expect.objectContaining({ theme: name === '浅色' ? 'light' : name === '深色' ? 'dark' : 'system' })));
});

it('shows and dismisses file-dialog and external-link errors', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  renderSettings();
  await screen.findByText(/v1.2.3/);
  bridge.app.OpenFileDialog.mockRejectedValueOnce(new Error('file dialog unavailable'));
  await userEvent.click(screen.getByRole('button', { name: '选择日志文件' }));
  await screen.findByText('file dialog unavailable');
  await userEvent.click(screen.getByRole('button', { name: '关闭' }));
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  bridge.app.OpenURL.mockRejectedValueOnce(new Error('browser unavailable'));
  await userEvent.click(screen.getByRole('button', { name: 'GitHub' }));
  await screen.findByText('browser unavailable');
});

it.each(['theme', 'language'])('reports a failed %s preference write to the user', async (preference) => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  renderSettings();
  await screen.findByText(/v1.2.3/);
  bridge.app.UpdateSettings.mockRejectedValueOnce(new Error('read-only'));
  await userEvent.click(screen.getByRole('combobox', { name: preference === 'theme' ? '主题' : '语言' }));
  await userEvent.click(screen.getByRole('option', { name: preference === 'theme' ? '浅色' : 'English' }));
  await screen.findByText('保存设置失败');
});

it('keeps dark primary button text above a 4.5 to 1 contrast ratio', async () => {
  renderSettings();
  await screen.findByText(/v1.2.3/);
  const luminance = (color: string) => {
    const channels = color.startsWith('#')
      ? [1, 3, 5].map(index => parseInt(color.slice(index, index + 2), 16) / 255)
      : color.match(/[\d.]+/g)!.slice(0, 3).map(value => Number(value) / 255);
    const linear = channels.map(value => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4);
    return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
  };
  const foreground = luminance(screen.getByLabelText('primary color').textContent!);
  const background = luminance('#282a36');
  expect((Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)).toBeGreaterThanOrEqual(4.5);
});

it('preserves logging edits made while a theme preference is still being saved', async () => {
  const first = deferred<void>();
  bridge.app.UpdateSettings.mockImplementationOnce(async next => {
    await first.promise;
    bridge.setSettings(next);
  });
  renderSettings();
  await screen.findByText(/v1.2.3/);
  await userEvent.click(screen.getByRole('combobox', { name: '主题' }));
  await userEvent.click(screen.getByRole('option', { name: '浅色' }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledTimes(1));
  fireEvent.change(screen.getByRole('textbox', { name: '日志文件路径' }), { target: { value: '/tmp/fresh.log' } });
  await userEvent.click(screen.getByRole('switch', { name: '详细日志模式' }));
  await act(async () => { first.resolve(); });
  await waitFor(async () => expect(await bridge.app.GetSettings()).toEqual(settings({ theme: 'light', logFile: '/tmp/fresh.log', verbose: true })));
});

it('waits for the initial logging preferences before allowing edits', async () => {
  const initial = deferred<ReturnType<typeof settings>>();
  bridge.app.GetSettings.mockReturnValueOnce(initial.promise);
  renderSettings();
  await screen.findByText(/v1.2.3/);
  expect(screen.getByRole('switch', { name: '详细日志模式' })).toBeDisabled();
  expect(screen.getByRole('textbox', { name: '日志文件路径' })).toBeDisabled();
  expect(screen.getByRole('button', { name: '选择日志文件' })).toBeDisabled();
  await act(async () => { initial.resolve(settings({ verbose: true, logFile: '/tmp/original.log' })); });
  expect(screen.getByRole('switch', { name: '详细日志模式' })).toBeChecked();
  expect(screen.getByRole('textbox', { name: '日志文件路径' })).toHaveValue('/tmp/original.log');
  expect(screen.getByRole('textbox', { name: '日志文件路径' })).toBeEnabled();
});
