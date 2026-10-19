<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import { api, ApiError } from '../lib/api';
type Provider = { id: string; name: string; type: 'openai'; baseUrl: string; enabled: boolean; apiKeySet?: boolean; apiKey?: string; clearApiKey?: boolean };
type Model = { id: string; providerId: string; modelId: string; name: string; enabled: boolean; vision: boolean };
type Application = { id: string; name: string; description: string; enabled: boolean; modelId: string; inputMode: 'text' | 'vision'; systemPrompt: string; builtin: boolean };
type Preset = { id: string; name: string; baseUrl: string; type: 'openai' };
type Config = { revision: number; providers: Provider[]; models: Model[]; applications: Application[]; presets: Preset[] };
const config = ref<Config>({ revision: 0, providers: [], models: [], applications: [], presets: [] });
const baseline = ref('');
const loading = ref(true), saving = ref(false), error = ref(''), success = ref('');
const tab = ref('providers'), selectedProvider = ref(''), presetId = ref('');
const discovering = ref(false), testing = ref(''), discoveryError = ref('');
const discovered = ref<{ id: string; name: string; vision: boolean }[]>([]), selectedDiscovered = ref<string[]>([]), search = ref('');
const testResults = ref<Record<string, { ok: boolean; message: string }>>({});
const dirty = computed(() => Boolean(baseline.value) && snapshot() !== baseline.value);
const busy = computed(() => saving.value || discovering.value || Boolean(testing.value));
const provider = computed<Provider>(() => config.value.providers.find(p => p.id === selectedProvider.value) || { id: '', name: '', type: 'openai', baseUrl: '', enabled: false });
const providerModels = computed(() => config.value.models.filter(m => m.providerId === selectedProvider.value));
const filteredDiscovered = computed(() => discovered.value.filter(m => `${m.name} ${m.id}`.toLowerCase().includes(search.value.toLowerCase())));
const enabledModelIds = computed(() => config.value.models.filter(m => m.enabled && config.value.providers.some(p => p.id === m.providerId && p.enabled)).map(m => m.id));
const modelOptions = computed(() => config.value.models.map(m => ({ title: `${config.value.providers.find(p => p.id === m.providerId)?.name} / ${m.name || m.modelId}${enabledModelIds.value.includes(m.id) ? '' : '（已停用）'}`, value: m.id, props: { disabled: !enabledModelIds.value.includes(m.id) } })));
function snapshot() { return JSON.stringify({ revision: config.value.revision, providers: config.value.providers, models: config.value.models, applications: config.value.applications }); }
function uid(prefix: string) { return `${prefix}-${Array.from(crypto.getRandomValues(new Uint8Array(12)), n => n.toString(16).padStart(2, '0')).join('')}`; }
function apply(data: Config) {
  config.value = { ...data, providers: data.providers.map(p => ({ ...p, apiKey: '', clearApiKey: false })) };
  if (!data.providers.some(p => p.id === selectedProvider.value)) selectedProvider.value = data.providers[0]?.id || '';
  baseline.value = snapshot();
}
async function load() {
  if (dirty.value && !window.confirm('重新加载将丢弃尚未保存的修改，是否继续？')) return;
  loading.value = true; error.value = ''; success.value = '';
  try { apply(await api.get<Config>('/settings/ai')); resetDiscovery(); testResults.value = {}; }
  catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function validate() {
  for (const p of config.value.providers) {
    if (!p.name.trim()) return '请填写每个服务商的名称。';
    if (!p.baseUrl.trim() && !p.enabled) continue;
    if (!p.baseUrl.trim()) return '启用服务商前请填写 API Base URL。';
    try { if (new URL(p.baseUrl).protocol !== 'https:') return 'API Base URL 必须使用 HTTPS。'; } catch { return '请输入完整有效的 API Base URL。'; }
  }
  for (const m of config.value.models) if (!m.modelId.trim() || !m.name.trim()) return '请填写每个模型的名称和模型 ID。';
  for (const a of config.value.applications) {
    if (!a.name.trim()) return '请填写应用名称。';
    if (a.enabled && !config.value.models.some(m => m.id === a.modelId)) return `请在「应用绑定」中为「${a.name}」选择模型，或先关闭该应用。`;
    if (a.enabled && a.inputMode === 'vision' && !config.value.models.find(m => m.id === a.modelId)?.vision) return `「${a.name}」使用图片识别，请选择支持图片的模型。`;
  }
  return '';
}
async function save() {
  error.value = validate(); success.value = ''; if (error.value) return;
  saving.value = true;
  try {
    const { presets: _presets, ...payload } = config.value;
    apply(await api.put<Config>('/settings/ai', payload));
    success.value = 'AI 配置已保存'; testResults.value = {}; resetDiscovery();
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 409 ? '其他管理员已更新配置。你的修改仍保留，请先复制需要保留的内容，再重新加载最新配置。' : (e as Error).message;
  } finally { saving.value = false; }
}
function resetDiscovery() { discovered.value = []; selectedDiscovered.value = []; search.value = ''; discoveryError.value = ''; }
function selectProvider(id: string) { selectedProvider.value = id; resetDiscovery(); }
function addProvider() {
  const preset = config.value.presets.find(p => p.id === presetId.value);
  const p: Provider = { id: uid('provider'), name: preset?.name || '新服务商', type: 'openai', baseUrl: preset?.baseUrl || '', enabled: true, apiKey: '', apiKeySet: false, clearApiKey: false };
  config.value.providers.push(p); selectProvider(p.id); success.value = ''; tab.value = 'providers';
}
function removeProvider(p: Provider) {
  if (config.value.models.some(m => m.providerId === p.id)) { error.value = '请先移除该服务商下的模型；已绑定应用的模型需要先解除绑定。'; return; }
  config.value.providers = config.value.providers.filter(item => item.id !== p.id);
  selectProvider(config.value.providers[0]?.id || '');
}
function addModel(upstream?: { id: string; name: string; vision: boolean }) {
  if (!provider.value) return;
  if (upstream && providerModels.value.some(m => m.modelId === upstream.id)) return;
  config.value.models.push({ id: uid('model'), providerId: provider.value.id, modelId: upstream?.id || '', name: upstream?.name || '', enabled: true, vision: upstream?.vision || false });
}
function removeModel(m: Model) {
  const app = config.value.applications.find(a => a.modelId === m.id);
  if (app) { error.value = `「${app.name}」正在使用此模型，请先在应用绑定中更换或清空模型。`; return; }
  config.value.models = config.value.models.filter(item => item.id !== m.id);
}
async function discover() {
  if (!provider.value.id || busy.value) return;
  const current = provider.value;
  error.value = ''; success.value = '';
  discovering.value = true; resetDiscovery();
  try { discovered.value = (await api.post<{ items: { id: string; name: string; vision: boolean }[] }>(`/settings/ai/providers/${encodeURIComponent(current.id)}/models`, { revision: config.value.revision, provider: { ...current } })).items; if (!discovered.value.length) discoveryError.value = '服务商未返回模型，可手动添加模型 ID。'; }
  catch (e) { discoveryError.value = (e as Error).message; }
  finally { discovering.value = false; }
}
function importSelected() { for (const item of discovered.value.filter(m => selectedDiscovered.value.includes(m.id))) addModel(item); resetDiscovery(); }
async function test(m: Model) {
  if (dirty.value || busy.value) return;
  testing.value = m.id;
  try { const result = await api.post<{ ok: boolean; message: string; latencyMs: number }>(`/settings/ai/models/${encodeURIComponent(m.id)}/test`); testResults.value[m.id] = { ok: result.ok, message: `${result.message}（${result.latencyMs} ms）` }; }
  catch (e) { testResults.value[m.id] = { ok: false, message: (e as Error).message }; }
  finally { testing.value = ''; }
}
function addApplication() { config.value.applications.push({ id: uid('app'), name: '新 AI 应用', description: '', enabled: false, modelId: '', inputMode: 'text', systemPrompt: '', builtin: false }); }
function removeApplication(a: Application) { if (!a.builtin) config.value.applications = config.value.applications.filter(item => item.id !== a.id); }
function warnExit(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = ''; } }
onBeforeRouteLeave(() => !dirty.value || window.confirm('AI 配置尚未保存，确定离开？'));
onMounted(() => { void load(); window.addEventListener('beforeunload', warnExit); });
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnExit));
</script>

