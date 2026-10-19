<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { api, ApiError } from "../lib/api";
import { auth } from "../lib/auth";
import { completedQuestionCount, questionScoreTotal, questionGroupLabel } from "../lib/mechanical-assignment";
import type { AssignmentAnswer, AssignmentQuestion, AssignmentQuestionType, Submission, Task } from "../lib/types";
import FileUpload from "./FileUpload.vue";
import AttachmentList from "./AttachmentList.vue";
import AssignmentPromptPreview from "./AssignmentPromptPreview.vue";
import JudgePanel from "./JudgePanel.vue";

const props = defineProps<{
  task: Task;
  canManage: boolean;
  canSubmit: boolean;
}>();
const emit = defineEmits<{
  updated: [task: Task];
  notice: [message: string];
}>();
const questions = computed(() => props.task.questions || []);
const maxScore = computed(() => questionScoreTotal(questions.value));
const hasAutomaticQuestions = computed(() => questions.value.some(question => question.autoGrade || question.type === "code"));
const published = computed(() => props.task.status !== "draft");
const submissions = ref<Submission[]>([]);
const latestSubmissions = computed(() => {
  const byUser = new Map<string, Submission>();
  for (const item of submissions.value) {
    const current = byUser.get(item.userId);
    if (!current || item.version > current.version || item.version === current.version && item.createdAt > current.createdAt) byUser.set(item.userId, item);
  }
  return [...byUser.values()].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
});
const mySubmission = ref<Submission | null>(props.task.mySubmission || null);
const preview = ref(false);
const loading = ref(false);
const saving = ref(false);
const uploadCount = ref(0);
const uploadBusyIds = ref(new Set<string>());
const error = ref("");
const savedNotice = ref("");
const answerFiles = reactive<Record<string, string[]>>({});
const answerResponses = reactive<Record<string, string>>({});
const judgeRunIds = reactive<Record<string, string>>({});
const selectedChoices = reactive<Record<string, string[]>>({});
const reviews = reactive<Record<string, Record<string, AssignmentAnswer>>>({});
const reviewBusyId = ref("");

function questionType(question: AssignmentQuestion): AssignmentQuestionType {
  return question.type || "drawing";
}
function questionTypeLabel(question: AssignmentQuestion) {
  return ({ single_choice: "单选题", multiple_choice: "多选题", fill_blank: "填空题", short_answer: "简答题", drawing: "绘图题", code: "在线代码题" })[questionType(question)];
}
function answerComplete(question: AssignmentQuestion, response = "", fileIds: string[] = [], judgeRunId = "") {
  switch (questionType(question)) {
    case "code": return Boolean(judgeRunId);
    case "drawing": return fileIds.length > 0;
    case "short_answer": return Boolean(response.trim()) || fileIds.length > 0;
    case "multiple_choice": {
      try { return (JSON.parse(response) as unknown[]).length > 0; }
      catch { return false; }
    }
    default: return Boolean(response.trim());
  }
}
function responseFor(question: AssignmentQuestion) {
  if (questionType(question) === "multiple_choice") return JSON.stringify(selectedChoices[question.id] || []);
  return answerResponses[question.id] || "";
}
function initializeAnswers() {
  for (const question of questions.value) {
    answerFiles[question.id] = [];
    answerResponses[question.id] = "";
    judgeRunIds[question.id] = "";
    selectedChoices[question.id] = [];
  }
  for (const answer of mySubmission.value?.answers || []) {
    if (answerFiles[answer.questionId]) answerFiles[answer.questionId] = [...answer.fileIds];
    if (answerResponses[answer.questionId] !== undefined) answerResponses[answer.questionId] = answer.response || "";
    if (judgeRunIds[answer.questionId] !== undefined) judgeRunIds[answer.questionId] = answer.judgeRunId || "";
    const question = questions.value.find(item => item.id === answer.questionId);
    if (question && questionType(question) === "multiple_choice" && answer.response) {
      try { selectedChoices[question.id] = JSON.parse(answer.response) as string[]; }
      catch { selectedChoices[question.id] = []; }
    }
  }
}
function setUploadBusy(questionId: string, busy: boolean) {
  const ids = new Set(uploadBusyIds.value);
  if (busy) ids.add(questionId);
  else ids.delete(questionId);
  uploadBusyIds.value = ids;
  uploadCount.value = ids.size;
}
const answerProgress = computed(() => Object.fromEntries(questions.value.map(question => [question.id, { response: responseFor(question), fileIds: answerFiles[question.id] || [], judgeRunId: judgeRunIds[question.id] || "" }])));
const completedCount = computed(() => completedQuestionCount(questions.value, answerProgress.value));
const isFinalized = computed(() => Boolean(mySubmission.value?.finalizedAt) && mySubmission.value?.status !== "changes_requested");
const canEditAnswers = computed(() => props.canSubmit && published.value && !isFinalized.value);
const canInteract = computed(() => props.canManage && preview.value || canEditAnswers.value);
const messageOf = (reason: unknown) => reason instanceof ApiError || reason instanceof Error ? reason.message : "操作失败，请稍后重试";
const answerFor = (submission: Submission | null | undefined, questionId: string) => submission?.answers?.find(answer => answer.questionId === questionId);

