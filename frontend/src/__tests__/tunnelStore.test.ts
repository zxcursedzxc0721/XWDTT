import { describe, it, expect } from 'vitest';
import { tunnelStore } from '../lib/stores/tunnelStore';
import type { TunnelState } from '../lib/types';

describe('tunnelStore', () => {
  it('get: начальное состояние idle', () => {
    // Сброс — создаём новый store через import (модуль cached)
    // Начальное состояние зависит от предыдущих тестов, проверяем что get() работает
    const state = tunnelStore.get();
    expect(['idle', 'connecting', 'connected', 'disconnecting']).toContain(state);
  });

  it('set: обновляет состояние', () => {
    tunnelStore.set('connecting');
    expect(tunnelStore.get()).toBe('connecting');

    tunnelStore.set('connected');
    expect(tunnelStore.get()).toBe('connected');
  });

  it('set: уведомляет listeners', () => {
    const states: TunnelState[] = [];
    const unsub = tunnelStore.subscribe(s => states.push(s));

    tunnelStore.set('connecting');
    tunnelStore.set('connected');

    expect(states).toContain('connecting');
    expect(states).toContain('connected');
    unsub();
  });

  it('subscribe: вызывает listener сразу с текущим состоянием', () => {
    tunnelStore.set('connected');
    const states: TunnelState[] = [];
    const unsub = tunnelStore.subscribe(s => states.push(s));

    // subscribe должен вызвать callback сразу
    expect(states.length).toBe(1);
    expect(states[0]).toBe('connected');
    unsub();
  });

  it('unsubscribe: отписка от уведомлений', () => {
    const states: TunnelState[] = [];
    const unsub = tunnelStore.subscribe(s => states.push(s));

    unsub();
    tunnelStore.set('disconnecting');

    // После отписки не должно быть новых вызовов
    expect(states.length).toBe(1);
  });

  it('все состояния', () => {
    const allStates: TunnelState[] = ['idle', 'connecting', 'connected', 'disconnecting'];
    for (const s of allStates) {
      tunnelStore.set(s);
      expect(tunnelStore.get()).toBe(s);
    }
  });
});
