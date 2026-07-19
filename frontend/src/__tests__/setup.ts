// Polyfill localStorage for Node.js 26 test environment
import { beforeEach } from 'vitest';

const store = new Map<string, string>();

const mockStorage: Storage = {
  get length() { return store.size; },
  clear() { store.clear(); },
  getItem(key: string) { return store.get(key) ?? null; },
  setItem(key: string, value: string) { store.set(key, String(value)); },
  removeItem(key: string) { store.delete(key); },
  key(index: number) { return [...store.keys()][index] ?? null; },
};

globalThis.localStorage = mockStorage;

beforeEach(() => {
  store.clear();
});
