import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import ProfilesPage from './index';
import { deferred, installBridge, profile, settings } from '../../test/bridge';
import { I18nProvider } from '../../providers/I18nProvider';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); });

function renderProfiles() {
  return render(<I18nProvider><ProfilesPage /></I18nProvider>);
}

async function openNewProfile() {
  await userEvent.click(await screen.findByRole('button', { name: '添加配置' }));
  return within(screen.getByRole('dialog'));
}

async function fillRequired(form: ReturnType<typeof within>) {
  await userEvent.type(form.getByLabelText(/配置名称/), 'My server');
  await userEvent.type(form.getByLabelText(/SSH主机/), 'ssh.test');
  await userEvent.type(form.getByLabelText(/^用户名/), 'alice');
}

it('validates required fields before writing and saves a new profile', async () => {
  renderProfiles();
  const form = await openNewProfile();
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  expect(bridge.app.AddProfile).not.toHaveBeenCalled();
  await fillRequired(form);
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已添加');
  expect(await screen.findByText('My server')).toBeInTheDocument();
  expect(bridge.app.AddProfile).toHaveBeenCalledWith(expect.objectContaining({
    name: 'My server', host: 'ssh.test', user: 'alice', port: '22', httpAddr: ':8080',
    jumpHosts: [], jumpHostsList: [], tunRoutes: [], tunNAT: [],
  }));
});

it('saves structured jump hosts, routes and NAT rules through the native payload', async () => {
  renderProfiles();
  const form = await openNewProfile();
  await fillRequired(form);
  await userEvent.click(form.getByRole('switch', { name: '启用跳板机' }));
  await userEvent.type(form.getByLabelText(/主机地址/), 'jump.test');
  await userEvent.type(form.getAllByLabelText(/^用户名/)[1], 'bastion');
  const jumpPort = form.getAllByLabelText(/^端口/)[1];
  await userEvent.clear(jumpPort);
  await userEvent.type(jumpPort, '2222');
  await userEvent.click(form.getByRole('switch', { name: /启用 TUN/ }));
  await userEvent.click(form.getByRole('button', { name: '添加路由' }));
  await userEvent.type(form.getByPlaceholderText('例如: 192.168.1.0/24'), '192.168.50.0/24');
  await userEvent.click(form.getByRole('button', { name: '添加路由' }));
  await userEvent.click(form.getByRole('button', { name: '添加 NAT 规则' }));
  await userEvent.type(form.getByPlaceholderText('本地 CIDR (如 10.0.0.0/24)'), '10.10.0.0/24');
  await userEvent.type(form.getByPlaceholderText('远程 CIDR (如 192.168.0.0/24)'), '192.168.0.0/24');
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已添加');
  expect(bridge.app.AddProfile).toHaveBeenCalledWith(expect.objectContaining({
    jumpHosts: ['bastion@jump.test:2222'],
    jumpHostsList: [{ host: 'jump.test', port: '2222', user: 'bastion', password: '' }],
    tunMode: true, tunRoutes: ['192.168.50.0/24'],
    tunNAT: [{ srcCIDR: '10.10.0.0/24', dstCIDR: '192.168.0.0/24' }],
  }));
});

it('keeps form values and native errors visible after a failed save, then allows retry', async () => {
  bridge.app.AddProfile.mockRejectedValueOnce(new Error('configuration file is read-only'));
  renderProfiles();
  const form = await openNewProfile();
  await fillRequired(form);
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await form.findByText('configuration file is read-only');
  expect(form.getByLabelText(/配置名称/)).toHaveValue('My server');
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已添加');
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

it('edits an existing profile and deletes it only after confirmation', async () => {
  bridge.setProfiles([profile()]);
  renderProfiles();
  await screen.findByText('Office');
  await userEvent.click(screen.getByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  await userEvent.clear(form.getByLabelText(/配置名称/));
  await userEvent.type(form.getByLabelText(/配置名称/), 'Renamed');
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已更新');
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(bridge.app.UpdateProfile).toHaveBeenCalledWith(expect.objectContaining({ id: 'office', name: 'Renamed' }));
  await userEvent.click(screen.getByRole('button', { name: '删除' }));
  expect(bridge.app.DeleteProfile).not.toHaveBeenCalled();
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '删除' }));
  await screen.findByText('配置已删除');
  expect(screen.queryByText('Renamed')).not.toBeInTheDocument();
});

