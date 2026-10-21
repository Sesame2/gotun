import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, useThemeMode } from './ThemeProvider';
import { I18nProvider, useI18n } from './I18nProvider';
import { deferred, installBridge, settings } from '../test/bridge';
import { Dialog, DialogTitle } from '@mui/material';

let bridge: ReturnType<typeof installBridge>;
beforeEach(() => { bridge = installBridge(); });

function Preferences() {
  const { mode, actualMode, setMode } = useThemeMode();
  const { locale, setLocale, t } = useI18n();
  return <>
    <output aria-label="theme">{mode}/{actualMode}</output>
    <output aria-label="language">{locale}/{t.nav.home}</output>
    <button onClick={() => setMode('light')}>light</button>
    <button onClick={() => setMode('system')}>system</button>
    <button onClick={() => setLocale('en-US')}>English</button>
  </>;
}

function Providers() {
  return <I18nProvider><ThemeProvider><Preferences /></ThemeProvider></I18nProvider>;
}

it('loads saved language and theme and persists each change across remounts', async () => {
  const user = userEvent.setup();
  bridge.setSettings(settings({ verbose: true, logFile: '/tmp/keep.log' }));
  const first = render(<Providers />);
  await waitFor(() => expect(bridge.app.GetSettings).toHaveBeenCalledTimes(2));
  await user.click(screen.getByRole('button', { name: 'light' }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledWith(settings({ theme: 'light', verbose: true, logFile: '/tmp/keep.log' })));
  await user.click(screen.getByRole('button', { name: 'English' }));
  await waitFor(() => expect(screen.getByLabelText('language')).toHaveTextContent('en-US/Home'));
  expect(await bridge.app.GetSettings()).toEqual(settings({ theme: 'light', language: 'en-US', verbose: true, logFile: '/tmp/keep.log' }));
  first.unmount();
  render(<Providers />);
  await waitFor(() => expect(screen.getByLabelText('theme')).toHaveTextContent('light/light'));
  expect(screen.getByLabelText('language')).toHaveTextContent('en-US/Home');
});

it('tracks system color changes and removes the media listener on unmount', async () => {
  bridge.setSettings(settings({ theme: 'system' }));
  const media = { matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() };
  vi.mocked(window.matchMedia).mockReturnValue(media as unknown as MediaQueryList);
  const { unmount } = render(<Providers />);
  await waitFor(() => expect(screen.getByLabelText('theme')).toHaveTextContent('system/light'));
  const listener = media.addEventListener.mock.calls[0][1];
  act(() => listener({ matches: true }));
  expect(screen.getByLabelText('theme')).toHaveTextContent('system/dark');
  await userEvent.click(screen.getByRole('button', { name: 'light' }));
  act(() => listener({ matches: false }));
  expect(screen.getByLabelText('theme')).toHaveTextContent('light/light');
  unmount();
  expect(media.removeEventListener).toHaveBeenCalledWith('change', listener);
});

it('keeps a supported default language when native settings contain an unknown locale', async () => {
  bridge.setSettings(settings({ language: 'unknown' }));
  render(<Providers />);
  await waitFor(() => expect(bridge.app.GetSettings).toHaveBeenCalledTimes(2));
  expect(screen.getByLabelText('language')).toHaveTextContent('zh-CN/首页');
});

it('handles preference write errors without an unhandled rejection', async () => {
  const errorLog = vi.spyOn(console, 'error').mockImplementation(() => {});
  render(<Providers />);
  await waitFor(() => expect(bridge.app.GetSettings).toHaveBeenCalledTimes(2));
  bridge.app.UpdateSettings.mockRejectedValueOnce(new Error('read-only'));
  await userEvent.click(screen.getByRole('button', { name: 'English' }));
  await waitFor(() => expect(errorLog).toHaveBeenCalledWith('Failed to save language setting:', expect.any(Error)));
  expect((await bridge.app.GetSettings()).language).toBe('zh-CN');
});

it('updates document language and native color scheme with saved preferences', async () => {
  bridge.setSettings(settings({ theme: 'dark', language: 'en-US' }));
  render(<Providers />);
  await waitFor(() => expect(document.documentElement).toHaveAttribute('lang', 'en-US'));
  expect(document.documentElement).toHaveAttribute('data-theme', 'dark');
  expect(document.documentElement.style.colorScheme).toBe('dark');
  await userEvent.click(screen.getByRole('button', { name: 'light' }));
  await waitFor(() => expect(document.documentElement.style.colorScheme).toBe('light'));
});

it('keeps dark dialog paper free of the elevated white overlay', async () => {
  render(<ThemeProvider><Dialog open><DialogTitle>Dialog contrast</DialogTitle></Dialog></ThemeProvider>);
  await screen.findByRole('dialog');
  expect(getComputedStyle(screen.getByRole('dialog')).backgroundImage).toBe('none');
});

it('preserves both preferences when language and theme writes overlap', async () => {
  const first = deferred<void>();
  bridge.app.UpdateSettings.mockImplementationOnce(async next => {
    await first.promise;
    bridge.setSettings(next);
  });
  render(<Providers />);
  await waitFor(() => expect(bridge.app.GetSettings).toHaveBeenCalledTimes(2));
  await userEvent.click(screen.getByRole('button', { name: 'English' }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledTimes(1));
  await userEvent.click(screen.getByRole('button', { name: 'light' }));
  await act(async () => { first.resolve(); });
  await waitFor(async () => expect(await bridge.app.GetSettings()).toEqual(settings({ language: 'en-US', theme: 'light' })));
});

it('does not let an earlier failed theme write roll back a later successful selection', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  const first = deferred<void>();
  bridge.app.UpdateSettings.mockReturnValueOnce(first.promise);
  render(<Providers />);
  await waitFor(() => expect(bridge.app.GetSettings).toHaveBeenCalledTimes(2));
  await userEvent.click(screen.getByRole('button', { name: 'light' }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledTimes(1));
  await userEvent.click(screen.getByRole('button', { name: 'system' }));
  await act(async () => { first.reject(new Error('read-only')); });
  await waitFor(() => expect(screen.getByLabelText('theme')).toHaveTextContent('system/light'));
  expect((await bridge.app.GetSettings()).theme).toBe('system');
});

it('does not replace saved user selections with a late initial settings response', async () => {
  const initial = deferred<ReturnType<typeof settings>>();
  bridge.app.GetSettings.mockReturnValueOnce(initial.promise).mockReturnValueOnce(initial.promise);
  render(<Providers />);
  await waitFor(() => expect(bridge.app.GetSettings).toHaveBeenCalledTimes(2));
  await userEvent.click(screen.getByRole('button', { name: 'light' }));
  await userEvent.click(screen.getByRole('button', { name: 'English' }));
  await waitFor(() => expect(bridge.app.UpdateSettings).toHaveBeenCalledTimes(2));
  await act(async () => { initial.resolve(settings()); });
  expect(screen.getByLabelText('theme')).toHaveTextContent('light/light');
  expect(screen.getByLabelText('language')).toHaveTextContent('en-US/Home');
});
