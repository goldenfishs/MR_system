<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { ApiError, api } from "../lib/api";
import { readUploadFile } from "../lib/upload-file";
import { auth } from "../lib/auth";
import { fileSize, type FileAsset } from "../lib/platform";
import AttachmentList from "./AttachmentList.vue";
const props = withDefaults(
  defineProps<{
    modelValue: string[];
    label?: string;
    buttonLabel?: string;
    hideAttachments?: boolean;
    accept?: string;
    single?: boolean;
    disabled?: boolean;
    maxBytes?: number;
  }>(),
  { label: "上传附件", buttonLabel: "选择文件上传", accept: "", maxBytes: 25 * 1024 * 1024 },
);
const emit = defineEmits<{
  "update:modelValue": [string[]];
  busy: [boolean];
}>();
const phase = ref("");
const busy = ref(false),
  error = ref(""),
  picker = ref(false),
  loading = ref(false),
  library = ref<FileAsset[]>([]);
let disposed = false;
let uploadController: AbortController | undefined;
onBeforeUnmount(() => {
  disposed = true;
  uploadController?.abort();
  if (busy.value) emit("busy", false);
});
function add(ids: string[]) {
  emit(
    "update:modelValue",
    props.single ? ids.slice(-1) : [...new Set([...props.modelValue, ...ids])],
  );
}
function valid(file: { name: string; mime?: string; type?: string }) {
  if (!props.accept) return true;
  return props.accept.split(",").some((pattern) => {
    const p = pattern.trim().toLowerCase();
    return p.startsWith(".")
      ? file.name.toLowerCase().endsWith(p)
      : p.endsWith("/*")
        ? (file.mime || file.type || "").startsWith(p.slice(0, -1))
        : p === (file.mime || file.type || "");
  });
}
async function upload(event: Event) {
  if (props.disabled || busy.value || disposed) return;
  const input = event.target as HTMLInputElement;
  const chosen = [...(input.files || [])];
  if (!chosen.length) return;
  error.value = "";
  busy.value = true;
  emit("busy", true);
  const controller = new AbortController();
  uploadController = controller;
  let current = [...props.modelValue];
  try {
    for (const file of chosen) {
      if (file.size > props.maxBytes)
        throw new Error(
          `${file.name} 超过 ${fileSize(props.maxBytes)}，请缩小后上传。`,
        );
      if (!valid(file))
        throw new Error(`不支持 ${file.name}，请选择 ${props.accept}。`);
      if (!file.size) throw new Error("文件为空或尚未下载完成，请先保存到本地后重新选择。");
      phase.value = "正在读取文件…";
      const contents = await readUploadFile(file, controller.signal);
      if (disposed) return;
      phase.value = "正在上传…";
      const form = new FormData();
      form.append("file", contents, file.name);
      const { item } = await api.upload<{ item: FileAsset }>(
        "/files",
        form,
        controller.signal,
      );
      if (disposed) return;
      current = props.single ? [item.id] : [...current, item.id];
      emit("update:modelValue", current);
    }
  } catch (e) {
    if (!disposed) error.value = e instanceof ApiError && e.status === 0
      ? "上传连接中断，尚未确认上传成功。可先点‘从我的文件选择’检查；没有文件时，请将文件保存到手机本地后重新选择上传，或切换网络再试。"
      : (e as Error).message;
  } finally {
    // Keep the picker alive until the selected file has been consumed.
    input.value = "";
    phase.value = "";
    uploadController = undefined;
    if (!disposed) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
async function openLibrary() {
  picker.value = true;
  loading.value = true;
  error.value = "";
  try {
    const { items } = await api.get<{ items: FileAsset[] }>("/files");
    if (disposed) return;
    library.value = items.filter(
      (f) =>
        f.ownerId === auth.user?.id && valid(f) && f.size <= props.maxBytes,
    );
  } catch (e) {
    if (!disposed) error.value = (e as Error).message;
  } finally {
    if (!disposed) loading.value = false;
  }
}
</script>
<template>
  <div class="file-upload">
    <div class="upload-heading">
      <strong>{{ label }}</strong
      ><span>单个文件最多 {{ fileSize(maxBytes) }}</span>
    </div>
    <p v-if="!accept" class="text-caption text-medium-emphasis mb-2">支持代码源文件、SolidWorks（SLDPRT / SLDASM / SLDDRW）、STEP、CAD（DWG / DXF）、常见文档和压缩包；多文件工程建议打包为 ZIP。</p>
    <div class="upload-actions">
      <label :class="['upload-button', { disabled: disabled || busy }]"
        ><v-icon size="18">mdi-upload</v-icon
        >{{ busy ? phase || "正在上传…" : buttonLabel
        }}<input
          type="file"
          :accept="accept"
          :multiple="!single"
          :disabled="disabled || busy"
          @change="upload" /></label
      ><v-btn
        size="small"
        variant="text"
        color="primary"
        :disabled="disabled || busy"
        @click="openLibrary"
        >从我的文件选择</v-btn
      >
    </div>
    <v-progress-linear
      v-if="busy"
      indeterminate
      color="primary"
      class="my-3"
    /><v-alert
      v-if="error"
      type="error"
      variant="tonal"
      density="compact"
      class="mt-2"
      >{{ error }}</v-alert
    ><AttachmentList
      v-if="!hideAttachments"
      :ids="modelValue"
      :removable="!disabled && !busy"
      @remove="
        emit(
          'update:modelValue',
          modelValue.filter((id) => id !== $event),
        )
      "
    /><v-dialog v-model="picker" max-width="640"
      ><v-card
        ><v-card-title>选择已上传文件</v-card-title
        ><v-card-text
          ><v-progress-linear v-if="loading" indeterminate /><v-alert
            v-else-if="error"
            type="error"
            variant="tonal"
            >{{ error
            }}<v-btn variant="text" @click="openLibrary">重试</v-btn></v-alert
          >
          <p v-else-if="!library.length" class="text-medium-emphasis">
            还没有符合要求的文件，请先上传。
          </p>
          <v-list v-else
            ><v-list-item
              v-for="file in library"
              :key="file.id"
              :title="file.name"
              :subtitle="fileSize(file.size)"
              prepend-icon="mdi-file-outline"
              @click="
                add([file.id]);
                picker = false;
              " /></v-list></v-card-text
        ><v-card-actions
          ><v-spacer /><v-btn @click="picker = false"
            >关闭</v-btn
          ></v-card-actions
        ></v-card
      ></v-dialog
    >
  </div>
</template>
<style scoped>
.file-upload {
  margin: 16px 0;
}
.upload-heading {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 10px;
  font-size: 14px;
}
.upload-heading strong {
  font-weight: 500;
}
.upload-heading span {
  font-size: 12px;
  color: #757575;
}
.upload-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.upload-button {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  background: #e3f2fd;
  color: #1565c0;
  font-size: 13px;
  border-radius: 4px;
  cursor: pointer;
}
.upload-button input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
  width: 100%;
}
.upload-button.disabled {
  opacity: 0.5;
  cursor: default;
}
</style>
