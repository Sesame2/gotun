import React from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import {
  Home as HomeIcon,
  Settings as SettingsIcon,
  CloudQueue as CloudIcon,
  ListAlt as LogsIcon,
  Dns as ProfilesIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useProxyStatus } from '../../hooks/useProxyStatus';
import { useI18n } from '../../providers/I18nProvider';
import '../../styles/layout.css';
import logoImg from '../../assets/images/logo-universal.png';

const Layout: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const location = useLocation();
  const { status } = useProxyStatus();
  const { t } = useI18n();

  const navItems = [
    { path: '/', label: t.nav.home, icon: <HomeIcon /> },
    { path: '/profiles', label: t.nav.profiles, icon: <ProfilesIcon /> },
    { path: '/logs', label: t.nav.logs, icon: <LogsIcon /> },
    { path: '/settings', label: t.nav.settings, icon: <SettingsIcon /> },
  ];

  const getStatusText = () => {
    switch (status.status) {
      case 'running':
        return t.home.running;
      case 'starting':
        return t.home.starting;
      case 'stopping':
        return t.home.stopping;
      case 'error':
        return t.home.error;
      default:
        return t.home.stopped;
    }
  };

  const getStatusClass = () => {
    switch (status.status) {
      case 'running':
        return 'running';
      case 'starting':
      case 'stopping':
        return 'starting';
      default:
        return 'stopped';
    }
  };

  const getPageTitle = () => {
    const item = navItems.find(item => item.path === location.pathname);
    return item?.label || 'GoTun';
  };

  return (
    <div className={`layout ${isDark ? 'dark' : ''}`}>
      {/* 窗口拖动区域 */}
      <div className="titlebar" />

      <aside
        className="layout-sidebar"
      >
        <div className="logo">
          <img src={logoImg} alt="GoTun" />
          <h1>GoTun</h1>
        </div>

        <nav className="nav-menu">
          {navItems.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}
            >
              <span className="icon">{item.icon}</span>
              <span className="label">{item.label}</span>
            </NavLink>
          ))}
        </nav>

        <div className="status-card">
          <div className="status-title">{t.home.proxyStatus}</div>
          <div className="status-value">
            <span className={`status-dot ${getStatusClass()}`}></span>
            {getStatusText()}
          </div>
          {status.status === 'running' && status.uptime && (
            <div style={{ fontSize: 12, color: '#888', marginTop: 4 }}>
              运行时间: {status.uptime}
            </div>
          )}
        </div>
      </aside>

      <main
        className="layout-content"
      >
        <header className="header">
          <h2>{getPageTitle()}</h2>
        </header>
        <div className="main">
          <Outlet />
        </div>
      </main>
    </div>
  );
};

export default Layout;
