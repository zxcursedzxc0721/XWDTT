import { describe, it, expect, beforeEach } from 'vitest';
import { settingsStore } from '../lib/store';
import { DEFAULT_SETTINGS } from '../lib/types';

beforeEach(() => {
  localStorage.clear();
});

describe('settingsStore', () => {
  it('get: возвращает DEFAULT_SETTINGS если пусто', () => {
    const settings = settingsStore.get();
    expect(settings).toEqual(DEFAULT_SETTINGS);
  });

  it('get: мержит сохранённые с дефолтными', () => {
    // Сохраняем частичные настройки (имитируем старую версию без obfsMode)
    localStorage.setItem('wdtt_settings:v1', JSON.stringify({ autoStart: false }));

    const settings = settingsStore.get();
    expect(settings.autoStart).toBe(false);
    expect(settings.obfsMode).toBe(DEFAULT_SETTINGS.obfsMode);
  });

  it('save → get: roundtrip', () => {
    const custom = { autoStart: false, obfsMode: 'video' as const, obfsAccepted: true };
    settingsStore.save(custom);

    const loaded = settingsStore.get();
    expect(loaded).toEqual(custom);
  });

  it('get: невалидный JSON → дефолт', () => {
    localStorage.setItem('wdtt_settings:v1', '{broken');
    const settings = settingsStore.get();
    expect(settings).toEqual(DEFAULT_SETTINGS);
  });

  it('get: пустой объект → дефолт', () => {
    localStorage.setItem('wdtt_settings:v1', '{}');
    const settings = settingsStore.get();
    expect(settings).toEqual(DEFAULT_SETTINGS);
  });

  it('save: перезаписывает предыдущие', () => {
    settingsStore.save({ autoStart: false, obfsMode: 'audio', obfsAccepted: false });
    settingsStore.save({ autoStart: true, obfsMode: 'video', obfsAccepted: true });

    const settings = settingsStore.get();
    expect(settings.autoStart).toBe(true);
    expect(settings.obfsMode).toBe('video');
    expect(settings.obfsAccepted).toBe(true);
  });
});
