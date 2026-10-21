import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it } from 'vitest';
import HomePage from './index';
import { I18nProvider } from '../../providers/I18nProvider';
import { deferred, installBridge, profile, proxyStats, settings, tunStats } from '../../test/bridge';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); });

function renderHome() {
  return render(<I18nProvider><HomePage /></I18nProvider>);
}

it('selects the first profile, remembers it, and starts and stops HTTP proxy', async () => {
  localStorage.setItem('gotun_last_profile_id', 'deleted-profile');
  bridge.setProfiles([profile()]);
  renderHome();
  const start = await screen.findByRole('button', { name: '启动' });
  await waitFor(() => expect(start).toBeEnabled());
  expect(localStorage.getItem('gotun_last_profile_id')).toBe('office');
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running', uptime: '10s' }));
  await userEvent.click(start);
  await screen.findByText('代理服务已启动');
  expect(bridge.app.StartProxy).toHaveBeenCalledWith('office');
  expect(await screen.findByRole('button', { name: '停止' })).toBeEnabled();
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats());
  await userEvent.click(screen.getByRole('button', { name: '停止' }));
  await screen.findByText('代理服务已停止');
  expect(bridge.app.StopProxy).toHaveBeenCalledTimes(1);
});

it('displays HTTP start and connection failures and re-enables actions', async () => {
  bridge.setProfiles([profile()]);
  renderHome();
  await waitFor(() => expect(screen.getByRole('button', { name: '启动' })).toBeEnabled());
  bridge.app.StartProxy.mockRejectedValueOnce(new Error('port occupied'));
  await userEvent.click(screen.getByRole('button', { name: '启动' }));
  await screen.findByText('port occupied');
  expect(screen.getByRole('button', { name: '启动' })).toBeEnabled();
  bridge.app.TestConnection.mockRejectedValueOnce(new Error('SSH refused'));
  await userEvent.click(screen.getByRole('button', { name: '测试连接' }));
  await screen.findByText('SSH refused');
});

it('keeps start disabled without a profile and links to configuration management', async () => {
  renderHome();
  await screen.findByText('暂无配置文件');
  expect(screen.getByRole('button', { name: '启动' })).toBeDisabled();
  expect(screen.getByRole('link', { name: '添加配置' })).toHaveAttribute('href', '#/profiles');
});

it('requests confirmation before elevation and never starts TUN without it', async () => {
  bridge.setProfiles([profile({ tunMode: true })]);
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  await userEvent.click(await screen.findByRole('button', { name: '以管理员启动' }));
  expect(bridge.app.StartTunProxy).not.toHaveBeenCalled();
  expect(bridge.app.RequestElevationRestart).not.toHaveBeenCalled();
  const dialog = within(screen.getByRole('dialog'));
  await userEvent.click(dialog.getByRole('button', { name: '确认提权' }));
  await waitFor(() => expect(bridge.app.RequestElevationRestart).toHaveBeenCalledTimes(1));
});

it('runs TUN as admin, disables HTTP while active, and stops TUN', async () => {
  bridge.setProfiles([profile({ tunMode: true })]);
  bridge.app.IsRunningAsAdmin.mockResolvedValue(true);
  bridge.app.RequiresTunElevation.mockResolvedValue(false);
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running', tunDevice: 'utun9', tunIP: '10.0.0.1', isGlobal: true }));
  await userEvent.click(screen.getByRole('button', { name: '启动TUN' }));
  await screen.findByText('TUN代理服务已启动');
  expect(await screen.findByText('utun9')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('tab', { name: 'HTTP/SOCKS5' }));
  expect(screen.getByRole('button', { name: '启动' })).toBeDisabled();
  await userEvent.click(tab);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats());
  await userEvent.click(screen.getByRole('button', { name: '停止TUN' }));
  await screen.findByText('TUN代理服务已停止');
});

it('preserves running HTTP state when stop fails', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running' }));
  bridge.app.StopProxy.mockRejectedValueOnce(new Error('cannot release listener'));
  renderHome();
  await userEvent.click(await screen.findByRole('button', { name: '停止' }));
  await screen.findByText('cannot release listener');
  expect(screen.getByRole('button', { name: '停止' })).toBeEnabled();
});

