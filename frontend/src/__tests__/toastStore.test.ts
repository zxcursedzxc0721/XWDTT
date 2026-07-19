import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { toastStore } from '../lib/stores/toastStore';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
});

describe('toastStore', () => {
  it('show: устанавливает сообщение', () => {
    const messages: (string | null)[] = [];
    const unsub = toastStore.subscribe(msg => messages.push(msg));

    toastStore.show('Hello');

    expect(messages).toContain('Hello');
    unsub();
  });

  it('subscribe: вызывает listener сразу с текущим состоянием', () => {
    // Сначала очищаем — ждём пока предыдущий таймер сработает
    vi.advanceTimersByTime(3000);

    const messages: (string | null)[] = [];
    const unsub = toastStore.subscribe(msg => messages.push(msg));

    // subscribe должен вызвать callback сразу с текущим значением
    expect(messages.length).toBe(1);
    // Текущее значение — null (таймер предыдущего test очистил)
    expect(messages[0]).toBeNull();
    unsub();
  });

  it('show: автоочистка после таймаута', () => {
    const messages: (string | null)[] = [];
    const unsub = toastStore.subscribe(msg => messages.push(msg));

    toastStore.show('Temp', 1000);
    expect(messages).toContain('Temp');

    vi.advanceTimersByTime(1000);

    // После таймаута сообщение должно очиститься
    expect(messages).toContain(null);
    unsub();
  });

  it('show: новый вызов отменяет предыдущий таймер', () => {
    const messages: (string | null)[] = [];
    const unsub = toastStore.subscribe(msg => messages.push(msg));

    toastStore.show('First', 1000);
    toastStore.show('Second', 1000);

    expect(messages).toContain('Second');

    vi.advanceTimersByTime(1000);

    // 'Second' должен очиститься
    expect(messages).toContain(null);
    unsub();
  });
});
