<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { api } from "../lib/api";
import type { Task } from "../lib/types";
import type { JudgeRun, JudgeService } from "../lib/platform";
import FileUpload from "./FileUpload.vue";
const props = defineProps<{
  task: Task;
  modelValue?: string;
  disabled?: boolean;
  readonly?: boolean;
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
    service.value?.languages.filter(
      (l) =>
        !props.task.judge?.languageIds.length ||
        props.task.judge.languageIds.includes(l.id),
    ) || [],
);
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
      if (!props.readonly) emit("run", run.value);
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
  if (!source.value[0] || !language.value) {
    error.value = "请选择编程语言并上传一个源码文件。";
    return;
  }
  submitting.value = true;
  error.value = "";
  stop();
  const current = token;
  try {
    const { item } = await api.post<{ item: JudgeRun }>(
      `/tasks/${props.task.id}/judge`,
      { sourceFileId: source.value[0], languageId: language.value },
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
    run.value &&
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
      <h3>代码自动评测</h3>
      <v-chip size="x-small"
        >{{
          task.judge?.totalCases || task.judge?.testCases.length || 0
        }}
        个测试用例</v-chip
      >
    </div>
    <p class="text-caption text-medium-emphasis mb-4">
      按指定测试用例编译运行。自动评测分数供验收参考，最终成绩由管理员审核。
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
        >在线编译服务尚未配置，暂不能完成本题的自动评测。请联系管理员配置服务；可以先准备源码和完成说明。</v-alert
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
          :disabled="disabled || working || !service?.configured"
        /><FileUpload
          v-model="source"
          label="源码文件（UTF-8）"
          :max-bytes="65536"
          accept=".c,.cpp,.cc,.cxx,.py,.java,.js,.ts,.go,.rs,.cs,.kt,.rb,.swift,.php,.h,.hpp,.lua,.pas,.hs,.scala,.m,.ml,.pl,.sh,.txt"
          single
          :disabled="disabled || working || !service?.configured"
          @busy="uploading = $event"
        /><v-btn
          color="primary"
          variant="tonal"
          prepend-icon="mdi-play"
          :loading="working"
          :disabled="
            disabled || uploading || !service?.configured || !source.length
          "
          @click="start"
          >编译并运行测试</v-btn
        ></template
      >
      <div v-if="run" class="judge-result">
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
