import { useState, useEffect, useCallback } from 'react';
import { service } from '../../wailsjs/go/models';
import {
  GetProfiles,
  GetProfile,
  AddProfile,
  UpdateProfile,
  DeleteProfile,
  GetSettings,
  UpdateSettings,
} from '../../wailsjs/go/main/App';

type SSHProfile = service.SSHProfile;
type AppSettings = service.AppSettings;

export function useProfiles() {
  const [profiles, setProfiles] = useState<SSHProfile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchProfiles = useCallback(async () => {
    try {
      setLoading(true);
      const data = await GetProfiles();
      setProfiles(data || []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取配置列表失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchProfiles();
  }, [fetchProfiles]);

  const addProfile = async (profile: Partial<SSHProfile>) => {
    try {
      await AddProfile(profile as SSHProfile);
      await fetchProfiles();
    } catch (err) {
      throw err;
    }
  };

  const updateProfile = async (profile: SSHProfile) => {
    try {
      await UpdateProfile(profile);
      await fetchProfiles();
    } catch (err) {
      throw err;
    }
  };

  const deleteProfile = async (id: string) => {
    try {
      await DeleteProfile(id);
      await fetchProfiles();
    } catch (err) {
      throw err;
    }
  };

  const getProfile = async (id: string): Promise<SSHProfile | null> => {
    try {
      const profile = await GetProfile(id);
      return profile as SSHProfile;
    } catch (err) {
      return null;
    }
  };

  return {
    profiles,
    loading,
    error,
    fetchProfiles,
    addProfile,
    updateProfile,
    deleteProfile,
    getProfile,
  };
}

export function useSettings() {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchSettings = useCallback(async () => {
    try {
      setLoading(true);
      const data = await GetSettings();
      setSettings(data as AppSettings);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取设置失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSettings();
  }, [fetchSettings]);

  const updateSettings = async (newSettings: AppSettings) => {
    try {
      await UpdateSettings(newSettings);
      setSettings(newSettings);
    } catch (err) {
      throw err;
    }
  };

  return {
    settings,
    loading,
    error,
    fetchSettings,
    updateSettings,
  };
}