it('displays connection test failures without deleting the profile', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.TestConnection.mockRejectedValueOnce(new Error('SSH authentication failed'));
  renderProfiles();
  await screen.findByText('Office');
  await userEvent.click(screen.getByRole('button', { name: '测试连接' }));
  await screen.findByText('SSH authentication failed');
  expect(screen.getByText('Office')).toBeInTheDocument();
});

it('preserves a legacy jump-host chain when only renaming its profile', async () => {
  bridge.setProfiles([profile({ jumpHosts: ['bastion@jump.test:2222'] })]);
  renderProfiles();
  await screen.findByText('Office');
  await userEvent.click(screen.getByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  await userEvent.type(form.getByLabelText(/配置名称/), ' renamed');
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已更新');
  expect(bridge.app.UpdateProfile).toHaveBeenCalledWith(expect.objectContaining({
    jumpHosts: ['bastion@jump.test:2222'],
  }));
});

it('allows explicitly disabling a legacy jump-host chain', async () => {
  bridge.setProfiles([profile({ jumpHosts: ['bastion@jump.test:2222'] })]);
  renderProfiles();
  await screen.findByText('Office');
  await userEvent.click(screen.getByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  await userEvent.click(form.getByRole('switch', { name: '启用跳板机' }));
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已更新');
  expect(bridge.app.UpdateProfile).toHaveBeenCalledWith(expect.objectContaining({ jumpHosts: [], jumpHostsList: [] }));
});

it('discards unsaved edits when the dialog is cancelled', async () => {
  bridge.setProfiles([profile({ tunMode: true, tunRoutes: ['192.168.1.0/24'] })]);
  renderProfiles();
  await screen.findByText('Office');
  await userEvent.click(screen.getByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  const route = form.getByPlaceholderText('例如: 192.168.1.0/24');
  await userEvent.clear(route);
  await userEvent.type(route, '10.10.0.0/24');
  await userEvent.click(form.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await userEvent.click(screen.getByRole('button', { name: '编辑' }));
  expect(within(screen.getByRole('dialog')).getByPlaceholderText('例如: 192.168.1.0/24')).toHaveValue('192.168.1.0/24');
  expect(bridge.app.UpdateProfile).not.toHaveBeenCalled();
});

it('keeps the profile and confirmation dialog when deletion fails', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.DeleteProfile.mockRejectedValueOnce(new Error('cannot delete active profile'));
  renderProfiles();
  await screen.findByText('Office');
  await userEvent.click(screen.getByRole('button', { name: '删除' }));
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '删除' }));
  await waitFor(() => expect(bridge.app.DeleteProfile).toHaveBeenCalled());
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }));
  await screen.findByText('cannot delete active profile');
  expect(screen.getByText('Office')).toBeInTheDocument();
});

it('refuses an incomplete jump host without saving', async () => {
  renderProfiles();
  const form = await openNewProfile();
  await fillRequired(form);
  await userEvent.click(form.getByRole('switch', { name: '启用跳板机' }));
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  expect(bridge.app.AddProfile).not.toHaveBeenCalled();
  expect(form.getByLabelText(/配置名称/)).toHaveValue('My server');
});

it('shows required-field and jump-host errors inside the open editor', async () => {
  renderProfiles();
  const form = await openNewProfile();
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  expect(form.getByRole('alert')).toHaveTextContent('请填写必填字段');
  await fillRequired(form);
  await userEvent.click(form.getByRole('switch', { name: '启用跳板机' }));
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  expect(form.getByRole('alert')).toHaveTextContent('跳板机 1 缺少主机或用户名');
});

