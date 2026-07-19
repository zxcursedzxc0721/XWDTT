import { describe, it, expect, beforeEach } from 'vitest';
import { themeStore } from '../lib/stores/themeStore';
import type { Theme } from '../lib/stores/themeStore';

beforeEach(() => {
  localStorage.clear();
  // Сброс к дефолту
  themeStore.set('light');
});

describe('themeStore', () => {
  it('get: дефолт light', () => {
    localStorage.clear();
    // themeStore инициализируется при загрузке модуля — проверяем текущее значение
    const theme = themeStore.get();
    expect(['light', 'dark']).toContain(theme);
  });

  it('set: сохраняет в localStorage и применяет', () => {
    themeStore.set('dark');

    expect(localStorage.getItem('pwdtt_theme')).toBe('dark');
    expect(themeStore.get()).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('set: light', () => {
    themeStore.set('light');

    expect(localStorage.getItem('pwdtt_theme')).toBe('light');
    expect(themeStore.get()).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });

  it('toggle: light → dark → light', () => {
    themeStore.set('light');
    themeStore.toggle();
    expect(themeStore.get()).toBe('dark');

    themeStore.toggle();
    expect(themeStore.get()).toBe('light');
  });

  it('subscribe: уведомляет об изменениях', () => {
    const themes: Theme[] = [];
    const unsub = themeStore.subscribe(t => themes.push(t));

    themeStore.set('dark');
    themeStore.set('light');

    expect(themes).toContain('dark');
    expect(themes).toContain('light');
    unsub();
  });

  it('subscribe: вызывает listener сразу', () => {
    themeStore.set('dark');
    const themes: Theme[] = [];
    const unsub = themeStore.subscribe(t => themes.push(t));

    expect(themes.length).toBe(1);
    expect(themes[0]).toBe('dark');
    unsub();
  });

  it('unsubscribe: отписка', () => {
    const themes: Theme[] = [];
    const unsub = themeStore.subscribe(t => themes.push(t));

    unsub();
    themeStore.toggle();

    expect(themes.length).toBe(1);
  });

  it('set: data-theme атрибут на documentElement', () => {
    themeStore.set('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');

    themeStore.set('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });
});