<template>
  <div class="page-content ai-settings">
    <div class="page-heading">
<div>
<v-btn to="/admin" variant="text" size="small" prepend-icon="mdi-arrow-left" class="back-link">系统管理</v-btn>
<h1 class="page-title">AI 配置</h1>
<p class="page-subtitle">统一管理服务商和模型，再为发票识别等应用选择合适的模型。</p>
</div>
<div class="heading-actions">
<v-chip v-if="dirty" color="warning" variant="tonal" size="small">未保存</v-chip>
<v-btn variant="text" prepend-icon="mdi-refresh" :disabled="busy" :loading="loading" @click="load">重新加载</v-btn>
<v-btn color="primary" prepend-icon="mdi-content-save-outline" :loading="saving" :disabled="loading || busy || !dirty" @click="save">保存配置</v-btn>
</div>
</div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>
<v-alert v-if="success && !dirty" type="success" variant="tonal" class="mb-4" closable @click:close="success = ''">{{ success }}</v-alert>
<v-progress-linear v-if="loading" indeterminate color="primary" />
    <template v-else-if="baseline">
      <v-tabs v-model="tab" color="primary" class="mb-5">
<v-tab value="providers" prepend-icon="mdi-server-network">服务商与模型</v-tab>
<v-tab value="applications" prepend-icon="mdi-view-grid-outline">应用绑定</v-tab>
</v-tabs>
      <div v-if="tab === 'providers'" class="provider-layout">
        <v-card class="provider-sidebar" variant="outlined">
