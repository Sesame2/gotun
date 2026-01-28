import React from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter, Routes, Route } from 'react-router-dom';
import CssBaseline from '@mui/material/CssBaseline';
import './styles/global.css';

import Layout from './components/Layout';
import HomePage from './pages/Home';
import ProfilesPage from './pages/Profiles';
import LogsPage from './pages/Logs';
import SettingsPage from './pages/Settings';
import { ThemeProvider, I18nProvider } from './providers';

const container = document.getElementById('root');
const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <I18nProvider>
      <ThemeProvider>
        <CssBaseline />
        <HashRouter>
          <Routes>
            <Route path="/" element={<Layout />}>
              <Route index element={<HomePage />} />
              <Route path="profiles" element={<ProfilesPage />} />
              <Route path="logs" element={<LogsPage />} />
              <Route path="settings" element={<SettingsPage />} />
            </Route>
          </Routes>
        </HashRouter>
      </ThemeProvider>
    </I18nProvider>
  </React.StrictMode>
);

