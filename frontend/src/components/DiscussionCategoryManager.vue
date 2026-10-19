<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import { groupLabel, manageableGroups } from "../lib/permissions";
import { sortDiscussionCategories } from "../lib/discussion-categories";
import type { DiscussionCategory } from "../lib/types";

const props = defineProps<{ categories: DiscussionCategory[]; loading: boolean; direction: string }>();
const emit = defineEmits<{ changed: []; notice: [message: string] }>();
const expanded = ref(false), editing = ref(false), saving = ref(false), error = ref(""), deleteId = ref("");
const form = reactive({ id: "", name: "", description: "", direction: "", sortOrder: 0 });
const scopes = computed(() => [
  ...(auth.user?.role === "admin" ? [{ title: "全队通用", value: "" }] : []),
  ...manageableGroups("discussions").map(group => ({ title: group.title, value: group.value })),
]);
const canManage = computed(() => scopes.value.length > 0);
const categories = computed(() => sortDiscussionCategories(props.categories.filter(item => item.canManage)));
const busy = computed(() => saving.value || props.loading);
function edit(category?: DiscussionCategory) {
  if (busy.value) return;
  error.value = ""; deleteId.value = "";
  const defaultScope = scopes.value.some(scope => scope.value === props.direction) ? props.direction : scopes.value[0]?.value || "";
  Object.assign(form, category ? { id: category.id, name: category.name, description: category.description, direction: category.direction, sortOrder: category.sortOrder } : { id: "", name: "", description: "", direction: defaultScope, sortOrder: 0 });
  editing.value = true;
}
async function save() {
  if (busy.value) return;
  error.value = "";
  if (!form.name.trim()) { error.value = "请填写分类名称。"; return; }
  const order = Number(form.sortOrder);
  if (!Number.isInteger(order) || order < 0 || order > 9999) { error.value = "排序请输入 0～9999 的整数，数字越小越靠前。"; return; }
  saving.value = true;
  try {
    const data = { name: form.name.trim(), description: form.description.trim(), sortOrder: order };
    if (form.id) await api.patch(`/discussion-categories/${encodeURIComponent(form.id)}`, data);
    else await api.post("/discussion-categories", { ...data, direction: form.direction });
    editing.value = false; emit("notice", form.id ? "分类已更新。" : "分类已创建，提问时即可选择。"); emit("changed");
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function archive(category: DiscussionCategory) {
  if (busy.value) return;
  saving.value = true; error.value = ""; deleteId.value = "";
  try {
    await api.patch(`/discussion-categories/${encodeURIComponent(category.id)}`, { archived: !category.archived });
    emit("notice", category.archived ? "分类已恢复，新提问可以再次使用。" : "分类已停用，原有问题保留，新提问不再使用。"); emit("changed");
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function remove(category: DiscussionCategory) {
  if (busy.value || deleteId.value !== category.id) return;
  saving.value = true; error.value = "";
  try {
    await api.delete(`/discussion-categories/${encodeURIComponent(category.id)}`);
    deleteId.value = ""; emit("notice", "空分类已删除。"); emit("changed");
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
</script>

<template>
  <v-card v-if="canManage" variant="outlined" class="category-manager">
    <button type="button" class="category-manager-heading" :aria-expanded="expanded" aria-controls="discussion-category-management" :disabled="saving" @click="expanded = !expanded">
      <v-icon color="primary">mdi-folder-cog-outline</v-icon>
      <span><strong>问题分类管理</strong><span class="category-manager-caption">按主题整理问题，让提问与查找更方便</span></span>
      <v-icon class="ml-auto">{{ expanded ? 'mdi-chevron-up' : 'mdi-chevron-down' }}</v-icon>
    </button>
    <div v-if="expanded" id="discussion-category-management" class="category-manager-body">
      <div class="category-manager-toolbar"><p>全队通用分类可用于任何组。组内分类只用于对应组；停用会保留已有问题。</p><v-btn color="primary" variant="tonal" prepend-icon="mdi-plus" :disabled="busy || editing" @click="edit()">新建分类</v-btn></div>
      <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>
      <form v-if="editing" class="category-editor" @submit.prevent="save">
        <h3>{{ form.id ? '编辑分类' : '新建分类' }}</h3>
        <div class="category-editor-fields">
          <v-text-field v-model="form.name" label="分类名称 *" placeholder="例如：编程基础、机械设计" maxlength="30" counter :disabled="busy" required />
          <v-select v-model="form.direction" :items="scopes" label="适用范围" :disabled="busy || !!form.id" :hint="form.id ? '创建后适用范围不再变更' : '其他组仍可浏览已公开的问题'" persistent-hint />
          <v-text-field v-model="form.sortOrder" type="number" label="排序" min="0" max="9999" step="1" hint="数字越小越靠前" persistent-hint :disabled="busy" />
        </div>
        <v-textarea v-model="form.description" label="分类说明（选填）" placeholder="简单说明哪些问题适合放在这里" rows="2" auto-grow maxlength="200" counter :disabled="busy" />
        <div class="category-editor-actions"><v-btn variant="text" :disabled="busy" @click="editing = false; error = ''">取消</v-btn><v-btn type="submit" color="primary" :loading="saving" :disabled="props.loading">保存分类</v-btn></div>
      </form>
      <div v-if="categories.length" class="category-manager-list">
        <div v-for="category in categories" :key="category.id" class="category-manager-row" :class="{ 'is-archived': category.archived }">
          <div class="category-row-info"><div class="category-row-title"><strong>{{ category.name }}</strong><v-chip size="x-small" variant="tonal">{{ category.direction ? groupLabel(category.direction) : '全队通用' }}</v-chip><v-chip v-if="category.archived" size="x-small" variant="outlined">已停用</v-chip></div><p v-if="category.description">{{ category.description }}</p><span class="category-row-meta">{{ category.questionCount || 0 }} 个可见问题 · 排序 {{ category.sortOrder }}</span></div>
          <div v-if="deleteId !== category.id" class="category-row-actions"><v-btn size="small" variant="text" :disabled="busy || editing" @click="edit(category)">编辑</v-btn><v-btn size="small" variant="text" :disabled="busy || editing" @click="archive(category)">{{ category.archived ? '恢复' : '停用' }}</v-btn><v-btn v-if="!category.questionCount" size="small" color="error" variant="text" :disabled="busy || editing" @click="deleteId = category.id">删除</v-btn></div>
          <div v-else class="category-delete-confirm"><span>确定删除此空分类？</span><v-btn size="small" variant="text" :disabled="busy" @click="deleteId = ''">取消</v-btn><v-btn size="small" color="error" variant="tonal" :disabled="props.loading" :loading="saving" @click="remove(category)">确认删除</v-btn></div>
        </div>
      </div>
      <p v-else class="category-manager-empty">还没有你可以管理的分类，点击“新建分类”开始整理。</p>
    </div>
  </v-card>
</template>

<style scoped>
.category-manager { margin:20px 0; border-color:rgba(var(--v-theme-primary),.16); border-radius:14px; }
.category-manager-heading { display:flex; align-items:center; gap:12px; width:100%; padding:18px 20px; text-align:left; color:inherit; }
.category-manager-heading:focus-visible { outline:2px solid rgb(var(--v-theme-primary)); outline-offset:-3px; }
.category-manager-heading strong { font-size:15px; font-weight:600; }
.category-manager-caption { display:block; font-size:12px; opacity:.65; margin-top:3px; }
.category-manager-body { padding:0 20px 20px; }
.category-manager-toolbar { display:flex; align-items:center; gap:20px; justify-content:space-between; margin:0 0 16px; }
.category-manager-toolbar p { font-size:13px; line-height:1.7; color:rgb(var(--v-theme-on-surface)); opacity:.68; }
.category-manager-toolbar .v-btn { flex-shrink:0; }
.category-editor { padding:20px; margin-bottom:20px; background:rgba(var(--v-theme-primary),.035); border:1px solid rgba(var(--v-theme-primary),.12); border-radius:12px; }
.category-editor h3 { font-size:16px; margin-bottom:18px; }
.category-editor-fields { display:grid; grid-template-columns:2fr 1.5fr 1fr; gap:14px; }
.category-editor-actions { display:flex; justify-content:flex-end; gap:8px; }
.category-manager-row { display:flex; gap:16px; justify-content:space-between; align-items:center; padding:16px 0; border-top:1px solid rgba(var(--v-theme-on-surface),.09); }
.category-row-info { min-width:0; flex:1; }
.category-row-title { display:flex; flex-wrap:wrap; align-items:center; gap:8px; }
.category-row-title strong { font-size:14px; overflow-wrap:anywhere; }
.category-row-info p { font-size:13px; margin:6px 0; line-height:1.6; opacity:.7; overflow-wrap:anywhere; }
.category-row-meta { display:block; font-size:12px; opacity:.55; margin-top:6px; }
.category-row-actions,.category-delete-confirm { display:flex; gap:2px; align-items:center; flex-wrap:wrap; }
.category-delete-confirm { font-size:12px; }
.is-archived .category-row-title strong { opacity:.55; }
.category-manager-empty { font-size:13px; padding:18px 0 4px; opacity:.65; }
@media (max-width:680px) { .category-manager-heading { padding:16px; } .category-manager-body { padding:0 16px 16px; } .category-manager-toolbar,.category-manager-row { align-items:flex-start; flex-direction:column; gap:10px; } .category-editor { padding:14px; } .category-editor-fields { grid-template-columns:1fr; gap:0; } .category-row-actions { align-self:flex-end; } }
</style>