async function loadSubmissions() {
  loading.value = true;
  error.value = "";
  try {
    const result = await api.get<{ items: Submission[] }>(`/tasks/${encodeURIComponent(props.task.id)}/submissions`);
    submissions.value = result.items.sort((a, b) => b.version - a.version || b.createdAt.localeCompare(a.createdAt));
    mySubmission.value = result.items.find(item => item.userId === auth.user?.id) || props.task.mySubmission || null;
    initializeAnswers();
  } catch (reason) {
    error.value = messageOf(reason);
  } finally {
    loading.value = false;
  }
}
function ensureReview(submission: Submission) {
  if (reviews[submission.id]) return reviews[submission.id]!;
  const draft: Record<string, AssignmentAnswer> = {};
  for (const question of questions.value) {
    const existing = answerFor(submission, question.id);
    draft[question.id] = {
      questionId: question.id,
      fileIds: [...(existing?.fileIds || [])],
      score: existing?.score ?? 0,
      feedback: existing?.feedback || "",
      status: existing?.status || "pending",
    };
  }
  reviews[submission.id] = draft;
  return draft;
}
function totalFor(submission: Submission) {
  return (submission.answers || []).reduce((sum, answer) => sum + Number(answer.score || 0), 0);
}
function reviewTotal(submission: Submission) {
  const draft = ensureReview(submission);
  return questions.value.reduce((sum, question) => sum + Number(draft[question.id]?.score || 0), 0);
}
function progressFor(submission: Submission) {
  return questions.value.filter(question => {
    const answer = answerFor(submission, question.id);
    return answerComplete(question, answer?.response || "", answer?.fileIds || [], answer?.judgeRunId || "");
  }).length;
}
function uploadLabel(question: AssignmentQuestion) {
  return `第${questions.value.indexOf(question) + 1}题 · 成果文件`;
}

function questionJudgeTask(question: AssignmentQuestion): Task {
  return { ...props.task, maxScore: question.maxScore, judge: question.judge };
}

