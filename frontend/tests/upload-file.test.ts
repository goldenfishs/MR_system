import test from 'node:test';
import assert from 'node:assert/strict';
import { readUploadFile } from '../src/lib/upload-file.ts';

class Reader {
  static LOADING = 1;
  static mode = 'success';
  readyState = 0;
  result: ArrayBuffer | null = null;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  readAsArrayBuffer(file: Blob) {
    this.readyState = 1;
    if (Reader.mode === 'pending') return;
    queueMicrotask(async () => {
      if (Reader.mode === 'error') { this.onerror?.(); return; }
      this.result = Reader.mode === 'partial' ? new ArrayBuffer(1) : await file.arrayBuffer();
      this.readyState = 2; this.onload?.();
    });
  }
  abort() { this.readyState = 2; this.onabort?.(); }
}
function setup(t: any, mode: string) {
  Reader.mode = mode;
  const original = Object.getOwnPropertyDescriptor(globalThis,'FileReader');
  Object.defineProperty(globalThis,'FileReader',{configurable:true,value:Reader});
  t.after(() => { if (original) Object.defineProperty(globalThis,'FileReader',original); else Reflect.deleteProperty(globalThis,'FileReader'); });
}
const file = () => new File(['%PDF-1.4 test contents'],'中文简历.pdf',{type:'application/pdf'});
test('materializes provider file bytes and preserves MIME',async t => {
 setup(t,'success'); const f=file(); const result=await readUploadFile(f,new AbortController().signal);
 assert.equal(await result.text(),await f.text()); assert.equal(result.type,'application/pdf');
 const form=new FormData();form.append('file',result,f.name);assert.equal((form.get('file') as File).name,f.name);
});
test('unreadable cloud file gives local-download guidance',async t => {
 setup(t,'error'); await assert.rejects(readUploadFile(file(),new AbortController().signal),/无法读取所选文件/);
});
test('partial reads are not sent as corrupted uploads',async t => {
 setup(t,'partial'); await assert.rejects(readUploadFile(file(),new AbortController().signal),/未完整读取/);
});
test('cancelled read releases its timeout',async t => {
 setup(t,'pending');const c=new AbortController();const p=readUploadFile(file(),c.signal);c.abort();await assert.rejects(p,/已取消/);
});
