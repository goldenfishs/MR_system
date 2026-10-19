<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { api } from '../lib/api';
import { auth } from '../lib/auth';
import { previewUrl, type FileAsset } from '../lib/platform';
import { financeMoney as money, yuanToCents, financeStatusLabels as statusLabels, type FinanceClaim, type FinanceLedgerEntry, type FinanceSummary, type FinanceStatus } from '../lib/finance';
import FileUpload from '../components/FileUpload.vue';
import AttachmentList from '../components/AttachmentList.vue';
const route = useRoute();
const management = computed(() => route.path === '/finance-management' && auth.user?.role === 'admin');
const tab = ref('claims'), loading = ref(false), saving = ref(false), error = ref(''), success = ref('');
const uploads = reactive<Record<string, boolean>>({});
const busy = computed(() => saving.value || Object.values(uploads).some(Boolean));
const claims = ref<FinanceClaim[]>([]), ledger = ref<FinanceLedgerEntry[]>([]);
const summary = ref<FinanceSummary>({ incomeCents: 0, expenseCents: 0, balanceCents: 0, pendingCents: 0 });
const filter = reactive({ status: '', q: '', from: '', to: '', direction: '' });
const appliedFilter = reactive({ status: '', q: '', from: '', to: '', direction: '' });
const selected = ref<string[]>([]), detail = ref<FinanceClaim | null>(null), ledgerDetail = ref<FinanceLedgerEntry | null>(null);
const editor = ref(false), ledgerEditor = ref(false), editingId = ref(''), editingVersion = ref(0);
const today = () => { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`; };
const blankForm = () => ({ title: '', amount: '', category: '材料采购', incurredOn: today(), notes: '', invoiceFileIds: [] as string[], paymentFileIds: [] as string[] });
const form = reactive(blankForm());
const entryForm = reactive({ direction: 'income' as 'income' | 'expense', amount: '', occurredOn: today(), category: '', counterparty: '', note: '', fileIds: [] as string[] });
const action = ref(''), reason = ref(''), paidOn = ref(today()), paymentReference = ref(''), payoutFileIds = ref<string[]>([]), reverseReason = ref('');
const metadata = reactive<Record<string, FileAsset>>({});
const categories = ['材料采购', '加工制作', '交通出行', '差旅住宿', '比赛费用', '日常运营', '其他'];
const statuses = Object.entries(statusLabels).map(([value, title]) => ({ value, title }));
const actionLabels: Record<string, string> = { submit: '提交报销', submitted: '提交报销', approve: '审核通过', approved: '审核通过', return: '退回修改', returned: '退回修改', pay: '确认付款', paid: '确认付款', resubmit: '修改并重新提交', cancel: '撤回申请', cancelled: '撤回申请' };
const statusColor = (s: FinanceStatus) => ({ submitted: 'info', approved: 'warning', returned: 'error', paid: 'success', cancelled: 'grey' })[s];
const allSelected = computed(() => claims.value.length > 0 && claims.value.every(c => selected.value.includes(c.id)));
const selectedTotal = computed(() => claims.value.filter(c => selected.value.includes(c.id)).reduce((sum, c) => sum + c.amountCents, 0));
const claimPreviewAmount = computed(() => {
  try { return money(yuanToCents(form.amount)); } catch { return money(0); }
});
const claimReceiptCount = computed(() => form.invoiceFileIds.length + form.paymentFileIds.length);
const personalStats = computed(() => [
  { label: '待审核', value: claims.value.filter(c => c.status === 'submitted').length + ' 笔' },
  { label: '待付款', value: money(claims.value.filter(c => c.status === 'approved').reduce((n,c) => n+c.amountCents,0)) },
  { label: '已完成', value: money(claims.value.filter(c => c.status === 'paid').reduce((n,c) => n+c.amountCents,0)) },
]);
function params(includeStatus = true, values = filter) {
  const p = new URLSearchParams();
  if (!management.value) p.set('scope', 'mine');
  for (const key of ['q','from','to'] as const) if (values[key]) p.set(key, values[key]);
  if (includeStatus && values.status) p.set('status', values.status);
  if (!includeStatus && values.direction) p.set('direction', values.direction);
  return p;
}
let loadSequence = 0;
async function load() {
  const sequence = ++loadSequence; loading.value = true; error.value = '';
  const requestedFilter = { ...filter };
  try {
    if (filter.from && filter.to && filter.from > filter.to) throw new Error('开始日期不能晚于结束日期。');
    const [claimResult, ledgerResult] = await Promise.all([
      api.get<{ items: FinanceClaim[] }>(`/finance/claims?${params(true, requestedFilter)}`),
      management.value ? api.get<{ items: FinanceLedgerEntry[]; summary: FinanceSummary }>(`/finance/ledger?${params(false, requestedFilter)}`) : Promise.resolve(null),
    ]);
    if (sequence !== loadSequence) return;
    Object.assign(appliedFilter, requestedFilter);
    claims.value = claimResult.items || []; selected.value = selected.value.filter(id => claims.value.some(c => c.id === id));
    if (ledgerResult) { ledger.value = ledgerResult.items || []; summary.value = ledgerResult.summary; }
    if (detail.value) detail.value = claims.value.find(c => c.id === detail.value!.id) || null;
    if (ledgerDetail.value) ledgerDetail.value = ledger.value.find(c => c.id === ledgerDetail.value!.id) || null;
  } catch (e) { if (sequence === loadSequence) error.value = (e as Error).message; }
  finally { if (sequence === loadSequence) loading.value = false; }
}
function resetPage() {
  detail.value = null; ledgerDetail.value = null; editor.value = false; ledgerEditor.value = false; selected.value = []; action.value = ''; tab.value = 'claims';
  Object.assign(filter, { status: '', q: '', from: '', to: '', direction: '' }); success.value = ''; void load();
}
watch(management, resetPage, { immediate: true });
async function loadMetadata(ids: string[]) {
  await Promise.all(ids.filter(id => !metadata[id]).map(async id => {
    try { metadata[id] = (await api.get<{ item: FileAsset }>(`/files/${encodeURIComponent(id)}`)).item; } catch { /* Download remains visible. */ }
  }));
}
function openDetail(item: FinanceClaim) {
  detail.value = item; editor.value = false; action.value = ''; reason.value = ''; error.value = ''; success.value = '';
  void loadMetadata([...item.invoiceFileIds, ...item.paymentFileIds, ...(item.payoutFileIds || [])]);
}
function newClaim(item?: FinanceClaim) {
  if (busy.value) return;
  editingId.value = item?.id || ''; editingVersion.value = item?.version || 0;
  Object.assign(form, item ? { title: item.title, amount: (item.amountCents / 100).toFixed(2), category: item.category, incurredOn: item.incurredOn, notes: item.notes, invoiceFileIds: [...item.invoiceFileIds], paymentFileIds: [...item.paymentFileIds] } : blankForm());
  editor.value = true; detail.value = null; error.value = ''; success.value = '';
}
async function saveClaim() {
  if (busy.value) return;
  error.value = ''; saving.value = true;
  try {
    if (!form.title.trim()) throw new Error('请填写报销事由。');
    if (!form.invoiceFileIds.length && !form.paymentFileIds.length) throw new Error('请至少上传一份发票或支付记录。');
    if (form.invoiceFileIds.length + form.paymentFileIds.length > 20) throw new Error('每份申请最多保存 20 个凭证文件。');
    const payload = { title: form.title.trim(), amountCents: yuanToCents(form.amount), category: form.category, incurredOn: form.incurredOn, notes: form.notes, invoiceFileIds: form.invoiceFileIds, paymentFileIds: form.paymentFileIds };
    const result = editingId.value
      ? await api.patch<{ item: FinanceClaim }>(`/finance/claims/${encodeURIComponent(editingId.value)}`, { ...payload, action: 'resubmit', version: editingVersion.value })
      : await api.post<{ item: FinanceClaim }>('/finance/claims', payload);
    editor.value = false; await load(); openDetail(result.item); success.value = '报销申请已提交，等待财务审核。';
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
function prepareAction(next: string) { action.value = next; reason.value = ''; paidOn.value = today(); paymentReference.value = ''; payoutFileIds.value = []; error.value = ''; }
async function performAction() {
  if (!detail.value || busy.value) return;
  saving.value = true; error.value = '';
  try {
    if (action.value === 'return' && !reason.value.trim()) throw new Error('请填写退回原因，方便申请人修改。');
    if (action.value === 'pay' && !paymentReference.value.trim()) throw new Error('请填写付款流水号或付款说明。');
    const result = await api.patch<{ item: FinanceClaim }>(`/finance/claims/${encodeURIComponent(detail.value.id)}`, {
      action: action.value, version: detail.value.version, reason: reason.value.trim(), paidOn: paidOn.value,
      paymentReference: paymentReference.value.trim(), payoutFileIds: payoutFileIds.value,
    });
    action.value = ''; await load(); openDetail(result.item); success.value = '操作已保存。';
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function openLinkedClaim(id: string) {
  if (loading.value || busy.value) return;
  loading.value = true; error.value = '';
  try {
    const existing = claims.value.find(c => c.id === id);
    const item = existing || (await api.get<{ items: FinanceClaim[] }>('/finance/claims')).items.find(c => c.id === id);
    if (!item) throw new Error('未找到关联报销申请，请刷新后重试。');
    tab.value = 'claims'; ledgerDetail.value = null; openDetail(item);
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function newEntry() {
  Object.assign(entryForm, { direction: 'income', amount: '', occurredOn: today(), category: '', counterparty: '', note: '', fileIds: [] });
  ledgerEditor.value = true; ledgerDetail.value = null; error.value = ''; success.value = '';
}
async function saveEntry() {
  if (busy.value) return;
  saving.value = true; error.value = '';
  try {
    await api.post('/finance/ledger', { ...entryForm, amount: undefined, amountCents: yuanToCents(entryForm.amount) });
    ledgerEditor.value = false; await load(); success.value = '收支已登记入账。';
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function reverseEntry() {
  if (busy.value || !ledgerDetail.value) return;
  saving.value = true; error.value = '';
  try {
    if (!reverseReason.value.trim()) throw new Error('请填写冲销原因。');
    await api.post(`/finance/ledger/${encodeURIComponent(ledgerDetail.value.id)}/reverse`, { reason: reverseReason.value.trim() });
    reverseReason.value = ''; await load(); success.value = '已冲销，原始记录保留在账本中。';
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
function toggleAll() { selected.value = allSelected.value ? [] : claims.value.map(c => c.id); }
const exporting = ref(false);
async function exportFile(format: 'csv' | 'zip', ledgerExport = false) {
  if (exporting.value) return;
  exporting.value = true; error.value = '';
  try {
    const p = params(!ledgerExport, appliedFilter); p.set('format', format);
    if (!ledgerExport && selected.value.length) p.set('ids', selected.value.join(','));
    const response = await fetch(`/api/finance/${ledgerExport ? 'ledger/export' : 'export'}?${p}`, { credentials: 'same-origin', cache: 'no-store' });
    if (!response.ok) { const body = await response.json().catch(() => ({})); throw new Error(body.error || '导出失败，请重试。'); }
    const blob = await response.blob(); const url = URL.createObjectURL(blob); const link = document.createElement('a');
    link.href = url; link.download = `${ledgerExport ? '收支账本' : '报销凭证'}-${today()}.${format}`;
    document.body.appendChild(link); link.click(); link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000);
    success.value = '导出已准备完成，请查看浏览器下载。';
  } catch (e) { error.value = (e as Error).message; }
  finally { exporting.value = false; }
}
const timestamp = (value: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
</script>

<template>
  <div class="page-content finance-page">
    <header class="finance-hero" :class="{ 'finance-hero-management': management }">
      <div class="hero-copy">
        <div class="eyebrow"><v-icon size="14">{{ management ? 'mdi-chart-box-outline' : 'mdi-wallet-outline' }}</v-icon>{{ management ? 'FINANCE WORKSPACE' : 'MY FINANCE' }}</div>
        <h1>{{ management ? '财务管理' : '财务中心' }}</h1>
        <p>{{ management ? '从凭证审核到付款入账，清楚掌握每一笔资金流向。' : '报销、审核、付款进度，都在这里清晰可查。' }}</p>
      </div>
      <div class="hero-actions">
        <span v-if="management" class="secure-label"><v-icon size="15">mdi-shield-check-outline</v-icon>仅财务管理员可见</span>
        <v-btn v-if="!management && !editor" color="primary" prepend-icon="mdi-plus" size="large" :disabled="busy" @click="newClaim()">新建报销</v-btn>
        <v-btn v-if="management && tab === 'ledger' && !ledgerEditor" color="primary" prepend-icon="mdi-plus" size="large" :disabled="busy" @click="newEntry">登记收支</v-btn>
      </div>
    </header>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>
    <v-alert v-if="success" type="success" variant="tonal" class="mb-4" closable @click:close="success = ''">{{ success }}</v-alert>
    <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-3" />
    <div v-if="management" class="finance-stats">
      <article class="stat-card balance-card"><div class="stat-label"><v-icon size="18">mdi-wallet</v-icon><span>账面余额</span></div><strong>{{ money(summary.balanceCents) }}</strong><small>累计入账减累计支出</small></article>
      <article class="stat-card"><div class="stat-label"><v-icon size="18" color="success">mdi-arrow-down-left</v-icon><span>累计入账</span></div><strong class="income">{{ money(summary.incomeCents) }}</strong><small>已登记的资金转入</small></article>
      <article class="stat-card"><div class="stat-label"><v-icon size="18" color="error">mdi-arrow-up-right</v-icon><span>累计支出</span></div><strong>{{ money(summary.expenseCents) }}</strong><small>已实际付款并记账</small></article>
      <article class="stat-card pending-card"><div class="stat-label"><v-icon size="18" color="warning">mdi-clock-outline</v-icon><span>待付款</span></div><strong class="pending">{{ money(summary.pendingCents) }}</strong><small>审核通过，尚未计入支出</small></article>
    </div>
    <div v-else-if="!editor && !detail" class="finance-stats personal-stats"><article v-for="(item,index) in personalStats" :key="item.label" class="stat-card"><div class="stat-label"><v-icon size="18" :color="['warning','primary','success'][index]">{{ ['mdi-clipboard-text-clock-outline','mdi-cash-clock','mdi-check-circle-outline'][index] }}</v-icon><span>{{ item.label }}</span></div><strong>{{ item.value }}</strong><small>按当前列表筛选统计</small></article></div>
    <v-tabs v-if="management" v-model="tab" color="primary" class="finance-tabs" :disabled="busy"><v-tab value="claims" prepend-icon="mdi-clipboard-check-outline" @click="ledgerEditor = false; ledgerDetail = null">报销审核</v-tab><v-tab value="ledger" prepend-icon="mdi-book-open-page-variant-outline" @click="detail = null; action = ''">收支账本</v-tab></v-tabs>
    <section v-if="editor" class="finance-panel editor-panel">
      <div class="editor-topline"><v-btn variant="text" prepend-icon="mdi-arrow-left" :disabled="busy" @click="editor = false">返回我的报销</v-btn><span class="draft-badge"><v-icon size="15">mdi-lock-outline</v-icon>凭证仅本人和财务管理员可见</span></div>
      <div class="editor-layout">
        <div class="editor-main">
          <div class="editor-heading"><div class="editor-step">{{ editingId ? '重新提交' : '报销申请' }}</div><h2>{{ editingId ? '修改申请信息' : '填写报销信息' }}</h2><p>填写实际支出，并上传发票或支付记录。提交后进入财务审核。</p></div>
          <form class="claim-form" @submit.prevent="saveClaim"><div class="form-section"><h3><span>01</span>基本信息</h3><v-text-field variant="outlined" v-model="form.title" label="报销事由 *" placeholder="例如：电机驱动板采购" maxlength="160" required :disabled="busy" />
            <div class="form-grid"><v-text-field variant="outlined" v-model="form.amount" label="报销金额（元）*" inputmode="decimal" placeholder="0.00" prefix="¥" required :disabled="busy" /><v-combobox variant="outlined" v-model="form.category" :items="categories" label="费用类别 *" :disabled="busy" /><v-text-field variant="outlined" v-model="form.incurredOn" type="date" label="费用发生日期 *" min="2000-01-01" :max="today()" required :disabled="busy" /></div>
            <v-textarea variant="outlined" v-model="form.notes" label="用途说明 / 备注" placeholder="补充说明采购内容、使用场景等（选填）" maxlength="4000" rows="3" :disabled="busy" /></div>
            <div class="form-section receipts-form-section"><div class="section-heading"><div><h3><span>02</span>上传凭证</h3><p>发票和支付记录至少上传一项</p></div><v-chip size="small" :color="claimReceiptCount ? 'success' : 'default'" variant="tonal">已添加 {{ claimReceiptCount }} 份</v-chip></div>
              <div class="receipt-columns"><FileUpload v-model="form.invoiceFileIds" label="发票" accept=".png,.jpg,.jpeg,.webp,.pdf,.ofd,.zip" :disabled="saving" @busy="uploads.invoice = $event" /><FileUpload v-model="form.paymentFileIds" label="支付记录" accept=".png,.jpg,.jpeg,.webp,.pdf,.zip" :disabled="saving" @busy="uploads.payment = $event" /></div>
            </div>
            <div class="panel-actions"><v-btn variant="text" :disabled="busy" @click="editor = false">取消</v-btn><v-btn type="submit" color="primary" prepend-icon="mdi-send-outline" size="large" :loading="saving" :disabled="busy && !saving">提交审核</v-btn></div>
          </form>
        </div>
        <aside class="application-aside"><div class="amount-preview"><span>本次申请金额</span><strong>{{ claimPreviewAmount }}</strong><small>{{ form.category || '未选择费用类别' }}</small></div>
          <div class="aside-card"><h3>提交后流程</h3><ol class="process-steps"><li class="active"><span>1</span><div><strong>提交申请</strong><small>财务收到凭证并开始审核</small></div></li><li><span>2</span><div><strong>审核确认</strong><small>审核通过后进入待付款</small></div></li><li><span>3</span><div><strong>完成报销</strong><small>实际付款后更新状态</small></div></li></ol></div>
          <div class="privacy-note"><v-icon size="18">mdi-shield-lock-outline</v-icon><p>你的凭证仅对你本人和财务管理员开放，不会公开给其他成员。</p></div>
        </aside>
      </div>
    </section>
    <section v-else-if="detail && tab === 'claims'" class="finance-panel detail-panel">
      <v-btn variant="text" prepend-icon="mdi-arrow-left" :disabled="busy" @click="detail = null; action = ''">返回{{ management ? '报销列表' : '我的报销' }}</v-btn>
      <div class="detail-heading"><div><h2>{{ detail.title }}</h2><p class="muted">{{ detail.userName }} · {{ detail.category }} · {{ detail.incurredOn }}</p></div><div><strong class="detail-amount">{{ money(detail.amountCents) }}</strong><v-chip :color="statusColor(detail.status)" size="small" variant="tonal">{{ statusLabels[detail.status] }}</v-chip></div></div>
      <p v-if="detail.notes" class="preserve-lines detail-notes">{{ detail.notes }}</p>
      <v-alert v-if="detail.reviewNote && detail.status === 'returned'" type="warning" variant="tonal" class="my-4">退回原因：{{ detail.reviewNote }}</v-alert>
      <div class="receipt-columns"><section v-for="group in [{title: '发票', ids: detail.invoiceFileIds}, {title: '支付记录', ids: detail.paymentFileIds}, ...(detail.payoutFileIds?.length ? [{title: '付款凭证', ids: detail.payoutFileIds}] : [])]" :key="group.title" class="receipt-section"><h3>{{ group.title }}</h3><div class="receipt-previews"><template v-for="id in group.ids" :key="id"><a v-if="metadata[id]?.mime.startsWith('image/') && metadata[id]?.mime !== 'image/svg+xml'" :href="previewUrl(id)" target="_blank" rel="noopener" :aria-label="`查看${metadata[id]?.name}`"><img :src="previewUrl(id)" :alt="metadata[id]?.name || group.title" loading="lazy" /></a><a v-else-if="metadata[id]?.mime === 'application/pdf'" class="pdf-preview" :href="previewUrl(id)" target="_blank" rel="noopener"><v-icon>mdi-file-pdf-box</v-icon>预览 PDF</a></template></div><p v-if="!group.ids.length" class="muted">未提供{{ group.title }}</p><AttachmentList :ids="group.ids" /></section></div>
      <p v-if="detail.paidAt" class="muted">付款日期：{{ detail.paidOn || detail.paidAt.slice(0,10) }} · 付款流水 / 说明：{{ detail.paymentReference || '—' }}</p>
      <div v-if="!action" class="panel-actions detail-actions">
        <template v-if="management"><v-btn v-if="detail.status === 'submitted'" color="primary" @click="prepareAction('approve')">审核通过</v-btn><v-btn v-if="['submitted','approved'].includes(detail.status)" variant="outlined" color="warning" @click="prepareAction('return')">退回修改</v-btn><v-btn v-if="detail.status === 'approved'" color="primary" prepend-icon="mdi-check-circle-outline" @click="prepareAction('pay')">登记付款完成</v-btn></template>
        <template v-else><v-btn v-if="detail.status === 'returned'" color="primary" @click="newClaim(detail)">修改并重新提交</v-btn><v-btn v-if="['submitted','returned'].includes(detail.status)" variant="outlined" @click="prepareAction('cancel')">撤回申请</v-btn></template>
      </div>
      <form v-if="action" class="action-panel" @submit.prevent="performAction"><h3>{{ {approve:'确认审核通过', return:'退回申请', pay:'登记付款完成', cancel:'撤回申请'}[action] }}</h3><p v-if="action === 'approve'">确认凭证与金额无误后，申请进入“待付款”。此时尚不计入账本支出。</p><p v-if="action === 'cancel'">撤回后保留申请与操作记录。如需报销，请重新提交一份申请。</p><v-textarea variant="outlined" v-if="action === 'return'" v-model="reason" label="退回原因 *" rows="3" maxlength="2000" required :disabled="busy" />
        <template v-if="action === 'pay'"><v-alert type="info" variant="tonal" density="compact" class="mb-4">请确认已完成线下转账，本操作仅记账，并自动生成一笔账本支出。</v-alert><div class="form-grid"><v-text-field variant="outlined" v-model="paidOn" type="date" label="实际付款日期 *" :min="detail.incurredOn" :max="today()" required :disabled="busy" /><v-text-field variant="outlined" v-model="paymentReference" label="付款流水号 / 付款说明 *" maxlength="300" required :disabled="busy" /></div><FileUpload v-model="payoutFileIds" label="付款凭证（选填）" accept=".png,.jpg,.jpeg,.webp,.pdf" :disabled="saving" @busy="uploads.payout = $event" /></template><div class="panel-actions"><v-btn variant="text" :disabled="busy" @click="action = ''">取消</v-btn><v-btn color="primary" type="submit" :loading="saving" :disabled="busy && !saving">{{ action === 'pay' ? '确认已付款并入账' : '确认' }}</v-btn></div>
      </form>
      <details v-if="detail.revisions?.length" class="revision-section"><summary>历史申请版本（{{ detail.revisions.length }}）</summary><p class="muted">保留每次退回重交前的内容与凭证，用于核对修改过程。</p><article v-for="revision in detail.revisions" :key="revision.version" class="revision-card"><div class="revision-heading"><h3>版本 {{ revision.version }} · {{ revision.title }}</h3><strong>{{ money(revision.amountCents) }}</strong></div><p class="muted">{{ revision.category }} · 费用日期 {{ revision.incurredOn }} · 留存于 {{ timestamp(revision.savedAt) }}</p><p v-if="revision.notes" class="preserve-lines">{{ revision.notes }}</p><div class="receipt-columns"><section><h3>原发票</h3><p v-if="!revision.invoiceFileIds.length" class="muted">未提供发票</p><AttachmentList :ids="revision.invoiceFileIds" /></section><section><h3>原支付记录</h3><p v-if="!revision.paymentFileIds.length" class="muted">未提供支付记录</p><AttachmentList :ids="revision.paymentFileIds" /></section></div></article></details>
      <section class="history-section"><h3>操作记录</h3><ol class="history-list"><li v-for="(event,index) in detail.events" :key="index"><strong>{{ actionLabels[event.action] || event.action }}</strong><span>{{ event.actorName }} · {{ timestamp(event.at) }}</span><p v-if="event.note" class="preserve-lines">{{ event.note }}</p></li></ol><p class="muted text-caption">申请编号：{{ detail.id }} · 创建于 {{ timestamp(detail.createdAt) }}</p></section>
    </section>
    <section v-else-if="tab === 'ledger' && ledgerEditor" class="finance-panel editor-panel"><v-btn variant="text" prepend-icon="mdi-arrow-left" :disabled="busy" @click="ledgerEditor = false">返回账本</v-btn><h2>登记收支</h2><p class="muted">仅登记已经实际发生的收支。报销付款会自动入账，无需重复登记。</p><form @submit.prevent="saveEntry"><div class="form-grid"><v-select variant="outlined" v-model="entryForm.direction" :items="[{title:'转入 / 收入',value:'income'}, {title:'转出 / 支出',value:'expense'}]" label="收支方向 *" :disabled="busy" /><v-text-field variant="outlined" v-model="entryForm.amount" label="金额（元）*" inputmode="decimal" required :disabled="busy" /><v-text-field variant="outlined" v-model="entryForm.occurredOn" type="date" label="实际发生日期 *" min="2000-01-01" :max="today()" required :disabled="busy" /></div><div class="form-grid"><v-combobox variant="outlined" v-model="entryForm.category" :items="['期初余额','经费拨款','赞助','材料采购','加工制作','其他']" label="类别 *" :disabled="busy" /><v-text-field variant="outlined" v-model="entryForm.counterparty" label="对方名称 *" maxlength="160" required :disabled="busy" /></div><v-textarea variant="outlined" v-model="entryForm.note" label="备注 / 用途" rows="3" maxlength="4000" :disabled="busy" /><FileUpload v-model="entryForm.fileIds" label="收支凭证（选填）" accept=".png,.jpg,.jpeg,.webp,.pdf,.ofd,.zip" :disabled="saving" @busy="uploads.ledger = $event" /><div class="panel-actions"><v-btn :disabled="busy" variant="text" @click="ledgerEditor = false">取消</v-btn><v-btn type="submit" color="primary" :loading="saving" :disabled="busy && !saving">确认登记</v-btn></div></form></section>
    <section v-else-if="tab === 'ledger' && ledgerDetail" class="finance-panel detail-panel"><v-btn variant="text" prepend-icon="mdi-arrow-left" :disabled="busy" @click="ledgerDetail = null; reverseReason = ''">返回账本</v-btn><div class="detail-heading"><div><h2>{{ ledgerDetail.category }}</h2><p>{{ ledgerDetail.counterparty }} · {{ ledgerDetail.occurredOn }}</p></div><strong class="detail-amount" :class="{income:ledgerDetail.direction === 'income'}">{{ ledgerDetail.direction === 'income' ? '+' : '−' }}{{ money(ledgerDetail.amountCents) }}</strong></div><p class="preserve-lines">{{ ledgerDetail.note || '无备注' }}</p><AttachmentList :ids="ledgerDetail.fileIds" /><p class="muted">登记人：{{ ledgerDetail.createdByName }} · {{ timestamp(ledgerDetail.createdAt) }}</p><v-alert v-if="ledgerDetail.reversedAt" type="warning" variant="tonal">已冲销，不计入余额。原因：{{ ledgerDetail.reversalReason }}</v-alert><v-alert v-else-if="ledgerDetail.claimId" type="info" variant="tonal">此笔支出由报销付款自动生成，保留关联申请与付款记录。<v-btn class="ml-2" variant="text" color="primary" size="small" :loading="loading" @click="openLinkedClaim(ledgerDetail.claimId)">查看关联报销</v-btn></v-alert><form v-else class="action-panel" @submit.prevent="reverseEntry"><h3>冲销错误登记</h3><p class="muted">冲销后从余额计算中排除，原始记录仍会保留。实际资金并不会因此退回。</p><v-textarea variant="outlined" v-model="reverseReason" label="冲销原因 *" required rows="2" maxlength="2000" :disabled="busy" /><v-btn type="submit" color="warning" variant="outlined" :loading="saving">确认冲销</v-btn></form></section>
    <section v-else class="finance-panel list-panel">
      <div class="list-heading"><h2>{{ tab === 'ledger' ? '收支明细' : management ? '报销申请' : '我的报销' }}</h2><v-btn variant="text" prepend-icon="mdi-refresh" :disabled="loading" @click="load">刷新</v-btn></div><p v-if="tab === 'ledger'" class="muted">初始资金可通过“登记收支 → 转入”，以“期初余额”为类别录入。已冲销记录保留，但不计入余额。</p>
      <form class="filters" @submit.prevent="load"><v-text-field variant="outlined" v-model="filter.q" :label="tab === 'ledger' ? '搜索类别、对方或备注' : management ? '搜索申请人、事由或备注' : '搜索报销事由或备注'" prepend-inner-icon="mdi-magnify" density="compact" hide-details clearable @click:clear="filter.q = ''" /><v-select variant="outlined" v-if="tab === 'claims'" v-model="filter.status" :items="[{title:'全部状态',value:''},...statuses]" label="状态" density="compact" hide-details /><v-select variant="outlined" v-else v-model="filter.direction" :items="[{title:'全部收支',value:''},{title:'转入 / 收入',value:'income'},{title:'转出 / 支出',value:'expense'}]" label="收支方向" density="compact" hide-details /><v-text-field variant="outlined" v-model="filter.from" type="date" :label="tab === 'ledger' ? '收支日期起' : '费用发生日期起'" density="compact" hide-details /><v-text-field variant="outlined" v-model="filter.to" type="date" :label="tab === 'ledger' ? '收支日期止' : '费用发生日期止'" density="compact" hide-details /><v-btn type="submit" color="primary" variant="tonal" :loading="loading">筛选</v-btn></form>
      <div v-if="management" class="export-toolbar"><span>{{ tab === 'claims' && selected.length ? `已选 ${selected.length} 笔 · ${money(selectedTotal)}` : '导出当前筛选结果' }}</span><div><v-btn v-if="tab === 'claims' && selected.length" variant="text" size="small" @click="selected = []">清空选择</v-btn><v-btn variant="outlined" size="small" prepend-icon="mdi-table-arrow-down" :loading="exporting" :disabled="loading" @click="exportFile('csv', tab === 'ledger')">导出表格</v-btn><v-btn v-if="tab === 'claims'" variant="outlined" size="small" prepend-icon="mdi-folder-download-outline" :loading="exporting" :disabled="loading" @click="exportFile('zip')">打包凭证</v-btn></div></div>
      <div class="table-scroll"><table v-if="tab === 'claims'" class="finance-table"><thead><tr><th v-if="management" class="check-cell"><input type="checkbox" aria-label="选择全部当前申请" :checked="allSelected" @change="toggleAll" /></th><th>报销事由</th><th v-if="management">申请人</th><th>金额</th><th>费用日期</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in claims" :key="item.id"><td v-if="management" class="check-cell"><input v-model="selected" type="checkbox" :value="item.id" :aria-label="`选择${item.title}`" /></td><td><button class="title-link" @click="openDetail(item)">{{ item.title }}</button><small>{{ item.category }}</small></td><td v-if="management">{{ item.userName }}</td><td class="money-cell">{{ money(item.amountCents) }}</td><td>{{ item.incurredOn }}</td><td><v-chip :color="statusColor(item.status)" size="small" variant="tonal">{{ statusLabels[item.status] }}</v-chip></td><td><v-btn size="small" variant="text" color="primary" @click="openDetail(item)">{{ management && item.status === 'submitted' ? '审核' : management && item.status === 'approved' ? '付款' : '详情' }}</v-btn></td></tr><tr v-if="!claims.length"><td :colspan="management ? 7 : 5" class="empty-cell"><v-icon size="36" color="grey">mdi-receipt-text-outline</v-icon><p>{{ loading ? '正在加载…' : '暂无符合条件的报销申请' }}</p></td></tr></tbody></table>
      <table v-else class="finance-table"><thead><tr><th>收支日期</th><th>类别 / 对方</th><th>转入</th><th>转出</th><th>来源 / 状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in ledger" :key="item.id" :class="{reversed: item.reversedAt}"><td>{{ item.occurredOn }}</td><td><strong>{{ item.category }}</strong><small>{{ item.counterparty }}</small></td><td class="money-cell income">{{ item.direction === 'income' ? money(item.amountCents) : '—' }}</td><td class="money-cell">{{ item.direction === 'expense' ? money(item.amountCents) : '—' }}</td><td><v-chip size="small" :color="item.reversedAt ? 'grey' : item.claimId ? 'primary' : undefined" variant="tonal">{{ item.reversedAt ? '已冲销' : item.claimId ? '报销付款' : '手工登记' }}</v-chip></td><td><v-btn size="small" variant="text" color="primary" @click="ledgerDetail = item; reverseReason = ''">详情</v-btn></td></tr><tr v-if="!ledger.length"><td colspan="6" class="empty-cell"><v-icon size="36" color="grey">mdi-book-open-outline</v-icon><p>{{ loading ? '正在加载…' : '暂无符合条件的收支记录' }}</p></td></tr></tbody></table></div>
    </section>
  </div>
</template>

<style scoped>
.finance-page { max-width:1440px; margin:0 auto; min-width:0; }.finance-header { display:flex; align-items:center; justify-content:space-between; gap:20px; margin-bottom:26px; }.eyebrow { color:#567488; font-size:11px; letter-spacing:.16em; font-weight:700; margin-bottom:5px; }h1 { font-size:27px; letter-spacing:-.02em; }h2 { font-size:20px; margin:12px 0; }h3 { font-size:15px; margin-bottom:12px; }.finance-header p,.muted { color:#687786; font-size:14px; margin:7px 0 18px; }.finance-header p { margin-bottom:0; }
.finance-stats { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:14px; margin-bottom:24px; }.personal-stats { grid-template-columns:repeat(3,minmax(0,1fr)); }.finance-stats article { background:#fff; border:1px solid #e1e8ed; border-radius:12px; padding:20px; }.finance-stats span { display:block; color:#697784; font-size:13px; }.finance-stats strong { display:block; font-size:clamp(20px,2.1vw,29px); letter-spacing:-.04em; margin-top:10px; overflow-wrap:anywhere; font-variant-numeric:tabular-nums; }.finance-stats small { font-size:11px; color:#7c8993; display:block; margin-top:6px; }.income { color:#17816b; }.pending { color:#ac741e; }.finance-tabs { margin-bottom:18px; border-bottom:1px solid #e1e8ed; }
.finance-panel { background:#fff; border:1px solid #e1e8ed; border-radius:12px; padding:24px; }.editor-panel,.detail-panel { max-width:1080px; }.form-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:16px; }.receipt-columns { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,300px),1fr)); gap:24px; margin:12px 0; }.receipt-section { min-width:0; background:#f8fafb; border:1px solid #e8edf1; padding:16px; border-radius:10px; }.receipt-previews { display:flex; flex-wrap:wrap; gap:10px; }.receipt-previews img { width:140px; height:160px; object-fit:contain; background:white; border:1px solid #dce4ea; border-radius:6px; }.pdf-preview { display:inline-flex; align-items:center; gap:8px; color:#23618a; padding:12px; border:1px solid #dce4ea; border-radius:6px; }
.panel-actions { display:flex; justify-content:flex-end; gap:10px; flex-wrap:wrap; margin-top:18px; }.detail-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; margin:16px 0; }.detail-heading h2 { overflow-wrap:anywhere; }.detail-heading > div:last-child { display:flex; align-items:flex-end; flex-direction:column; gap:8px; }.detail-amount { font-size:28px; white-space:nowrap; font-variant-numeric:tabular-nums; }.preserve-lines { white-space:pre-wrap; overflow-wrap:anywhere; }.detail-notes { padding:16px; background:#f7f9fb; border-radius:8px; }.action-panel { padding:20px; border:1px solid #ccdae7; background:#f7fafc; border-radius:10px; margin-top:24px; }.action-panel > p { margin-bottom:18px; font-size:14px; }.history-section { margin-top:30px; border-top:1px solid #e6ebef; padding-top:24px; }.revision-section { margin-top:26px; border:1px solid #dfe7ed; border-radius:8px; padding:16px; }.revision-section summary { cursor:pointer; font-size:14px; font-weight:600; }.revision-section > p { margin-top:14px; }.revision-card { background:#f8fafb; padding:16px; border-radius:8px; margin-top:12px; }.revision-heading { display:flex; flex-wrap:wrap; justify-content:space-between; gap:12px; }.revision-heading h3 { margin:0; }.revision-heading strong { font-variant-numeric:tabular-nums; }.history-list { list-style:none; padding:0 0 0 16px; border-left:2px solid #e5ebf1; margin:20px 0; }.history-list li { position:relative; padding:0 0 20px 12px; font-size:13px; }.history-list li:before { content:''; position:absolute; left:-22px; top:5px; width:10px; height:10px; border-radius:50%; background:#5c809d; }.history-list span { display:block; color:#74828d; margin:4px 0; }.history-list p { margin-top:6px; }
.list-heading { display:flex; justify-content:space-between; align-items:center; margin-bottom:12px; }.filters { display:grid; grid-template-columns:minmax(220px,2fr) repeat(3,minmax(140px,1fr)) auto; align-items:center; gap:12px; }.export-toolbar { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:12px; margin:20px 0 12px; font-size:13px; color:#667783; }.export-toolbar > div { display:flex; flex-wrap:wrap; gap:8px; }.table-scroll { overflow-x:auto; max-width:100%; margin-top:20px; }.finance-table { width:100%; border-collapse:collapse; text-align:left; font-size:14px; min-width:640px; }.finance-table th { color:#6a7b89; background:#f5f8fa; font-size:12px; font-weight:600; white-space:nowrap; padding:14px 12px; }.finance-table td { padding:16px 12px; border-bottom:1px solid #edf1f4; vertical-align:middle; }.finance-table tbody tr:hover { background:#fbfcfd; }.finance-table td small { display:block; margin-top:5px; color:#7d8a96; font-size:12px; }.finance-table .check-cell { width:35px; }.check-cell input { width:16px; height:16px; accent-color:#246a9c; cursor:pointer; }.title-link { color:#255a7e; font-weight:600; text-align:left; max-width:330px; overflow-wrap:anywhere; }.title-link:hover { text-decoration:underline; }.money-cell { font-weight:600; white-space:nowrap; font-variant-numeric:tabular-nums; }.finance-table .empty-cell { text-align:center; padding:54px 20px; color:#7d8a96; }.empty-cell p { margin-top:10px; }.reversed td { color:#8a969f; }.reversed .money-cell { text-decoration:line-through; }
@media(max-width:1100px) { .filters { grid-template-columns:repeat(2,minmax(0,1fr)); }.filters > :first-child { grid-column:1 / -1; }.finance-stats { grid-template-columns:repeat(2,minmax(0,1fr)); }.personal-stats { grid-template-columns:repeat(3,minmax(0,1fr)); } }
@media(max-width:600px) { .finance-header { align-items:flex-start; flex-direction:column; margin-bottom:20px; }.finance-panel { padding:16px; }.finance-stats { gap:8px; }.finance-stats article { padding:14px; }.personal-stats { grid-template-columns:1fr; }.personal-stats article { display:flex; align-items:center; justify-content:space-between; gap:8px; }.personal-stats strong { margin:0; font-size:22px; }.personal-stats small { display:none; }.detail-heading { flex-direction:column; }.detail-heading > div:last-child { flex-direction:row; align-items:center; }.detail-amount { font-size:24px; }.filters { gap:10px; }.form-grid { grid-template-columns:1fr; }.panel-actions > .v-btn { flex:1; }.receipt-previews img { width:110px; height:130px; } }

/* Finance workspace visual refresh */
.finance-page { max-width:1540px; padding-top:28px; }
.finance-hero { position:relative; display:flex; align-items:center; justify-content:space-between; gap:24px; overflow:hidden; padding:30px 34px; margin-bottom:24px; border:1px solid #dbe7ef; border-radius:20px; background:linear-gradient(115deg,#f4f9fc 0%,#edf5fa 56%,#f7f9fd 100%); }
.finance-hero::after { content:""; position:absolute; width:240px; height:240px; right:13%; top:-155px; border-radius:50%; border:1px solid #c4d9e8; box-shadow:0 0 0 24px #ffffff26,0 0 0 50px #ffffff20; pointer-events:none; }
.hero-copy,.hero-actions { position:relative; z-index:1; }.hero-copy { min-width:0; }.eyebrow { display:flex; align-items:center; gap:7px; color:#42647b; font-size:11px; letter-spacing:.16em; font-weight:750; margin-bottom:9px; }
.finance-hero h1 { color:#152b3c; font-size:30px; font-weight:650; letter-spacing:-.035em; line-height:1.2; }.finance-hero p { color:#64798a; font-size:14px; margin:8px 0 0; }.hero-actions { display:flex; align-items:center; justify-content:flex-end; gap:16px; flex-wrap:wrap; }.hero-actions :deep(.v-btn) { min-height:46px; border-radius:11px; box-shadow:0 5px 14px #1769aa22; }.secure-label { display:inline-flex; align-items:center; gap:6px; color:#587185; font-size:12px; white-space:nowrap; }
.finance-stats { gap:14px; margin:0 0 26px; }.finance-stats .stat-card { min-width:0; position:relative; overflow:hidden; padding:19px 21px 17px; border:1px solid #e4ebf0; border-radius:16px; background:linear-gradient(150deg,#fff,#fbfcfd); box-shadow:0 5px 18px #243d5010; transition:transform .18s ease,box-shadow .18s ease; }.finance-stats .stat-card:hover { transform:translateY(-2px); box-shadow:0 9px 22px #243d5018; }.stat-label { display:flex; align-items:center; gap:8px; color:#657583; font-size:13px; font-weight:550; }.finance-stats .stat-card strong { margin-top:15px; font-size:clamp(21px,1.8vw,28px); font-weight:650; letter-spacing:-.045em; line-height:1.2; }.finance-stats .stat-card small { margin-top:9px; font-size:11px; }.finance-stats .balance-card { color:#fff; border-color:#173d53; background:linear-gradient(125deg,#18394d,#21596a); box-shadow:0 8px 20px #173d5324; }.balance-card .stat-label,.balance-card .stat-label span,.balance-card small { color:#d3e3ec; }.finance-stats .balance-card strong { color:#fff; }.finance-stats .pending-card { background:linear-gradient(150deg,#fffdf8,#fffaf0); border-color:#f0e5d1; }
.finance-stats .income { color:#16816e; }.finance-stats .pending { color:#ab741e; }.personal-stats { grid-template-columns:repeat(3,minmax(0,1fr)); }.personal-stats .stat-card { padding:17px 20px; }.personal-stats .stat-card strong { margin-top:13px; font-size:23px; }
.finance-tabs { display:flex; width:max-content; max-width:100%; gap:6px; padding:5px; margin:0 0 18px; border:1px solid #e0e8ee; border-radius:14px; background:#f7f9fb; }.finance-tabs :deep(.v-slide-group__container) { overflow:visible; }.finance-tabs :deep(.v-tab) { min-height:42px; border-radius:10px; font-weight:550; letter-spacing:0; }.finance-tabs :deep(.v-tab--selected) { color:#155f90; background:#fff; box-shadow:0 2px 8px #263c5018; }
.finance-panel { padding:28px; border-color:#e3eaf0; border-radius:18px; box-shadow:0 8px 24px #203a4c0c; }.editor-panel { max-width:none; }.detail-panel { max-width:1180px; }.editor-topline { display:flex; align-items:center; justify-content:space-between; gap:16px; padding-bottom:17px; margin-bottom:23px; border-bottom:1px solid #edf1f4; }.draft-badge { display:flex; align-items:center; gap:6px; color:#577388; font-size:12px; }.editor-layout { display:grid; grid-template-columns:minmax(0,1fr) 286px; gap:34px; align-items:start; }.editor-main { min-width:0; }.editor-heading { margin:1px 0 24px; }.editor-heading h2 { margin:6px 0 7px; color:#1b2d3b; font-size:23px; font-weight:620; letter-spacing:-.025em; }.editor-heading p { margin:0; color:#71808d; font-size:13px; line-height:1.7; }.editor-step { color:#176b9d; font-size:11px; font-weight:700; letter-spacing:.1em; }.claim-form { min-width:0; }.form-section { padding:0 0 18px; margin-bottom:18px; border-bottom:1px solid #edf1f4; }.form-section h3 { display:flex; align-items:center; gap:10px; margin:0 0 16px; color:#243746; font-size:15px; font-weight:620; }.form-section h3 span { display:grid; place-items:center; width:25px; height:25px; border-radius:8px; color:#23688f; background:#e9f3f9; font-size:11px; font-variant-numeric:tabular-nums; }.form-section .form-grid { grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px 16px; }.form-section .form-grid > :last-child:nth-child(odd) { grid-column:1/-1; }.form-section :deep(.v-field) { border-radius:10px; }.form-section :deep(.v-field--variant-outlined .v-field__outline) { color:#596f81; }.form-section :deep(.v-field--focused .v-field__outline) { color:#3b83ae; }
.section-heading { display:flex; align-items:center; justify-content:space-between; gap:14px; margin-bottom:13px; }.section-heading h3 { margin-bottom:4px; }.section-heading p { margin:0; color:#84919b; font-size:12px; }.receipts-form-section .receipt-columns { gap:14px; margin:0; }.receipts-form-section :deep(.file-upload) { min-height:140px; padding:17px; border:1px dashed #cad8e2; border-radius:12px; background:#f8fafc; }.receipts-form-section :deep(.upload-heading) { margin:0 0 10px; color:#405665; font-size:13px; }.panel-actions { margin-top:19px; }.application-aside { position:sticky; top:24px; display:grid; gap:14px; }.amount-preview { padding:22px; border-radius:16px; color:#fff; background:radial-gradient(circle at 95% 0%,#4e8aa044,transparent 42%),linear-gradient(135deg,#1c4156,#23677a); box-shadow:0 8px 20px #173d5320; }.amount-preview span,.amount-preview small { display:block; color:#d2e4ec; font-size:12px; }.amount-preview strong { display:block; overflow-wrap:anywhere; margin:13px 0 8px; font-size:29px; font-weight:650; letter-spacing:-.04em; font-variant-numeric:tabular-nums; }.aside-card { padding:19px 18px 13px; border:1px solid #e4ebf0; border-radius:15px; background:#fff; }.aside-card h3 { margin:0 0 16px; color:#2a3e4e; font-size:14px; font-weight:620; }.process-steps { list-style:none; padding:0; margin:0; }.process-steps li { position:relative; display:flex; gap:11px; min-height:56px; }.process-steps li:not(:last-child)::after { position:absolute; content:""; left:12px; top:27px; bottom:0; border-left:1px solid #dbe4ea; }.process-steps li > span { z-index:1; display:grid; flex:0 0 25px; place-items:center; width:25px; height:25px; border:1px solid #dce5ea; border-radius:50%; color:#788a98; background:#fff; font-size:11px; }.process-steps li.active > span { border-color:#4e96bd; color:#fff; background:#3780a8; }.process-steps li strong,.process-steps li small { display:block; }.process-steps li strong { margin:2px 0 3px; color:#344b5b; font-size:12px; font-weight:600; }.process-steps li small { color:#82909b; font-size:11px; line-height:1.45; }.privacy-note { display:flex; align-items:flex-start; gap:9px; padding:13px 14px; border:1px solid #e0ebf0; border-radius:12px; color:#47718a; background:#f4f9fb; }.privacy-note :deep(.v-icon) { flex:0 0 auto; margin-top:1px; }.privacy-note p { margin:0; color:#657c8b; font-size:11px; line-height:1.65; }
.list-panel { overflow:hidden; }.list-heading { min-height:40px; margin-bottom:14px; }.list-heading h2 { color:#253847; font-size:18px; font-weight:620; }.filters { padding:15px; border:1px solid #e8edf1; border-radius:14px; background:#f8fafb; }.filters :deep(.v-field) { border-radius:9px; background:#fff; }.export-toolbar { padding:2px 2px 0; }.table-scroll { margin-top:14px; border:1px solid #e9eef2; border-radius:12px; }.finance-table { border-collapse:separate; border-spacing:0; }.finance-table th { background:#f7f9fb; color:#637583; padding:13px 14px; }.finance-table td { padding:14px; }.finance-table tbody tr:last-child td { border-bottom:0; }.finance-table tbody tr:hover { background:#f8fbfd; }.finance-table .title-link { color:#205e83; }.finance-table .money-cell { color:#314858; }.finance-table .money-cell.income { color:#16816e; }
.detail-heading { padding:17px 20px; margin:20px 0; border:1px solid #e8edf1; border-radius:14px; background:#f8fafb; }.action-panel { border-radius:14px; border-color:#d9e6ed; background:#f5f9fb; }.history-section { border-top-color:#e7edf1; }.revision-section { border-color:#e4ebf0; border-radius:12px; }
@media(max-width:1180px) { .finance-page { max-width:100%; }.finance-stats { grid-template-columns:repeat(2,minmax(0,1fr)); }.personal-stats { grid-template-columns:repeat(3,minmax(0,1fr)); }.editor-layout { grid-template-columns:minmax(0,1fr) 260px; gap:20px; } }
@media(max-width:850px) { .finance-hero { padding:25px; }.editor-layout { grid-template-columns:1fr; }.application-aside { position:static; grid-template-columns:repeat(2,minmax(0,1fr)); }.amount-preview { grid-row:span 2; }.finance-stats .stat-card strong { font-size:clamp(20px,3vw,27px); } }
@media(max-width:600px) { .finance-page { padding-top:18px; }.finance-hero { align-items:flex-start; flex-direction:column; gap:18px; padding:21px 19px; margin-bottom:18px; border-radius:16px; }.finance-hero h1 { font-size:25px; }.finance-hero p { font-size:13px; line-height:1.6; }.hero-actions { width:100%; justify-content:stretch; }.hero-actions :deep(.v-btn) { width:100%; }.secure-label { display:none; }.finance-stats { gap:9px; margin-bottom:18px; }.finance-stats .stat-card { padding:14px; border-radius:13px; }.stat-label { gap:5px; font-size:11px; }.finance-stats .stat-card strong { margin-top:11px; font-size:clamp(17px,5vw,21px); }.finance-stats .stat-card small { font-size:10px; line-height:1.4; }.personal-stats { grid-template-columns:1fr; }.personal-stats .stat-card { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:0 8px; }.personal-stats .stat-card strong { grid-column:2; grid-row:1/3; align-self:center; font-size:20px; }.personal-stats .stat-card small { grid-column:1; }.finance-tabs { width:100%; }.finance-tabs :deep(.v-tab) { flex:1; min-width:0; padding:0 10px; font-size:12px; }.finance-panel { padding:17px; border-radius:15px; }.editor-topline { align-items:flex-start; flex-direction:column; gap:5px; }.editor-heading { margin-bottom:19px; }.editor-heading h2 { font-size:21px; }.form-section { padding-bottom:12px; margin-bottom:15px; }.form-section .form-grid { grid-template-columns:1fr; gap:0; }.form-section .form-grid > :last-child:nth-child(odd) { grid-column:auto; }.receipts-form-section .receipt-columns { grid-template-columns:1fr; gap:10px; }.receipts-form-section :deep(.file-upload) { min-height:120px; }.section-heading { align-items:flex-start; }.application-aside { grid-template-columns:1fr; gap:10px; }.amount-preview { grid-row:auto; }.amount-preview strong { font-size:26px; }.aside-card { padding:17px; }.filters { grid-template-columns:1fr; padding:12px; }.filters > :first-child { grid-column:auto; }.export-toolbar { align-items:flex-start; flex-direction:column; }.export-toolbar > div { width:100%; }.export-toolbar :deep(.v-btn) { flex:1; }.list-heading { align-items:flex-start; }.finance-table { min-width:690px; } }
@media(min-width:1250px) { .form-section .form-grid { grid-template-columns:repeat(3,minmax(0,1fr)); }.form-section .form-grid > :last-child:nth-child(odd) { grid-column:auto; } }
</style>
