import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import locales, { LocaleKey, LocaleMessages } from '../locales';
import { GetSettings, UpdateSettings } from '../../wailsjs/go/main/App';

interface I18nContextType {
  locale: LocaleKey;
  setLocale: (locale: LocaleKey) => void;
  t: LocaleMessages;
}

const I18nContext = createContext<I18nContextType | undefined>(undefined);

export const useI18n = () => {
  const context = useContext(I18nContext);
  if (!context) {
    throw new Error('useI18n must be used within an I18nProvider');
  }
  return context;
};

interface I18nProviderProps {
  children: React.ReactNode;
}

export const I18nProvider: React.FC<I18nProviderProps> = ({ children }) => {
  const [locale, setLocaleState] = useState<LocaleKey>('zh-CN');

  // 从设置加载语言
  useEffect(() => {
    GetSettings()
      .then((settings) => {
        if (settings.language && (settings.language === 'zh-CN' || settings.language === 'en-US')) {
          setLocaleState(settings.language as LocaleKey);
        }
      })
      .catch(console.error);
  }, []);

  const setLocale = useCallback(async (newLocale: LocaleKey) => {
    setLocaleState(newLocale);
    try {
      const settings = await GetSettings();
      await UpdateSettings({ ...settings, language: newLocale });
    } catch (err) {
      console.error('Failed to save language setting:', err);
    }
  }, []);

  const t = locales[locale];

  return (
    <I18nContext.Provider value={{ locale, setLocale, t }}>
      {children}
    </I18nContext.Provider>
  );
};

export default I18nProvider;
