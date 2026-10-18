import { act, renderHook } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { useTunStatus, isTunRunning, isTunStarting, isTunStopped, isTunError } from './useTunStatus';
import { installBridge, tunStats } from '../test/bridge';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); vi.useFakeTimers(); });

it('loads native status and permissions, then starts and stops using the selected profile', async () => {
  const { result } = renderHook(() => useTunStatus(100));
  await act(async () => {});
  expect(result.current.loading).toBe(false);
  expect(result.current.isAdmin).toBe(false);
  expect(result.current.needsElevation).toBe(true);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running', tunDevice: 'utun9' }));
  await act(async () => { await result.current.startTun('office'); });
  expect(bridge.app.StartTunProxy).toHaveBeenCalledWith('office');
  expect(result.current.tunStatus?.tunDevice).toBe('utun9');
  bridge.app.GetTunStatus.mockResolvedValue(tunStats());
  await act(async () => { await result.current.stopTun(); });
  expect(result.current.tunStatus?.status).toBe('stopped');
  expect(result.current.loading).toBe(false);
});

it.each(['start', 'stop', 'elevation'] as const)('surfaces %s failure and allows retry', async (action) => {
  const { result } = renderHook(() => useTunStatus());
  await act(async () => {});
  const native = action === 'start' ? bridge.app.StartTunProxy : action === 'stop' ? bridge.app.StopTunProxy : bridge.app.RequestElevationRestart;
  native.mockRejectedValueOnce(new Error('permission denied'));
  const invoke = () => action === 'start' ? result.current.startTun('office') : action === 'stop' ? result.current.stopTun() : result.current.requestElevation();
  await act(async () => { await expect(invoke()).rejects.toThrow('permission denied'); });
  expect(result.current.error).toBe('permission denied');
  expect(result.current.loading).toBe(false);
  await act(async () => { await invoke(); });
  expect(native).toHaveBeenCalledTimes(2);
});

it('refreshes permission changes and recovers from a status error', async () => {
  bridge.app.GetTunStatus.mockRejectedValueOnce(new Error('native unavailable'));
  const { result } = renderHook(() => useTunStatus());
  await act(async () => {});
  expect(result.current.error).toBe('native unavailable');
  bridge.app.IsRunningAsAdmin.mockResolvedValue(true);
  bridge.app.RequiresTunElevation.mockResolvedValue(false);
  await act(async () => { await result.current.refresh(); });
  expect(result.current.error).toBeNull();
  expect(result.current.isAdmin).toBe(true);
  expect(result.current.needsElevation).toBe(false);
});

it('polls running status and removes its timer when stopped or unmounted', async () => {
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running' }));
  const { result, unmount } = renderHook(() => useTunStatus(100));
  await act(async () => {});
  bridge.app.GetTunStatus.mockResolvedValue(tunStats());
  await act(async () => { await vi.advanceTimersByTimeAsync(100); });
  expect(result.current.tunStatus?.status).toBe('stopped');
  const stoppedCalls = bridge.app.GetTunStatus.mock.calls.length;
  await act(async () => { await vi.advanceTimersByTimeAsync(500); });
  expect(bridge.app.GetTunStatus).toHaveBeenCalledTimes(stoppedCalls);
  bridge.app.GetTunStatus.mockResolvedValue(tunStats({ status: 'running' }));
  await act(async () => { await result.current.refresh(); });
  unmount();
  const calls = bridge.app.GetTunStatus.mock.calls.length;
  await act(async () => { await vi.advanceTimersByTimeAsync(500); });
  expect(bridge.app.GetTunStatus).toHaveBeenCalledTimes(calls);
});

it.each(['starting', 'stopping'])('continues polling an initial %s transition until it completes', async (status) => {
  bridge.app.GetTunStatus.mockResolvedValueOnce(tunStats({ status })).mockResolvedValue(tunStats());
  const { result } = renderHook(() => useTunStatus(100));
  await act(async () => {});
  expect(result.current.tunStatus?.status).toBe(status);
  await act(async () => { await vi.advanceTimersByTimeAsync(100); });
  expect(result.current.tunStatus?.status).toBe('stopped');
});

it('classifies all relevant states without treating unknown values as running', () => {
  expect(isTunRunning('running')).toBe(true);
  expect(isTunStarting('starting')).toBe(true);
  expect(isTunStopped(undefined)).toBe(true);
  expect(isTunStopped('stopped')).toBe(true);
  expect(isTunError('error')).toBe(true);
  for (const status of ['unknown', undefined]) {
    expect(isTunRunning(status)).toBe(false);
    expect(isTunStarting(status)).toBe(false);
    expect(isTunError(status)).toBe(false);
  }
});
