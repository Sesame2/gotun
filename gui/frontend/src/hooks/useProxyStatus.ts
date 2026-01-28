import { useState, useEffect, useCallback } from 'react';
import { service } from '../../wailsjs/go/models';
import { GetProxyStatus } from '../../wailsjs/go/main/App';

type ProxyStats = service.ProxyStats;

export function useProxyStatus(pollInterval: number = 2000) {
  const [status, setStatus] = useState<ProxyStats>({
    status: 'stopped',
    httpAddr: ':8080',
    sshServer: '',
    sshUser: '',
    systemProxy: false,
    totalRequests: 0,
  } as ProxyStats);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    try {
      const stats = await GetProxyStatus();
      setStatus(stats as ProxyStats);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取状态失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, pollInterval);
    return () => clearInterval(interval);
  }, [fetchStatus, pollInterval]);

  return { status, loading, error, refresh: fetchStatus };
}
