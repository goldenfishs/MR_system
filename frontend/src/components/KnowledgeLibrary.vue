<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from "vue";
import type { PDFDocumentLoadingTask, PDFDocumentProxy, RenderTask } from "pdfjs-dist";
import pdfWorkerSrc from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
import { groupOptions, hasPermission, manageableGroups } from "../lib/permissions";
import { auth } from "../lib/auth";
import { dateTime, downloadUrl, previewUrl, type FileAsset } from "../lib/platform";
import type { KnowledgeResource } from "../lib/learning";
import FileUpload from "./FileUpload.vue";
import AttachmentList from "./AttachmentList.vue";

const props = defineProps<{ direction?: string }>();
const { smAndDown } = useDisplay();
const resources = ref<KnowledgeResource[]>([]);
const loading = ref(false), error = ref(""), notice = ref("");
const search = ref(""), kind = ref("all"), dialog = ref(false);
const saving = ref(false), uploading = ref(false);
const selected = ref<KnowledgeResource | null>(null);
const removing = ref<KnowledgeResource | null>(null);
const editingId = ref("");
const fileMetadata = ref<Record<string, FileAsset>>({});
type PdfState = { status: "loading" | "ready" | "error"; page: number; pages: number; zoom: number; rendering: boolean; error: string };
const pdfStates = reactive<Record<string, PdfState>>({});
const pdfDocuments = new Map<string, PDFDocumentProxy>();
const pdfLoadingTasks = new Map<string, PDFDocumentLoadingTask>();
const pdfRenderTasks = new Map<string, RenderTask>();
const pdfCanvases = new Map<string, HTMLCanvasElement>();
const pdfRenderVersions = new Map<string, number>();
let pdfAbort: AbortController | null = null;
let pdfGeneration = 0;
const form = reactive({ title: "", description: "", direction: "", kind: "file", fileIds: [] as string[], url: "" });
const canManage = computed(() => hasPermission("knowledge", props.direction || undefined));
const canEdit = (resource: KnowledgeResource) => hasPermission("knowledge", resource.direction);
function clearPdfPreview() {
  pdfGeneration++;
  pdfAbort?.abort();
  pdfAbort = null;
  for (const task of pdfRenderTasks.values()) task.cancel();
  for (const task of pdfLoadingTasks.values()) void task.destroy();
  pdfRenderTasks.clear();
  pdfLoadingTasks.clear();
  pdfDocuments.clear();
  pdfCanvases.clear();
  pdfRenderVersions.clear();
  for (const id of Object.keys(pdfStates)) delete pdfStates[id];
}
onBeforeUnmount(clearPdfPreview);
async function renderPdf(id: string) {
  const version = (pdfRenderVersions.get(id) || 0) + 1;
  pdfRenderVersions.set(id, version);
  const previous = pdfRenderTasks.get(id);
  if (previous) {
    previous.cancel();
    try { await previous.promise; } catch { /* Superseded page render. */ }
  }
  const document = pdfDocuments.get(id);
  const canvas = pdfCanvases.get(id);
  const state = pdfStates[id];
  if (!document || !canvas || !state || version !== pdfRenderVersions.get(id)) return;
  state.rendering = true;
  try {
    const page = await document.getPage(state.page);
    if (version !== pdfRenderVersions.get(id) || pdfDocuments.get(id) !== document) return;
    const base = page.getViewport({ scale: 1 });
    const availableWidth = Math.max(240, (canvas.parentElement?.clientWidth || 680) - 32);
    const viewport = page.getViewport({ scale: availableWidth / base.width * state.zoom });
    const pixelRatio = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.ceil(viewport.width * pixelRatio);
    canvas.height = Math.ceil(viewport.height * pixelRatio);
    canvas.style.width = `${viewport.width}px`;
    canvas.style.height = `${viewport.height}px`;
    const task = page.render({
      canvas, viewport, background: "#ffffff",
      transform: pixelRatio === 1 ? undefined : [pixelRatio, 0, 0, pixelRatio, 0, 0],
    });
    pdfRenderTasks.set(id, task);
    await task.promise;
  } catch (error) {
    if (version === pdfRenderVersions.get(id) && pdfStates[id]) {
      state.status = "error";
      state.error = error instanceof Error ? error.message : "无法绘制 PDF 页面";
    }
  } finally {
    if (version === pdfRenderVersions.get(id) && pdfStates[id]) {
      state.rendering = false;
      pdfRenderTasks.delete(id);
    }
  }
}
function setPdfCanvas(id: string, element: unknown) {
  if (element instanceof HTMLCanvasElement && pdfCanvases.get(id) !== element) {
    pdfCanvases.set(id, element);
    void renderPdf(id);
  } else if (!element) {
    pdfCanvases.delete(id);
  }
}
async function loadPdf(id: string, generation: number, signal: AbortSignal) {
  pdfStates[id] = { status: "loading", page: 1, pages: 0, zoom: 1, rendering: false, error: "" };
  try {
    const response = await fetch(previewUrl(id), { credentials: "same-origin", signal });
    if (response.status === 401) window.dispatchEvent(new Event("session-expired"));
    if (!response.ok || !response.headers.get("Content-Type")?.startsWith("application/pdf")) {
      throw new Error(response.status === 403 ? "没有权限预览此文档" : "无法读取 PDF 文档");
    }
    const data = await response.arrayBuffer();
    if (generation !== pdfGeneration) return;
    const pdfjs = await import("pdfjs-dist");
    if (generation !== pdfGeneration) return;
    pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerSrc;
    const task = pdfjs.getDocument({ data: new Uint8Array(data) });
    pdfLoadingTasks.set(id, task);
    const document = await task.promise;
    if (generation !== pdfGeneration) { void task.destroy(); return; }
    pdfDocuments.set(id, document);
    pdfStates[id] = { status: "ready", page: 1, pages: document.numPages, zoom: 1, rendering: false, error: "" };
    await nextTick();
    if (pdfCanvases.has(id)) void renderPdf(id);
  } catch (error) {
    if (generation !== pdfGeneration) return;
    pdfStates[id] = { status: "error", page: 1, pages: 0, zoom: 1, rendering: false, error: error instanceof Error ? error.message : "无法预览 PDF 文档" };
  }
}
function changePdfPage(id: string, delta: number) {
  const state = pdfStates[id];
  if (!state) return;
  state.page = Math.max(1, Math.min(state.pages, state.page + delta));
  void renderPdf(id);
}
function changePdfZoom(id: string, delta: number) {
  const state = pdfStates[id];
  if (!state) return;
  state.zoom = Math.max(0.75, Math.min(2, Math.round((state.zoom + delta) * 100) / 100));
  void renderPdf(id);
}
watch(selected, async (resource) => {
  clearPdfPreview();
  if (!resource) return;
  const generation = pdfGeneration;
  pdfAbort = new AbortController();
  await Promise.all(resource.fileIds.map(async (id) => {
    if (fileMetadata.value[id]) return;
    try { fileMetadata.value[id] = (await api.get<{ item: FileAsset }>(`/files/${encodeURIComponent(id)}`)).item; }
    catch { /* AttachmentList still offers the server's download response. */ }
  }));
  if (generation !== pdfGeneration) return;
  for (const id of resource.fileIds) {
    if (fileMetadata.value[id]?.mime === "application/pdf") void loadPdf(id, generation, pdfAbort.signal);
  }
});
const directions = computed(() => auth.user?.role === "admin" ? groupOptions : manageableGroups("knowledge"));
const kinds = [
  { value: "file", title: "文件资料", icon: "mdi-folder-outline", color: "primary" },
  { value: "pdf", title: "PDF 文档", icon: "mdi-file-pdf-box", color: "deep-orange" },
  { value: "video", title: "视频课程", icon: "mdi-play-circle-outline", color: "deep-purple" },
];
const typeOf = (value: string) => kinds.find((item) => item.value === value) || kinds[0]!;
const filtered = computed(() => resources.value.filter((item) =>
  (kind.value === "all" || item.kind === kind.value) &&
  `${item.title} ${item.description}`.toLowerCase().includes(search.value.toLowerCase()),
));
const safeUrl = (value: string) => /^https?:\/\//i.test(value);
let loadVersion = 0;
async function load() {
  const version = ++loadVersion;
  loading.value = true;
  error.value = "";
  try {
    const query = props.direction ? `?direction=${encodeURIComponent(props.direction)}` : "";
    const result = await api.get<{ resources: KnowledgeResource[] }>(`/knowledge-base${query}`);
    if (version === loadVersion) resources.value = result.resources;
  } catch (e) {
    if (version === loadVersion) error.value = (e as Error).message;
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}
watch(() => props.direction, () => { search.value = ""; void load(); }, { immediate: true });
function openEditor(resource?: KnowledgeResource) {
  editingId.value = resource?.id || "";
  Object.assign(form, {
    title: resource?.title || "", description: resource?.description || "",
    direction: resource?.direction || props.direction || (auth.user?.role === "admin" ? "" : directions.value[0]?.value || ""), kind: resource?.kind || "file",
    fileIds: [...(resource?.fileIds || [])], url: resource?.url || "",
  });
  dialog.value = true;
}
async function save() {
  if (saving.value || uploading.value) return;
  if (!hasPermission("knowledge", form.direction)) { notice.value = "没有该组知识库的管理权限"; return; }
  if (!form.title.trim()) { notice.value = "请填写资料标题"; return; }
  if (!form.fileIds.length && !form.url.trim()) { notice.value = "请上传资料文件或填写资料链接"; return; }
  if (form.url.trim() && !safeUrl(form.url.trim())) { notice.value = "资料链接须以 https:// 或 http:// 开头"; return; }
  saving.value = true;
  try {
    const body = { ...form, url: form.url.trim() };
    const result = editingId.value
      ? await api.patch<{ resource: KnowledgeResource }>(`/knowledge-base/${editingId.value}`, body)
      : await api.post<{ resource: KnowledgeResource }>("/knowledge-base", body);
    resources.value = [result.resource, ...resources.value.filter((item) => item.id !== result.resource.id)];
    if (selected.value?.id === result.resource.id) selected.value = result.resource;
    dialog.value = false;
    notice.value = editingId.value ? "资料已更新" : "资料已发布到知识库";
    await load();
  } catch (e) { notice.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function remove() {
  if (!removing.value || saving.value) return;
  saving.value = true;
  try {
    await api.delete(`/knowledge-base/${removing.value.id}`);
    resources.value = resources.value.filter((item) => item.id !== removing.value?.id);
    if (selected.value?.id === removing.value.id) selected.value = null;
    removing.value = null;
    notice.value = "资料已从知识库移除";
  } catch (e) { notice.value = (e as Error).message; }
  finally { saving.value = false; }
}
</script>

<template>
  <section class="knowledge-library">
    <div class="library-toolbar">
      <div class="library-filters">
        <v-text-field v-model="search" aria-label="搜索知识库" placeholder="搜索资料标题或内容" prepend-inner-icon="mdi-magnify" variant="outlined" density="compact" hide-details />
        <v-select v-model="kind" :items="[{ value: 'all', title: '全部资料' }, ...kinds]" aria-label="筛选资料类型" variant="outlined" density="compact" hide-details />
      </div>
      <div class="library-actions">
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
        <v-btn v-if="canManage" color="primary" prepend-icon="mdi-plus" @click="openEditor()">发布资料</v-btn>
      </div>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <div v-if="loading" class="library-empty"><v-progress-circular indeterminate color="primary" /><p>正在读取知识库…</p></div>
    <div v-else-if="filtered.length" class="resource-grid">
      <v-card v-for="resource in filtered" :key="resource.id" class="resource-card" elevation="1">
        <div class="resource-type"><v-icon :color="typeOf(resource.kind).color">{{ typeOf(resource.kind).icon }}</v-icon><span>{{ typeOf(resource.kind).title }}</span><v-chip size="x-small" variant="tonal">{{ resource.direction || '全组共享' }}</v-chip></div>
        <h2>{{ resource.title }}</h2><p>{{ resource.description || '打开资料，查看学习内容和下载附件。' }}</p>
        <div class="resource-footer"><span>{{ dateTime(resource.createdAt) }}</span><v-btn color="primary" variant="text" size="small" append-icon="mdi-arrow-right" @click="selected = resource">查看资料</v-btn></div>
      </v-card>
    </div>
    <v-card v-else class="library-empty" elevation="1"><v-icon size="42" color="indigo-lighten-3">mdi-bookshelf</v-icon><h2>{{ search || kind !== 'all' ? '没有匹配的资料' : '知识库正在积累' }}</h2><p>{{ canManage ? '发布文件、PDF 文档或视频课程，供成员集中查看和下载。' : '管理员发布资料后，可在这里查看学习内容和下载文件。' }}</p><v-btn v-if="canManage" color="primary" variant="tonal" @click="openEditor()">发布第一份资料</v-btn></v-card>

    <v-dialog :model-value="Boolean(selected)" max-width="760" :fullscreen="smAndDown" scrollable @update:model-value="!$event && (selected = null)">
      <v-card v-if="selected" class="resource-dialog"><v-card-title class="dialog-heading"><div><span>{{ typeOf(selected.kind).title }} · {{ selected.direction || '全组共享' }}</span><h2>{{ selected.title }}</h2></div><v-btn icon="mdi-close" variant="text" aria-label="关闭资料" @click="selected = null" /></v-card-title>
        <v-card-text><p class="resource-description">{{ selected.description || '暂无资料说明' }}</p>
          <template v-for="id in selected.fileIds" :key="id">
            <div v-if="fileMetadata[id]?.mime === 'application/pdf'" class="pdf-preview" :aria-label="fileMetadata[id]?.name || 'PDF 资料预览'">
              <div class="pdf-toolbar">
                <span class="pdf-name">{{ fileMetadata[id]?.name }}</span>
                <div v-if="pdfStates[id]?.status === 'ready'" class="pdf-controls">
                  <v-btn icon="mdi-chevron-left" size="small" variant="text" title="上一页" aria-label="上一页" :disabled="pdfStates[id]?.page === 1" @click="changePdfPage(id, -1)" />
                  <span class="pdf-page">{{ pdfStates[id]?.page }} / {{ pdfStates[id]?.pages }}</span>
                  <v-btn icon="mdi-chevron-right" size="small" variant="text" title="下一页" aria-label="下一页" :disabled="pdfStates[id]?.page === pdfStates[id]?.pages" @click="changePdfPage(id, 1)" />
                  <span class="pdf-divider" />
                  <v-btn icon="mdi-magnify-minus-outline" size="small" variant="text" title="缩小" aria-label="缩小" :disabled="(pdfStates[id]?.zoom || 1) <= 0.75" @click="changePdfZoom(id, -0.25)" />
                  <span class="pdf-zoom">{{ Math.round((pdfStates[id]?.zoom || 1) * 100) }}%</span>
                  <v-btn icon="mdi-magnify-plus-outline" size="small" variant="text" title="放大" aria-label="放大" :disabled="(pdfStates[id]?.zoom || 1) >= 2" @click="changePdfZoom(id, 0.25)" />
                </div>
              </div>
              <div v-if="!pdfStates[id] || pdfStates[id]?.status === 'loading'" class="pdf-status"><v-progress-circular indeterminate color="primary" /><span>正在打开 PDF…</span></div>
              <div v-else-if="pdfStates[id]?.status === 'error'" class="pdf-status"><v-icon color="warning" size="30">mdi-file-alert-outline</v-icon><span>{{ pdfStates[id]?.error || 'PDF 预览不可用' }}</span><v-btn :href="downloadUrl(id)" color="primary" variant="tonal" prepend-icon="mdi-download">下载文档</v-btn></div>
              <div v-else class="pdf-pages"><v-progress-circular v-if="pdfStates[id]?.rendering" class="pdf-rendering" indeterminate size="24" color="primary" /><canvas :ref="(element) => setPdfCanvas(id, element)" /></div>
            </div>
            <video v-else-if="fileMetadata[id]?.mime.startsWith('video/')" :src="previewUrl(id)" controls preload="metadata" class="video-preview" />
            <img v-else-if="['image/png', 'image/jpeg'].includes(fileMetadata[id]?.mime || '')" :src="previewUrl(id)" :alt="fileMetadata[id]?.name || '资料图片'" class="image-preview" />
          </template>
          <AttachmentList :ids="selected.fileIds" /><v-btn v-if="safeUrl(selected.url)" :href="selected.url" target="_blank" rel="noopener noreferrer" color="primary" variant="tonal" :prepend-icon="selected.kind === 'video' ? 'mdi-play-circle-outline' : 'mdi-open-in-new'" class="mt-4">{{ selected.kind === 'video' ? '观看视频课程' : '打开资料链接' }}</v-btn><p class="resource-date">发布于 {{ dateTime(selected.createdAt) }}</p></v-card-text>
        <v-card-actions v-if="canEdit(selected)" class="px-6 pb-5"><v-btn color="primary" prepend-icon="mdi-pencil-outline" @click="openEditor(selected)">编辑资料</v-btn><v-spacer /><v-btn color="error" variant="text" @click="removing = selected">移除资料</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="dialog" max-width="760" :fullscreen="smAndDown" :persistent="saving || uploading" scrollable><v-card class="resource-dialog"><v-card-title class="dialog-heading"><h2>{{ editingId ? '编辑知识库资料' : '发布知识库资料' }}</h2><v-btn icon="mdi-close" variant="text" aria-label="关闭资料编辑" :disabled="saving || uploading" @click="dialog = false" /></v-card-title><v-card-text><v-form @submit.prevent="save">
      <v-text-field v-model="form.title" label="资料标题 *" maxlength="200" variant="outlined" required />
      <v-textarea v-model="form.description" label="资料说明" rows="4" maxlength="20000" variant="outlined" />
      <v-row><v-col cols="12" sm="6"><v-select v-model="form.direction" :items="[...(auth.user?.role === 'admin' ? [{ value: '', title: '全组共享' }] : []), ...directions]" label="面向组别" variant="outlined" /></v-col><v-col cols="12" sm="6"><v-select v-model="form.kind" :items="kinds" label="资料类型" variant="outlined" /></v-col></v-row>
      <v-text-field v-model="form.url" :label="form.kind === 'video' ? '视频课程链接（选填）' : '资料链接（选填）'" type="url" placeholder="https://" variant="outlined" />
      <FileUpload v-model="form.fileIds" label="资料文件（PDF、讲义、视频或压缩包）" :disabled="saving" @busy="uploading = $event" />
      <div class="editor-footer"><span>资料面向指定组别和全组共享区展示</span><v-btn color="primary" type="submit" :loading="saving" :disabled="uploading">{{ editingId ? '保存修改' : '发布资料' }}</v-btn></div>
    </v-form></v-card-text></v-card></v-dialog>
    <v-dialog :model-value="Boolean(removing)" max-width="440" :persistent="saving" @update:model-value="!$event && (removing = null)"><v-card><v-card-title>移除这份资料？</v-card-title><v-card-text>“{{ removing?.title }}”将不再显示在知识库中。</v-card-text><v-card-actions><v-spacer /><v-btn :disabled="saving" @click="removing = null">取消</v-btn><v-btn color="error" :loading="saving" @click="remove">确认移除</v-btn></v-card-actions></v-card></v-dialog>
    <v-snackbar :model-value="Boolean(notice)" @update:model-value="!$event && (notice = '')">{{ notice }}<template #actions><v-btn variant="text" @click="notice = ''">关闭</v-btn></template></v-snackbar>
  </section>
</template>

<style scoped>
.pdf-preview{width:100%;border:1px solid #dce3e7;border-radius:4px;margin:16px 0;overflow:hidden;background:#f3f5f6}.pdf-toolbar,.pdf-controls{display:flex;align-items:center;gap:6px}.pdf-toolbar{justify-content:space-between;min-height:48px;padding:4px 10px;background:#fff;border-bottom:1px solid #dce3e7}.pdf-name{font-size:13px;color:#455a64;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}.pdf-controls{flex-shrink:0}.pdf-page,.pdf-zoom{min-width:48px;text-align:center;font-size:12px;color:#455a64}.pdf-divider{height:20px;border-left:1px solid #dce3e7;margin:0 4px}.pdf-pages{position:relative;max-height:min(72vh,720px);min-height:300px;overflow:auto;text-align:center;padding:16px}.pdf-pages canvas{display:block;margin:0 auto;max-width:none;box-shadow:0 1px 5px #20304028;background:#fff}.pdf-rendering{position:sticky;top:12px;left:calc(50% - 12px);z-index:1}.pdf-status{min-height:300px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:14px;padding:24px;text-align:center;font-size:13px;color:#687680}.video-preview,.image-preview{display:block;width:100%;max-height:520px;object-fit:contain;background:#f5f7f9;border-radius:4px;margin:16px 0}
@media(max-width:600px){.pdf-toolbar{flex-wrap:wrap}.pdf-name{width:100%}.pdf-controls{width:100%;justify-content:center}.pdf-pages{min-height:240px;max-height:65vh;padding:8px}}
.library-toolbar,.library-actions,.library-filters,.resource-type,.resource-footer,.dialog-heading,.editor-footer{display:flex;align-items:center;gap:12px}.library-toolbar{justify-content:space-between;margin-bottom:20px}.library-filters{width:480px;max-width:100%}.library-filters>.v-text-field{flex:1.6;min-width:0}.library-filters>.v-select{flex:1;min-width:0}.library-actions{flex-shrink:0}.resource-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:18px}.resource-card{padding:20px;border-radius:6px!important;display:flex;flex-direction:column;box-shadow:0 1px 4px #2437461a!important}.resource-type{font-size:12px;color:#687680;margin-bottom:16px}.resource-type>.v-chip{margin-left:auto}.resource-card h2{font-size:17px;font-weight:500;line-height:1.6;color:#263238}.resource-card>p{font-size:14px;color:#687680;line-height:1.8;margin:10px 0 20px;display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}.resource-footer{justify-content:space-between;margin-top:auto;border-top:1px solid #edf0f2;padding-top:12px}.resource-footer>span{font-size:11px;color:#839099}.library-empty{text-align:center;padding:52px 24px;border-radius:6px!important}.library-empty h2{font-size:18px;font-weight:500;color:#455a64;margin:14px 0 10px}.library-empty p{font-size:14px;color:#687680;line-height:1.8;margin:14px 0 22px}.resource-dialog{border-radius:6px!important}.dialog-heading{justify-content:space-between;padding:20px 24px!important;white-space:normal}.dialog-heading h2{font-size:20px;line-height:1.5;font-weight:500}.dialog-heading span{font-size:12px;color:#687680}.resource-description{white-space:pre-wrap;overflow-wrap:anywhere;font-size:14px;line-height:1.9;margin-bottom:20px}.resource-date{font-size:12px;color:#839099;margin-top:24px}.editor-footer{justify-content:space-between;border-top:1px solid #e8ecef;padding-top:20px;margin-top:20px}.editor-footer span{font-size:12px;color:#687680}
@media(max-width:1350px){.resource-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:800px){.library-toolbar{flex-wrap:wrap}.library-filters{width:100%}.library-actions{margin-left:auto}}@media(max-width:600px){.resource-grid{grid-template-columns:1fr}.resource-footer{gap:4px}.editor-footer{flex-direction:column;align-items:stretch}.dialog-heading{padding:16px!important}.dialog-heading h2{font-size:18px}.resource-type{gap:8px}}
</style>
