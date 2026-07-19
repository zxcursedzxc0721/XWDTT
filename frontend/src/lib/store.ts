import type { Server, AppSettings } from './types';
import { DEFAULT_SETTINGS } from './types';

const SERVERS_KEY = 'wdtt_servers:v1';
const SETTINGS_KEY = 'wdtt_settings:v1';
const LAST_SERVER_KEY = 'wdtt_last_server:v1';

function parse<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

export const serverStore = {
  getAll: (): Server[] => parse<Server[]>(SERVERS_KEY, []),
  save: (servers: Server[]) => localStorage.setItem(SERVERS_KEY, JSON.stringify(servers)),
  add: (server: Omit<Server, 'id'>): Server => {
    const s: Server = { ...server, id: crypto.randomUUID() };
    const all = serverStore.getAll();
    serverStore.save([...all, s]);
    return s;
  },
  update: (server: Server) => {
    serverStore.save(serverStore.getAll().map(s => s.id === server.id ? server : s));
  },
  remove: (id: string) => {
    serverStore.save(serverStore.getAll().filter(s => s.id !== id));
  },
  getLastSelectedId: (): string | null => parse<string | null>(LAST_SERVER_KEY, null),
  setLastSelectedId: (id: string | null) => {
    if (id) localStorage.setItem(LAST_SERVER_KEY, JSON.stringify(id));
    else localStorage.removeItem(LAST_SERVER_KEY);
  },
};

export const settingsStore = {
  get: (): AppSettings => {
    const saved = parse<Partial<AppSettings>>(SETTINGS_KEY, {});
    return { ...DEFAULT_SETTINGS, ...saved };
  },
  save: (settings: AppSettings) => localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings)),
};
