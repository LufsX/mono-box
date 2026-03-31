import { writable } from 'svelte/store';

export interface Settings {
  clashApiPort: number;
  clashApiSecret: string;
}

const DEFAULT_SETTINGS: Settings = {
  clashApiPort: 9090,
  clashApiSecret: ''
};

function createSettingsStore() {
  const { subscribe, set, update } = writable<Settings>(DEFAULT_SETTINGS);

  return {
    subscribe,
    load: () => {
      const saved = localStorage.getItem('mono-box-settings');
      if (saved) {
        try {
          set(JSON.parse(saved));
        } catch (e) {
          console.error('Failed to load settings:', e);
        }
      }
    },
    save: (settings: Settings) => {
      localStorage.setItem('mono-box-settings', JSON.stringify(settings));
      set(settings);
    },
    update
  };
}

export const settings = createSettingsStore();