async function publish() {
  if (!props.canManage || saving.value || published.value) return;
  saving.value = true;
  try {
    const result = await api.post<{ item: Task }>(`/tasks/${encodeURIComponent(props.task.id)}/publish`, {});
    emit("updated", result.item);
    emit("notice", "作业已发布，组内成员现在可以在页面逐题作答。");
  } catch (reason) {
    emit("notice", messageOf(reason));
  } finally {
    saving.value = false;
  }
}
async function saveDraft(finalize = false) {
  if (!canEditAnswers.value || saving.value || uploadCount.value) return;
  if (finalize && completedCount.value !== questions.value.length) {
    emit("notice", "请完成所有题目后，再统一提交。");
    return;
  }
  saving.value = true;
  savedNotice.value = "";
  const answers = questions.value.map(question => ({ questionId: question.id, response: responseFor(question), fileIds: [...(answerFiles[question.id] || [])], ...(questionType(question) === "code" && judgeRunIds[question.id] ? { judgeRunId: judgeRunIds[question.id] } : {}) }));
  const allFileIds = answers.flatMap(answer => answer.fileIds);
  if (new Set(allFileIds).size !== allFileIds.length) {
    saving.value = false;
    emit("notice", "同一个文件不能关联到多道题，请为不同题目分别上传或选择文件。");
    return;
  }
  try {
    const result = await api.post<{ item: Submission }>(`/tasks/${encodeURIComponent(props.task.id)}/submission-draft`, { answers });
    mySubmission.value = result.item;
    submissions.value = [result.item, ...submissions.value.filter(item => item.id !== result.item.id)];
    if (finalize) {
      const finalized = await api.post<{ item: Submission }>(`/tasks/${encodeURIComponent(props.task.id)}/submission-finalize`, {});
      mySubmission.value = finalized.item;
      submissions.value = [finalized.item, ...submissions.value.filter(item => item.id !== finalized.item.id)];
      emit("notice", finalized.item.status === "approved"
        ? `作业已完成评阅，得分 ${totalFor(finalized.item)} / ${maxScore.value}。`
        : hasAutomaticQuestions.value ? "作业已提交，客观题已自动判分，其余题目等待组长或管理员评阅。" : "作业已统一提交，等待组长或管理员逐题评阅。");
    } else {
      savedNotice.value = "进度已保存，可以继续补交或替换每题文件。";
    }
  } catch (reason) {
    emit("notice", messageOf(reason));
  } finally {
    saving.value = false;
  }
}
function responseSummary(question: AssignmentQuestion, answer: AssignmentAnswer | undefined) {
  if (!answer) return "尚未作答";
  if (questionType(question) === "code") return answer.judgeRunId ? "在线判题记录：" + answer.judgeRunId : "尚未运行代码";
  if (questionType(question) === "single_choice" || questionType(question) === "multiple_choice") {
    let choices: string[] = [];
    if (questionType(question) === "single_choice" && answer.response) choices = [answer.response];
    if (questionType(question) === "multiple_choice" && answer.response) {
      try { choices = JSON.parse(answer.response) as string[]; }
      catch { choices = []; }
    }
    return choices.length
      ? choices.map(value => {
        const index = Number(value);
        return `${String.fromCharCode(65 + index)}. ${question.options?.[index] || ""}`;
      }).join("；")
      : "尚未选择";
  }
  return answer.response || (answer.fileIds.length ? "已上传绘图 / 成果文件" : "尚未作答");
}
async function saveReview(submission: Submission) {
  if (!props.canManage || reviewBusyId.value || !submission.finalizedAt) return;
  const draft = ensureReview(submission);
  for (const question of questions.value) {
    const score = Number(draft[question.id]?.score);
    if (!Number.isFinite(score) || score < 0 || score > question.maxScore) {
      emit("notice", `第 ${questions.value.indexOf(question) + 1} 题分数须在 0–${question.maxScore} 之间。`);
      return;
    }
    if (draft[question.id]?.status === "changes_requested" && !String(draft[question.id]?.feedback || "").trim()) {
      emit("notice", `第 ${questions.value.indexOf(question) + 1} 题标记为需要修改时，请填写本题反馈。`);
      return;
    }
  }
  reviewBusyId.value = submission.id;
  try {
    const questionReviews = questions.value.filter(question => {
      const changed = draft[question.id];
      const existing = answerFor(submission, question.id);
      if (existing?.autoGraded && changed?.status === existing.status && changed?.score === existing.score && (changed?.feedback || "") === (existing.feedback || "")) return false;
      const status = changed?.status;
      return status === "approved" || status === "changes_requested";
    }).map(question => ({
      questionId: question.id,
      score: Number(draft[question.id]?.score || 0),
      feedback: String(draft[question.id]?.feedback || ""),
      status: draft[question.id]!.status!,
    }));
    if (!questionReviews.length) {
      emit("notice", "请先选择至少一道题的评阅状态，再保存评分。");
      return;
    }
    const result = await api.patch<{ item: Submission }>(`/submissions/${encodeURIComponent(submission.id)}`, { questionReviews });
    submissions.value = submissions.value.map(item => item.id === result.item.id ? result.item : item);
    if (mySubmission.value?.id === result.item.id) mySubmission.value = result.item;
    delete reviews[submission.id];
    emit("notice", `逐题评分已保存，自动总分：${totalFor(result.item)} / ${maxScore.value}。`);
  } catch (reason) {
    emit("notice", messageOf(reason));
  } finally {
    reviewBusyId.value = "";
  }
}

onMounted(() => {
  initializeAnswers();
  if (props.canManage || props.canSubmit) void loadSubmissions();
});
watch(() => questions.value.map(question => question.id).join("|"), initializeAnswers);
</script>

