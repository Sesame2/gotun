// 从 wails 生成的模型重新导出类型
import { service } from '../../wailsjs/go/models';

// 类型别名，方便使用
export type SSHProfile = service.SSHProfile;
export type ProxyStats = service.ProxyStats;
export type AppSettings = service.AppSettings;

// 代理状态
export type ProxyStatusType = 'stopped' | 'starting' | 'running' | 'stopping' | 'error';

// 创建配置文件表单数据
export interface ProfileFormData {
  name: string;
  host: string;
  port: string;
  user: string;
  password: string;
  keyFile: string;
  httpAddr: string;
  socksAddr: string;
  systemProxy: boolean;
  ruleFile: string;
}

// 主题模式
export type ThemeMode = 'light' | 'dark';
