import { useState, useEffect, useCallback } from 'react';
import {
  GetTunStatus,
  StartTunProxy,
  StopTunProxy,
  RequiresTunElevation,
  RequestElevationRestart,
  IsRunningAsAdmin,
} from '../../wailsjs/go/main/App';
import { TunStats, TunStatusType } from '../types';

interface UseTunStatusReturn {
  tunStatus: TunStats | null;
  loading: boolean;
  error: string | null;
  isAdmin: boolean;
  needsElevation: boolean;
  startTun: (profileId: string) => Promise<void>;
  stopTun: () => Promise<void>;
  requestElevation: () => Promise<void>;
  refresh: () => Promise<void>;
}

export function useTunStatus(pollInterval: number = 2000): UseTunStatusReturn {
  const [tunStatus, setTunStatus] = useState<TunStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isAdmin, setIsAdmin] = useState(false);
  const [needsElevation, setNeedsElevation] = useState(false);

  const fetchStatus = useCallback(async () => {
    try {
      const status = await GetTunStatus();
      setTunStatus(status);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取TUN状态失败');
    }
  }, []);

  const checkElevation = useCallback(async () => {
    try {
      const [admin, elevation] = await Promise.all([
        IsRunningAsAdmin(),
        RequiresTunElevation(),
      ]);
      setIsAdmin(admin);
      setNeedsElevation(elevation);
    } catch (err) {
      console.error('检查权限状态失败:', err);
    }
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    await Promise.all([fetchStatus(), checkElevation()]);
    setLoading(false);
  }, [fetchStatus, checkElevation]);

  // 初始加载
  useEffect(() => {
    refresh();
  }, [refresh]);

  // 轮询状态
  useEffect(() => {
    // 过渡状态也需要轮询，才能观察启动或停止完成。
    if (!tunStatus || !['running', 'starting', 'stopping'].includes(tunStatus.status)) {
      return;
    }

    const interval = setInterval(fetchStatus, pollInterval);
    return () => clearInterval(interval);
  }, [tunStatus?.status, pollInterval, fetchStatus]);

  const startTun = useCallback(async (profileId: string) => {
    setLoading(true);
    setError(null);
    try {
      await StartTunProxy(profileId);
      await fetchStatus();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : '启动TUN失败';
      setError(errorMessage);
      throw err;
    } finally {
      setLoading(false);
    }
  }, [fetchStatus]);

  const stopTun = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      await StopTunProxy();
      await fetchStatus();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : '停止TUN失败';
      setError(errorMessage);
      throw err;
    } finally {
      setLoading(false);
    }
  }, [fetchStatus]);

  const requestElevation = useCallback(async () => {
    try {
      await RequestElevationRestart();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : '请求提权失败';
      setError(errorMessage);
      throw err;
    }
  }, []);

  return {
    tunStatus,
    loading,
    error,
    isAdmin,
    needsElevation,
    startTun,
    stopTun,
    requestElevation,
    refresh,
  };
}

// 辅助函数：判断TUN是否正在运行
export function isTunRunning(status: TunStatusType | string | undefined): boolean {
  return status === 'running';
}

// 辅助函数：判断TUN是否正在启动
export function isTunStarting(status: TunStatusType | string | undefined): boolean {
  return status === 'starting';
}

// 辅助函数：判断TUN是否已停止
export function isTunStopped(status: TunStatusType | string | undefined): boolean {
  return status === 'stopped' || status === undefined;
}

// 辅助函数：判断TUN是否有错误
export function isTunError(status: TunStatusType | string | undefined): boolean {
  return status === 'error';
}
