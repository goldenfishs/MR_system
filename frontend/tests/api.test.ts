import test from 'node:test';
import assert from 'node:assert/strict';
import { api } from '../src/lib/api.ts';

test('file upload works without modern AbortSignal static methods', async t => {
  for (const key of ['any', 'timeout']) {
    const descriptor = Object.getOwnPropertyDescriptor(AbortSignal, key)!;
    Object.defineProperty(AbortSignal, key, { configurable: true, value: undefined });
    t.after(() => Object.defineProperty(AbortSignal, key, descriptor));
  }
  assert.equal(typeof AbortSignal.any, 'undefined');
  assert.equal(typeof AbortSignal.timeout, 'undefined');
  let calls = 0;
  const form = new FormData();
  form.append('file', new Blob(['%PDF-1.4\n']), 'resume.pdf');
  t.mock.method(globalThis, 'fetch', async (url: string, init: RequestInit) => {
    calls++;
    assert.equal(url, '/api/files');
    assert.equal(init.body, form);
    assert.equal(init.credentials, 'same-origin');
    assert.equal(init.signal?.aborted, false);
    return Response.json({ item: { id: 'uploaded-pdf' } }, { status: 201 });
  });
  const result = await api.upload<{item:{id:string}}>('/files', form, new AbortController().signal);
  assert.equal(result.item.id, 'uploaded-pdf');
  assert.equal(calls, 1);
});

test('caller cancellation still aborts the upload', async t => {
  const caller = new AbortController();
  t.mock.method(globalThis, 'fetch', (_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
    init.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
    caller.abort();
    assert.equal(init.signal?.aborted, true);
  }));
  await assert.rejects(api.upload('/files', new FormData(), caller.signal), /连接失败/);
});

test('upload timeout remains distinct from network errors', async t => {
  let trigger: () => void = () => {};
  t.mock.method(globalThis, 'setTimeout', (callback: () => void, delay: number) => {
    assert.equal(delay, 120000); trigger = callback; return 123;
  });
  const cleared = t.mock.method(globalThis, 'clearTimeout', () => {});
  t.mock.method(globalThis, 'fetch', (_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
    init.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
    trigger();
  }));
  await assert.rejects(api.upload('/files', new FormData()), /请求等待超时/);
  assert.equal(cleared.mock.callCount(), 1);
});

test('server validation error is preserved', async t => {
  t.mock.method(globalThis, 'fetch', async () => Response.json({error:'文件超过大小限制'}, {status:413}));
  await assert.rejects(api.upload('/files', new FormData()), /文件超过大小限制/);
});

test('AI requests use the longer timeout without changing ordinary APIs', async t => {
  const delays: number[] = [];
  t.mock.method(globalThis, 'setTimeout', (_callback: () => void, delay: number) => { delays.push(delay); return 1; });
  t.mock.method(globalThis, 'clearTimeout', () => {});
  t.mock.method(globalThis, 'fetch', async () => Response.json({reply:'ok'}));
  await api.post('/ai/chat', {messages:[]});
  await api.post('/settings/ai-service/test', {});
  await api.get('/ai/status');
  assert.deepEqual(delays, [65000, 65000, 20000]);
});

test('closing the session can cancel an AI request', async t => {
  const caller = new AbortController();
  t.mock.method(globalThis, 'fetch', (_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
    init.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
    caller.abort();
  }));
  await assert.rejects(api.post('/ai/chat', {messages:[]}, caller.signal), /连接失败/);
});
