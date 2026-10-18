import { GetSettings, UpdateSettings } from '../wailsjs/go/main/App';
import { service } from '../wailsjs/go/models';

let pendingUpdate: Promise<void> = Promise.resolve();

// The native API replaces all settings, so each UI edit must merge with the latest saved values.
export function saveSettingsPatch(patch: Partial<service.AppSettings>): Promise<service.AppSettings> {
  const update = pendingUpdate.then(async () => {
    const latest = await GetSettings();
    const next = { ...latest, ...patch };
    await UpdateSettings(next);
    return next;
  });
  pendingUpdate = update.then(() => undefined, () => undefined);
  return update;
}
