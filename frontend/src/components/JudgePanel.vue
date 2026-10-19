<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { api } from "../lib/api";
import type { Task } from "../lib/types";
import type { JudgeRun, JudgeService } from "../lib/platform";
import CodeEditor from "./CodeEditor.vue";
import { auth } from "../lib/auth";
import type { FileAsset } from "../lib/platform";
const props = defineProps<{
  task: Task;
  modelValue?: string;
  questionId?: string;
  disabled?: boolean;
  readonly?: boolean;
  preview?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [string];
  busy: [boolean];
  run: [JudgeRun];
}>();
const service = ref<JudgeService | null>(null),
  source = ref<string[]>([]),
  language = ref<number>(),
  run = ref<JudgeRun | null>(null),
  loading = ref(false),
  uploading = ref(false),
  submitting = ref(false),
  error = ref("");
const working = computed(
  () =>
    submitting.value ||
    run.value?.status === "queued" ||
    run.value?.status === "running",
);
watch(
  () => working.value || uploading.value,
  (value) => emit("busy", value),
);
const languages = computed(
  () =>
    (service.value?.configured ? service.value.languages : [{ id: 50, name: "C (GCC 9.2.0)" }, { id: 54, name: "C++ (GCC 9.2.0)" }, { id: 71, name: "Python 3" }]).filter(
      (l) =>
        (/^(?:C(?:\+\+)?(?:\s|$)|Python(?:\s|$))/i.test(l.name)) && (!props.task.judge?.languageIds.length ||
        props.task.judge.languageIds.includes(l.id)),
    ) || [],
);
const code = ref("");
const languageName = computed(() => languages.value.find(l => l.id === language.value)?.name || "");
const pythonLanguage = computed(() => /^python/i.test(languageName.value));
const cLanguage = computed(() => /^C(?:\s|$)/i.test(languageName.value));
const defaultCode = computed(() => pythonLanguage.value ? "# Python 3\n" : cLanguage.value
  ? "#include <stdio.h>\n\nint main(void) {\n    return 0;\n}\n"
  : "#include <bits/stdc++.h>\nusing namespace std;\n\nint main() {\n    return 0;\n}\n");
const draftKey = () => `code-draft:${auth.user?.id}:${props.task.id}:${props.questionId || "task"}:${props.preview ? "preview" : "answer"}:${language.value}`;
function restoreDraft() {
  try { code.value = localStorage.getItem(draftKey()) ?? defaultCode.value; } catch { code.value = defaultCode.value; }
}
watch(language, restoreDraft, { flush: "sync" });
watch(code, () => {
  try { localStorage.setItem(draftKey(), code.value); } catch { /* Optional local draft. */ }
  if (!submitting.value && !loading.value && run.value) { stop(); run.value = null; emit("update:modelValue", ""); }
}, { flush: "sync" });
const resultLabel = (status: string) =>
  ({
    Accepted: "通过",
    "Wrong Answer": "答案错误",
    "Compilation Error": "编译错误",
    "Time Limit Exceeded": "超时",
    "Runtime Error": "运行错误",
    queued: "等待评测",
    running: "正在评测",
    completed: "已完成",
    error: "评测失败",
    accepted: "通过",
    wrong_answer: "答案错误",
    compilation_error: "编译错误",
    time_limit_exceeded: "超时",
  })[status] || status;
