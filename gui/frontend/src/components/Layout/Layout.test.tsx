import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { expect, it } from 'vitest';
import Layout from './index';
import { I18nProvider } from '../../providers/I18nProvider';
import { installBridge, proxyStats, settings } from '../../test/bridge';

it('renders localized navigation, native proxy state, and routes page content', async () => {
  const bridge = installBridge();
  bridge.setSettings(settings({ language: 'en-US' }));
  bridge.app.GetProxyStatus.mockResolvedValue(proxyStats({ status: 'running', uptime: '3m' }));
  render(<I18nProvider><MemoryRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
    <Routes><Route element={<Layout />}>
      <Route index element={<p>home content</p>} />
      <Route path="profiles" element={<p>profile content</p>} />
    </Route></Routes>
  </MemoryRouter></I18nProvider>);
  expect(await screen.findByText('Running')).toBeInTheDocument();
  expect(screen.getByRole('navigation', { name: 'Main Navigation' })).toBeInTheDocument();
  expect(screen.getByText('Uptime: 3m')).toBeInTheDocument();
  expect(screen.getByRole('img', { name: 'GoTun' })).toHaveAttribute('width', '32');
  expect(screen.getByText('home content')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('link', { name: 'Profiles' }));
  expect(screen.getByText('profile content')).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Profiles' })).toBeInTheDocument();
});

it('lets keyboard users skip navigation to the focusable main content', async () => {
  installBridge();
  render(<I18nProvider><MemoryRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
    <Layout />
  </MemoryRouter></I18nProvider>);
  await userEvent.tab();
  const skip = screen.getByRole('link', { name: '跳转到主要内容' });
  expect(skip).toHaveFocus();
  expect(skip).toHaveAttribute('href', '#main-content');
  await userEvent.keyboard('{Enter}');
  expect(screen.getByRole('main')).toHaveFocus();
});