it('cancels elevation and reports a rejected native elevation request', async () => {
  bridge.setProfiles([profile({ tunMode: true })]);
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  await userEvent.click(screen.getByRole('button', { name: '以管理员启动' }));
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }));
  expect(bridge.app.RequestElevationRestart).not.toHaveBeenCalled();
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  bridge.app.RequestElevationRestart.mockRejectedValueOnce(new Error('elevation cancelled'));
  await userEvent.click(screen.getByRole('button', { name: '以管理员启动' }));
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认提权' }));
  await screen.findByText('elevation cancelled');
  expect(bridge.app.StartTunProxy).not.toHaveBeenCalled();
});

it('labels configuration selection and connects keyboard tabs to their panels', async () => {
  bridge.setProfiles([profile({ tunMode: true })]);
  renderHome();
  const selection = await screen.findByRole('combobox', { name: '选择配置' });
  expect(selection).toBeEnabled();
  const httpTab = screen.getByRole('tab', { name: 'HTTP/SOCKS5' });
  const tunTab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tunTab).toBeEnabled());
  expect(screen.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', httpTab.id);
  expect(httpTab).toHaveAttribute('aria-controls', screen.getByRole('tabpanel').id);
  httpTab.focus();
  await userEvent.keyboard('{ArrowRight}{Enter}');
  expect(tunTab).toHaveFocus();
  expect(tunTab).toHaveAttribute('aria-selected', 'true');
  expect(screen.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', tunTab.id);
});

it('keeps a named disabled connection button inside a hoverable tooltip wrapper', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running' }));
  renderHome();
  await screen.findByRole('button', { name: '停止' });
  const button = screen.getByRole('button', { name: '测试连接' });
  expect(button).toBeDisabled();
  expect(button.parentElement?.tagName).toBe('SPAN');
  await userEvent.hover(button.parentElement!);
  expect(await screen.findByRole('tooltip')).toHaveTextContent('测试连接');
});

it.each(['starting', 'stopping'])('disables HTTP actions during native %s state', async (status) => {
  bridge.setProfiles([profile({ tunMode: true })]);
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status }));
  renderHome();
  expect(await screen.findByRole('button', { name: status === 'starting' ? '启动中...' : '停止中...' })).toBeDisabled();
  expect(screen.getByRole('button', { name: '测试连接' })).toBeDisabled();
  expect(screen.getByRole('combobox', { name: '选择配置' })).toHaveAttribute('aria-disabled', 'true');
});

it.each(['starting', 'stopping'])('disables TUN actions during native %s state', async (status) => {
  bridge.setProfiles([profile({ tunMode: true })]);
  bridge.app.IsRunningAsAdmin.mockResolvedValue(true);
  bridge.app.RequiresTunElevation.mockResolvedValue(false);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status }));
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  expect(screen.getByRole('button', { name: status === 'starting' ? '启动中...' : '停止中...' })).toBeDisabled();
  expect(screen.getByRole('button', { name: '测试连接' })).toBeDisabled();
});

it('waits for the native permission check before enabling TUN start', async () => {
  bridge.setProfiles([profile({ tunMode: true })]);
  const permission = deferred<boolean>();
  bridge.app.RequiresTunElevation.mockReturnValue(permission.promise);
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  expect(screen.getByRole('button', { name: '启动TUN' })).toBeDisabled();
  await act(async () => { permission.resolve(true); });
  expect(screen.getByRole('button', { name: '以管理员启动' })).toBeEnabled();
});

it('renders all Home details in the saved English locale', async () => {
  bridge.setSettings(settings({ language: 'en-US' }));
  bridge.setProfiles([profile({ tunMode: true, tunGlobal: true, jumpHosts: ['jump.test'] })]);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running', tunDevice: 'utun9', routes: Array(6).fill('10.0.0.0/24'), natRules: ['10.0.0.0/24 -> 192.168.0.0/24'] }));
  const { container } = renderHome();
  await screen.findByText('Control Panel');
  expect(screen.getByRole('combobox', { name: 'Select Profile' })).toBeInTheDocument();
  expect(container.textContent).not.toMatch(/[\u3400-\u9fff]/);
});

