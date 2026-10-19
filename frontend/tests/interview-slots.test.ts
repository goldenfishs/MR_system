import test from 'node:test';
import assert from 'node:assert/strict';
import { beijingTimeISO, beijingDay, beijingClock, slotAvailable, slotDays, type InterviewSlot } from '../src/lib/interview-slots.ts';

const slot: InterviewSlot = { id:'one', teamId:'team', direction:'运营', startAt:'2026-10-22T16:30:00Z', endAt:'2026-10-22T16:45:00Z', room:'A101', capacity:2, booked:1, remaining:1, closed:false };

test('Beijing calendar conversion does not depend on the browser timezone', () => {
  assert.equal(beijingTimeISO('2026-10-23', '00:30'), '2026-10-22T16:30:00.000Z');
  assert.equal(beijingDay(slot.startAt), '2026-10-23');
  assert.equal(beijingClock(slot.startAt), '00:30');
  assert.equal(beijingTimeISO('2026-02-30', '09:00'), '');
  assert.equal(beijingTimeISO('2026-10-23', '24:00'), '');
  assert.equal(beijingTimeISO('', '09:00'), '');
});

test('only future, open, non-full slots can be booked', () => {
  const now = Date.parse('2026-10-22T15:00:00Z');
  assert.equal(slotAvailable(slot, now), true);
  assert.equal(slotAvailable({ ...slot, closed:true }, now), false);
  assert.equal(slotAvailable({ ...slot, remaining:0 }, now), false);
  assert.equal(slotAvailable(slot, Date.parse(slot.startAt)), false);
});

test('date tabs group by Beijing dates and remain sorted and unique', () => {
  assert.deepEqual(slotDays([slot,{ ...slot, id:'two', startAt:'2026-10-22T15:00:00Z' },slot]), ['2026-10-22','2026-10-23']);
});
