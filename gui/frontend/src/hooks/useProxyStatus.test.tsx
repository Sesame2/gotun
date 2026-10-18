import { act, renderHook } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { useProxyStatus } from './useProxyStatus';
import { installBridge, proxyStats } from '../test/bridge';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); vi.useFakeTimers(); });

it('loads immediately, polls the chosen interval, and stops polling after unmount', async () => {
  const { result, unmount } = renderHook(() => useProxyStatus(1000));
  expect(result.current.loading).toBe(true);
  await act(async () => {});
  expect(result.current.status.status).toBe('stopped');
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running', uptime: '2s' }));
  await act(async () => { await vi.advanceTimersByTimeAsync(999); });
  expect(result.current.status.status).toBe('stopped');
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(result.current.status.uptime).toBe('2s');
  unmount();
  const calls = bridge.app.GetProxyStatus.mock.calls.length;
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  expect(bridge.app.GetProxyStatus).toHaveBeenCalledTimes(calls);
});

it('retains the last status on failure then clears the error on the next poll', async () => {
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running' }));
  const { result } = renderHook(() => useProxyStatus(100));
  await act(async () => {});
  bridge.app.GetProxyStatus.mockRejectedValueOnce(new Error('bridge disconnected'));
  await act(async () => { await vi.advanceTimersByTimeAsync(100); });
  expect(result.current.error).toBe('bridge disconnected');
  expect(result.current.status.status).toBe('running');
  await act(async () => { await vi.advanceTimersByTimeAsync(100); });
  expect(result.current.error).toBeNull();
});

it('replaces its timer when the interval changes and supports manual refresh', async () => {
  const { result, rerender } = renderHook(({ interval }) => useProxyStatus(interval), { initialProps: { interval: 100 } });
  await act(async () => {});
  rerender({ interval: 500 });
  await act(async () => {});
  const calls = bridge.app.GetProxyStatus.mock.calls.length;
  await act(async () => { await vi.advanceTimersByTimeAsync(499); });
  expect(bridge.app.GetProxyStatus).toHaveBeenCalledTimes(calls);
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ totalRequests: 12 }));
  await act(async () => { await result.current.refresh(); });
  expect(result.current.status.totalRequests).toBe(12);
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(bridge.app.GetProxyStatus).toHaveBeenCalledTimes(calls + 2);
});
