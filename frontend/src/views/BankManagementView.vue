<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { api } from "../lib/api";
import { groupLabel } from "../lib/permissions";

type Visibility = "public" | "internal";
type Catalog = { id: string; name: string; count: number };
type Settings = { repository: string; branch: string; catalogs: string[]; defaultVisibility: Visibility };
type Question = { id: string; title: string; direction: string; bankVisibility: Visibility; sourceKey?: string; catalogs?: string[]; tags?: string[] };
type Preview = { commit: string; catalogs: Catalog[]; items: { key: string; title: string; direction: string; catalogs: string[]; action: "add" | "unchanged" | "update" | "conflict"; reason?: string }[]; counts: { add: number; unchanged: number; update: number; conflict: number } };
type SyncResult = { commit: string; at: string; imported: number; updated: number; unchanged: number; conflicts: number };
type BankState = { settings: Settings; catalogs: Catalog[]; questions: Question[]; lastSync?: SyncResult | null };
const settings = reactive<Settings>({ repository: "goldenfishs/MR_question_bank", branch: "main", catalogs: ["c", "cpp", "python", "stm32"], defaultVisibility: "internal" });
const catalogs = ref<Catalog[]>([]);
const questions = ref<Question[]>([]);
const lastSync = ref<BankState["lastSync"]>(null);
const savedSettings = ref("");
const previewSettings = ref("");
const preview = ref<Preview | null>(null);
const busy = ref("");
const error = ref("");
const notice = ref("");
const loaded = ref(false);
const selected = ref<string[]>([]);
const search = ref("");
const direction = ref("");
const catalog = ref("");
const visibility = ref("");
const source = ref("");
const syncTime = computed(() => lastSync.value?.at ? new Date(lastSync.value.at).toLocaleString("zh-CN") : "");
const page = ref(1);
const previewPage = ref(1);
const previewFilter = ref("all");
const settingsKey = () => JSON.stringify(settings);
const settingsDirty = computed(() => savedSettings.value !== settingsKey());
const previewValid = computed(() => !!preview.value && previewSettings.value === settingsKey());
const catalogChoices = computed(() => catalogs.value.length ? catalogs.value : [
  { id: "c", name: "C 语言", count: 0 }, { id: "cpp", name: "C++", count: 0 },
  { id: "python", name: "Python", count: 0 }, { id: "stm32", name: "STM32 单片机", count: 0 },
]);
const filtered = computed(() => questions.value.filter(q =>
  (!search.value || [q.title, q.sourceKey, ...(q.tags || [])].join(" ").toLowerCase().includes(search.value.trim().toLowerCase())) &&
  (!direction.value || q.direction === direction.value) && (!catalog.value || q.catalogs?.includes(catalog.value)) &&
  (!visibility.value || q.bankVisibility === visibility.value) && (!source.value || (source.value === "repository" ? !!q.sourceKey : !q.sourceKey)),
));
const directions = computed(() => [...new Set(questions.value.map(q => q.direction))].filter(Boolean).map(value => ({ value, title: groupLabel(value) })));
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / 25)));
const rows = computed(() => filtered.value.slice((page.value - 1) * 25, page.value * 25));
const allFilteredSelected = computed(() => filtered.value.length > 0 && filtered.value.every(q => selected.value.includes(q.id)));
const publicCount = computed(() => questions.value.filter(q => q.bankVisibility === "public").length);
const previewItems = computed(() => (preview.value?.items || []).filter(q => previewFilter.value === "all" || q.action === previewFilter.value));
const previewRows = computed(() => previewItems.value.slice((previewPage.value - 1) * 20, previewPage.value * 20));
const actionLabels = { add: "新增", unchanged: "已是最新", update: "更新", conflict: "暂不覆盖" };
watch([search, direction, catalog, visibility, source], () => { page.value = 1; selected.value = []; });
watch(previewFilter, () => previewPage.value = 1);
watch(pages, count => { if (page.value > count) page.value = count; });
function message(e: unknown) { return e instanceof Error ? e.message : "操作失败，请稍后重试。"; }
async function load(resetSettings = false) {
  const state = await api.get<BankState>("/bank-sync");
  questions.value = state.questions || [];
  catalogs.value = state.catalogs || [];
  lastSync.value = state.lastSync;
  if (resetSettings || !loaded.value) {
    Object.assign(settings, state.settings);
    savedSettings.value = settingsKey();
  }
  loaded.value = true;
}
async function refresh() {
  busy.value = "load"; error.value = "";
  try { await load(); } catch (e) { error.value = message(e); } finally { busy.value = ""; }
}
async function saveSettings() {
  await api.put("/bank-sync/settings", { ...settings });
  savedSettings.value = settingsKey();
}
async function checkUpdates() {
  if (!settings.catalogs.length || busy.value) return;
  busy.value = "preview"; error.value = ""; notice.value = ""; preview.value = null;
  try {
    await saveSettings();
    preview.value = await api.post<Preview>("/bank-sync/preview");
    previewSettings.value = settingsKey();
    if (preview.value.catalogs?.length) catalogs.value = preview.value.catalogs;
    previewPage.value = 1;
    notice.value = "已检查仓库。确认下面的变更后，点击“同步到网站”。";
  } catch (e) { error.value = message(e); } finally { busy.value = ""; }
}
async function applySync() {
  if (!previewValid.value || busy.value) return;
  busy.value = "apply"; error.value = ""; notice.value = "";
  try {
    const result = await api.post<SyncResult>("/bank-sync/apply", { commit: preview.value!.commit });
    preview.value = null;
    notice.value = `同步完成：新增 ${result.imported} 题，更新 ${result.updated} 题，已是最新 ${result.unchanged} 题${result.conflicts ? `，${result.conflicts} 题因冲突保留网站版本` : ""}。可在下方调整公开范围。`;
    await load();
  } catch (e) { error.value = message(e); } finally { busy.value = ""; }
}
function toggleAll() { selected.value = allFilteredSelected.value ? [] : filtered.value.map(q => q.id); }
function toggle(id: string) { selected.value = selected.value.includes(id) ? selected.value.filter(value => value !== id) : [...selected.value, id]; }
async function setVisibility(value: Visibility, ids = selected.value) {
  if (!ids.length || busy.value) return;
  busy.value = "visibility"; error.value = ""; notice.value = "";
  try {
    await api.post("/bank-sync/visibility", { ids, visibility: value });
    notice.value = `已将 ${ids.length} 道题目设为${value === "public" ? "公开可刷" : "隐藏（内部题库）"}。`;
    selected.value = []; await load();
  } catch (e) { error.value = message(e); } finally { busy.value = ""; }
}
function learningLink(q?: Question) {
  return { path: "/learning", query: { section: "bank", direction: q?.direction || "视觉算法", ...(q ? { edit: q.id } : { create: "problem", visibility: "internal" }), from: "bank-management" } };
}
function catalogName(id: string) { return catalogChoices.value.find(c => c.id === id)?.name || id; }
onMounted(refresh);
</script>

