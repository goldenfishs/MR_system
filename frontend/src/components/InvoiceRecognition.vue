<script setup lang="ts">
import { ref, watch } from 'vue';
import { api } from '../lib/api';
import { previewUrl, type FileAsset } from '../lib/platform';
import pdfWorkerSrc from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
const props = defineProps<{ fileIds: string[]; disabled?: boolean }>();
export interface InvoiceResult { amountCents: number | null; incurredOn: string; title: string; invoiceNumber: string; seller: string; currency: string }
const emit = defineEmits<{ result: [InvoiceResult]; busy: [boolean] }>();
const config = ref<{available:boolean; model:string; providerHost?:string; inputMode?:string}>();
const loading = ref(false), error = ref(''), result = ref<InvoiceResult | null>(null), selected = ref('');
const files = ref<{title:string; value:string}[]>([]);
watch(selected, () => { result.value=null; error.value=''; });
api.get<NonNullable<typeof config.value>>('/finance/recognition-config').then(v => config.value=v).catch(()=>{});
watch(()=>props.fileIds.join(','), async () => {
  const ids=[...props.fileIds];
  result.value=null; error.value=''; selected.value=ids.includes(selected.value) ? selected.value : ids[0] || '';
  files.value=await Promise.all(ids.map(async id=> { try { const v=await api.get<{item:FileAsset}>(`/files/${encodeURIComponent(id)}`); return {value:id,title:v.item.name}; } catch { return {value:id,title:'发票文件'}; } }));
},{immediate:true});
async function recognize() {
  if (loading.value || props.disabled || !selected.value) return;
  loading.value=true; emit('busy',true); error.value=''; result.value=null;
  try {
    const fileId=selected.value;
    const {item:file}=await api.get<{item:FileAsset}>(`/files/${encodeURIComponent(fileId)}`);
    let imageDataUrl: string | undefined; let extractedText: string | undefined;
    if(file.mime==='application/pdf') {
      const res=await fetch(previewUrl(fileId),{credentials:'same-origin'});
      if(!res.ok) throw new Error('无法读取 PDF，请重新上传。');
      const bytes=await res.arrayBuffer(); if(bytes.byteLength>25*1024*1024) throw new Error('PDF 过大，请上传发票截图。');
      const pdfjs=await import('pdfjs-dist'); pdfjs.GlobalWorkerOptions.workerSrc=pdfWorkerSrc;
      const task=pdfjs.getDocument({data:new Uint8Array(bytes)});
      try {
        const pdf=await task.promise;
        if(pdf.numPages!==1) throw new Error('多页 PDF 请按每张发票拆成单页 PDF 或图片后识别，避免遗漏金额。');
        const page=await pdf.getPage(1);
        if(config.value?.inputMode==='text') {
          const content=await page.getTextContent(); extractedText=content.items.map(item=>'str' in item ? item.str : '').join(' ');
          if(extractedText.trim().length<20) throw new Error('此 PDF 没有可提取文字，扫描件需管理员配置图片识别模型。');
          if(extractedText.length>30000) throw new Error('PDF 文字过长，请拆分后重试。');
        } else {
        const original=page.getViewport({scale:1});
        const viewport=page.getViewport({scale:Math.min(2,1800/Math.max(original.width,original.height))});
        const canvas=document.createElement('canvas'); canvas.width=Math.ceil(viewport.width); canvas.height=Math.ceil(viewport.height);
        await page.render({canvas,viewport}).promise;
        imageDataUrl=canvas.toDataURL('image/jpeg',0.9); canvas.width=0;canvas.height=0;
        }
      } finally { await task.destroy(); }
    }
    result.value=(await api.post<{item:InvoiceResult}>('/finance/recognize',{fileId,imageDataUrl,extractedText})).item;
  } catch(e) { error.value=(e as Error).message; }
  finally {loading.value=false;emit('busy',false);}
}
defineExpose({ recognizeAndFill: async () => { await recognize(); if (result.value && result.value.currency === 'CNY') apply(); } });
function apply() { if(result.value) {emit('result',result.value);result.value=null;} }
</script>
<template>
  <div v-if="fileIds.length" class="recognition">
    <template v-if="config?.available">
      <p>识别将把选中的发票发送给管理员配置的模型服务{{ config.providerHost ? `（${config.providerHost}）` : '' }}。请核对识别结果后填入，仍需财务审核。</p>
      <div class="recognition-actions"><v-select v-if="files.length>1" v-model="selected" :items="files" label="选择需要识别的发票" density="compact" hide-details :disabled="loading || disabled" /><v-btn variant="tonal" color="primary" prepend-icon="mdi-text-recognition" :disabled="disabled || !selected" :loading="loading" @click="recognize">识别发票</v-btn></div>
    </template>
    <p v-else>可手动填写报销信息。管理员可在「系统管理 → AI 配置 → 应用绑定」配置识别模型。</p>
    <v-alert v-if="error" type="warning" variant="tonal" density="compact" class="mt-3">{{ error }} 可继续手动填写。</v-alert>
    <div v-if="result" class="recognition-result"><strong>识别结果 · 请核对</strong><dl><dt>金额</dt><dd>{{ result.amountCents === null ? '未识别' : `¥${(result.amountCents/100).toFixed(2)}` }} {{result.currency}}</dd><dt>日期</dt><dd>{{result.incurredOn || '未识别'}}</dd><dt>内容</dt><dd>{{result.title || '未识别'}}</dd><dt>销售方</dt><dd>{{result.seller || '未识别'}}</dd><dt>发票号码</dt><dd>{{result.invoiceNumber || '未识别'}}</dd></dl><v-btn variant="tonal" color="primary" :disabled="disabled || (result.currency !== '' && result.currency !== 'CNY')" @click="apply">核对无误，填入表单</v-btn><p v-if="result.currency && result.currency!=='CNY'">目前仅支持人民币报销，请手动核对币种。</p></div>
  </div>
</template>
<style scoped>
.recognition{margin-top:14px}.recognition p{font-size:12px;color:#757575;line-height:1.7;margin:8px 0 12px}.recognition-actions{display:flex;gap:12px;align-items:center;flex-wrap:wrap}.recognition-actions>.v-input{min-width:180px;max-width:400px}.recognition-result{border:1px solid #ddd;padding:16px;margin-top:14px;border-radius:4px;font-size:13px}.recognition-result dl{display:grid;grid-template-columns:72px minmax(0,1fr);gap:8px;margin:14px 0}.recognition-result dt{color:#757575}.recognition-result dd{overflow-wrap:anywhere}
</style>
