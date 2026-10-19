<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
import { groupOptions, groupLabel, hasPermission } from "../lib/permissions";
import { auth } from "../lib/auth";
import type { Discussion, DiscussionCategory } from "../lib/types";
import { categoryMatchesDirection, discussionCategoryName, selectableDiscussionCategories, sortDiscussionCategories, uncategorizedDiscussion, validDiscussionCategoryId } from "../lib/discussion-categories";
import DiscussionCategoryManager from "../components/DiscussionCategoryManager.vue";
import FileUpload from "../components/FileUpload.vue";
import AttachmentList from "../components/AttachmentList.vue";
import AssignmentPromptPreview from "../components/AssignmentPromptPreview.vue";

const route = useRoute(), router = useRouter();
const { smAndDown } = useDisplay();
const discussions = ref<Discussion[]>([]);
const categories = ref<DiscussionCategory[]>([]);
const loading = ref(true), saving = ref(false), error = ref(""), notice = ref("");
const search = ref("");
const categoryFilter = ref(""), selectedCategoryId = ref("");
const selected = ref<Discussion | null>(null);
const open = ref(false), reply = ref(""), uploadBusy = ref(false);
const form = reactive({ title: "", content: "", direction: "", categoryId: "", fileIds: [] as string[] });
const directions = ["机械", "电控", "视觉算法", "硬件", "运营"];
const canManageDiscussion = (item: Discussion) => hasPermission("discussions", item.direction);
const manager = computed(() => Boolean(selected.value && canManageDiscussion(selected.value)));
const canReply = computed(() => Boolean(selected.value && !selected.value.published && (manager.value || selected.value.createdBy === auth.user?.id)));
const direction = computed(() => typeof route.query.direction === "string" && directions.includes(route.query.direction) ? route.query.direction : "");
const groupFilter = computed({
  get: () => direction.value,
  set: (value: string | null) => {
    const query = { ...route.query };
    if (value) query.direction = value;
    else delete query.direction;
    void router.replace({ query });
  },
});
const groupFilters = [{ title: "全部组别", value: "" }, ...groupOptions];
const categoryTitle = (category: DiscussionCategory) => `${category.name}${category.direction ? ` · ${groupLabel(category.direction)}` : ""}${category.archived ? "（已停用）" : ""}`;
const categoryName = (id?: string) => discussionCategoryName(categories.value, id);
const categoryFilters = computed(() => [
  { title: "全部分类", value: "" },
  { title: "未分类", value: uncategorizedDiscussion },
  ...sortDiscussionCategories(categories.value.filter(category => !direction.value || categoryMatchesDirection(category, direction.value))).map(category => ({ title: categoryTitle(category), value: category.id })),
]);
const formCategories = computed(() => [
  { title: "未分类", value: "" },
  ...selectableDiscussionCategories(categories.value, form.direction || "").map(category => ({ title: categoryTitle(category), value: category.id })),
]);
const formCategoryDescription = computed(() => categories.value.find(category => category.id === form.categoryId)?.description || "选择最贴近问题的主题，方便管理员解答。");
const detailCategories = computed(() => [
  { title: "未分类", value: "" },
  ...selectableDiscussionCategories(categories.value, selected.value?.direction || "", selected.value?.categoryId).map(category => ({ title: categoryTitle(category), value: category.id })),
]);
const hasFilters = computed(() => Boolean(search.value || categoryFilter.value));
let loadVersion = 0;
const filtered = computed(() => discussions.value.filter((item) => {
  if (categoryFilter.value === uncategorizedDiscussion ? Boolean(item.categoryId) : categoryFilter.value && item.categoryId !== categoryFilter.value) return false;
  return `${item.title} ${item.content} ${item.direction} ${item.authorName || ""} ${categoryName(item.categoryId)}`.toLowerCase().includes((search.value || "").trim().toLowerCase());
}));
function normalize(item: Discussion): Discussion {
  return { ...item, replies: Array.isArray(item.replies) ? item.replies : [], status: item.status || "open", published: Boolean(item.published) };
}
async function load() {
  const version = ++loadVersion;
  loading.value = true; error.value = "";
  try {
    const [data, categoryData] = await Promise.all([
      api.get<{ discussions?: Discussion[]; items?: Discussion[] }>(direction.value ? `/discussions?direction=${encodeURIComponent(direction.value)}` : "/discussions"),
      api.get<{ categories: DiscussionCategory[] }>("/discussion-categories"),
    ]);
    if (version !== loadVersion) return;
    categories.value = categoryData.categories || [];
    discussions.value = (data.discussions || data.items || []).map(normalize);
    if (!categoryFilters.value.some(item => item.value === categoryFilter.value)) categoryFilter.value = "";
    if (selected.value) selected.value = discussions.value.find(item => item.id === selected.value?.id) || null;
  } catch (e) { if (version === loadVersion) error.value = (e as Error).message; }
  finally { if (version === loadVersion) loading.value = false; }
}
function ask() { Object.assign(form, { title: "", content: "", direction: direction.value, categoryId: validDiscussionCategoryId(categories.value, direction.value, categoryFilter.value), fileIds: [] }); open.value = true; }
async function create() {
  if (saving.value || uploadBusy.value || loading.value) return;
  if (!form.title.trim() || !form.content.trim()) { notice.value = "请填写问题标题和内容。"; return; }
  saving.value = true;
  try { await api.post("/discussions", { title: form.title.trim(), content: form.content.trim(), direction: form.direction || "", categoryId: form.categoryId || "", fileIds: form.fileIds }); open.value = false; notice.value = "问题已提交，管理员与对应组管理员可以查看并解答。"; await load(); }
  catch (e) { notice.value = (e as Error).message; }
  finally { saving.value = false; }
}
function view(item: Discussion) { selected.value = item; selectedCategoryId.value = item.categoryId || ""; reply.value = ""; }
async function addReply() {
  if (!selected.value || !canReply.value || !reply.value.trim() || saving.value) return;
  const id = selected.value.id;
  saving.value = true;
  try { const data = await api.post<{ discussion?: Discussion }>(`/discussions/${encodeURIComponent(id)}/replies`, { content: reply.value.trim() }); if (data.discussion && selected.value?.id === id) { selected.value = normalize(data.discussion); reply.value = ""; } notice.value = "回复已提交。"; await load(); }
  catch (e) { notice.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function update(item: Discussion, patch: { status?: "open" | "resolved"; published?: boolean; categoryId?: string }) {
  if (!canManageDiscussion(item) || saving.value) return;
  saving.value = true;
  try { const data = await api.patch<{ discussion: Discussion }>(`/discussions/${encodeURIComponent(item.id)}`, patch); Object.assign(item, normalize(data.discussion)); if (selected.value?.id === item.id) { selected.value = item; selectedCategoryId.value = item.categoryId || ""; } notice.value = patch.categoryId !== undefined ? "问题分类已更新。" : patch.published ? "已公开到讨论区。" : patch.status === "resolved" ? "已标记为已解答。" : "已更新。"; await load(); }
  catch (e) { notice.value = (e as Error).message; }
  finally { saving.value = false; }
}
function date(value?: string) { return value ? new Date(value).toLocaleString("zh-CN", { dateStyle: "medium", timeStyle: "short" }) : ""; }
watch([() => form.direction, categories], () => { form.categoryId = validDiscussionCategoryId(categories.value, form.direction || "", form.categoryId); });
watch([direction, () => auth.user?.id], () => {
  discussions.value = [];
  categories.value = [];
  selected.value = null;
  reply.value = "";
  void load();
}, { immediate: true });
onUnmounted(() => { loadVersion++; });
</script>
<template>
  <div class="page-content discussion-page">
    <div class="discussion-header"><div><h1 class="page-title">讨论区<span class="text-secondary">.</span></h1><p class="page-subtitle">向指定技术组提问，把重要解答沉淀成全队都能查阅的经验。</p></div><v-btn color="primary" prepend-icon="mdi-comment-plus-outline" @click="ask">提出问题</v-btn></div>
    <DiscussionCategoryManager :categories="categories" :loading="loading" :direction="direction" @changed="load" @notice="notice = $event" />
    <div class="discussion-tools">
      <v-text-field v-model="search" class="discussion-search" label="搜索问题、分类或作者" prepend-inner-icon="mdi-magnify" hide-details clearable />
      <v-select v-model="groupFilter" class="discussion-filter" :items="groupFilters" label="组别" hide-details />
      <v-select v-model="categoryFilter" class="discussion-filter" :items="categoryFilters" label="问题分类" hide-details :disabled="loading" />
      <v-btn variant="text" :loading="loading" @click="load">刷新</v-btn>
    </div>
    <div v-if="!loading && !error" class="discussion-results"><span>{{ filtered.length }} 个可见问题</span><v-btn v-if="hasFilters" size="small" variant="text" @click="search = ''; categoryFilter = ''">清除搜索与分类</v-btn></div>
    <v-alert v-if="error" type="error" variant="tonal" class="my-4">{{ error }} <v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <v-progress-linear v-if="loading" indeterminate color="primary" />
    <div v-else-if="filtered.length" class="discussion-grid"><v-card v-for="item in filtered" :key="item.id" class="discussion-card" @click="view(item)">
      <div class="discussion-card-top"><v-chip size="small" :color="item.published ? 'success' : 'primary'" variant="tonal">{{ item.published ? '公开精选' : item.status === 'resolved' ? '已解答' : '待解答' }}</v-chip><v-chip v-if="item.direction" size="small" variant="outlined">{{ groupLabel(item.direction) }}</v-chip><v-chip size="small" variant="tonal" color="secondary" prepend-icon="mdi-tag-outline">{{ categoryName(item.categoryId) }}</v-chip><v-spacer /><span class="text-caption text-medium-emphasis">{{ date(item.createdAt) }}</span></div>
      <h2>{{ item.title }}</h2><p>{{ item.content }}</p><div class="discussion-card-foot"><span>{{ item.authorName || '队员' }}</span><span>{{ item.replies.length }} 条回复</span><span v-if="item.fileIds?.length"><v-icon size="14">mdi-paperclip</v-icon> {{ item.fileIds.length }} 个附件</span><v-btn v-if="canManageDiscussion(item) && !item.published" size="small" variant="text" :disabled="saving || (!item.replies.length && item.status !== 'resolved')" @click.stop="update(item, { status: item.status === 'resolved' ? 'open' : 'resolved' })">{{ item.status === 'resolved' ? '重新打开' : '标记解答' }}</v-btn><v-btn v-if="canManageDiscussion(item) && item.status === 'resolved'" size="small" variant="text" color="success" :disabled="saving" @click.stop="update(item, { published: !item.published })">{{ item.published ? '取消公开' : '公开精选' }}</v-btn></div>
    </v-card></div>
    <v-card v-else-if="!error" class="pa-10 mt-5 text-center text-medium-emphasis">{{ hasFilters ? '这个分类或搜索条件下还没有可见问题，试试其他分类。' : direction ? '这个组还没有可见问题，欢迎从第一个提问开始。' : '还没有问题，欢迎从第一个提问开始。' }}</v-card>
    <v-dialog v-model="open" max-width="680" :fullscreen="smAndDown" scrollable :persistent="saving || uploadBusy"><v-card><v-card-title class="d-flex align-center">提出一个问题<v-spacer /><v-btn icon="mdi-close" variant="text" aria-label="关闭提问" :disabled="saving || uploadBusy" @click="open = false" /></v-card-title><v-card-text><form @submit.prevent="create"><v-text-field v-model="form.title" label="问题标题 *" maxlength="160" :disabled="saving" required /><v-select v-model="form.direction" :items="groupOptions" label="指定技术组（可选）" :disabled="saving" clearable /><v-select v-model="form.categoryId" :items="formCategories" label="问题分类" :disabled="saving || loading" :hint="formCategoryDescription" persistent-hint class="mb-3" /><v-textarea v-model="form.content" label="问题详情 *" rows="7" maxlength="10000" counter :disabled="saving" required /><FileUpload v-model="form.fileIds" label="问题图片与附件（选填）" accept=".png,.jpg,.jpeg,.pdf,.zip,.7z,.rar,.doc,.docx,.txt,.md,.csv" :disabled="saving" @busy="uploadBusy = $event" /><v-alert type="info" variant="tonal" density="compact" class="mb-4">提问默认仅你、管理员和对应组管理员可见。重要解答可由管理员公开。</v-alert><div class="d-flex justify-end"><v-btn variant="text" :disabled="saving || uploadBusy" @click="open = false">取消</v-btn><v-btn type="submit" color="primary" :loading="saving" :disabled="uploadBusy || loading">提交问题</v-btn></div></form></v-card-text></v-card></v-dialog>
    <v-dialog :model-value="!!selected" max-width="760" :fullscreen="smAndDown" scrollable @update:model-value="!$event && (selected = null)"><v-card v-if="selected"><v-card-title class="d-flex align-center discussion-title"><span class="discussion-title-text">{{ selected.title }}</span><v-btn icon="mdi-close" variant="text" aria-label="关闭问题" @click="selected = null" /></v-card-title><v-card-text><div class="detail-meta"><v-chip size="small" :color="selected.published ? 'success' : 'primary'" variant="tonal">{{ selected.published ? '公开精选' : selected.status === 'resolved' ? '已解答' : '待解答' }}</v-chip><v-chip size="small" variant="tonal" color="secondary" prepend-icon="mdi-tag-outline">{{ categoryName(selected.categoryId) }}</v-chip><span>{{ selected.direction ? `${groupLabel(selected.direction)} · ` : '' }}{{ selected.authorName || '队员' }} · {{ date(selected.createdAt) }}</span></div><div v-if="manager" class="detail-category-editor"><v-select v-model="selectedCategoryId" :items="detailCategories" label="调整问题分类" density="compact" hide-details :disabled="saving || loading" /><v-btn variant="tonal" color="primary" :loading="saving" :disabled="loading || selectedCategoryId === (selected.categoryId || '')" @click="update(selected, { categoryId: selectedCategoryId })">保存分类</v-btn></div><p class="detail-content">{{ selected.content }}</p><AssignmentPromptPreview :ids="selected.fileIds || []" /><AttachmentList :ids="selected.fileIds || []" /><v-divider class="my-5" /><h3>回复（{{ selected.replies.length }}）</h3><div v-if="selected.replies.length" class="reply-list"><div v-for="item in selected.replies" :key="item.id" class="reply-item"><strong>{{ item.userName }}</strong><span>{{ date(item.createdAt) }}</span><p>{{ item.content }}</p></div></div><p v-else class="text-medium-emphasis">还没有回复。</p><form v-if="canReply" class="reply-form" @submit.prevent="addReply"><v-textarea v-model="reply" label="写下你的回复" rows="3" maxlength="5000" :disabled="saving" hide-details /><v-btn class="mt-3" type="submit" color="primary" :loading="saving" :disabled="!reply.trim()">回复</v-btn></form><v-alert v-else-if="selected.published" type="info" variant="tonal" class="mt-5">此问答已公开为精选内容。如需继续讨论，管理员可先取消公开。</v-alert><div v-if="manager" class="mt-5 d-flex flex-wrap ga-2"><v-btn v-if="!selected.published" variant="tonal" :disabled="saving || (!selected.replies.length && selected.status !== 'resolved')" @click="update(selected, { status: selected.status === 'resolved' ? 'open' : 'resolved' })">{{ selected.status === 'resolved' ? '重新打开' : '标记为已解答' }}</v-btn><v-btn v-if="manager && selected.status === 'resolved'" color="success" variant="tonal" :disabled="saving" @click="update(selected, { published: !selected.published })">{{ selected.published ? '取消公开' : '公开精选' }}</v-btn></div></v-card-text></v-card></v-dialog>
    <v-snackbar :model-value="!!notice" @update:model-value="!$event && (notice = '')">{{ notice }}</v-snackbar>
  </div>
</template>
<style scoped>
.discussion-header { display:flex; align-items:flex-start; justify-content:space-between; gap:18px; }
.discussion-tools { display:flex; align-items:center; gap:16px; margin:22px 0; }
.discussion-tools .discussion-search { flex:1.8; min-width:220px; }
.discussion-tools .discussion-filter { flex:1; min-width:180px; }
.discussion-results { display:flex; align-items:center; justify-content:space-between; font-size:13px; color:#72777d; margin:-8px 0 16px; }
.detail-category-editor { display:flex; align-items:center; gap:12px; margin-top:20px; padding:14px; border:1px solid rgba(var(--v-theme-primary),.15); border-radius:10px; background:rgba(var(--v-theme-primary),.03); }
.detail-category-editor .v-input { flex:1; min-width:0; }
.detail-category-editor .v-btn { flex-shrink:0; }
.discussion-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:18px; }
.discussion-card { padding:20px; cursor:pointer; transition:transform .16s ease, box-shadow .16s ease; }
.discussion-card:hover { transform:translateY(-2px); box-shadow:0 8px 24px #273d5917; }
.discussion-card-top,.discussion-card-foot,.detail-meta { display:flex; align-items:center; gap:9px; flex-wrap:wrap; }
.discussion-card h2 { font-size:17px; font-weight:500; margin:17px 0 9px; overflow-wrap:anywhere; }
.discussion-card p { color:#62666b; font-size:14px; line-height:1.75; white-space:pre-wrap; overflow-wrap:anywhere; display:-webkit-box; -webkit-line-clamp:3; -webkit-box-orient:vertical; overflow:hidden; }
.discussion-card-foot { margin-top:15px; color:#7b7f84; font-size:12px; }
.discussion-title { white-space:normal; overflow-wrap:anywhere; font-size:18px; gap:12px; }
.discussion-title-text { min-width:0; flex:1; }
.discussion-title .v-btn { flex-shrink:0; }
.detail-meta { overflow-wrap:anywhere; color:#72777d; font-size:13px; }
.detail-content { margin:23px 0; white-space:pre-wrap; line-height:1.9; overflow-wrap:anywhere; }
.reply-list { margin:15px 0 22px; }
.reply-item { border-left:3px solid #d9e2ff; background:#f8faff; padding:12px 15px; margin:9px 0; }
.reply-item strong { font-size:13px; }
.reply-item span { margin-left:10px; color:#8c9197; font-size:11px; }
.reply-item p { white-space:pre-wrap; overflow-wrap:anywhere; margin:7px 0 0; line-height:1.7; }
.reply-form { margin-top:18px; }
@media (max-width:1000px) { .discussion-tools { flex-wrap:wrap; } .discussion-tools .discussion-search { flex-basis:100%; } }
@media (max-width:680px) { .discussion-header { flex-wrap:wrap; } .discussion-grid { grid-template-columns:1fr; } .discussion-tools { flex-wrap:wrap; } .discussion-tools .v-input { flex-basis:100%; } .detail-category-editor { align-items:stretch; flex-direction:column; } .detail-category-editor .v-btn { align-self:flex-end; } }
</style>
