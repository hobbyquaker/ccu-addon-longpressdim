import {describe, expect, it} from 'vitest';

import {formatDateTime, formatTime, keyLabel, keyNumber, percent} from './model.js';

describe('model helpers', () => {
    it('reads the key number and labels it', () => {
        expect(keyNumber('HmIP-RCV-1:48')).toBe(48);
        expect(keyNumber('BidCoS-Wir:7')).toBe(7);
        expect(keyNumber('nope')).toBe(0);
        expect(keyLabel('HmIP-RCV-1:48', 'up')).toBe('▲ 48');
        expect(keyLabel('BidCoS-RF:3', 'down')).toBe('▼ 3');
    });

    it('formats a level as a percentage with the default', () => {
        expect(percent(0.05)).toBe('5 %');
        expect(percent(0.03)).toBe('3 %');
        expect(percent(undefined)).toBe('5 %');
        expect(percent(0, 3)).toBe('3 %');
        expect(percent(1.005)).toBe('100.5 %');
    });

    it('formats times and leaves the zero time empty', () => {
        expect(formatDateTime(undefined, 'de')).toBe('');
        expect(formatDateTime('0001-01-01T00:00:00Z', 'de')).toBe('');
        expect(formatTime('0001-01-01T00:00:00Z', 'en')).toBe('');
        expect(formatDateTime('2026-10-27T15:40:29+01:00', 'de')).toMatch(/27\.10\.26/);
        expect(formatTime('2026-10-27T15:40:29+01:00', 'en')).toMatch(/\d\d:\d\d:\d\d/);
    });
});