<template>
  <div class="bank-management">
    <header class="page-heading">
      <div><div class="eyebrow">题库管理</div><h1>让题库随课程成长</h1><p>从共同维护的仓库同步题目，也可以自己出题。公开范围由你决定。</p></div>
      <v-btn color="primary" prepend-icon="mdi-plus" :to="learningLink()">新增题目</v-btn>
    </header>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}<v-btn v-if="!loaded" variant="text" :disabled="!!busy" @click="refresh">重试</v-btn></v-alert>
    <v-alert v-if="notice" type="success" variant="tonal" class="mb-4" closable @click:close="notice = ''">{{ notice }}</v-alert>
    <v-progress-linear v-if="busy === 'load'" indeterminate color="primary" />
    <template v-if="loaded">
      <section class="summary-grid" aria-label="题库概况">
        <div><span>网站题目</span><strong>{{ questions.length }}</strong></div>
        <div><span>公开可刷</span><strong>{{ publicCount }}</strong></div>
        <div><span>隐藏 / 内部</span><strong>{{ questions.length - publicCount }}</strong></div>
        <div><span>最近同步版本</span><strong class="commit">{{ lastSync?.commit?.slice(0, 8) || '尚未同步' }}</strong><small v-if="syncTime">{{ syncTime }}</small></div>
      </section>
      <section class="panel sync-panel">
        <div class="section-heading"><div><h2><v-icon size="23" color="primary">mdi-source-repository</v-icon> 从 GitHub 同步</h2><p>选择题库 → 检查更新 → 同步到网站</p></div><v-chip size="small" variant="tonal" color="primary">只读拉取</v-chip></div>
        <v-row class="mt-2"><v-col cols="12" md="7"><v-text-field v-model="settings.repository" label="题库仓库（所有者/仓库名）" placeholder="goldenfishs/MR_question_bank" variant="outlined" density="comfortable" :disabled="!!busy" hide-details /></v-col><v-col cols="12" md="5"><v-text-field v-model="settings.branch" label="分支" variant="outlined" density="comfortable" :disabled="!!busy" hide-details /></v-col></v-row>
        <p class="field-label">要同步的题库 <span>共享题目会自动去重</span></p>
        <div class="catalog-grid"><label v-for="item in catalogChoices" :key="item.id" class="catalog-choice" :class="{ checked: settings.catalogs.includes(item.id) }"><input v-model="settings.catalogs" type="checkbox" :value="item.id" :disabled="!!busy" /><span>{{ item.name }}</span><small v-if="item.count">{{ item.count }} 题</small></label></div>
        <div class="sync-settings"><v-select v-model="settings.defaultVisibility" :items="[{ title: '先隐藏，审核后再公开（推荐）', value: 'internal' }, { title: '同步后直接公开可刷', value: 'public' }]" label="新导入题目的默认状态" variant="outlined" density="comfortable" hide-details :disabled="!!busy" /><p>此设置仅用于新题。已有题目的公开状态和历史作答会保留。</p></div>
        <div class="action-line"><v-btn color="primary" variant="flat" prepend-icon="mdi-cloud-download-outline" :loading="busy === 'preview'" :disabled="!!busy || !settings.repository.trim() || !settings.branch.trim() || !settings.catalogs.length" @click="checkUpdates">{{ settingsDirty ? '保存并检查更新' : '检查仓库更新' }}</v-btn><span v-if="!settings.catalogs.length">请至少选择一个题库</span><span v-else>不会自动发布作业，也不会删除网站中的题目。</span></div>
        <div v-if="preview" class="preview">
          <div class="section-heading"><div><h3>本次同步预览 <code>{{ preview.commit.slice(0, 8) }}</code></h3><p>存在冲突或历史作答保护的题目不会覆盖，请查看下方说明。</p></div><v-btn color="primary" :loading="busy === 'apply'" :disabled="!!busy || !previewValid || !preview.items.length" prepend-icon="mdi-sync" @click="applySync">同步到网站</v-btn></div>
          <v-alert v-if="!previewValid" type="warning" variant="tonal" class="my-3">配置已修改，请重新检查仓库更新。</v-alert>
          <div class="preview-counts"><v-chip v-for="key in (['add', 'update', 'unchanged', 'conflict'] as const)" :key="key" :color="key === 'conflict' ? 'warning' : 'primary'" variant="tonal">{{ actionLabels[key] }} {{ preview.counts[key] }}</v-chip></div>
          <v-select v-model="previewFilter" class="preview-filter" :items="[{ title: '全部变更', value: 'all' }, ...(['add', 'update', 'unchanged', 'conflict'] as const).map(value => ({ title: actionLabels[value], value }))]" label="筛选预览" density="compact" variant="outlined" hide-details />
          <div class="table-scroll"><table><thead><tr><th>题目</th><th>处理方式</th><th>说明</th></tr></thead><tbody><tr v-for="item in previewRows" :key="item.key"><td>{{ item.title }}<small>{{ (item.catalogs || []).map(catalogName).join(' · ') }}</small></td><td><v-chip size="small" :color="item.action === 'conflict' ? 'warning' : 'primary'" variant="tonal">{{ actionLabels[item.action] }}</v-chip></td><td>{{ item.reason || '—' }}</td></tr><tr v-if="!previewRows.length"><td colspan="3" class="empty">没有符合条件的变更</td></tr></tbody></table></div>
          <v-pagination v-if="previewItems.length > 20" v-model="previewPage" :length="Math.ceil(previewItems.length / 20)" :total-visible="5" density="comfortable" />
        </div>
      </section>
      <section class="panel">
        <div class="section-heading"><div><h2>网站题目</h2><p>公开题所有登录成员均可练习；隐藏题仅相应管理员可见，仍可选入作业。</p></div><v-btn variant="text" prepend-icon="mdi-refresh" :disabled="!!busy" @click="refresh">刷新</v-btn></div>
        <div class="filters"><v-text-field v-model="search" label="搜索题目 / 标签" prepend-inner-icon="mdi-magnify" variant="outlined" density="compact" hide-details clearable /><v-select v-model="catalog" :items="[{ title: '全部题库', value: '' }, ...catalogChoices.map(c => ({ title: c.name, value: c.id }))]" label="题库" variant="outlined" density="compact" hide-details /><v-select v-model="direction" :items="[{ title: '全部组别', value: '' }, ...directions]" label="组别" variant="outlined" density="compact" hide-details /><v-select v-model="visibility" :items="[{ title: '全部状态', value: '' }, { title: '公开可刷', value: 'public' }, { title: '隐藏 / 内部', value: 'internal' }]" label="状态" variant="outlined" density="compact" hide-details /><v-select v-model="source" :items="[{ title: '全部来源', value: '' }, { title: '仓库同步', value: 'repository' }, { title: '自主添加 / 现有题目', value: 'local' }]" label="来源" variant="outlined" density="compact" hide-details /></div>
        <div class="bulk-actions"><span>筛选出 {{ filtered.length }} 题 · 已选 {{ selected.length }} 题</span><v-btn variant="text" size="small" :disabled="!!busy || !filtered.length" @click="toggleAll">{{ allFilteredSelected ? '取消全选' : '选择全部筛选结果' }}</v-btn><div class="bulk-buttons"><v-btn size="small" variant="tonal" color="teal" prepend-icon="mdi-earth" :disabled="!!busy || !selected.length" @click="setVisibility('public')">设为公开</v-btn><v-btn size="small" variant="tonal" prepend-icon="mdi-eye-off-outline" :disabled="!!busy || !selected.length" @click="setVisibility('internal')">隐藏题目</v-btn></div></div>
        <div class="table-scroll"><table><thead><tr><th class="check-column"><input type="checkbox" aria-label="选择全部筛选结果" :checked="allFilteredSelected" :disabled="!!busy || !filtered.length" @change="toggleAll" /></th><th>题目</th><th>题库 / 组别</th><th>可见范围</th><th>操作</th></tr></thead><tbody><tr v-for="q in rows" :key="q.id"><td><input type="checkbox" :aria-label="`选择 ${q.title}`" :checked="selected.includes(q.id)" :disabled="!!busy" @change="toggle(q.id)" /></td><td><router-link class="question-title" :to="learningLink(q)">{{ q.title }}</router-link><small>{{ q.sourceKey ? '仓库同步' : '自主添加 / 现有题目' }}<template v-if="q.tags?.length"> · {{ q.tags.slice(0, 3).join(' / ') }}</template></small></td><td>{{ q.catalogs?.map(catalogName).join(' / ') || '—' }}<small>{{ groupLabel(q.direction) }}</small></td><td><v-chip size="small" :color="q.bankVisibility === 'public' ? 'teal' : 'grey'" variant="tonal">{{ q.bankVisibility === 'public' ? '公开可刷' : '隐藏 / 内部' }}</v-chip></td><td class="row-actions"><v-btn size="small" variant="text" :to="learningLink(q)">编辑</v-btn><v-btn size="small" variant="text" :disabled="!!busy" @click="setVisibility(q.bankVisibility === 'public' ? 'internal' : 'public', [q.id])">{{ q.bankVisibility === 'public' ? '隐藏' : '公开' }}</v-btn></td></tr><tr v-if="!rows.length"><td colspan="5" class="empty"><v-icon size="32" class="mb-2">mdi-book-search-outline</v-icon><p>没有符合条件的题目</p><small>调整筛选条件，或从上方同步、新增题目。</small></td></tr></tbody></table></div>
        <v-pagination v-if="pages > 1" v-model="page" :length="pages" :total-visible="5" density="comfortable" />
      </section>
    </template>
  </div>