it('rejects whitespace-only required values before writing a profile', async () => {
  renderProfiles();
  const form = await openNewProfile();
  await userEvent.type(form.getByLabelText(/配置名称/), '   ');
  await userEvent.type(form.getByLabelText(/SSH主机/), '   ');
  await userEvent.type(form.getByLabelText(/^用户名/), '   ');
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  expect(bridge.app.AddProfile).not.toHaveBeenCalled();
  expect(form.getByRole('alert')).toHaveTextContent('请填写必填字段');
});

it('labels the password controls and hides the password when reopening an editor', async () => {
  bridge.setProfiles([profile({ password: 'local-test-password' })]);
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  expect(form.getByLabelText(/^密码$/)).toHaveAttribute('type', 'password');
  await userEvent.click(form.getByRole('button', { name: '显示密码' }));
  expect(form.getByLabelText(/^密码$/)).toHaveAttribute('type', 'text');
  await userEvent.click(form.getByRole('button', { name: '隐藏密码' }));
  expect(form.getByLabelText(/^密码$/)).toHaveAttribute('type', 'password');
  await userEvent.click(form.getByRole('button', { name: '显示密码' }));
  await userEvent.click(form.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await userEvent.click(screen.getByRole('button', { name: '编辑' }));
  expect(within(screen.getByRole('dialog')).getByLabelText(/^密码$/)).toHaveAttribute('type', 'password');
});

it('chooses private keys and rules, preserves paths on cancellation, and shows dialog errors', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  renderProfiles();
  const form = await openNewProfile();
  bridge.app.OpenFileDialog.mockResolvedValueOnce('/tmp/test-key');
  await userEvent.click(form.getByRole('button', { name: '选择密钥文件' }));
  await waitFor(() => expect(form.getByLabelText('私钥文件路径')).toHaveValue('/tmp/test-key'));
  await userEvent.click(form.getByRole('button', { name: '选择密钥文件' }));
  expect(form.getByLabelText('私钥文件路径')).toHaveValue('/tmp/test-key');
  bridge.app.OpenFileDialog.mockResolvedValueOnce('/tmp/test-rules.yaml');
  await userEvent.click(form.getByRole('button', { name: '选择规则文件' }));
  await waitFor(() => expect(form.getByLabelText('规则文件路径')).toHaveValue('/tmp/test-rules.yaml'));
  bridge.app.OpenFileDialog.mockRejectedValueOnce(new Error('file dialog unavailable'));
  await userEvent.click(form.getByRole('button', { name: '选择规则文件' }));
  expect(await form.findByRole('alert')).toHaveTextContent('file dialog unavailable');
});

it('adds and removes jump hosts, routes and NAT rows without changing a cancelled profile', async () => {
  const original = profile({
    jumpHostsList: [{ host: 'jump.test', user: 'bastion', port: '22', password: '' }],
    tunMode: true, tunRoutes: ['192.168.1.0/24'],
    tunNAT: [{ srcCIDR: '10.0.0.0/24', dstCIDR: '192.168.0.0/24' }],
  });
  bridge.setProfiles([original]);
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  await userEvent.clear(form.getByLabelText('主机地址'));
  await userEvent.type(form.getByLabelText('主机地址'), 'edited.test');
  await userEvent.click(form.getByRole('button', { name: '增加跳板机' }));
  await userEvent.click(form.getByRole('button', { name: '删除跳板机 2' }));
  await userEvent.click(form.getByRole('button', { name: '添加路由' }));
  await userEvent.click(form.getByRole('button', { name: '删除路由 2' }));
  await userEvent.click(form.getByRole('button', { name: '添加 NAT 规则' }));
  await userEvent.click(form.getByRole('button', { name: '删除 NAT 规则 2' }));
  const source = form.getByPlaceholderText('本地 CIDR (如 10.0.0.0/24)');
  await userEvent.clear(source);
  await userEvent.type(source, '10.10.0.0/24');
  await userEvent.click(form.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(original.jumpHostsList?.[0].host).toBe('jump.test');
  expect(original.tunNAT?.[0].srcCIDR).toBe('10.0.0.0/24');
  expect(original.tunRoutes).toEqual(['192.168.1.0/24']);
  expect(bridge.app.UpdateProfile).not.toHaveBeenCalled();
});

it('keeps an in-flight save attached to its editor and prevents duplicate submissions', async () => {
  const saving = deferred<void>();
  bridge.app.AddProfile.mockReturnValueOnce(saving.promise);
  renderProfiles();
  const form = await openNewProfile();
  await fillRequired(form);
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  expect(form.getByRole('button', { name: '保存' })).toBeDisabled();
  expect(form.getByRole('button', { name: '取消' })).toBeDisabled();
  await userEvent.keyboard('{Escape}');
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  await act(async () => saving.resolve());
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(bridge.app.AddProfile).toHaveBeenCalledTimes(1);
});

it('keeps deletion errors visible in the confirmation and supports cancellation', async () => {
  bridge.setProfiles([profile()]);
  bridge.app.DeleteProfile.mockRejectedValueOnce(new Error('profile is busy'));
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: '删除' }));
  const confirmation = within(screen.getByRole('dialog'));
  await userEvent.click(confirmation.getByRole('button', { name: '删除' }));
  expect(await confirmation.findByRole('alert')).toHaveTextContent('profile is busy');
  await userEvent.click(confirmation.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(screen.getByText('Office')).toBeInTheDocument();
});