<template>
  <section class="mechanical-assignment">
      <v-alert v-if="!published && canManage" type="warning" variant="tonal" class="mb-4">
      这份作业尚未发布，组内成员暂时不可见。确认题目、附件和分值后手动发布。
      <template #append><v-btn color="warning" variant="flat" :loading="saving" @click="publish">确认并发布</v-btn></template>
    </v-alert>
    <v-alert v-else-if="!published" type="info" variant="tonal" class="mb-4">该作业尚未发布。</v-alert>
      <div class="assignment-summary">
      <div><span>题目数量</span><strong>{{ questions.length }}</strong></div>
      <div><span>{{ canManage ? '作业总分' : '完成进度' }}</span><strong>{{ canManage ? `${maxScore} 分` : `${completedCount} / ${questions.length} 题` }}</strong></div>
      <div v-if="mySubmission"><span>提交状态</span><strong>{{ mySubmission.status === 'changes_requested' ? '需修改后重交' : mySubmission.status === 'approved' ? '已完成评阅' : isFinalized ? '已提交 · 待评阅' : '草稿已保存' }}</strong></div>
      <div v-if="mySubmission?.finalizedAt"><span>{{ mySubmission.status === 'approved' ? '作业成绩' : '已评题目得分' }}</span><strong>{{ totalFor(mySubmission) }} / {{ maxScore }} 分</strong></div>
    </div>

    <v-btn-toggle v-if="canManage" v-model="preview" mandatory color="primary" variant="outlined" :disabled="uploadCount > 0">
      <v-btn :value="false">成员作业与评阅</v-btn><v-btn :value="true" prepend-icon="mdi-play-circle-outline">试做 / 测试作业</v-btn>
    </v-btn-toggle>
    <v-alert v-if="canManage && preview" type="info" variant="tonal">管理测试：可在发布前或截止后试做题目、运行代码，不产生学生提交或成绩。测试后仍可编辑题目。</v-alert>
    <template v-if="canManage && !preview">
      <div class="assignment-manager-heading">
        <div><h3>成员作业与逐题评阅</h3><p>下载全部已提交成果，逐题打分与反馈，总分由系统自动汇总。</p></div>
        <v-btn :href="`/api/tasks/${encodeURIComponent(task.id)}/submissions/export`" :disabled="!latestSubmissions.some(item => item.finalizedAt)" color="primary" variant="tonal" prepend-icon="mdi-folder-download-outline">批量下载 ZIP</v-btn>
      </div>
      <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}<template #append><v-btn variant="text" @click="loadSubmissions">重试</v-btn></template></v-alert>
      <div v-if="loading" class="assignment-empty"><v-progress-circular indeterminate color="primary" />正在读取成员提交…</div>
      <div v-else-if="!latestSubmissions.length" class="assignment-empty"><v-icon size="32" color="grey">mdi-inbox-outline</v-icon><span>成员提交后会显示在这里。</span></div>
      <v-expansion-panels v-else variant="accordion" class="review-panels">
        <v-expansion-panel v-for="submission in latestSubmissions" :key="submission.id" :value="submission.id">
          <v-expansion-panel-title>
            <div class="member-summary"><strong>{{ submission.userName }}</strong><span>{{ submission.finalizedAt ? `已提交 · ${new Date(submission.finalizedAt).toLocaleString('zh-CN')}` : '进度草稿' }}</span><v-chip size="small" :color="submission.finalizedAt ? submission.status === 'approved' ? 'success' : submission.status === 'changes_requested' ? 'deep-orange' : 'primary' : 'blue-grey'" variant="tonal">{{ submission.finalizedAt ? `${progressFor(submission)} / ${questions.length} 题` : '未最终提交' }}</v-chip><v-chip v-if="submission.finalizedAt && submission.status !== 'pending'" size="small" color="teal" variant="tonal">{{ totalFor(submission) }} / {{ maxScore }} 分</v-chip></div>
          </v-expansion-panel-title>
          <v-expansion-panel-text>
            <div v-for="(question, index) in questions" :key="question.id" class="review-question">
              <div v-if="question.groupId && (index === 0 || questions[index - 1]?.groupId !== question.groupId)" class="assignment-group-heading"><h3>{{ question.groupTitle }}</h3><p>{{ question.groupDescription }}</p></div>
          <div class="question-heading"><div><span>{{ questionGroupLabel(questions, index) }} · {{ questionTypeLabel(question) }} · {{ question.maxScore }} 分</span><h4>{{ question.title }}</h4></div><v-chip v-if="answerFor(submission, question.id)?.status" size="small" :color="answerFor(submission, question.id)?.status === 'approved' ? 'success' : answerFor(submission, question.id)?.status === 'changes_requested' ? 'deep-orange' : 'blue-grey'" variant="tonal">{{ answerFor(submission, question.id)?.status === 'approved' ? answerFor(submission, question.id)?.autoGraded ? '自动评阅完成' : '已评阅' : answerFor(submission, question.id)?.status === 'changes_requested' ? '需修改' : '待评阅' }}</v-chip></div>
              <p class="question-description">{{ question.description }}</p>
              <v-chip v-if="question.autoGrade" size="x-small" color="primary" variant="tonal" class="mb-2">自动判分</v-chip>
              <img v-if="question.illustrationUrl" :src="question.illustrationUrl" :alt="question.illustrationAlt || '题目配图'" class="copied-question-illustration" /><AssignmentPromptPreview show-attachments :ids="question.fileIds" />

              <div v-if="questionType(question) !== 'drawing'" class="answer-response"><strong>成员答案</strong><p>{{ responseSummary(question, answerFor(submission, question.id)) }}</p></div>
              <JudgePanel v-if="questionType(question) === 'code' && answerFor(submission, question.id)?.judgeRunId" :task="questionJudgeTask(question)" :question-id="question.id" :model-value="answerFor(submission, question.id)?.judgeRunId" readonly />
              <AttachmentList :ids="answerFor(submission, question.id)?.fileIds" />
              <div v-if="submission.finalizedAt" class="question-review-form">
                <v-text-field v-model.number="ensureReview(submission)[question.id]!.score" type="number" :min="0" :max="question.maxScore" :label="`得分 / ${question.maxScore}`" variant="outlined" density="compact" hide-details />
                <v-select v-model="ensureReview(submission)[question.id]!.status" :items="[{ title: '待评阅', value: 'pending' }, { title: '已评阅', value: 'approved' }, { title: '需要修改', value: 'changes_requested' }]" label="评阅状态" variant="outlined" density="compact" hide-details />
                <v-textarea v-model="ensureReview(submission)[question.id]!.feedback" label="本题反馈（选填）" rows="2" auto-grow variant="outlined" density="compact" hide-details />
              </div>
              <v-alert v-else type="info" variant="tonal" density="compact">成员已保存进度，等待其统一提交。</v-alert>
            </div>
            <div v-if="submission.finalizedAt" class="review-footer"><strong>本次总分：{{ reviewTotal(submission) }} / {{ maxScore }}</strong><v-btn color="primary" :loading="reviewBusyId === submission.id" @click="saveReview(submission)">保存全部评阅</v-btn></div>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </template>

    <template v-else>
      <v-alert v-if="!canSubmit && !canManage" type="info" variant="tonal" class="mb-4">报名并通过{{ task.direction }}组审核后，才能提交本组作业。</v-alert>
      <template v-if="published || canManage && preview">
        <v-alert v-if="hasAutomaticQuestions" type="info" variant="tonal" density="compact">选择题和填空题在统一提交后自动判分；代码题先运行测试，提交作业时按通过用例比例计分。保存进度不会评分。</v-alert>
        <v-alert v-if="mySubmission?.feedback" :type="mySubmission.status === 'changes_requested' ? 'warning' : 'success'" variant="tonal" class="mb-4"><strong>管理员评阅反馈</strong><p class="mt-1">{{ mySubmission.feedback }}</p></v-alert>
        <div class="student-progress"><div><strong>逐题完成情况</strong><span>{{ completedCount }} / {{ questions.length }} 题</span></div><v-progress-linear :model-value="questions.length ? completedCount / questions.length * 100 : 0" color="primary" rounded /></div>
        <article v-for="(question, index) in questions" :key="question.id" class="student-question">
          <div v-if="question.groupId && (index === 0 || questions[index - 1]?.groupId !== question.groupId)" class="assignment-group-heading"><h3>{{ question.groupTitle }}</h3><p>{{ question.groupDescription }}</p></div>
          <div class="question-heading"><div><span>{{ questionGroupLabel(questions, index) }} · {{ questionTypeLabel(question) }} · {{ question.maxScore }} 分</span><h3>{{ question.title }}</h3></div><v-chip v-if="answerFor(mySubmission, question.id)?.status" size="small" :color="answerFor(mySubmission, question.id)?.status === 'approved' ? 'success' : answerFor(mySubmission, question.id)?.status === 'changes_requested' ? 'deep-orange' : 'blue-grey'" variant="tonal">{{ answerFor(mySubmission, question.id)?.status === 'approved' ? answerFor(mySubmission, question.id)?.autoGraded ? '自动评阅完成' : '已评阅' : answerFor(mySubmission, question.id)?.status === 'changes_requested' ? '请修改' : '待评阅' }}</v-chip></div>
          <p class="question-description">{{ question.description }}</p>
              <v-chip v-if="question.autoGrade" size="x-small" color="primary" variant="tonal" class="mb-2">自动判分</v-chip>
          <div v-if="question.fileIds.length" class="question-prompt-files">
            <img v-if="question.illustrationUrl" :src="question.illustrationUrl" :alt="question.illustrationAlt || '题目配图'" class="copied-question-illustration" /><AssignmentPromptPreview show-attachments :ids="question.fileIds" />

          </div>
          <v-alert v-if="answerFor(mySubmission, question.id)?.feedback" type="info" variant="tonal" density="compact" class="my-3">本题反馈：{{ answerFor(mySubmission, question.id)?.feedback }}</v-alert>
          <v-radio-group v-if="questionType(question) === 'single_choice'" v-model="answerResponses[question.id]" :disabled="!canInteract || saving" hide-details class="assignment-answer-options">
            <v-radio v-for="(option, optionIndex) in question.options || []" :key="optionIndex" :value="String(optionIndex)" :label="`${String.fromCharCode(65 + optionIndex)}. ${option}`" />
          </v-radio-group>
          <div v-else-if="questionType(question) === 'multiple_choice'" class="assignment-answer-options">
            <v-checkbox v-for="(option, optionIndex) in question.options || []" :key="optionIndex" v-model="selectedChoices[question.id]" :value="String(optionIndex)" :label="`${String.fromCharCode(65 + optionIndex)}. ${option}`" :disabled="!canInteract || saving" hide-details density="compact" />
          </div>
          <v-textarea v-else-if="questionType(question) === 'fill_blank'" v-model="answerResponses[question.id]" label="填写完整答案（支持多行）" variant="outlined" density="compact" rows="2" auto-grow maxlength="20000" :disabled="!canInteract || saving" hide-details />
          <JudgePanel v-else-if="questionType(question) === 'code'" :key="question.id" :task="questionJudgeTask(question)" :question-id="question.id" :preview="canManage && preview" v-model="judgeRunIds[question.id]" :disabled="!canInteract || saving" @busy="setUploadBusy(question.id + ':judge', $event)" />
          <template v-else-if="questionType(question) === 'short_answer'">
            <v-textarea v-model="answerResponses[question.id]" label="文字作答或上传文件" hint="填写文字或上传答题文件，完成其中一项即可；也可以同时提供。" persistent-hint variant="outlined" density="compact" rows="3" auto-grow maxlength="20000" :disabled="!canInteract || saving" />
            <FileUpload v-model="answerFiles[question.id]" label="答题文件（已填写文字时可选）" :disabled="!canInteract || saving" @busy="setUploadBusy(question.id, $event)" />
          </template>
          <FileUpload v-else v-model="answerFiles[question.id]" :label="uploadLabel(question)" :disabled="!canInteract || saving" @busy="setUploadBusy(question.id, $event)" />
          <details v-if="canManage && preview && question.autoGrade" class="mt-3"><summary>查看标准答案与解析</summary><p>{{ (question.answers || []).map(answer => ['single_choice', 'multiple_choice'].includes(questionType(question)) ? `${String.fromCharCode(65 + Number(answer))}. ${question.options?.[Number(answer)] || ''}` : answer).join('；') }}</p><p>{{ question.explanation }}</p></details>
          <div v-if="answerFor(mySubmission, question.id)?.score != null" class="question-score">本题得分：{{ answerFor(mySubmission, question.id)?.score }} / {{ question.maxScore }}</div>
        </article>
        <div v-if="!canManage" class="student-actions">
          <span v-if="isFinalized">{{ mySubmission?.status === 'approved' ? '本次作业已完成评阅，可查看逐题成绩和反馈。' : '已提交全部题目，已评题目显示得分，其余题目等待管理员评阅。' }}</span>
          <span v-else-if="canEditAnswers">可逐题作答并保存进度；全部完成前可以随时修改。</span>
          <span v-else>当前可以浏览题目要求和附件，报名审核通过后可提交作业。</span>
          <div v-if="canEditAnswers" class="student-buttons"><v-btn variant="tonal" :loading="saving && !savedNotice" :disabled="saving || uploadCount > 0" @click="saveDraft(false)">保存进度</v-btn><v-btn color="primary" :loading="saving && !savedNotice" :disabled="saving || uploadCount > 0 || completedCount !== questions.length" @click="saveDraft(true)">全部完成，统一提交</v-btn></div>
          <v-chip v-else-if="isFinalized" color="success" variant="tonal" prepend-icon="mdi-check-circle-outline">已统一提交</v-chip>
        </div>
        <v-alert v-if="savedNotice" type="success" variant="tonal" density="compact" class="mt-3">{{ savedNotice }}</v-alert>
      </template>
    </template>
  </section>
