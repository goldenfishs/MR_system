export interface InterviewSlot {
  id: string;
  teamId: string;
  direction: string;
  startAt: string;
  endAt: string;
  room: string;
  capacity: number;
  booked: number;
  remaining: number;
  closed: boolean;
}

const cstOffset = 8 * 60 * 60 * 1000;

/** datetime-local inputs have no zone; interview forms deliberately use Beijing time. */
export function beijingTimeISO(date: string, time: string): string {
  if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date) || !/^[0-9]{2}:[0-9]{2}$/.test(time)) return '';
  const stamp = Date.parse(`${date}T${time}:00+08:00`);
  if (!Number.isFinite(stamp)) return '';
  const result = new Date(stamp).toISOString();
  // Date.parse normalizes impossible calendar dates; reject those before submitting.
  if (beijingDay(result) !== date || beijingClock(result) !== time) return '';
  return result;
}

export function beijingDay(value: string): string {
  const stamp = Date.parse(value);
  return Number.isFinite(stamp) ? new Date(stamp + cstOffset).toISOString().slice(0, 10) : '';
}

export function beijingClock(value: string): string {
  const stamp = Date.parse(value);
  return Number.isFinite(stamp) ? new Date(stamp + cstOffset).toISOString().slice(11, 16) : '';
}

export function slotAvailable(slot: InterviewSlot, now: number): boolean {
  return !slot.closed && slot.remaining > 0 && Date.parse(slot.startAt) > now;
}

export function slotDays(slots: InterviewSlot[]): string[] {
  return [...new Set(slots.map(slot => beijingDay(slot.startAt)).filter(Boolean))].sort();
}

export function slotLabel(slot: InterviewSlot): string {
  return `${beijingDay(slot.startAt)} ${beijingClock(slot.startAt)}–${beijingClock(slot.endAt)} · ${slot.room}`;
}
