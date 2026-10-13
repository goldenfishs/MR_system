<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
import { hasPermission } from "../lib/permissions";
import { auth } from "../lib/auth";
import { downloadUrl, previewUrl, type FileAsset, type PersonalRecord, type UserWithProfile } from "../lib/platform";
import FileUpload from "../components/FileUpload.vue";
import AttachmentList from "../components/AttachmentList.vue";
const props = defineProps<{ personalOnly?: boolean; embedded?: boolean }>();
const { smAndDown } = useDisplay();
const assets = ref<Record<string, FileAsset>>({});
const preview = ref<FileAsset | null>(null);
const imageError = ref<Record<string, boolean>>({});
const records = ref<PersonalRecord[]>([]),
  users = ref<UserWithProfile[]>([]),
  loading = ref(true),
  error = ref(""),
  notice = ref(""),
  search = ref(""),
  kind = ref("all"),
  open = ref(false),
  saving = ref(false),
  uploading = ref(false);
const admin = computed(() => hasPermission("records") && !props.personalOnly);
const form = reactive({
  userId: "",
  title: "",
  kind: "certificate",
  description: "",
  fileIds: [] as string[],
  issuedAt: new Date().toISOString().slice(0, 10),
});
const kinds = [
  { title: "奖状证书", value: "certificate" },
  { title: "荣誉奖励", value: "award" },
  { title: "个人通知", value: "notice" },
];
let loadVersion = 0;
const label = (value: string) =>
  kinds.find((k) => k.value === value)?.title || value;
const recordDate = (value?: string) =>
  value?.match(/^\d{4}-\d{2}-\d{2}/)?.[0] || "未设置";