let timer: ReturnType<typeof setTimeout> | undefined;
let token = 0;
function stop() {
  clearTimeout(timer);
  token++;
}
async function refresh() {
  if (!run.value) return;
  const id = run.value.id;
  const current = token;
  try {
    const { item } = await api.get<{ item: JudgeRun }>(`/judge-runs/${id}`);
    if (current !== token) return;
    error.value = "";
    run.value = item;
    if (item.status === "completed" && !props.readonly) emit("run", item);
    if (item.status === "queued" || item.status === "running")
      timer = setTimeout(refresh, 1500);
  } catch (e) {
    if (current === token) error.value = (e as Error).message;
  }
}
async function load() {
  stop();
  const current = token;
  loading.value = true;
  error.value = "";
  run.value = null;
  source.value = [];
  service.value = null;
  language.value = languages.value[0]?.id;
  restoreDraft();
  try {
    if (!props.readonly) {
      try {
        const configuration = await api.get<JudgeService>("/judge/config");
        if (current !== token) return;
        service.value = configuration;
        language.value = languages.value[0]?.id;
      } catch (e) {
        error.value = (e as Error).message;
      }
    }
    if (props.modelValue) {
      const data = await api.get<{ item: JudgeRun }>(
        `/judge-runs/${props.modelValue}`,
      );
      if (current !== token) return;
      run.value = data.item;
      source.value = [run.value.sourceFileId];
      language.value = run.value.languageId;
      if (!props.readonly) {
        const response = await fetch(`/api/files/${encodeURIComponent(run.value.sourceFileId)}/download`, { credentials: "same-origin" });
        if (!response.ok) throw new Error("无法读取上次运行的源码，请重新编辑。");
        const text = await response.text();
        if (current !== token) return;
        code.value = text;
        emit("run", run.value);
      }
      if (run.value.status === "queued" || run.value.status === "running")
        timer = setTimeout(refresh, 1000);
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    if (current === token) loading.value = false;
  }
}
async function start() {
  if (props.disabled || working.value) return;
  if (!code.value.trim() || !language.value || new TextEncoder().encode(code.value).length > 65536) {
    error.value = "请选择语言并输入 64 KB 以内的代码。";
    return;
  }
  submitting.value = true;
  error.value = "";
  stop();
  const current = token;
  try {
    const form = new FormData();
    form.append("file", new File([code.value], pythonLanguage.value ? "main.py" : cLanguage.value ? "main.c" : "main.cpp", { type: "text/plain" }));
    const uploaded = await api.upload<{ item: FileAsset }>("/files", form);
    if (current !== token) return;
    source.value = [uploaded.item.id];
    const { item } = await api.post<{ item: JudgeRun }>(
      `/tasks/${props.task.id}/judge`,
      { sourceFileId: source.value[0], languageId: language.value, ...(props.preview ? { preview: true } : {}), ...(props.questionId ? { questionId: props.questionId } : {}) },
    );
    if (current !== token) return;
    run.value = item;
    emit("run", item);
    emit("update:modelValue", item.id);
    if (item.status === "queued" || item.status === "running")
      timer = setTimeout(refresh, 1000);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    if (current === token) submitting.value = false;
  }
}
watch([() => source.value[0], language], () => {
  if (
    !submitting.value && !loading.value && run.value &&
    (source.value[0] !== run.value.sourceFileId ||
      language.value !== run.value.languageId)
  ) {
    stop();
    run.value = null;
    emit("update:modelValue", "");
  }
});
watch(() => props.task.id, load, { immediate: true });
onBeforeUnmount(() => {
  stop();
  emit("busy", false);
});
</script>
<template>
  <section class="judge-panel">
    <div class="judge-heading">
      <v-icon color="primary">mdi-code-braces</v-icon>
      <h3>在线编程</h3>
      <v-chip size="x-small"
        >{{
          task.judge?.totalCases || task.judge?.testCases.length || 0
        }}
        个测试用例</v-chip
      >
    </div>
    <p class="text-caption text-medium-emphasis mb-4">
      选择语言，直接编写代码并运行题目测试用例。代码草稿会保存在当前浏览器。
    </p>
    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      density="compact"
      class="mb-4"
      >{{ error
      }}<v-btn variant="text" size="small" @click="run ? refresh() : load()"
        >重试</v-btn
      ></v-alert
    ><v-progress-linear v-if="loading" indeterminate /><template v-else
      ><v-alert
        v-if="!service?.configured && !readonly"
        type="info"
        variant="tonal"
        class="mb-4"
        >在线编译服务暂不可用，可以先编辑代码，恢复后再运行。</v-alert
      ><template v-if="!readonly"
        ><details v-if="task.judge?.testCases.length" class="mb-4">
          <summary class="text-body-2 text-primary">查看公开测试用例</summary>
          <div
            v-for="(example, index) in task.judge.testCases.filter(
              (c) => !c.hidden,
            )"
            :key="index"
            class="case-result"
          >
            <strong>示例 {{ index + 1 }}</strong>
            <div>输入</div>
            <pre>{{ example.input || "(无输入)" }}</pre>
            <div>预期输出</div>
            <pre>{{ example.expectedOutput || "(空输出)" }}</pre>
          </div>
        </details>
        <v-select
          v-model="language"
          :items="languages"
          item-title="name"
          item-value="id"
          label="编程语言"
          :disabled="disabled || working"
        /><CodeEditor v-model="code" :python="pythonLanguage" :disabled="disabled || working" />
        <v-btn
          color="primary"
          variant="tonal"
          prepend-icon="mdi-play"
          :loading="working"
          :disabled="
            disabled || uploading || !service?.configured || !code.trim() || !language
          "
          @click="start"
          >编译并运行测试</v-btn
        ></template
      >
      <div v-if="run" class="judge-result" aria-live="polite"><h3 class="mb-3">编译与运行结果</h3>
        <div class="d-flex align-center flex-wrap ga-3">
          <v-chip
            :color="
              run.status === 'error'
                ? 'error'
                : run.status === 'completed' && run.passed === run.total
                  ? 'success'
                  : 'primary'
            "
            size="small"
            >{{ resultLabel(run.status) }}</v-chip
          ><strong v-if="run.status === 'completed'"
            >{{ run.passed }} / {{ run.total }} 通过 · 自动评测
            {{ run.score }} / {{ task.maxScore }} 分</strong
          >
          <v-btn v-if="readonly && run.sourceFileId" size="small" variant="tonal" prepend-icon="mdi-download" :href="'/api/files/' + encodeURIComponent(run.sourceFileId) + '/download'">下载提交源码</v-btn>
        </div>
        <v-progress-linear v-if="working" indeterminate class="my-3" />
        <p v-if="run.error" class="text-error text-body-2 mt-3">
          {{ run.error }}
        </p>
        <div
          v-for="result in run.results"
          :key="result.index"
          class="case-result"
        >
          <div>
            用例 {{ result.index + 1 }}
            <span v-if="result.hidden">（隐藏用例）</span
            ><v-chip size="x-small" class="ml-2">{{
              resultLabel(result.status)
            }}</v-chip>
          </div>
          <template v-if="!result.hidden">
            <pre v-if="result.compileOutput">{{ result.compileOutput }}</pre>
            <pre v-if="result.stderr">{{ result.stderr }}</pre>
            <pre v-if="result.stdout">{{ result.stdout }}</pre>
          </template>
        </div>
      </div></template
    >
  </section>
</template>
<style scoped>
.judge-panel {
  border: 1px solid #dce5ed;
  background: #fafcfe;
  border-radius: 5px;
  padding: 18px;
  margin: 18px 0;
}
.judge-heading {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-wrap: wrap;
  margin-bottom: 9px;
}
.judge-heading h3 {
  font-size: 16px;
  font-weight: 500;
}
.judge-result {
  margin-top: 20px;
}
.judge-result strong {
  font-size: 13px;
  font-weight: 500;
}
.case-result {
  font-size: 13px;
  border-top: 1px solid #e4e9ee;
  padding-top: 12px;
  margin-top: 12px;
}
.case-result pre {
  background: #eef2f6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  padding: 12px;
  font-size: 12px;
  margin-top: 8px;
  max-height: 250px;
  overflow: auto;
}
</style>
