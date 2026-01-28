import React, { createContext, useContext, useState, useEffect, useMemo } from 'react';
import { ThemeProvider as MuiThemeProvider, createTheme, Theme, PaletteMode } from '@mui/material';
import { GetSettings, UpdateSettings } from '../../wailsjs/go/main/App';

type ThemeMode = 'light' | 'dark' | 'system';

interface ThemeContextType {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  actualMode: PaletteMode;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export const useThemeMode = () => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useThemeMode must be used within a ThemeProvider');
  }
  return context;
};

const getSystemTheme = (): PaletteMode => {
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  return 'dark';
};

const createAppTheme = (mode: PaletteMode): Theme => {
  const isDark = mode === 'dark';
  
  return createTheme({
    palette: {
      mode,
      primary: {
        main: '#5b6cf9',
        light: '#8e9afc',
        dark: '#3f4cc6',
      },
      secondary: {
        main: '#ff7043',
      },
      background: {
        default: isDark ? '#1a1a2e' : '#f5f5f5',
        paper: isDark ? '#16213e' : '#ffffff',
      },
      success: {
        main: '#4caf50',
      },
      error: {
        main: '#f44336',
      },
      warning: {
        main: '#ff9800',
      },
      text: {
        primary: isDark ? '#ffffff' : '#1f1f1f',
        secondary: isDark ? '#b0b0b0' : '#666666',
      },
    },
    shape: {
      borderRadius: 12,
    },
    typography: {
      fontFamily: '"Inter", "Roboto", "Helvetica", "Arial", sans-serif',
      h6: {
        fontWeight: 600,
      },
    },
    components: {
      MuiCard: {
        styleOverrides: {
          root: {
            backgroundImage: 'none',
            backgroundColor: isDark ? '#282a36' : '#ffffff',
          },
        },
      },
      MuiButton: {
        styleOverrides: {
          root: {
            textTransform: 'none',
            fontWeight: 500,
          },
        },
      },
      MuiSwitch: {
        styleOverrides: {
          root: {
            padding: 8,
          },
        },
      },
    },
  });
};

interface ThemeProviderProps {
  children: React.ReactNode;
}

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [mode, setModeState] = useState<ThemeMode>('dark');
  const [systemMode, setSystemMode] = useState<PaletteMode>(getSystemTheme());

  // 从设置加载主题
  useEffect(() => {
    GetSettings()
      .then((settings) => {
        if (settings.theme) {
          setModeState(settings.theme as ThemeMode);
        }
      })
      .catch(console.error);
  }, []);

  // 监听系统主题变化
  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = (e: MediaQueryListEvent) => {
      setSystemMode(e.matches ? 'dark' : 'light');
    };

    mediaQuery.addEventListener('change', handler);
    return () => mediaQuery.removeEventListener('change', handler);
  }, []);

  const setMode = async (newMode: ThemeMode) => {
    setModeState(newMode);
    try {
      const settings = await GetSettings();
      await UpdateSettings({ ...settings, theme: newMode });
    } catch (err) {
      console.error('Failed to save theme setting:', err);
    }
  };

  const actualMode: PaletteMode = mode === 'system' ? systemMode : mode as PaletteMode;
  const theme = useMemo(() => createAppTheme(actualMode), [actualMode]);

  return (
    <ThemeContext.Provider value={{ mode, setMode, actualMode }}>
      <MuiThemeProvider theme={theme}>{children}</MuiThemeProvider>
    </ThemeContext.Provider>
  );
};

export default ThemeProvider;