<div class="section-title">
<h2>模型服务商</h2>
<v-chip size="x-small" variant="tonal">{{ config.providers.length }}</v-chip>
</div>
<v-list nav aria-label="模型服务商">
<v-list-item v-for="p in config.providers" :key="p.id" :disabled="busy" :active="selectedProvider === p.id" color="primary" :title="p.name || '未命名服务商'" :subtitle="p.enabled ? '已启用' : '已停用'" prepend-icon="mdi-api" @click="selectProvider(p.id)" />
</v-list>
<div class="provider-add">
<v-select v-model="presetId" :items="[{ title: '自定义兼容服务', value: '' }, ...config.presets.map(p => ({ title: p.name, value: p.id }))]" label="选择服务商预设" density="compact" variant="outlined" hide-details/>
<v-btn block variant="tonal" color="primary" prepend-icon="mdi-plus" :disabled="busy" @click="addProvider">添加服务商</v-btn>
</div>
</v-card>
        <div v-if="provider.id" :key="provider.id" class="provider-content">
          <v-card variant="outlined" class="config-card">
<div class="section-title">
<div>
<h2>{{ provider.name || '新服务商' }}</h2>
<p>OpenAI 兼容接口 · 密钥仅保存在服务端</p>
</div>
<v-switch v-model="provider.enabled" label="启用" color="primary" hide-details density="compact" :disabled="busy" />
</div>
<fieldset :disabled="busy">
<div class="field-grid">
<v-text-field v-model="provider.name" label="服务商名称" variant="outlined" density="comfortable" maxlength="80"/>
<v-text-field v-model="provider.baseUrl" label="API Base URL" placeholder="https://api.example.com/v1" variant="outlined" density="comfortable" hint="填写基础地址，不包含 /chat/completions 或 /models" persistent-hint/>
<v-text-field v-model="provider.apiKey" label="API Key" type="password" autocomplete="new-password" variant="outlined" density="comfortable" :disabled="provider.clearApiKey" :placeholder="provider.apiKeySet ? '已设置；留空保留原密钥' : '填写 API Key'" :hint="provider.apiKeySet ? '已有密钥不会回显；输入新密钥可替换。' : '填写后即可获取模型列表，无需先保存。'" persistent-hint/>
<v-checkbox v-if="provider.apiKeySet" v-model="provider.clearApiKey" label="清除已保存的密钥" color="error" hide-details />
</div>
</fieldset>
<div class="card-footer">
<span class="muted">服务商 ID：{{ provider.id }}</span>
<v-btn variant="text" color="error" size="small" :disabled="busy" @click="removeProvider(provider)">移除服务商</v-btn>
</div>
</v-card>
          <v-card variant="outlined" class="config-card">
<div class="section-title">
<div>
<h2>已配置的模型</h2>
<p>添加需要使用的模型，并确认它是否支持图片输入。</p>
</div>
<div class="compact-actions">
<v-btn variant="text" color="primary" prepend-icon="mdi-cloud-download-outline" :loading="discovering" :disabled="busy || !provider.enabled || !provider.baseUrl.trim() || provider.clearApiKey || !(provider.apiKey?.trim() || provider.apiKeySet)" @click="discover">获取模型列表</v-btn>
<v-btn variant="tonal" color="primary" prepend-icon="mdi-plus" :disabled="busy" @click="addModel()">手动添加</v-btn>
</div>
</div>
<p class="save-hint">填写地址和密钥即可获取模型列表；选好模型并绑定应用后，再保存配置。测试连接需要先保存。</p>
<v-alert v-if="discoveryError" type="info" variant="tonal" class="mb-4">{{ discoveryError }}</v-alert>
            <div v-if="discovered.length" class="discovery-panel">
