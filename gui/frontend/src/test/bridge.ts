import { vi } from 'vitest';
import { service, main } from '../../wailsjs/go/models';

export const profile = (overrides: Partial<service.SSHProfile> = {}) => ({
  id: 'office', name: 'Office', host: 'ssh.example.test', port: '22', user: 'mei',
  httpAddr: ':8080', systemProxy: false, tunMode: false, tunGlobal: false,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  ...overrides,
} as service.SSHProfile);

export const settings = (overrides: Partial<service.AppSettings> = {}) => ({
  theme: 'dark', language: 'zh-CN', autoStart: false, minimizeToTray: false,
  autoConnect: false, defaultProfile: '', verbose: false, logFile: '', ...overrides,
});

export const proxyStats = (overrides: Partial<service.ProxyStats> = {}) => ({
  status: 'stopped', httpAddr: ':8080', sshServer: '', sshUser: '',
  systemProxy: false, totalRequests: 0, ...overrides,
});

export const tunStats = (overrides: Partial<service.TunStats> = {}) => ({
  status: 'stopped', isGlobal: false, ...overrides,
});

export const logEntry = (id: number, level: string, message: string): main.LogEntry => ({
  id, level, message, timestamp: '2026-01-01T12:00:00Z',
});

// Keep the generated Wails JS in the test path; replace only its native host boundary.
export function installBridge() {
  let storedSettings = settings();
  let storedProfiles: service.SSHProfile[] = [];
  const listeners = new Map<string, (...args: any[]) => void>();
  const app = {
    GetSettings: vi.fn(async () => ({ ...storedSettings })),
    UpdateSettings: vi.fn(async (next: service.AppSettings) => { storedSettings = { ...next }; }),
    GetProfiles: vi.fn(async () => [...storedProfiles]),
    GetProfile: vi.fn(async (id: string) => {
      const found = storedProfiles.find(item => item.id === id);
      if (!found) throw new Error('profile not found');
      return found;
    }),
    AddProfile: vi.fn(async (next: service.SSHProfile) => {
      storedProfiles.push(profile({ ...next, id: next.id || 'new-profile' }));
    }),
    UpdateProfile: vi.fn(async (next: service.SSHProfile) => {
      storedProfiles = storedProfiles.map(item => item.id === next.id ? next : item);
    }),
    DeleteProfile: vi.fn(async (id: string) => {
      storedProfiles = storedProfiles.filter(item => item.id !== id);
    }),
    GetProxyStatus: vi.fn(async () => proxyStats()),
    GetTunStatus: vi.fn(async () => tunStats()),
    IsRunningAsAdmin: vi.fn(async () => false),
    RequiresTunElevation: vi.fn(async () => true),
    RequestElevationRestart: vi.fn(async () => {}),
    StartProxy: vi.fn(async (_id: string) => {}),
    StopProxy: vi.fn(async () => {}),
    StartTunProxy: vi.fn(async (_id: string) => {}),
    StopTunProxy: vi.fn(async () => {}),
    TestConnection: vi.fn(async (_id: string) => {}),
    GetLogs: vi.fn(async (): Promise<main.LogEntry[]> => []),
    ClearLogs: vi.fn(async () => {}),
    SaveFileDialog: vi.fn(async (_title: string, _name: string) => ''),
    SaveLogsToFile: vi.fn(async (_path: string) => {}),
    OpenFileDialog: vi.fn(async (_title: string, _filters: unknown[]) => ''),
    GetVersion: vi.fn(async () => 'v1.2.3'),
    OpenURL: vi.fn(async (_url: string) => {}),
  };
  const runtime = {
    EventsOnMultiple: vi.fn((event: string, callback: (...args: any[]) => void) => {
      listeners.set(event, callback);
      return () => listeners.delete(event);
    }),
    EventsOff: vi.fn((event: string) => listeners.delete(event)),
  };
  Object.assign(window, { go: { main: { App: app } }, runtime });
  return {
    app, runtime,
    setProfiles: (next: service.SSHProfile[]) => { storedProfiles = next; },
    setSettings: (next: service.AppSettings) => { storedSettings = next; },
    emit: (event: string, ...args: any[]) => listeners.get(event)?.(...args),
    hasListener: (event: string) => listeners.has(event),
  };
}

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