</template>

<style scoped>
.copied-question-illustration { display: block; max-width: 100%; max-height: 360px; margin: 16px auto; }
.assignment-group-heading { padding: 16px; margin-bottom: 20px; border-left: 3px solid #4361c2; background: #f3f6fc; }
.assignment-group-heading p { white-space: pre-wrap; margin-top: 8px; color: #475569; }
.mechanical-assignment { display: grid; gap: 18px; }
.assignment-summary { display: flex; flex-wrap: wrap; gap: 12px; padding: 14px 16px; background: #f4f7fb; border-radius: 10px; }
.assignment-summary > div { display: grid; gap: 3px; min-width: 130px; padding-right: 20px; border-right: 1px solid #dce3ed; }
.assignment-summary > div:last-child { border-right: 0; }
.assignment-summary span, .question-heading span, .member-summary span { color: #65758b; font-size: 13px; }
.assignment-summary strong { color: #1c3656; font-size: 17px; }
.assignment-manager-heading, .review-footer, .student-actions, .student-progress > div, .member-summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.assignment-manager-heading h3 { margin-bottom: 4px; }
.assignment-manager-heading p, .question-description { color: #64748b; white-space: pre-wrap; }
.answer-response { margin: 14px 0; padding: 12px 14px; border-radius: 8px; background: #f5f8fc; }
.answer-response strong { display: block; margin-bottom: 5px; color: #52667c; font-size: 12px; }
.answer-response p { margin: 0; color: #20364e; white-space: pre-wrap; overflow-wrap: anywhere; }
.assignment-answer-options { margin: 4px 0 12px; }
.assignment-answer-options :deep(.v-label) { white-space: normal; }
.review-panels { border: 1px solid #e1e7ef; border-radius: 10px; }
.member-summary { width: 100%; justify-content: flex-start; flex-wrap: wrap; }
.member-summary > span { margin-right: auto; }
.review-question, .student-question { padding: 18px; border: 1px solid #e3e9f1; border-radius: 12px; background: #fff; }
.review-question + .review-question { margin-top: 14px; }
.question-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.question-heading h3, .question-heading h4 { margin: 4px 0 0; color: #20364e; }
.question-description { margin: 12px 0; }
.question-review-form { display: grid; grid-template-columns: minmax(120px, .6fr) minmax(160px, 1fr); gap: 12px; align-items: start; }
.question-review-form :deep(.v-textarea) { grid-column: 1 / -1; }
.review-footer { padding-top: 18px; }
.assignment-empty { min-height: 100px; display: flex; align-items: center; justify-content: center; gap: 10px; color: #718096; }
.student-progress { display: grid; gap: 9px; padding: 15px 17px; background: #f4f7fb; border-radius: 10px; }
.student-progress span { color: #64748b; }
.prompt-preview span { padding: 7px 10px; font-size: 13px; }
.question-score { margin-top: 8px; font-weight: 600; color: #176e52; }
.student-actions { padding: 16px; border: 1px solid #dce7f2; background: #f6faff; border-radius: 10px; }
.student-actions > span { color: #52667c; }
.student-buttons { display: flex; gap: 10px; flex-wrap: wrap; }
@media (max-width: 680px) { .assignment-manager-heading, .student-actions { align-items: stretch; flex-direction: column; }.question-review-form { grid-template-columns: 1fr; }.question-review-form :deep(.v-textarea) { grid-column: auto; }.assignment-summary > div { flex: 1; min-width: 100px; }.member-summary { align-items: flex-start; }.student-buttons .v-btn { flex: 1; } }
</style>
