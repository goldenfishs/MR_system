<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { api } from "../lib/api";
import { hasPermission } from "../lib/permissions";
import type { MechanicalPracticeResult } from "../lib/learning";
import type { Submission, Task } from "../lib/types";
import AttachmentList from "./AttachmentList.vue";

const props = defineProps<{ task: Task; canPractice: boolean; disabled?: boolean }>();
const emit = defineEmits<{ saved: [Submission]; busy: [boolean] }>();
const problem = computed(() => props.task.mechanicalProblem);
const singleChoice = ref("");
const multipleChoice = ref<string[]>([]);
const fillAnswers = ref<string[]>([]);
const saving = ref(false);
const error = ref("");
const result = ref<MechanicalPracticeResult | null>(null);
const answers = computed(() => problem.value?.type === "single_choice"
  ? singleChoice.value ? [singleChoice.value] : []
  : problem.value?.type === "multiple_choice" ? multipleChoice.value : fillAnswers.value);
const answered = computed(() => answers.value.length > 0 && answers.value.every((answer) => answer.trim() !== ""));
const locked = computed(() => props.disabled || saving.value || !props.canPractice);
const readOnlyNotice = computed(() => {
  if (hasPermission("tasks", props.task.direction))
    return "管理预览：可查看题目与作答形式，不会记录练习成绩。";
  const now = Date.now();
  if (props.task.locked || props.task.startsAt && Date.parse(props.task.startsAt) > now)
    return "本题尚未开放，请在开放时间后作答。";
  if (props.task.dueAt && Date.parse(props.task.dueAt) < now)
    return "本题已截止，可以继续浏览题目，暂不能提交答案。";
  return "当前账号可以浏览题目，暂不能提交练习答案。";
});
const optionLabel = (index: number) => String.fromCharCode(65 + index);
let requestGeneration = 0;

watch(() => props.task.id, () => {
  requestGeneration++;
  singleChoice.value = "";
  multipleChoice.value = [];
  fillAnswers.value = Array.from({ length: problem.value?.blankCount || 1 }, () => "");
  result.value = null;
  error.value = "";
  saving.value = false;
  emit("busy", false);
}, { immediate: true });
watch([singleChoice, multipleChoice, fillAnswers], () => {
  result.value = null;
  error.value = "";
}, { deep: true });

function answerText(answer: string, index: number) {
  if (problem.value?.type === "fill_blank") return `第 ${index + 1} 空：${answer}`;
  const optionIndex = Number(answer);
  return `${optionLabel(optionIndex)}. ${problem.value?.options[optionIndex] || ""}`;
}
async function submit() {
  if (locked.value || !props.canPractice || !answered.value || problem.value?.type === "drawing") return;
  const current = ++requestGeneration;
  saving.value = true;
  error.value = "";
  emit("busy", true);
  try {
    const data = await api.post<{ item: Submission; result: MechanicalPracticeResult }>(
      `/tasks/${encodeURIComponent(props.task.id)}/practice`,
      { answers: [...answers.value] },
    );
    if (current !== requestGeneration) return;
    result.value = data.result;
    emit("saved", data.item);
  } catch (e) {
    if (current === requestGeneration) error.value = (e as Error).message;
  } finally {
    if (current === requestGeneration) {
      saving.value = false;
      emit("busy", false);
    }
  }
}
onBeforeUnmount(() => {
  requestGeneration++;
  emit("busy", false);
});
</script>

