import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { useProfiles, useSettings } from './useConfig';
import { installBridge, profile, settings } from '../test/bridge';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); });

describe('useProfiles', () => {
  it('loads an empty native response as an empty list', async () => {
    bridge.app.GetProfiles.mockResolvedValueOnce(null as any);
    const { result } = renderHook(useProfiles);
    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.profiles).toEqual([]);
    expect(result.current.error).toBeNull();
  });

  it('refreshes native data after create, update and delete', async () => {
    const { result } = renderHook(useProfiles);
    await waitFor(() => expect(result.current.loading).toBe(false));
    await act(async () => { await result.current.addProfile(profile()); });
    expect(result.current.profiles.map(item => item.name)).toEqual(['Office']);
    await act(async () => { await result.current.updateProfile(profile({ name: 'Home' })); });
    expect(result.current.profiles[0].name).toBe('Home');
    expect(await result.current.getProfile('office')).toEqual(profile({ name: 'Home' }));
    await act(async () => { await result.current.deleteProfile('office'); });
    expect(result.current.profiles).toEqual([]);
    expect(await result.current.getProfile('office')).toBeNull();
  });

  it('preserves the last good list on read failure and recovers on refresh', async () => {
    bridge.setProfiles([profile()]);
    const { result } = renderHook(useProfiles);
    await waitFor(() => expect(result.current.profiles).toHaveLength(1));
    bridge.app.GetProfiles.mockRejectedValueOnce(new Error('disk unavailable'));
    await act(async () => { await result.current.fetchProfiles(); });
    expect(result.current.error).toBe('disk unavailable');
    expect(result.current.profiles).toHaveLength(1);
    bridge.setProfiles([]);
    await act(async () => { await result.current.fetchProfiles(); });
    expect(result.current.error).toBeNull();
    expect(result.current.profiles).toEqual([]);
  });

  it.each(['AddProfile', 'UpdateProfile', 'DeleteProfile'] as const)('propagates %s write failure without changing data', async (method) => {
    bridge.setProfiles([profile()]);
    const { result } = renderHook(useProfiles);
    await waitFor(() => expect(result.current.loading).toBe(false));
    bridge.app[method].mockRejectedValueOnce(new Error('permission denied'));
    const write = method === 'AddProfile' ? () => result.current.addProfile(profile())
      : method === 'UpdateProfile' ? () => result.current.updateProfile(profile({ name: 'changed' }))
      : () => result.current.deleteProfile('office');
    await act(async () => { await expect(write()).rejects.toThrow('permission denied'); });
    expect(result.current.profiles[0].name).toBe('Office');
  });

  it('handles non-Error read rejections', async () => {
    bridge.app.GetProfiles.mockRejectedValueOnce('native failure');
    const { result } = renderHook(useProfiles);
    await waitFor(() => expect(result.current.error).toBe('获取配置列表失败'));
    expect(result.current.loading).toBe(false);
  });
});

describe('useSettings', () => {
  it('loads settings, persists updates and refreshes external changes', async () => {
    const { result } = renderHook(useSettings);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.settings?.theme).toBe('dark');
    await act(async () => { await result.current.updateSettings(settings({ theme: 'light' })); });
    expect(result.current.settings?.theme).toBe('light');
    expect((await bridge.app.GetSettings()).theme).toBe('light');
    bridge.setSettings(settings({ verbose: true }));
    await act(async () => { await result.current.fetchSettings(); });
    expect(result.current.settings?.verbose).toBe(true);
  });

  it('recovers from an initial read error', async () => {
    bridge.app.GetSettings.mockRejectedValueOnce(new Error('corrupt settings'));
    const { result } = renderHook(useSettings);
    await waitFor(() => expect(result.current.error).toBe('corrupt settings'));
    expect(result.current.loading).toBe(false);
    await act(async () => { await result.current.fetchSettings(); });
    expect(result.current.error).toBeNull();
    expect(result.current.settings).toEqual(settings());
  });

  it('keeps the prior settings when persistence fails', async () => {
    const { result } = renderHook(useSettings);
    await waitFor(() => expect(result.current.loading).toBe(false));
    bridge.app.UpdateSettings.mockRejectedValueOnce(new Error('read-only file'));
    await act(async () => {
      await expect(result.current.updateSettings(settings({ theme: 'light' }))).rejects.toThrow('read-only file');
    });
    expect(result.current.settings?.theme).toBe('dark');
  });
});
