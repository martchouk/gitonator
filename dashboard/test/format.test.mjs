import test from 'node:test';
import assert from 'node:assert/strict';

import { relativeTime } from '../src/utils/format.ts';

function withNow(isoNow, fn) {
  const realNow = Date.now;
  Date.now = () => new Date(isoNow).getTime();
  try {
    return fn();
  } finally {
    Date.now = realNow;
  }
}

test('relativeTime abbreviates seconds as sec', () => {
  const value = withNow('2026-05-23T10:00:45.000Z', () =>
    relativeTime('2026-05-23T10:00:00.000Z')
  );

  assert.equal(value, '45 sec ago');
});

test('relativeTime abbreviates minutes as min', () => {
  const value = withNow('2026-05-23T10:45:00.000Z', () =>
    relativeTime('2026-05-23T10:00:00.000Z')
  );

  assert.equal(value, '45 min ago');
});