<template>
  <section v-if="problem" class="mechanical-practice">
    <template v-if="problem.type === 'drawing'">
      <div class="practice-heading"><v-icon color="primary">mdi-drawing</v-icon><h3>图纸自主练习</h3></div>
      <p class="practice-help">下载题面图纸后自行练习，无需上传作品或提交。</p>
      <details v-if="problem.explanation || problem.answerFileIds?.length" class="drawing-answer">
        <summary>查看参考答案</summary>
        <p v-if="problem.explanation" class="answer-explanation">{{ problem.explanation }}</p>
        <AttachmentList :ids="problem.answerFileIds" />
      </details>
      <p v-else class="practice-help mb-0">本题未提供参考答案，可根据题面自行练习。</p>
    </template>
    <template v-else>
      <div class="practice-heading"><v-icon color="primary">mdi-pencil-box-outline</v-icon><h3>在线练习</h3><v-chip size="x-small">{{ problem.type === 'single_choice' ? '单选题' : problem.type === 'multiple_choice' ? '多选题' : '填空题' }}</v-chip></div>
      <p class="practice-help">{{ problem.type === 'fill_blank' ? '按顺序填写答案，忽略首尾空格，区分大小写。所有空均正确才判定通过。' : problem.type === 'multiple_choice' ? '选出所有正确选项，全部选对才判定通过。' : '选择一个正确选项，提交后查看判定结果与解析。' }}</p>
      <v-alert v-if="!canPractice" type="info" variant="tonal" density="compact" class="mb-4">{{ readOnlyNotice }}</v-alert>
      <div v-if="!canPractice" class="readonly-problem">
        <ol v-if="problem.type !== 'fill_blank'" class="readonly-options" aria-label="题目选项">
          <li v-for="(option, index) in problem.options" :key="index"><span>{{ optionLabel(index) }}.</span><span>{{ option }}</span></li>
        </ol>
        <p v-else class="readonly-blanks">本题共 {{ problem.blankCount || 1 }} 空，请结合题面自行思考。</p>
      </div>
      <form v-else @submit.prevent="submit">
        <v-radio-group v-if="problem.type === 'single_choice'" v-model="singleChoice" :disabled="locked" aria-label="选择答案" hide-details class="practice-options">
          <v-radio v-for="(option, index) in problem.options" :key="index" :value="String(index)" :label="`${optionLabel(index)}. ${option}`" color="primary" />
        </v-radio-group>
        <div v-else-if="problem.type === 'multiple_choice'" class="practice-options">
          <v-checkbox v-for="(option, index) in problem.options" :key="index" v-model="multipleChoice" :value="String(index)" :label="`${optionLabel(index)}. ${option}`" :disabled="locked" color="primary" hide-details density="compact" />
        </div>
        <div v-else class="blank-answers">
          <v-text-field v-for="(_, index) in fillAnswers" :key="index" v-model="fillAnswers[index]" :label="`第 ${index + 1} 空`" :disabled="locked" autocomplete="off" hide-details="auto" />
        </div>
        <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-4">{{ error }}</v-alert>
        <v-btn v-if="canPractice" type="submit" color="primary" variant="tonal" prepend-icon="mdi-check" :loading="saving" :disabled="disabled || !answered">提交并判定</v-btn>
      </form>
      <div v-if="result" class="practice-result" role="status" aria-live="polite">
        <div class="result-heading"><v-chip :color="result.correct ? 'success' : 'warning'" size="small">{{ result.correct ? '回答正确' : '回答错误' }}</v-chip><strong>{{ result.score }} / {{ result.maxScore }} 分</strong></div>
        <h4>正确答案</h4>
        <ul class="correct-answers"><li v-for="(answer, index) in result.answers" :key="index">{{ answerText(answer, index) }}</li></ul>
        <template v-if="result.explanation"><h4>答案解析</h4><p class="answer-explanation">{{ result.explanation }}</p></template>
        <p class="practice-help mb-0">可以修改答案后再次练习。</p>
      </div>
    </template>
  </section>
</template>

<style scoped>
.mechanical-practice { border: 1px solid #dce5ed; background: #fafcfe; border-radius: 5px; padding: 18px; margin: 18px 0; min-width: 0; }
.practice-heading, .result-heading { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; margin-bottom: 10px; }
.practice-heading h3 { font-size: 16px; font-weight: 500; }
.practice-help { font-size: 13px; line-height: 1.7; color: #677887; margin-bottom: 16px; }
.practice-options { margin-bottom: 18px; }
.practice-options :deep(.v-selection-control) { align-items: flex-start; }
.practice-options :deep(.v-label) { white-space: normal; overflow-wrap: anywhere; line-height: 1.6; min-height: 40px; padding: 7px 0; opacity: 1; }
.readonly-options { list-style: none; padding: 0; margin: 0; }
.readonly-options li { display: flex; gap: 10px; padding: 10px 0; line-height: 1.7; overflow-wrap: anywhere; }
.readonly-options li > span:first-child { flex-shrink: 0; }
.readonly-blanks { margin: 0; line-height: 1.7; }
.blank-answers { display: grid; gap: 14px; margin-bottom: 18px; }
.practice-result { border-top: 1px solid #dce5ed; padding-top: 18px; margin-top: 20px; }
.practice-result strong, .practice-result h4 { font-size: 14px; font-weight: 500; }
.practice-result h4 { margin: 14px 0 7px; }
.correct-answers { list-style: none; padding: 0; font-size: 14px; overflow-wrap: anywhere; }
.correct-answers li { margin-bottom: 5px; white-space: pre-wrap; }
.answer-explanation { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; font-size: 14px; margin: 12px 0; }
.drawing-answer summary { cursor: pointer; color: #1976d2; font-size: 14px; }
@media (max-width: 600px) { .mechanical-practice { padding: 12px; } }
</style>
