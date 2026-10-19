// Materialize file-provider contents before sending them. iOS document providers
// may expose a File whose backing resource is temporary or still in the cloud.
export function readUploadFile(file: File, signal: AbortSignal): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const finish = (error?: Error, result?: Blob) => {
      if (settled) return;
      settled = true;
      if (timer !== undefined) clearTimeout(timer);
      signal.removeEventListener('abort', cancel);
      reader.onload = reader.onerror = reader.onabort = null;
      if (error) reject(error);
      else resolve(result!);
    };
    const cancel = () => {
      finish(new Error('已取消文件上传。'));
      if (reader.readyState === FileReader.LOADING) reader.abort();
    };
    if (signal.aborted) { cancel(); return; }
    signal.addEventListener('abort', cancel, { once: true });
    reader.onerror = () => finish(new Error('无法读取所选文件。请先将文件下载到手机本地（iPhone 可存到“文件 → 我的 iPhone”），再重新选择上传。'));
    reader.onabort = () => finish(new Error('文件读取已中断，请重新选择文件。'));
    reader.onload = () => {
      if (!(reader.result instanceof ArrayBuffer) || reader.result.byteLength !== file.size) {
        finish(new Error('文件未完整读取，请先下载到本地后重新选择。'));
        return;
      }
      finish(undefined, new Blob([reader.result], { type: file.type || 'application/octet-stream' }));
    };
    timer = setTimeout(() => {
      finish(new Error('读取文件超时。请先将文件下载到手机本地，再重新选择上传。'));
      if (reader.readyState === FileReader.LOADING) reader.abort();
    }, 60000);
    try { reader.readAsArrayBuffer(file); }
    catch { reader.onerror?.(new ProgressEvent('error') as ProgressEvent<FileReader>); }
  });
}