<div class="section-title">
<h3>选择要添加的模型</h3>
<v-btn size="small" variant="text" @click="resetDiscovery">收起</v-btn>
</div>
<v-text-field v-model="search" label="搜索模型" prepend-inner-icon="mdi-magnify" variant="outlined" density="compact" hide-details/>
<div class="discovery-list">
<v-checkbox v-for="m in filteredDiscovered" :key="m.id" v-model="selectedDiscovered" :value="m.id" :label="m.name === m.id ? m.id : `${m.name} · ${m.id}`" :disabled="providerModels.some(item => item.modelId === m.id)" color="primary" density="compact" hide-details />
</div>
<v-btn color="primary" variant="tonal" :disabled="!selectedDiscovered.length" @click="importSelected">添加选中的 {{ selectedDiscovered.length }} 个模型</v-btn>
</div>
            <p v-if="!providerModels.length" class="empty-state">尚未添加模型。点击「获取模型列表」选择添加，或手动填写模型 ID。</p>
            <div v-for="m in providerModels" :key="m.id" class="model-row">
<fieldset :disabled="busy">
<div class="field-grid">
<v-text-field v-model="m.name" label="显示名称" placeholder="例如：发票识别模型" variant="outlined" density="compact" hide-details/>
<v-text-field v-model="m.modelId" label="模型 ID" placeholder="服务商提供的准确模型 ID" variant="outlined" density="compact" hide-details/>
</div>
<div class="model-actions">
<v-switch v-model="m.enabled" label="启用模型" color="primary" density="compact" hide-details/>
<v-checkbox v-model="m.vision" label="支持图片输入" color="primary" density="compact" hide-details/>
<v-spacer/>
<v-btn size="small" variant="text" prepend-icon="mdi-connection" :loading="testing === m.id" :disabled="dirty || busy || !provider.enabled || !provider.apiKeySet || !m.enabled" @click="test(m)">测试连接</v-btn>
<v-btn size="small" variant="text" color="error" @click="removeModel(m)">移除</v-btn>
</div>
</fieldset>
<v-alert v-if="testResults[m.id]" :type="testResults[m.id].ok ? 'success' : 'error'" variant="tonal" density="compact">{{ testResults[m.id].message }}</v-alert>
</div>
<p class="muted">连接测试只发送简短文本，可能产生少量费用；不验证图片识别能力。图片能力请以服务商说明为准。</p>
          </v-card>
        </div>
        <v-card v-if="!provider.id" variant="outlined" class="empty-state">
<v-icon size="42" color="primary">mdi-server-network-outline</v-icon>
<h2>添加第一个模型服务商</h2>
<p>选择预设或自定义兼容接口，填写地址与密钥，然后添加模型。</p>
</v-card>
      </div>
      <div v-else class="applications-panel">
<div class="section-title">
<div>
<h2>按应用选择模型</h2>
<p>多个应用可以复用同一个模型；更换模型无需重复填写密钥。</p>
</div>
<v-btn variant="tonal" color="primary" prepend-icon="mdi-plus" :disabled="busy" @click="addApplication">添加应用配置</v-btn>
</div>
<v-alert v-if="!enabledModelIds.length" type="info" variant="tonal" class="mb-4">先在「服务商与模型」中添加并启用模型，再回来绑定应用。</v-alert>
<v-card v-for="a in config.applications" :key="a.id" variant="outlined" class="config-card mb-4">
<div class="section-title">
<div>
<h2>{{ a.name || '新 AI 应用' }} <v-chip size="x-small" :color="a.builtin ? 'primary' : 'grey'" variant="tonal">{{ a.builtin ? '已接入' : '待业务接入' }}</v-chip>
</h2>
<p>{{ a.builtin ? a.description : '保存可复用的应用配置；业务功能接入后才会实际调用模型。' }}</p>
</div>
<v-switch v-model="a.enabled" label="启用" color="primary" density="compact" hide-details :disabled="busy"/>
</div>
<fieldset :disabled="busy">
<div class="field-grid">
<v-text-field v-if="!a.builtin" v-model="a.name" label="应用名称" density="comfortable" variant="outlined"/>
<v-text-field v-if="!a.builtin" v-model="a.description" label="用途说明" density="comfortable" variant="outlined"/>
<v-select v-model="a.modelId" :items="modelOptions" label="使用模型" density="comfortable" variant="outlined" clearable @update:model-value="a.modelId = $event || ''" :hint="!a.modelId ? '选择此应用使用的模型' : !enabledModelIds.includes(a.modelId) ? '停用的模型仍保留绑定；应用暂不可调用' : '应用调用时使用此模型；服务商密钥统一管理'" persistent-hint/>
<v-select v-model="a.inputMode" :items="[{ title: a.builtin ? '图片识别（图片 / PDF 渲染）' : '图片与文本', value: 'vision' }, { title: a.builtin ? '文本处理（PDF 提取文字）' : '文本', value: 'text' }]" label="输入方式" density="comfortable" variant="outlined" :hint="a.inputMode === 'vision' ? '需使用支持图片输入的模型' : a.builtin ? '发票识别仅支持可提取文字的 PDF' : '由业务模块提供文本内容'" persistent-hint/>
</div>
<p v-if="a.builtin" class="muted">采用内置识别规则，固定输出格式并校验金额；切换模型不会改变财务审核流程。</p>
<v-textarea v-else v-model="a.systemPrompt" label="补充指令（可选）" variant="outlined" rows="3" auto-grow hint="填写该应用的角色、任务与回答要求，供业务模块接入时使用。" persistent-hint/>
</fieldset>
<div class="card-footer">
<span class="muted">应用 ID：{{ a.id }}</span>
<v-btn v-if="!a.builtin" color="error" variant="text" size="small" :disabled="busy" @click="removeApplication(a)">移除应用</v-btn>
</div>
</v-card>
</div>
    </template>
  </div>
