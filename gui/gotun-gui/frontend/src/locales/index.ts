import zhCN from './zh-CN';
import enUS from './en-US';

export type LocaleKey = 'zh-CN' | 'en-US';

export const locales = {
  'zh-CN': zhCN,
  'en-US': enUS,
};

export type LocaleMessages = typeof zhCN;

export default locales;
