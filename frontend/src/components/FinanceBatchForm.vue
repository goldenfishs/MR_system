<script setup lang="ts">
import { computed, reactive, ref, watch, nextTick, onBeforeUnmount } from 'vue';
import { api } from '../lib/api';
import { financeMoney as money, yuanToCents } from '../lib/finance';
import type { FileAsset } from '../lib/platform';
import FileUpload from './FileUpload.vue';
import AttachmentList from './AttachmentList.vue';
import InvoiceRecognition, { type InvoiceResult } from './InvoiceRecognition.vue';
const emit=defineEmits<{saved:[number]; cancel:[]; busy:[boolean]}>();
const ids=ref<string[]>([]), uploadBusy=ref(false), saving=ref(false), error=ref('');
type Row={id:string;name:string;title:string;amount:string;category:string;incurredOn:string;notes:string;paymentFileIds:string[];confirmed:boolean};
const rows=ref<Row[]>([]), rowBusy=reactive<Record<string,boolean>>({});
const recognizingAll=ref(false);
const recognitionRefs=new Map<string,InstanceType<typeof InvoiceRecognition>>();
const config=ref<{available:boolean;providerHost:string}>();
api.get<NonNullable<typeof config.value>>('/finance/recognition-config').then(v=>config.value=v).catch(()=>{});
async function recognizeAll(){if(busy.value)return;recognizingAll.value=true;try{for(const row of rows.value){await recognitionRefs.get(row.id)?.recognizeAndFill();await nextTick();}}finally{recognizingAll.value=false;}}
const busy=computed(()=>recognizingAll.value||saving.value||uploadBusy.value||Object.values(rowBusy).some(Boolean));
watch(busy,v=>emit('busy',v));
onBeforeUnmount(()=>emit('busy',false));
const total=computed(()=>money(rows.value.reduce((n,r)=>{try{return n+yuanToCents(r.amount);}catch{return n;}},0)));
let sequence=0;
watch(ids,async values=>{const current=++sequence;const entries=await Promise.all(values.map(async id=>{
 const old=rows.value.find(r=>r.id===id);if(old)return old;
 let name='发票';try{name=(await api.get<{item:FileAsset}>(`/files/${encodeURIComponent(id)}`)).item.name;}catch{}
 return {id,name,title:name.replace(/\.[^.]+$/,''),amount:'',category:'材料采购',incurredOn:'',notes:'',paymentFileIds:[],confirmed:false};
}));if(sequence===current)rows.value=entries;},{deep:true});
function fill(row:Row,r:InvoiceResult){if(r.title)row.title=r.title;if(r.amountCents!==null)row.amount=(r.amountCents/100).toFixed(2);if(r.incurredOn)row.incurredOn=r.incurredOn;row.notes=[row.notes,r.seller?`销售方：${r.seller}`:'',r.invoiceNumber?`发票号码：${r.invoiceNumber}`:''].filter(Boolean).join('\n');row.confirmed=false;}
const requestId=ref(''), lastPayload=ref('');
async function submit(){if(busy.value)return;error.value='';
 try{
  if(!rows.value.length||rows.value.length>20)throw new Error('每批请选择 1～20 张发票。');
  if(rows.value.some(r=>!r.confirmed))throw new Error('请逐条核对金额与费用信息，并勾选确认。');
  const items=rows.value.map((r,i)=>{if(!r.title.trim()||!r.category.trim()||!r.incurredOn)throw new Error(`请补全第 ${i+1} 条的事由、类别和日期。`);return {title:r.title,amountCents:yuanToCents(r.amount),category:r.category,incurredOn:r.incurredOn,notes:r.notes,invoiceFileIds:[r.id],paymentFileIds:r.paymentFileIds};});
  const serialized=JSON.stringify(items);if(lastPayload.value!==serialized){requestId.value=crypto.randomUUID();lastPayload.value=serialized;}
  saving.value=true;const result=await api.post<{items:unknown[]}>('/finance/claims/batch',{requestId:requestId.value,items});emit('saved',result.items.length);
 }catch(e){error.value=(e as Error).message;}finally{saving.value=false;}
}
const today=new Date().toLocaleDateString('sv-SE');
</script>
<template><section class="batch-form"><div class="batch-heading"><div><h2>批量报销</h2><p>每个发票文件生成一条独立申请。核对后统一提交，逐条审核和付款。</p></div><v-btn variant="text" :disabled="busy" @click="emit('cancel')">返回</v-btn></div>
<FileUpload v-model="ids" label="批量上传发票（每批最多 20 张）" accept=".png,.jpg,.jpeg,.webp,.pdf,.ofd" :disabled="busy" @busy="uploadBusy=$event" />
<p class="hint">一张发票一个文件。图片、单页 PDF 可识别；OFD 和多页 PDF 可上传后手动填写。多张发票放在同一文件中时请先拆分。</p>
<v-alert v-if="ids.length>20" type="warning" variant="tonal">已超过 20 张，请移除多余文件后提交。</v-alert>
<div v-if="config?.available && rows.length" class="bulk-recognition"><p class="hint">批量识别会将本批发票发送至 {{config.providerHost}}，自动填入建议值；请逐条核对后再提交。</p><v-btn variant="tonal" color="primary" prepend-icon="mdi-text-recognition" :loading="recognizingAll" :disabled="busy && !recognizingAll" @click="recognizeAll">识别全部发票并填入</v-btn></div><form @submit.prevent="submit"><article v-for="(row,index) in rows" :key="row.id" class="batch-row"><h3>{{index+1}}. {{row.name}}</h3><AttachmentList :ids="[row.id]" />
<InvoiceRecognition :ref="el => { if(el) recognitionRefs.set(row.id,el as InstanceType<typeof InvoiceRecognition>); else recognitionRefs.delete(row.id); }" :file-ids="[row.id]" :disabled="saving || uploadBusy || Object.values(rowBusy).some(Boolean)" @result="fill(row,$event)" @busy="rowBusy['recognize-'+row.id]=$event" />
<div class="fields"><v-text-field v-model="row.title" label="报销事由 *" required :disabled="busy" @update:model-value="row.confirmed=false"/><v-text-field v-model="row.amount" label="金额（元）*" inputmode="decimal" required :disabled="busy" @update:model-value="row.confirmed=false"/><v-combobox v-model="row.category" :items="['材料采购','加工制作','交通出行','差旅住宿','比赛费用','日常运营','其他']" label="费用类别 *" :disabled="busy" @update:model-value="row.confirmed=false"/><v-text-field v-model="row.incurredOn" type="date" label="费用日期 *" min="2000-01-01" :max="today" required :disabled="busy" @update:model-value="row.confirmed=false"/></div>
<v-textarea v-model="row.notes" label="备注" rows="2" :disabled="busy"/><FileUpload v-model="row.paymentFileIds" label="这张发票的支付记录（选填）" accept=".png,.jpg,.jpeg,.webp,.pdf,.zip" :disabled="busy" @busy="rowBusy['payment-'+row.id]=$event"/><v-checkbox v-model="row.confirmed" :disabled="busy" label="我已核对本条金额、日期及凭证" hide-details color="primary"/></article>
<v-alert v-if="error" type="error" variant="tonal" class="my-4">{{error}}</v-alert><div class="batch-footer"><span>{{rows.length}} 条申请 · 合计 <strong>{{total}}</strong></span><v-btn color="primary" type="submit" :loading="saving" :disabled="busy || !rows.length || rows.length>20">统一提交审核</v-btn></div></form>
</section></template>
<style scoped>
.batch-heading,.batch-footer{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap}.batch-heading{margin-bottom:22px}.batch-heading h2{font-size:20px;font-weight:500}.batch-heading p,.hint{color:#757575;font-size:13px;line-height:1.7;margin:8px 0 16px}.batch-row{padding:22px 0;border-top:1px solid #e0e0e0;margin-top:20px}.batch-row h3{font-size:15px;font-weight:500;overflow-wrap:anywhere;margin-bottom:12px}.fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px 20px;margin-top:18px}.batch-footer{padding:20px 0 0;border-top:1px solid #ddd;font-size:14px}@media(max-width:600px){.fields{grid-template-columns:1fr;gap:0}.batch-row{padding:16px 0}.batch-footer .v-btn{width:100%}}
</style>
