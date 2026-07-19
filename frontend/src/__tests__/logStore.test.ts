import { describe, it, expect, beforeEach } from 'vitest';
import { logStore } from '../lib/stores/logStore';

beforeEach(() => {
  logStore.clear();
});

describe('logStore', () => {
  it('push: добавляет запись', () => {
    logStore.push('INFO', 'test message');

    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].level).toBe('INFO');
    expect(entries[0].message).toBe('test message');
    expect(entries[0].count).toBe(1);
    expect(entries[0].time).toBeTruthy();
  });

  it('push: ERROR всегда добавляет новую запись', () => {
    logStore.push('ERROR', 'error 1');
    logStore.push('ERROR', 'error 1');
    logStore.push('ERROR', 'error 1');

    const entries = logStore.getAll();
    expect(entries.length).toBe(3);
    for (const e of entries) {
      expect(e.level).toBe('ERROR');
      expect(e.count).toBe(1);
    }
  });

  it('push: дубликаты увеличивают count', () => {
    logStore.push('INFO', 'same message');
    logStore.push('INFO', 'same message');
    logStore.push('INFO', 'same message');

    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].count).toBe(3);
  });

  it('push: записи с одинаковым тегом обновляются', () => {
    logStore.push('INFO', '[WG] Connecting...');
    logStore.push('INFO', '[WG] Connected!');

    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].message).toBe('[WG] Connected!');
    expect(entries[0].count).toBe(2);
  });

  it('push: тег с #номером нормализуется', () => {
    logStore.push('INFO', '[Worker #1] Running');
    logStore.push('INFO', '[Worker #2] Running');

    const entries = logStore.getAll();
    // Оба имеют тег "Worker" (номера обрезаются)
    expect(entries.length).toBe(1);
    expect(entries[0].count).toBe(2);
  });

  it('push: тег с числом в конце нормализуется', () => {
    logStore.push('INFO', '[Peer] Connected 1');
    logStore.push('INFO', '[Peer] Connected 2');

    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].count).toBe(2);
  });

  it('clear: очищает все записи', () => {
    logStore.push('INFO', 'msg1');
    logStore.push('ERROR', 'msg2');
    logStore.clear();

    expect(logStore.getAll()).toEqual([]);
  });

  it('MAX_ENTRIES: обрезка при превышении', () => {
    // Push 505 записей — должна остаться 500
    for (let i = 0; i < 505; i++) {
      logStore.push('INFO', `message ${i}`);
    }

    const entries = logStore.getAll();
    expect(entries.length).toBe(500);
    // Первые 5 должны быть обрезаны
    expect(entries[0].message).toBe('message 5');
  });

  it('extractTag: нет тега → пустая строка', () => {
    logStore.push('INFO', 'no tag here');
    // Запись добавляется без тега — дубликаты не группируются
    logStore.push('INFO', 'no tag here');

    // Последняя проверка — дубликат без тега группируется по message
    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].count).toBe(2);
  });

  it('subscribe: вызывает listener сразу', () => {
    logStore.push('INFO', 'test');
    const entries: any[][] = [];
    const unsub = logStore.subscribe(e => entries.push(e));

    expect(entries.length).toBe(1);
    expect(entries[0].length).toBe(1);
    unsub();
  });

  it('unsubscribe: отписка', () => {
    const entries: any[][] = [];
    const unsub = logStore.subscribe(e => entries.push(e));

    unsub();
    logStore.push('INFO', 'after unsub');

    expect(entries.length).toBe(1);
  });
});
