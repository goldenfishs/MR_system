<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { api } from "../lib/api";

type RecognitionSettings = {
  enabled: boolean;
  endpoint: string;
  model: string;
  inputMode: "text" | "vision";
  apiKeySet: boolean;
  available: boolean;
};

const settings = ref<RecognitionSettings | null>(null);
const form = reactive({ enabled: false, endpoint: "", model: "", apiKey: "", inputMode: "vision" as "text" | "vision" });
const inputModes = [
  { title: "PDF 文字识别（适合纯文本模型）", value: "text" },
  { title: "图片识别（需视觉模型，图片与 PDF）", value: "vision" },
];
const loading = ref(true);
const saving = ref(false);
const error = ref("");
const saved = ref(false);
const clearApiKey = ref(false);
const busy = computed(() => loading.value || saving.value);

function applySettings(value: RecognitionSettings) {
  settings.value = value;
  Object.assign(form, { enabled: value.enabled, endpoint: value.endpoint, model: value.model, inputMode: value.inputMode || "vision", apiKey: "" });
  clearApiKey.value = false;
}

function useDeepSeekPreset() {
  if (busy.value) return;
  form.endpoint = "https://api.deepseek.com/chat/completions";
  form.inputMode = "vision";
  // Model ID verified with the provider model listing.
  form.model = "deepseek-flash";
  saved.value = false;
}

async function load() {
  if (saving.value) return;
  loading.value = true;
  error.value = "";
  try {
    applySettings(await api.get<RecognitionSettings>("/settings/finance-recognition"));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (busy.value || !settings.value) return;
  error.value = "";
  saved.value = false;
  const endpoint = form.endpoint.trim();
  const model = form.model.trim();
  const apiKey = form.apiKey.trim();
  if (endpoint) {
    try {
      const url = new URL(endpoint);
      if (url.protocol !== "https:" || url.username || url.password || url.hash) throw new Error();
    } catch {
      error.value = "请填写有效的 HTTPS 接口地址，密钥请单独填写。";
      return;
    }
  }
  if (form.enabled && (!endpoint || !model || (!apiKey && (!settings.value.apiKeySet || clearApiKey.value)))) {
    error.value = "启用发票识别前，请填写接口地址、与识别方式匹配的模型名称和 API Key。";
    return;
  }
  saving.value = true;
  try {
    applySettings(await api.patch<RecognitionSettings>("/settings/finance-recognition", {
      enabled: form.enabled,
      endpoint,
      model,
      inputMode: form.inputMode,
      apiKey: clearApiKey.value ? "" : apiKey,
      clearApiKey: clearApiKey.value,
    }));
    saved.value = true;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <form id="finance-recognition" class="recognition-settings" @submit.prevent="save">
    <v-card class="recognition-card">
      <div class="recognition-heading">
        <v-icon color="primary">mdi-text-recognition</v-icon>
        <div><h2>发票识别</h2><p>自动提取报销凭证中的金额、日期与事项，减少重复填写。</p></div>
      </div>
      <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-4" />
      <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-4" role="alert">
        {{ error }}
        <v-btn v-if="!settings" type="button" variant="text" size="small" :disabled="busy" @click="load">重试</v-btn>
      </v-alert>
      <v-alert v-if="saved" type="success" variant="tonal" density="compact" class="mb-4" role="status">发票识别设置已保存。</v-alert>
      <template v-if="settings && !loading">
        <v-chip :color="settings.available ? 'success' : 'grey'" size="small" class="mb-4">{{ settings.available ? '已启用识别' : settings.enabled ? '配置未完整' : '识别已关闭' }}</v-chip>
        <v-alert type="info" variant="tonal" density="compact" class="mb-4">使用兼容 OpenAI Chat Completions 的接口。识别时会将凭证图片或提取的文字发送给配置的服务；用户核对结果后再提交报销，识别不会自动审核或付款。</v-alert>
        <v-switch v-model="form.enabled" label="启用发票识别" color="primary" inset hide-details class="mb-4" :disabled="busy" />
        <v-select v-model="form.inputMode" :items="inputModes" label="识别方式" :disabled="busy" :hint="form.inputMode === 'text' ? '先从电子 PDF 中提取文字，再交给模型整理。扫描件或照片不适用，需要选择图片识别。' : '将图片或 PDF 页面交给视觉模型识别，请确认服务商支持图片输入。'" persistent-hint class="mb-3" />
        <v-btn type="button" variant="text" size="small" prepend-icon="mdi-auto-fix" class="mb-3" :disabled="busy" @click="useDeepSeekPreset">使用 DeepSeek Flash 图片识别预设</v-btn>
        <v-text-field v-model="form.endpoint" label="接口地址" placeholder="https://api.example.com/v1/chat/completions" type="url" autocomplete="off" maxlength="2048" :disabled="busy" hint="填写完整的 HTTPS Chat Completions 地址，以服务商文档为准。" persistent-hint class="mb-3" />
        <v-text-field v-model="form.model" label="模型名称" placeholder="填写当前账号可用的模型 ID" autocomplete="off" maxlength="200" :disabled="busy" :hint="form.inputMode === 'text' ? '填写服务商提供的准确模型 ID；PDF 文字识别可使用纯文本模型。' : '填写服务商提供的准确模型 ID，并确认该模型支持图片输入。'" persistent-hint class="mb-3" />
        <v-text-field v-model="form.apiKey" type="password" label="API Key" autocomplete="new-password" :disabled="busy || clearApiKey" :hint="settings?.apiKeySet ? '已保存密钥。留空保留原密钥，填写新密钥则替换；不会返回已保存的密钥。' : '尚未保存密钥。密钥仅用于服务器请求识别服务。'" persistent-hint />
        <v-checkbox v-if="settings?.apiKeySet" v-model="clearApiKey" label="清除已保存的密钥" density="compact" hide-details :disabled="busy" class="mb-3" />
        <p v-if="clearApiKey" class="recognition-hint">清除密钥前请关闭发票识别；保存后可重新填写新密钥。</p>
        <div class="d-flex justify-end mt-5"><v-btn type="submit" color="primary" variant="tonal" prepend-icon="mdi-content-save-outline" :loading="saving" :disabled="busy">保存识别设置</v-btn></div>
      </template>
    </v-card>
  </form>
</template>

<style scoped>
.recognition-settings { max-width: 850px; margin-bottom: 22px; scroll-margin-top: 90px; }
.recognition-card { padding: 27px 30px; }
.recognition-heading { display: flex; gap: 14px; align-items: center; margin-bottom: 24px; }
.recognition-heading h2 { font-size: 16px; margin: 0 0 6px; }
.recognition-heading p, .recognition-hint { font-size: 13px; color: #757575; margin: 0; line-height: 1.7; }
@media (max-width: 600px) { .recognition-card { padding: 22px 18px; } }
</style>