it('reports profile loading errors instead of presenting an empty success state', async () => {
  bridge.app.GetProfiles.mockRejectedValueOnce(new Error('settings file unreadable'));
  renderProfiles();
  expect(await screen.findByRole('alert')).toHaveTextContent('settings file unreadable');
});

it('uses the selected English language for table actions and editor labels', async () => {
  bridge.setSettings(settings({ language: 'en-US' }));
  bridge.setProfiles([profile()]);
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
  const form = within(screen.getByRole('dialog'));
  expect(form.getByLabelText(/Profile Name/)).toHaveValue('Office');
  expect(form.getByRole('button', { name: 'Show password' })).toBeEnabled();
  expect(form.getByRole('button', { name: 'Save' })).toBeEnabled();
});

it('keeps every table action reachable for long profile values in a scrollable region', async () => {
  const name = 'A very long profile name '.repeat(18);
  bridge.setProfiles([profile({ name, host: 'long-host-'.repeat(35) + '.test' })]);
  renderProfiles();
  const region = await screen.findByRole('region', { name: '配置管理' });
  expect(region).toHaveAttribute('tabindex', '0');
  expect(region).toHaveStyle({ overflowX: 'auto' });
  const row = within(screen.getByText(name.trim(), { exact: false }).closest('tr')!);
  expect(row.getByRole('button', { name: '测试连接' })).toBeEnabled();
  expect(row.getByRole('button', { name: '编辑' })).toBeEnabled();
  expect(row.getByRole('button', { name: '删除' })).toBeEnabled();
  for (const heading of within(region).getAllByRole('columnheader')) {
    expect(heading).toHaveStyle({ whiteSpace: 'nowrap' });
  }
  expect(row.getByText('mei').closest('td')).toHaveStyle({ whiteSpace: 'nowrap' });
});

it('tests a connection once while pending, shows success and dismisses the result', async () => {
  const testing = deferred<void>();
  bridge.setProfiles([profile()]);
  bridge.app.TestConnection.mockReturnValueOnce(testing.promise);
  renderProfiles();
  const button = await screen.findByRole('button', { name: '测试连接' });
  await userEvent.click(button);
  expect(button).toBeDisabled();
  await act(async () => testing.resolve());
  expect(await screen.findByRole('alert')).toHaveTextContent('连接 Office 测试成功');
  await userEvent.click(screen.getByRole('button', { name: '关闭' }));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(bridge.app.TestConnection).toHaveBeenCalledTimes(1);
});