it('tests a connection, closes the result alert, and supports retry', async () => {
  bridge.setProfiles([profile()]);
  renderHome();
  const test = screen.getByRole('button', { name: '测试连接' });
  await waitFor(() => expect(test).toBeEnabled());
  await userEvent.click(test);
  await screen.findByText('SSH连接测试成功');
  await userEvent.click(screen.getByRole('button', { name: '关闭' }));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  await userEvent.click(test);
  expect(bridge.app.TestConnection).toHaveBeenCalledTimes(2);
});

it('marks the pending start action busy and prevents duplicate actions until it resolves', async () => {
  bridge.setProfiles([profile()]);
  const pending = deferred<void>();
  bridge.app.StartProxy.mockReturnValueOnce(pending.promise);
  renderHome();
  const start = screen.getByRole('button', { name: '启动' });
  await waitFor(() => expect(start).toBeEnabled());
  await userEvent.click(start);
  expect(start).toBeDisabled();
  expect(start).toHaveAttribute('aria-busy', 'true');
  expect(screen.getByRole('button', { name: '测试连接' })).toBeDisabled();
  await act(async () => { pending.resolve(); });
  await screen.findByText('代理服务已启动');
});

it('keeps native Wails string errors visible when a start fails', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.StartProxy.mockRejectedValueOnce('SSH address not reachable');
  renderHome();
  const start = screen.getByRole('button', { name: '启动' });
  await waitFor(() => expect(start).toBeEnabled());
  await userEvent.click(start);
  await screen.findByText('SSH address not reachable');
});

it.each(['start', 'stop'])('shows a failed TUN %s action and leaves it retryable', async (action) => {
  bridge.setProfiles([profile({ tunMode: true })]);
  bridge.app.IsRunningAsAdmin.mockResolvedValue(true);
  bridge.app.RequiresTunElevation.mockResolvedValue(false);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: action === 'stop' ? 'running' : 'stopped' }));
  const method = action === 'start' ? bridge.app.StartTunProxy : bridge.app.StopTunProxy;
  method.mockRejectedValueOnce(new Error('TUN device unavailable'));
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  const button = screen.getByRole('button', { name: action === 'start' ? '启动TUN' : '停止TUN' });
  await waitFor(() => expect(button).toBeEnabled());
  await userEvent.click(button);
  await screen.findByText('TUN device unavailable');
  expect(button).toBeEnabled();
});

it('persists selecting a different profile and disables TUN for a profile without it', async () => {
  bridge.setProfiles([profile({ tunMode: true }), profile({ id: 'second', name: 'Second' })]);
  renderHome();
  const select = screen.getByRole('combobox', { name: '选择配置' });
  await waitFor(() => expect(select).toHaveTextContent('Office'));
  await userEvent.click(select);
  await userEvent.click(screen.getByRole('option', { name: /Second/ }));
  expect(localStorage.getItem('gotun_last_profile_id')).toBe('second');
  expect(screen.getByRole('tab', { name: 'TUN模式' })).toBeDisabled();
});

it('keeps the global TUN stop action reachable when the selected profile has TUN disabled', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running' }));
  renderHome();
  const tab = screen.getByRole('tab', { name: 'TUN模式' });
  await waitFor(() => expect(tab).toBeEnabled());
  await userEvent.click(tab);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats());
  await userEvent.click(screen.getByRole('button', { name: '停止TUN' }));
  await screen.findByText('TUN代理服务已停止');
  expect(bridge.app.StopTunProxy).toHaveBeenCalledTimes(1);
});

it('reflects a profile activated from the native tray', async () => {
  bridge.setProfiles([profile(), profile({ id: 'remote', name: 'From Tray' })]);
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running', profileId: 'remote' }));
  renderHome();
  await waitFor(() => expect(screen.getByRole('combobox', { name: '选择配置' })).toHaveTextContent('From Tray'));
  expect(localStorage.getItem('gotun_last_profile_id')).toBe('remote');
});

it('reflects the tray TUN profile and opens its active panel', async () => {
  bridge.setProfiles([profile(), profile({ id: 'tun-tray', name: 'Tray TUN', tunMode: true })]);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running', profileId: 'tun-tray' }));
  renderHome();
  await waitFor(() => expect(screen.getByRole('combobox', { name: '选择配置' })).toHaveTextContent('Tray TUN'));
  expect(screen.getByRole('tab', { name: 'TUN模式' })).toHaveAttribute('aria-selected', 'true');
});
