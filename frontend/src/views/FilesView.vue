<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api } from "../lib/api";
import {
  downloadUrl,
  fileSize,
  dateTime,
  type FileAsset,
} from "../lib/platform";
import FileUpload from "../components/FileUpload.vue";
const files = ref<FileAsset[]>([]),
  uploaded = ref<string[]>([]),
  loading = ref(true),
  error = ref(""),
  search = ref(""),
  kind = ref("全部");
const typeOf = (f: FileAsset) =>
  f.mime.startsWith("image/")
    ? "图片"
    : /\.pdf$/i.test(f.name)
      ? "PDF"
      : /\.(zip|rar|7z|tar|gz)$/i.test(f.name)
        ? "压缩包"
        : /\.(c|cpp|h|hpp|py|js|ts|java|go|rs)$/i.test(f.name)
          ? "代码"
          : /\.(pcb|kicad_pcb|sch|epro|PcbDoc|SchDoc|gerber)$/i.test(f.name)
            ? "电路设计"
            : "其他";
const filtered = computed(() =>
  files.value.filter(
    (f) =>
      (kind.value === "全部" || typeOf(f) === kind.value) &&
      f.name.toLowerCase().includes(search.value.toLowerCase()),
  ),
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    files.value = (await api.get<{ items: FileAsset[] }>("/files")).items;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
function uploadedFiles(ids: string[]) {
  uploaded.value = ids;
  void load();
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <h1 class="page-title">文件中心</h1>
    <p class="page-subtitle">
      统一保存简历、设计文件、代码、图片和工程压缩包，可在报名与作业中重复选择。
    </p>
    <v-card class="pa-5 mb-5"
      ><FileUpload
        :model-value="uploaded"
        label="添加文件"
        @update:model-value="uploadedFiles"
    /></v-card>
    <div class="file-filters">
      <v-text-field
        v-model="search"
        label="搜索文件名"
        prepend-inner-icon="mdi-magnify"
        hide-details
      /><v-select
        v-model="kind"
        :items="['全部', '图片', 'PDF', '压缩包', '代码', '电路设计', '其他']"
        label="文件类型"
        hide-details
      /><v-btn variant="text" :loading="loading" @click="load">刷新</v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="my-4"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    ><v-progress-linear v-if="loading" indeterminate /><v-card
      v-else
      class="mt-4"
      ><v-list v-if="filtered.length" lines="two"
        ><v-list-item
          v-for="file in filtered"
          :key="file.id"
          prepend-icon="mdi-file-outline"
          :title="file.name"
          :subtitle="`${typeOf(file)} · ${fileSize(file.size)} · ${dateTime(file.createdAt)}`"
          ><template #append
            ><v-btn
              :href="downloadUrl(file.id)"
              variant="text"
              color="primary"
              icon="mdi-download"
              :aria-label="`下载${file.name}`" /></template></v-list-item
      ></v-list>
      <div v-else class="pa-10 text-center text-medium-emphasis">
        {{
          files.length
            ? "没有符合筛选条件的文件。"
            : "还没有文件，上传后会保存在这里。"
        }}
      </div></v-card
    >
  </div>
</template>
<style scoped>
.file-filters {
  display: flex;
  gap: 14px;
  align-items: center;
}
.file-filters > .v-input:first-child {
  flex: 2;
}
.file-filters > .v-input {
  flex: 1;
  min-width: 0;
}
@media (max-width: 600px) {
  .file-filters {
    flex-wrap: wrap;
  }
  .file-filters > .v-input:first-child {
    flex-basis: 100%;
  }
}
</style>