it('aligns wrapped connection actions with edit and delete before and during testing', async () => {
  const testing = deferred<void>();
  bridge.setProfiles([profile()]);
  bridge.app.TestConnection.mockReturnValueOnce(testing.promise);
  renderProfiles();
  const testButton = await screen.findByRole('button', { name: '测试连接' });
  const editButton = screen.getByRole('button', { name: '编辑' });
  const deleteButton = screen.getByRole('button', { name: '删除' });
  const expectSameAlignment = () => {
    const alignment = getComputedStyle(editButton).verticalAlign;
    expect(alignment).toBe('middle');
    expect(getComputedStyle(testButton.parentElement!).verticalAlign).toBe(alignment);
    expect(getComputedStyle(deleteButton).verticalAlign).toBe(alignment);
  };
  expectSameAlignment();
  await userEvent.click(testButton);
  expect(testButton).toBeDisabled();
  expect(within(testButton).getByRole('progressbar')).toBeInTheDocument();
  expectSameAlignment();
  await act(async () => testing.resolve());
  expectSameAlignment();
});

it('cancels profile deletion without calling the native delete operation', async () => {
  bridge.setProfiles([profile()]);
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: '删除' }));
  const confirmation = within(screen.getByRole('dialog'));
  expect(confirmation.getByText('确定要删除配置 "Office" 吗？')).toBeInTheDocument();
  await userEvent.click(confirmation.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(bridge.app.DeleteProfile).not.toHaveBeenCalled();
  expect(screen.getByText('Office')).toBeInTheDocument();
});

it('saves explicit removal of jump hosts, routes and NAT mappings', async () => {
  bridge.setProfiles([profile({
    jumpHostsList: [{ host: 'jump.test', user: 'bastion', port: '22' }],
    tunMode: true, tunRoutes: ['192.168.1.0/24'],
    tunNAT: [{ srcCIDR: '10.0.0.0/24', dstCIDR: '192.168.0.0/24' }],
  })]);
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: '编辑' }));
  const form = within(screen.getByRole('dialog'));
  await userEvent.click(form.getByRole('button', { name: '删除跳板机 1' }));
  await userEvent.click(form.getByRole('button', { name: '删除路由 1' }));
  await userEvent.click(form.getByRole('button', { name: '删除 NAT 规则 1' }));
  await userEvent.click(form.getByRole('button', { name: '保存' }));
  await screen.findByText('配置已更新');
  expect(bridge.app.UpdateProfile).toHaveBeenCalledWith(expect.objectContaining({
    jumpHosts: [], jumpHostsList: [], tunRoutes: [], tunNAT: [],
  }));
});

it('preserves route edits when switching global TUN mode on and off', async () => {
  renderProfiles();
  const form = await openNewProfile();
  await userEvent.click(form.getByRole('switch', { name: /启用 TUN/ }));
  await userEvent.click(form.getByRole('button', { name: '添加路由' }));
  await userEvent.type(form.getByRole('textbox', { name: '路由 1' }), '192.168.50.0/24');
  await userEvent.click(form.getByRole('switch', { name: '全局模式 (代理所有流量)' }));
  expect(form.queryByRole('textbox', { name: '路由 1' })).not.toBeInTheDocument();
  await userEvent.click(form.getByRole('switch', { name: '全局模式 (代理所有流量)' }));
  expect(form.getByRole('textbox', { name: '路由 1' })).toHaveValue('192.168.50.0/24');
});

it('keeps a pending delete attached to its confirmation and disables repeated actions', async () => {
  const deleting = deferred<void>();
  bridge.setProfiles([profile()]);
  bridge.app.DeleteProfile.mockReturnValueOnce(deleting.promise);
  renderProfiles();
  await userEvent.click(await screen.findByRole('button', { name: '删除' }));
  const confirmation = within(screen.getByRole('dialog'));
  await userEvent.click(confirmation.getByRole('button', { name: '删除' }));
  expect(confirmation.getByRole('button', { name: '删除' })).toBeDisabled();
  expect(confirmation.getByRole('button', { name: '取消' })).toBeDisabled();
  await userEvent.keyboard('{Escape}');
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  await act(async () => deleting.resolve());
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(bridge.app.DeleteProfile).toHaveBeenCalledTimes(1);
});