</template>

<style scoped>
.ai-settings{max-width:1500px}.page-heading,.heading-actions,.section-title,.compact-actions,.card-footer,.model-actions{display:flex;align-items:center;gap:12px}.page-heading{justify-content:space-between;align-items:flex-start;margin-bottom:20px}.heading-actions{flex-wrap:wrap;justify-content:flex-end}.back-link{margin-left:-12px;margin-bottom:6px}.provider-layout{display:grid;grid-template-columns:260px minmax(0,1fr);gap:20px;align-items:start}.provider-sidebar,.config-card{border-color:rgba(var(--v-border-color),var(--v-border-opacity))}.provider-sidebar{min-width:0}.provider-sidebar>.section-title{padding:18px 16px 0}.section-title{justify-content:space-between;margin-bottom:20px;align-items:flex-start}.section-title h2{font-size:18px;font-weight:500}.section-title h3{font-size:15px;font-weight:500}.section-title p,.muted,.save-hint{font-size:13px;color:rgba(var(--v-theme-on-surface),.65);line-height:1.7}.section-title p{margin:6px 0 0}.section-title .v-switch{flex:0 0 auto}.provider-add{display:grid;gap:12px;padding:16px}.provider-content{min-width:0;display:grid;gap:20px}.config-card{padding:24px}.field-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px}.card-footer{justify-content:space-between;margin-top:16px;flex-wrap:wrap}.card-footer .muted{overflow-wrap:anywhere}.compact-actions{flex-wrap:wrap;justify-content:flex-end}.model-row{padding:20px 0;border-top:1px solid rgba(var(--v-border-color),var(--v-border-opacity))}.model-actions{flex-wrap:wrap;margin-top:8px}.model-actions .v-input{flex:0 0 auto}.empty-state{text-align:center;padding:48px 24px;color:rgba(var(--v-theme-on-surface),.65);line-height:1.9}.empty-state h2{font-size:19px;font-weight:500;margin:14px 0 8px}.discovery-panel{background:rgba(var(--v-theme-primary),.03);padding:18px;margin-bottom:20px;border:1px solid rgba(var(--v-border-color),var(--v-border-opacity));border-radius:4px}.discovery-list{max-height:300px;overflow:auto;margin:10px 0}.save-hint{margin:-8px 0 16px;color:rgb(var(--v-theme-primary))}.applications-panel{max-width:1050px}fieldset{border:0;padding:0;margin:0;min-width:0}.v-card{border-radius:4px}.v-input{min-width:0}@media(max-width:1050px){.page-heading{flex-direction:column}.provider-layout{grid-template-columns:220px minmax(0,1fr)}.field-grid{grid-template-columns:1fr}.section-title{flex-wrap:wrap}.compact-actions{justify-content:flex-start}}@media(max-width:700px){.provider-layout{grid-template-columns:1fr}.config-card{padding:18px}.heading-actions{justify-content:flex-start}.provider-sidebar :deep(.v-list){max-height:220px;overflow:auto}.model-actions{gap:4px}.model-actions .v-spacer{display:none}}
</style>
