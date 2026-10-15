<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from "vue";
import type { MechanicalProblem } from "../lib/learning";
import FileUpload from "./FileUpload.vue";

const props = defineProps<{
  modelValue: MechanicalProblem;
  disabled?: boolean;
  allowDrawing?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [MechanicalProblem];
  busy: [boolean];
}>();
const uploading = ref(false);
const locked = computed(() => props.disabled || uploading.value);
const choices = computed(() =>
  ["single_choice", "multiple_choice"].includes(props.modelValue.type),
);
const types = computed(() => [
  { title: "单选题 · 自动判定", value: "single_choice" },
  { title: "多选题 · 自动判定", value: "multiple_choice" },
  { title: "填空题 · 自动判定", value: "fill_blank" },
  ...(props.allowDrawing !== false ? [{ title: "图纸练习 · 无需提交", value: "drawing" }] : []),
]);
const optionLabel = (index: number) => String.fromCharCode(65 + index);

function update<K extends keyof MechanicalProblem>(
  key: K,
  value: MechanicalProblem[K],
) {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
function changeType(type: MechanicalProblem["type"]) {
  if (type === props.modelValue.type) return;
  const wasChoice = choices.value;
  const isChoice = type === "single_choice" || type === "multiple_choice";
  emit("update:modelValue", {
    ...props.modelValue,
    type,
    options: isChoice
      ? wasChoice
        ? [...props.modelValue.options]
        : ["", "", "", ""]
      : [],
    answers: isChoice && wasChoice
      ? type === "single_choice"
        ? props.modelValue.answers.slice(0, 1)
        : [...props.modelValue.answers]
      : type === "fill_blank"
        ? [""]
        : [],
    blankCount: type === "fill_blank" ? 1 : 0,
    answerFileIds: type === "drawing" ? props.modelValue.answerFileIds : [],
  });
}
function changeOption(index: number, value: string) {
  update("options", props.modelValue.options.map((option, i) =>
    i === index ? value : option,
  ));
}
function removeOption(index: number) {
  emit("update:modelValue", {
    ...props.modelValue,
    options: props.modelValue.options.filter((_, i) => i !== index),
    answers: props.modelValue.answers
      .filter((value) => Number(value) !== index)
      .map((value) => Number(value) > index ? String(Number(value) - 1) : value),
  });
}
function toggleAnswer(index: number, checked: boolean) {
  const value = String(index);
  update("answers", checked
    ? [...new Set([...props.modelValue.answers, value])].sort((a, b) => Number(a) - Number(b))
    : props.modelValue.answers.filter((answer) => answer !== value),
  );
}
function changeBlank(index: number, value: string) {
  update("answers", props.modelValue.answers.map((answer, i) =>
    i === index ? value : answer,
  ));
}
function setBlanks(answers: string[]) {
  emit("update:modelValue", { ...props.modelValue, answers, blankCount: answers.length });
}
function onBusy(value: boolean) {
  uploading.value = value;
  emit("busy", value);
}
onBeforeUnmount(() => {
  if (uploading.value) emit("busy", false);
});
</script>

<template>
  <section class="mechanical-fields">
    <div class="fields-heading">
      <v-icon color="primary" size="21">mdi-clipboard-check-outline</v-icon>
      <h3>题型与标准答案</h3>
    </div>
    <v-select
      :model-value="modelValue.type"
      :items="types"
      label="题型 *"
      :disabled="locked"
      @update:model-value="changeType($event)"
    />

    <template v-if="choices">
      <p class="field-help">填写选项，并勾选标准答案。{{ modelValue.type === 'single_choice' ? '单选题只能选择一个正确选项。' : '多选题需选全所有正确选项才判定正确。' }}</p>
      <v-radio-group
        v-if="modelValue.type === 'single_choice'"
        :model-value="modelValue.answers[0]"
        :disabled="locked"
        hide-details
        aria-label="单选题标准答案"
        @update:model-value="update('answers', $event === null ? [] : [$event])"
      >
        <div v-for="(option, index) in modelValue.options" :key="index" class="option-row">
          <v-radio :value="String(index)" :aria-label="`设 ${optionLabel(index)} 为正确选项`" color="primary" />
          <v-text-field
            :model-value="option"
            :label="`选项 ${optionLabel(index)} *`"
            :disabled="locked"
            density="compact"
            hide-details="auto"
            @update:model-value="changeOption(index, $event)"
          />
          <v-btn icon="mdi-close" size="small" variant="text" :disabled="locked || modelValue.options.length <= 2" :aria-label="`删除选项 ${optionLabel(index)}`" @click="removeOption(index)" />
        </div>
      </v-radio-group>
      <template v-else>
        <div v-for="(option, index) in modelValue.options" :key="index" class="option-row">
          <v-checkbox-btn :model-value="modelValue.answers.includes(String(index))" :disabled="locked" :aria-label="`设 ${optionLabel(index)} 为正确选项`" color="primary" @update:model-value="toggleAnswer(index, !!$event)" />
          <v-text-field :model-value="option" :label="`选项 ${optionLabel(index)} *`" :disabled="locked" density="compact" hide-details="auto" @update:model-value="changeOption(index, $event)" />
          <v-btn icon="mdi-close" size="small" variant="text" :disabled="locked || modelValue.options.length <= 2" :aria-label="`删除选项 ${optionLabel(index)}`" @click="removeOption(index)" />
        </div>
      </template>
      <v-btn variant="text" color="primary" size="small" prepend-icon="mdi-plus" :disabled="locked || modelValue.options.length >= 20" @click="update('options', [...modelValue.options, ''])">添加选项</v-btn>
    </template>

    <template v-else-if="modelValue.type === 'fill_blank'">
      <p class="field-help">按题面中的空格顺序填写标准答案。判定时忽略首尾空格，区分大小写；所有空均正确才判定通过。</p>
      <div v-for="(answer, index) in modelValue.answers" :key="index" class="blank-row">
        <v-text-field :model-value="answer" :label="`第 ${index + 1} 空标准答案 *`" :disabled="locked" density="compact" hide-details="auto" @update:model-value="changeBlank(index, $event)" />
        <v-btn icon="mdi-close" size="small" variant="text" :disabled="locked || modelValue.answers.length <= 1" :aria-label="`删除第 ${index + 1} 空`" @click="setBlanks(modelValue.answers.filter((_, i) => i !== index))" />
      </div>
      <v-btn variant="text" color="primary" size="small" prepend-icon="mdi-plus" :disabled="locked || modelValue.answers.length >= 10" @click="setBlanks([...modelValue.answers, ''])">添加空格</v-btn>
    </template>

    <v-alert v-else type="info" variant="tonal" density="compact" class="mb-4">图纸放在题面附件中，成员可下载后自行练习，无需提交作品。参考答案和答案文件均为选填。</v-alert>
    <v-textarea
      :model-value="modelValue.explanation"
      :label="modelValue.type === 'drawing' ? '参考答案 / 练习提示（选填）' : '答案解析（选填，提交后显示）'"
      :disabled="locked"
      rows="3"
      auto-grow
      class="mt-4"
      @update:model-value="update('explanation', $event)"
    />
    <FileUpload
      v-if="modelValue.type === 'drawing'"
      :model-value="modelValue.answerFileIds"
      label="参考答案文件（选填，成员可下载）"
      :disabled="locked"
      @update:model-value="update('answerFileIds', $event)"
      @busy="onBusy"
    />
  </section>
</template>

<style scoped>
.mechanical-fields { border: 1px solid #e0e6ec; border-radius: 5px; padding: 18px; margin: 18px 0; min-width: 0; }
.fields-heading { display: flex; align-items: center; gap: 8px; margin-bottom: 18px; }
.fields-heading h3 { font-size: 15px; font-weight: 500; }
.field-help { font-size: 13px; color: #677887; line-height: 1.7; margin-bottom: 12px; }
.option-row, .blank-row { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; width: 100%; }
.option-row > :deep(.v-input), .blank-row > :deep(.v-input) { flex: 1; min-width: 0; }
.option-row > :deep(.v-selection-control) { flex: 0 0 40px; }
@media (max-width: 600px) { .mechanical-fields { padding: 12px; } .option-row, .blank-row { gap: 3px; } }
</style>