</template>

<style scoped>
.bank-management { max-width: 1360px; margin: 0 auto; padding: 28px 30px 36px; color: #1e293b; }
.page-heading,.section-heading,.action-line,.bulk-actions { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.page-heading { margin-bottom: 28px; }
.eyebrow { color: #5269a1; font-size: 13px; font-weight: 700; margin-bottom: 7px; }
h1 { font-size: 28px; line-height: 1.4; font-weight: 750; }
h2 { font-size: 19px; display: flex; align-items: center; gap: 8px; }
h3 { font-size: 16px; }
p { color: #64748b; line-height: 1.7; margin-top: 6px; font-size: 14px; }
.summary-grid { display: grid; grid-template-columns: repeat(4,1fr); gap: 14px; margin-bottom: 24px; }
.summary-grid > div { background: #fff; border: 1px solid #e2e8f0; border-radius: 14px; padding: 18px 22px; }
.summary-grid small { color: #64748b; font-size: 11px; }.summary-grid span { color: #64748b; display: block; font-size: 13px; }
.summary-grid strong { display: block; font-size: 29px; margin-top: 5px; font-weight: 700; }
.summary-grid .commit { font-size: 19px; line-height: 43px; font-family: ui-monospace,monospace; }
.panel { border: 1px solid #e2e8f0; background: #fff; padding: 24px; border-radius: 16px; margin-bottom: 24px; }
.field-label { color: #334155; font-weight: 600; margin: 22px 0 10px; }.field-label span { color: #64748b; font-weight: 400; margin-left: 10px; font-size: 12px; }
.catalog-grid { display: grid; grid-template-columns: repeat(4,1fr); gap: 10px; }.catalog-choice { display: flex; align-items: center; gap: 10px; padding: 16px 14px; border: 1px solid #dbe3ee; border-radius: 10px; cursor: pointer; }.catalog-choice.checked { background: #f0f5ff; border-color: #7f9bce; }.catalog-choice span { font-weight: 600; font-size: 14px; }.catalog-choice small { margin-left: auto; color: #64748b; white-space: nowrap; }
input[type=checkbox] { width: 17px; height: 17px; accent-color: #3f5c99; cursor: pointer; flex-shrink: 0; }
.sync-settings { display: grid; grid-template-columns: minmax(260px,380px) 1fr; gap: 20px; align-items: center; margin-top: 22px; }.action-line { justify-content: flex-start; margin-top: 22px; }.action-line > span { color: #64748b; font-size: 13px; }
.preview { border-top: 1px solid #e2e8f0; margin-top: 26px; padding-top: 24px; }code { background: #f1f5f9; padding: 3px 7px; border-radius: 5px; color: #52617a; font-size: 12px; margin-left: 6px; }.preview-counts { display: flex; flex-wrap: wrap; gap: 10px; margin: 18px 0; }.preview-filter { max-width: 220px; margin-bottom: 15px; }
.filters { display: grid; grid-template-columns: 2fr repeat(4,1fr); gap: 10px; margin: 24px 0 18px; }.bulk-actions { justify-content: flex-start; font-size: 13px; flex-wrap: wrap; padding: 12px 0; }.bulk-buttons { margin-left: auto; display: flex; gap: 8px; }
.table-scroll { width: 100%; overflow-x: auto; }table { width: 100%; border-collapse: collapse; font-size: 14px; }th { text-align: left; font-size: 12px; font-weight: 600; color: #64748b; background: #f8fafc; padding: 13px 12px; white-space: nowrap; }td { border-bottom: 1px solid #edf1f5; padding: 15px 12px; vertical-align: middle; }td small { display: block; color: #7b8799; font-size: 12px; margin-top: 5px; }td:first-child,th:first-child { padding-left: 12px; }.check-column { width: 42px; }.question-title { color: #263e69; text-decoration: none; font-weight: 600; }.question-title:hover { text-decoration: underline; }.row-actions { white-space: nowrap; }.empty { text-align: center; color: #94a3b8; padding: 40px 15px; }
@media (max-width: 1100px) { .filters { grid-template-columns: repeat(3,1fr); }.filters > :first-child { grid-column: span 2; }.catalog-grid { grid-template-columns: repeat(2,1fr); } }
@media (max-width: 700px) { .bank-management { padding: 20px 14px 28px; } .page-heading,.section-heading,.action-line { flex-wrap: wrap; }.summary-grid { grid-template-columns: repeat(2,1fr); }.summary-grid > div { padding: 14px; }.panel { padding: 17px; }.sync-settings { grid-template-columns: 1fr; gap: 6px; }.filters { grid-template-columns: repeat(2,1fr); }.bulk-buttons { margin-left: 0; }.catalog-choice { padding: 13px 9px; gap: 7px; }h1 { font-size: 24px; }table { min-width: 660px; } }
</style>
