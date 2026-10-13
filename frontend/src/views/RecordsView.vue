<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import type { PersonalRecord, UserWithProfile } from "../lib/platform";
import FileUpload from "../components/FileUpload.vue";
import AttachmentList from "../components/AttachmentList.vue";
const { smAndDown } = useDisplay();
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
const admin = computed(() => auth.user?.role === "admin");
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
const label = (value: string) =>
  kinds.find((k) => k.value === value)?.title || value;
const recordDate = (value?: string) =>
  value?.match(/^\d{4}-\d{2}-\d{2}/)?.[0] || "未设置";
const filtered = computed(() =>
  records.value.filter(
    (r) =>
      (kind.value === "all" || r.kind === kind.value) &&
      `${r.title} ${r.userName} ${r.description}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
  ),
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    records.value = (
      await api.get<{ items: PersonalRecord[] }>("/records")
    ).items;
    if (admin.value)
      users.value = (
        await api.get<{ items: UserWithProfile[] }>("/users")
      ).items;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
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
  if (saving.value || uploading.value) return;
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
    notice.value = "已发放到该用户的个人档案。";
    await load();
  } catch (e) {
    notice.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <div class="record-header">
      <div>
        <h1 class="page-title">{{ admin ? "档案发放" : "我的档案" }}</h1>
        <p class="page-subtitle">
          奖状、荣誉与个人通知按人归档，随时查找和下载。
        </p>
      </div>
      <v-btn
        v-if="admin"
        color="primary"
        prepend-icon="mdi-plus"
        @click="create"
        >发放档案</v-btn
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
        label="档案类型"
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
          <div>
            <span class="text-caption text-medium-emphasis">{{
              label(item.kind)
            }}</span>
            <h2>{{ item.title }}</h2>
          </div>
        </div>
        <p v-if="admin" class="recipient">接收人：{{ item.userName }}</p>
        <p class="description">{{ item.description || "暂无补充说明。" }}</p>
        <AttachmentList :ids="item.fileIds" />
        <p class="record-date">
          发放日期：{{ recordDate(item.issuedAt || item.createdAt) }}
        </p></v-card
      >
    </div>
    <v-card v-else class="pa-10 mt-5 text-center text-medium-emphasis">{{
      records.length
        ? "没有匹配的档案。"
        : admin
          ? "还没有发放档案。"
          : "管理员发放的奖状、荣誉和通知会出现在这里。"
    }}</v-card
    ><v-dialog
      v-model="open"
      max-width="720"
      :fullscreen="smAndDown"
      scrollable
      :persistent="saving || uploading"
      ><v-card
        ><v-card-title class="d-flex align-center"
          >发放个人档案<v-spacer /><v-btn
            icon="mdi-close"
            aria-label="关闭档案发放"
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
              label="档案标题 *"
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
              label="奖状、证书或通知附件"
              :disabled="saving"
              @busy="uploading = $event"
            /><v-alert
              type="info"
              variant="tonal"
              density="compact"
              class="mb-5"
              >接收人可在自己的档案中查询和下载；其他普通用户无法访问。</v-alert
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
    ><v-snackbar
      :model-value="!!notice"
      @update:model-value="!$event && (notice = '')"
      >{{ notice }}</v-snackbar
    >
  </div>
</template>
<style scoped>
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
  grid-template-columns: 1fr 1fr;
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