const visible = computed(() => props.personalOnly ? records.value.filter(r => r.userId === auth.user?.id) : records.value);
const images = (item: PersonalRecord) => item.fileIds.map(id => assets.value[id]).filter((asset): asset is FileAsset => Boolean(asset) && ["image/png", "image/jpeg"].includes(asset!.mime));
const filtered = computed(() =>
  visible.value.filter(
    (r) =>
      (kind.value === "all" || r.kind === kind.value) &&
      `${r.title} ${r.userName} ${r.description}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
  ),
);
async function load() {
  const version = ++loadVersion;
  loading.value = true;
  error.value = "";
  try {
    const items = (
      await api.get<{ items: PersonalRecord[] }>("/records")
    ).items;
    if (version !== loadVersion) return;
    records.value = items.filter(item => !props.personalOnly || item.userId === auth.user?.id).map(item => ({ ...item, fileIds: item.fileIds || [] }));
    const fileIds = [...new Set(records.value.flatMap(record => record.fileIds || []))];
    await Promise.all(fileIds.map(async id => {
      if (assets.value[id]) return;
      try { const data = await api.get<{ item: FileAsset }>(`/files/${encodeURIComponent(id)}`); if (version === loadVersion) assets.value[id] = data.item; }
      catch { /* The download link remains available. */ }
    }));
    if (version !== loadVersion) return;
    if (admin.value) {
      const data = (
        await api.get<{ items: UserWithProfile[] }>("/users?permission=records")
      );
      if (version === loadVersion) users.value = data.items;
    }
  } catch (e) {
    if (version === loadVersion) error.value = (e as Error).message;
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}
function create() {
  Object.assign(form, {
    userId: "",
    title: "",
    kind: "certificate",
    description: "",
    fileIds: [],
    issuedAt: new Date().toISOString().slice(0, 10),
  });
  open.value = true;
}
async function save() {
  if (!admin.value || saving.value || uploading.value) return;
  if (!form.userId || !form.title.trim()) {
    notice.value = "请选择接收人并填写标题。";
    return;
  }
  saving.value = true;
  try {
    await api.post("/records", {
      ...form,
      issuedAt: form.issuedAt ? new Date(form.issuedAt).toISOString() : "",
    });
    open.value = false;
    notice.value = "已发放到该用户的荣誉中心。";
    await load();
  } catch (e) {
    notice.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
onMounted(load);
onUnmounted(() => { loadVersion++; });
</script>
<template>
  <div :class="props.embedded ? 'embedded-records' : 'page-content'">
    <div class="record-header">
      <div>
        <h1 class="page-title">{{ props.embedded ? "个人档案" : "荣誉中心" }}</h1>
        <p class="page-subtitle">
          面试通过证书、获奖证书与荣誉按人归档，随时查找和下载。
        </p>
      </div>
      <v-btn
        v-if="admin"
        color="primary"
        prepend-icon="mdi-plus"
        @click="create"
        >发放证书</v-btn
      >
    </div>
    <div class="record-filters">
      <v-text-field
        v-model="search"
        :label="admin ? '搜索标题、姓名或说明' : '搜索标题或说明'"
        prepend-inner-icon="mdi-magnify"
        hide-details
      /><v-select
        v-model="kind"
        :items="[{ title: '全部类型', value: 'all' }, ...kinds]"
        label="证书类型"
        hide-details
      />
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="my-4"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    ><v-progress-linear v-if="loading" indeterminate />
    <div v-else-if="filtered.length" class="record-grid">
      <v-card v-for="item in filtered" :key="item.id" class="record-card"
        ><div class="d-flex align-center ga-3">
          <v-avatar
            :color="item.kind === 'notice' ? 'primary' : 'amber-darken-2'"
            variant="tonal"
            ><v-icon>{{
              item.kind === "notice"
                ? "mdi-email-outline"
                : "mdi-certificate-outline"
            }}</v-icon></v-avatar
          >
          <div class="record-title">
            <span class="text-caption text-medium-emphasis">{{
              label(item.kind)
            }}</span>
            <h2>{{ item.title }}</h2>
          </div>
        </div>
        <p v-if="admin" class="recipient">接收人：{{ item.userName }}</p>
        <p class="description">{{ item.description || "暂无补充说明。" }}</p>
        <div v-if="images(item).length" class="certificate-images">
          <button v-for="asset in images(item)" :key="asset.id" type="button" class="certificate-image" :aria-label="`预览${asset.name}`" @click="preview = asset">
            <img v-if="!imageError[asset.id]" :src="previewUrl(asset.id)" :alt="asset.name" loading="lazy" @error="imageError[asset.id] = true" />
            <span v-else><v-icon>mdi-image-outline</v-icon>点击查看图片</span>
            <span class="preview-caption"><v-icon size="16">mdi-magnify-plus-outline</v-icon>预览证书</span>
          </button>
        </div>
        <AttachmentList :ids="item.fileIds" />
        <p class="record-date">
          发放日期：{{ recordDate(item.issuedAt || item.createdAt) }}
        </p></v-card
      >
    </div>
    <v-card v-else-if="!error" class="pa-10 mt-5 text-center text-medium-emphasis">{{
      visible.length
        ? "没有匹配的证书。"
        : admin
          ? "还没有发放证书。"
          : "管理员发放的面试通过证书、奖状和荣誉会出现在这里。"
    }}</v-card
    ><v-dialog
      v-model="open"
      max-width="720"
      :fullscreen="smAndDown"
      scrollable
      :persistent="saving || uploading"
      ><v-card
        ><v-card-title class="d-flex align-center"
          >发放证书与荣誉<v-spacer /><v-btn
            icon="mdi-close"
            aria-label="关闭证书发放"
            variant="text"
            :disabled="saving || uploading"
            @click="open = false" /></v-card-title
        ><v-card-text
          ><form @submit.prevent="save">
            <v-autocomplete
              v-model="form.userId"
              :items="
                users.map((u) => ({
                  title: `${u.name} · ${u.email}${u.profile?.studentNumber ? ' · ' + u.profile.studentNumber : ''}`,
                  value: u.id,
                }))
              "
              label="接收人 *"
              aria-required="true"
            /><v-text-field
              v-model="form.title"
              label="证书标题 *"
              maxlength="200"
              required
            /><v-row
              ><v-col cols="12" sm="6"
                ><v-select
                  v-model="form.kind"
                  :items="kinds"
                  label="类型" /></v-col
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.issuedAt"
                  type="date"
                  label="发放日期" /></v-col></v-row
            ><v-textarea
              v-model="form.description"
              label="说明（选填）"
              rows="4"
              maxlength="10000"
            /><FileUpload
              v-model="form.fileIds"
              label="奖状、证书或通知附件（PNG / JPEG 可直接预览）"
              :disabled="saving"
              @busy="uploading = $event"
            /><v-alert
              type="info"
              variant="tonal"
              density="compact"
              class="mb-5"
              >接收人可在荣誉中心和个人中心查询、预览和下载；其他普通用户无法访问。</v-alert
            >
            <div class="d-flex justify-end">
              <v-btn
                color="primary"
                type="submit"
                :loading="saving"
                :disabled="uploading"
                >确认发放</v-btn
              >
            </div>
          </form></v-card-text
        ></v-card
      ></v-dialog
    ><v-dialog :model-value="!!preview" max-width="1080" scrollable @update:model-value="!$event && (preview = null)"><v-card v-if="preview"><v-card-title class="d-flex align-center"><span class="preview-title">{{ preview.name }}</span><v-spacer /><v-btn icon="mdi-close" variant="text" aria-label="关闭图片预览" @click="preview = null" /></v-card-title><v-card-text class="image-viewer"><img v-if="!imageError[preview.id]" :src="previewUrl(preview.id)" :alt="preview.name" @error="imageError[preview.id] = true" /><v-alert v-else type="warning" variant="tonal">图片预览暂不可用，请下载附件查看。</v-alert><v-btn class="mt-3" :href="downloadUrl(preview.id)" color="primary" variant="tonal" prepend-icon="mdi-download">下载证书</v-btn></v-card-text></v-card></v-dialog><v-snackbar
      :model-value="!!notice"
      @update:model-value="!$event && (notice = '')"
      >{{ notice }}</v-snackbar
    >
  </div>
</template>
<style scoped>
.embedded-records { margin-top: 30px; }
.embedded-records .page-title { font-size:22px; }
.certificate-images { display:grid; grid-template-columns:1fr; gap:10px; margin:16px 0; }
.certificate-image { position:relative; width:100%; border:1px solid #eee; border-radius:5px; overflow:hidden; background:#fafbfc; min-height:140px; display:block; }
.certificate-image img { display:block; width:100%; max-height:260px; object-fit:contain; }
.preview-caption { position:absolute; bottom:8px; right:8px; background:#ffffffeb; padding:5px 9px; border-radius:4px; font-size:12px; color:#1976d2; display:flex; align-items:center; gap:5px; }
.image-viewer { text-align:center; background:#f7f8fa; }
.image-viewer img { max-width:100%; max-height:78vh; object-fit:contain; }
.preview-title { min-width:0; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
.record-title { min-width:0; }
.record-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}
.record-filters {
  display: flex;
  gap: 18px;
  margin-bottom: 20px;
}
.record-filters > .v-input {
  min-width: 0;
}
.record-filters > .v-input:first-child {
  flex: 2;
}
.record-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
}
.record-card {
  padding: 22px;
}
.record-card h2 {
  font-size: 17px;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.recipient {
  font-size: 13px;
  color: #1976d2;
  margin-top: 15px;
}
.description {
  font-size: 14px;
  color: #616161;
  line-height: 1.8;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 16px 0;
}
.record-date {
  font-size: 12px;
  color: #757575;
  border-top: 1px solid #eee;
  padding-top: 14px;
  margin-top: 18px;
}
@media (max-width: 650px) {
  .record-header {
    flex-wrap: wrap;
    margin-bottom: 16px;
  }
  .record-grid {
    grid-template-columns: 1fr;
  }
  .record-filters {
    flex-wrap: wrap;
  }
  .record-filters > .v-input:first-child {
    flex-basis: 100%;
  }
}
</style>
