<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import { refreshLearningAccess } from "../lib/learning-access";
import { groupLabel, hasPermission, manageableGroups, memberTypeNames } from "../lib/permissions";
import { dateTime } from "../lib/format";
import type { TeamSpace } from "../lib/platform";
import type { MemberType } from "../lib/types";
import { currentTrainingStage, defaultTrainingStages, trainingStatusColors, trainingStatusNames, type TrainingPlan, type TrainingProgress, type TrainingStage } from "../lib/training";

const isManager = computed(() => hasPermission("assessment"));
const groups = computed(() => manageableGroups("assessment"));
const tab = ref(isManager.value ? "manage" : "mine");
const teams = ref<TeamSpace[]>([]), plans = ref<TrainingPlan[]>([]), progress = ref<TrainingProgress[]>([]);
const loading = ref(true), saving = ref(false), error = ref(""), notice = ref("");
const direction = ref<string>(groups.value[0]?.value || ""), teamId = ref(""), stageFilter = ref(""), status = ref("training"), search = ref("");
const selectedIds = ref<string[]>([]), detail = ref<TrainingProgress | null>(null);
const planDialog = ref(false), planError = ref(""), planDraft = reactive({ title: "", stages: [] as TrainingStage[] });
const decisionDialog = ref(false), decisionError = ref(""), decisionStatus = ref<"passed" | "eliminated">("passed"), decisionNote = ref("");
const targetMemberType = ref<Exclude<MemberType, "freshman">>("reserve");
const decisionRecords = ref<TrainingProgress[]>([]);
let loadVersion = 0;

const scopedProgress = computed(() => progress.value.filter(item => hasPermission("assessment", item.direction)));
const mine = computed(() => progress.value.filter(item => item.userId === auth.user?.id).sort((a, b) => b.cycle - a.cycle || b.createdAt.localeCompare(a.createdAt)));
const legacyTeam = "__legacy_training__";
const actualTeamId = computed(() => teamId.value === legacyTeam ? "" : teamId.value);
const teamOptions = computed(() => [
  ...teams.value.filter(team => team.directions.includes(direction.value) || scopedProgress.value.some(item => item.teamId === team.id && item.direction === direction.value)).map(team => ({ title: team.name, value: team.id })),
  ...(scopedProgress.value.some(item => !item.teamId && item.direction === direction.value) || plans.value.some(item => !item.teamId && item.direction === direction.value) ? [{ title: "历史招新", value: legacyTeam }] : []),
]);
const scope = computed(() => scopedProgress.value.filter(item => item.teamId === actualTeamId.value && item.direction === direction.value));
const plan = computed(() => plans.value.find(item => item.teamId === actualTeamId.value && item.direction === direction.value));
const stageSignature = (stage: TrainingStage | undefined) => stage ? JSON.stringify([stage.id, stage.title, stage.order, stage.final]) : "";
const progressStage = (item: TrainingProgress) => currentTrainingStage(item) || item.stages.find(stage => stage.id === item.decisions.at(-1)?.stageId);
const stages = computed(() => {
  const items = new Map<string, TrainingStage>();
  for (const item of scope.value) for (const stage of item.stages) items.set(stageSignature(stage), stage);
  for (const stage of plan.value?.stages || []) items.set(stageSignature(stage), stage);
  return [...items.values()].sort((a, b) => a.order - b.order);
});
const stageOptions = computed(() => {
  const options = stages.value.map(stage => {
    const value = stageSignature(stage);
    const count = scope.value.filter(item => item.status === "training" && stageSignature(currentTrainingStage(item)) === value).length;
    return { value, title: `第 ${stage.order} 阶段 · ${stage.title}${stage.final ? ' · 最终考核' : ''} · ${count} 人待考核` };
  });
  return options.map((option, index) => {
    const duplicates = options.filter(item => item.title === option.title);
    if (duplicates.length < 2) return option;
    const current = plan.value?.stages.some(stage => stageSignature(stage) === option.value);
    return { ...option, title: `${option.title} · ${current ? '当前方案' : `培养记录 ${duplicates.findIndex(item => item.value === options[index]!.value) + 1}`}` };
  });
});
const roster = computed(() => scope.value.filter(item => (!status.value || item.status === status.value) && (!stageFilter.value || stageSignature(progressStage(item)) === stageFilter.value) && `${item.userName} ${item.email}`.toLowerCase().includes(search.value.toLowerCase())));
const canDecide = (item: TrainingProgress) => item.status === "training" && Boolean(stageFilter.value) && stageSignature(currentTrainingStage(item)) === stageFilter.value && hasPermission("assessment", item.direction) && (auth.user?.role === "admin" || item.userId !== auth.user?.id);
const eligible = computed(() => roster.value.filter(canDecide));
const selectedRecords = computed(() => eligible.value.filter(item => selectedIds.value.includes(item.id)));
const allSelected = computed(() => eligible.value.length > 0 && eligible.value.every(item => selectedIds.value.includes(item.id)));
const finalDecision = computed(() => decisionRecords.value.length > 0 && decisionRecords.value.every(item => currentTrainingStage(item)?.final));
const stats = computed(() => [
  { label: "培养中", value: scope.value.filter(item => item.status === "training").length, icon: "mdi-school-outline", color: "primary" },
  { label: "考核通过", value: scope.value.filter(item => item.status === "completed").length, icon: "mdi-check-decagram-outline", color: "success" },
  { label: "已淘汰", value: scope.value.filter(item => item.status === "eliminated").length, icon: "mdi-account-cancel-outline", color: "error" },
]);
const teamName = (id: string) => !id || id === legacyTeam ? "历史招新" : teams.value.find(team => team.id === id)?.name || "战队培养";
const stageName = (item: TrainingProgress) => currentTrainingStage(item)?.title || item.decisions.at(-1)?.stageTitle || "等待阶段安排";
const stageDecision = (item: TrainingProgress, id: string) => item.decisions.find(decision => decision.stageId === id);
const byteLength = (value: string) => new TextEncoder().encode(value).length;
function stageState(item: TrainingProgress, stage: TrainingStage) {
  const decision = stageDecision(item, stage.id);
  if (decision?.status === "passed") return { label: "已通过", color: "success", icon: "mdi-check" };
  if (decision?.status === "eliminated") return { label: "未通过 · 已淘汰", color: "error", icon: "mdi-close" };
  if (stage.id === item.currentStageId && item.status === "training") return { label: "当前阶段", color: "primary", icon: "mdi-clock-outline" };
  return { label: "未开始", color: "grey", icon: "mdi-circle-small" };
}
function selectScope() {
  if (!teamOptions.value.some(item => item.value === teamId.value)) teamId.value = teamOptions.value[0]?.value || "";
  if (!stages.value.some(item => stageSignature(item) === stageFilter.value)) {
    const current = scope.value.find(item => item.status === "training");
    stageFilter.value = stageSignature(current ? currentTrainingStage(current) : stages.value[0]);
  }
}
watch(direction, () => { teamId.value = ""; stageFilter.value = ""; selectScope(); });
watch(teamId, () => { stageFilter.value = ""; selectScope(); });
watch([direction, teamId, stageFilter, status, search], () => { selectedIds.value = []; });

async function load() {
  const version = ++loadVersion;
  loading.value = true; error.value = "";
  try {
    const [teamData, planData, progressData] = await Promise.all([
      api.get<{ items: TeamSpace[] }>("/teams"),
      api.get<{ items: TrainingPlan[] }>("/training-plans"),
      api.get<{ items: TrainingProgress[] }>("/training-progress"),
    ]);
    if (version !== loadVersion) return;
    teams.value = teamData.items || [];
    plans.value = (planData.items || []).map(item => ({ ...item, stages: item.stages || [] }));
    progress.value = (progressData.items || []).map(item => ({ ...item, stages: item.stages || [], decisions: item.decisions || [] }));
    selectedIds.value = [];
    if (isManager.value) selectScope();
  } catch (e) {
    if (version === loadVersion) error.value = (e as Error).message;
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}
function toggleAll() {
  selectedIds.value = allSelected.value ? [] : eligible.value.map(item => item.id);
}
function openPlan() {
  if (!teamId.value || !hasPermission("assessment", direction.value)) return;
  planDraft.title = plan.value?.title || `${groupLabel(direction.value)}新生培养`;
  planDraft.stages = plan.value?.stages.length ? plan.value.stages.map(stage => ({ ...stage })) : defaultTrainingStages();
  planError.value = ""; planDialog.value = true;
}
function reorderStage(index: number, offset: number) {
  const target = index + offset;
  if (target < 0 || target >= planDraft.stages.length) return;
  const [stage] = planDraft.stages.splice(index, 1);
  if (stage) planDraft.stages.splice(target, 0, stage);
}
function addStage() {
  planDraft.stages.splice(Math.max(0, planDraft.stages.length - 1), 0, { id: "", title: "", description: "", order: planDraft.stages.length, final: false });
}
async function savePlan() {
  if (saving.value || !teamId.value || !hasPermission("assessment", direction.value)) return;
  if (!planDraft.title.trim() || !planDraft.stages.length || planDraft.stages.some(stage => !stage.title.trim())) { planError.value = "请填写方案名称与每个阶段的名称。"; return; }
  if (byteLength(planDraft.title.trim()) > 200 || planDraft.stages.some(stage => byteLength(stage.title.trim()) > 200)) { planError.value = "方案与阶段名称请保持在 200 字节以内（约 66 个汉字）。"; return; }
  if (planDraft.stages.length > 12 || planDraft.stages.some(stage => byteLength(stage.description.trim()) > 5000)) { planError.value = "最多配置 12 个阶段，每个阶段说明最多 5000 字节。"; return; }
  const stageInput = planDraft.stages.map((stage, index) => ({ ...(stage.id ? { id: stage.id } : {}), title: stage.title.trim(), description: stage.description.trim(), final: index === planDraft.stages.length - 1 }));
  saving.value = true; planError.value = "";
  try {
    if (plan.value) await api.patch(`/training-plans/${plan.value.id}`, { title: planDraft.title.trim(), stages: stageInput });
    else await api.post("/training-plans", { teamId: actualTeamId.value, direction: direction.value, title: planDraft.title.trim(), stages: stageInput });
    planDialog.value = false;
    notice.value = "阶段方案已保存，新录取的培养周期将使用此方案。";
    await load();
  } catch (e) { planError.value = (e as Error).message; }
  finally { saving.value = false; }
}
function openDecision(value: "passed" | "eliminated") {
  if (!selectedRecords.value.length || !hasPermission("assessment", direction.value)) return;
  if (selectedRecords.value.length > 100) { notice.value = "每次最多确认 100 人，请分批操作。"; return; }
  decisionRecords.value = [...selectedRecords.value];
  decisionStatus.value = value; decisionNote.value = ""; decisionError.value = ""; targetMemberType.value = "reserve";
  decisionDialog.value = true;
}
async function confirmDecision() {
  if (saving.value || !decisionRecords.value.length || decisionRecords.value.some(item => !hasPermission("assessment", item.direction))) return;
  if (byteLength(decisionNote.value.trim()) > 5000) { decisionError.value = "考核反馈请保持在 5000 字节以内。"; return; }
  saving.value = true; decisionError.value = "";
  try {
    await api.post("/training-progress/decisions", {
      progressIds: decisionRecords.value.map(item => item.id),
      stageId: decisionRecords.value[0]!.currentStageId,
      status: decisionStatus.value,
      note: decisionNote.value.trim(),
      ...(decisionStatus.value === "passed" && finalDecision.value ? { targetMemberType: targetMemberType.value } : {}),
    });
    decisionDialog.value = false;
    notice.value = decisionStatus.value === "eliminated" ? `已记录 ${decisionRecords.value.length} 人的淘汰结果。` : finalDecision.value ? `已确认 ${decisionRecords.value.length} 人通过最终考核并晋升。` : `已确认 ${decisionRecords.value.length} 人通过本阶段。`;
    await refreshLearningAccess(); await load();
  } catch (e) { decisionError.value = (e as Error).message; }
  finally { saving.value = false; }
}
onMounted(load);
onUnmounted(() => { loadVersion++; });
</script>

<template>
  <div class="page-content training-page">
    <div class="training-heading">
      <div><h1 class="page-title">新生考核</h1><p class="page-subtitle">从基础学习到最终考核，记录每一步成长与选拔结果。</p></div>
      <v-btn variant="text" prepend-icon="mdi-refresh" :disabled="loading || saving" @click="load">刷新记录</v-btn>
    </div>
    <v-tabs v-if="isManager" v-model="tab" color="primary" class="training-tabs"><v-tab value="manage">阶段管理</v-tab><v-tab value="mine">我的进度</v-tab></v-tabs>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <v-progress-linear v-if="loading" color="primary" indeterminate class="mb-5" />
    <template v-else-if="tab === 'manage' && isManager">
      <div class="scope-controls">
        <v-select v-model="direction" :items="groups" label="管理组别" hide-details />
        <v-select v-model="teamId" :items="teamOptions" label="招募团队" hide-details :disabled="!teamOptions.length" />
        <v-btn color="primary" variant="tonal" prepend-icon="mdi-tune-variant" :disabled="!teamId || saving" @click="openPlan">配置阶段</v-btn>
      </div>
      <v-card v-if="!teamId" class="training-empty"><v-icon size="42" color="primary">mdi-account-group-outline</v-icon><h2>还没有可管理的招募团队</h2><p>管理员发布团队并开放该组报名后，即可配置新生培养阶段。</p></v-card>
      <template v-else>
        <div class="training-stats"><v-card v-for="stat in stats" :key="stat.label" class="stat-card"><v-icon :color="stat.color" size="25">{{ stat.icon }}</v-icon><div><strong>{{ stat.value }}</strong><span>{{ stat.label }}</span></div></v-card></div>
        <v-card class="plan-card">
          <div class="plan-heading"><div><h2>{{ plan?.title || `${groupLabel(direction)}新生培养` }}</h2><p>{{ plan ? '新录取人员按此方案开始培养；已有周期保留当时的阶段安排。' : '未单独配置时，使用基础学习、阶段考核、最终考核三个阶段。' }}</p></div><v-chip size="small" variant="outlined">按阶段依次确认</v-chip></div>
          <div class="stage-overview"><div v-for="(stage, index) in (plan?.stages.length ? plan.stages : defaultTrainingStages())" :key="stage.id || index" class="overview-stage"><span class="stage-number">{{ index + 1 }}</span><div><strong>{{ stage.title }}</strong><small>{{ stage.final ? '最终通过后晋升' : '通过后进入下一阶段' }}</small></div></div></div>
        </v-card>
        <v-card class="roster-card">
          <div class="roster-heading"><div><h2>阶段名单</h2><p>选择同一阶段的培养中人员，批量确认通过或淘汰。</p></div><span class="roster-count">{{ roster.length }} 条记录</span></div>
          <div class="roster-filters"><v-text-field v-model="search" placeholder="搜索姓名或邮箱" prepend-inner-icon="mdi-magnify" hide-details /><v-select v-model="stageFilter" :items="[{ title: '全部阶段', value: '' }, ...stageOptions]" label="考核阶段" hide-details /><v-select v-model="status" :items="[{ title: '全部状态', value: '' }, ...Object.entries(trainingStatusNames).map(([value, title]) => ({ title, value }))]" label="培养状态" hide-details /></div>
          <div class="batch-toolbar"><v-checkbox :model-value="allSelected" :indeterminate="selectedRecords.length > 0 && !allSelected" label="选择本阶段待考核人员" hide-details density="compact" :disabled="!eligible.length || saving" @update:model-value="toggleAll" /><v-spacer /><span class="selected-count">已选 {{ selectedRecords.length }} 人</span><v-btn size="small" variant="tonal" color="error" :disabled="!selectedRecords.length || saving" @click="openDecision('eliminated')">淘汰</v-btn><v-btn size="small" color="primary" :disabled="!selectedRecords.length || saving" prepend-icon="mdi-check" @click="openDecision('passed')">确认通过</v-btn></div>
          <p v-if="!stageFilter && roster.some(item => item.status === 'training')" class="stage-tip">先选择一个当前阶段，即可勾选人员进行批量考核。</p>
          <v-table v-if="roster.length" class="training-table"><thead><tr><th class="select-column"></th><th>成员</th><th>当前阶段</th><th>状态</th><th>录取周期</th><th>记录</th></tr></thead><tbody><tr v-for="item in roster" :key="item.id"><td><v-checkbox v-model="selectedIds" :value="item.id" hide-details density="compact" :disabled="!canDecide(item) || saving" :aria-label="`选择${item.userName}`" /></td><td><strong>{{ item.userName }}</strong><small>{{ item.email }}</small></td><td>{{ stageName(item) }}</td><td><v-chip size="small" :color="trainingStatusColors[item.status]" variant="tonal">{{ trainingStatusNames[item.status] }}</v-chip></td><td>第 {{ item.cycle }} 次<small>{{ dateTime(item.createdAt) }}</small></td><td><v-btn size="small" variant="text" color="primary" @click="detail = item">查看进度</v-btn></td></tr></tbody></v-table>
          <div v-else class="roster-empty"><v-icon size="36" color="grey">mdi-account-search-outline</v-icon><p>没有符合条件的培养记录</p></div>
        </v-card>
      </template>
    </template>
    <template v-else>
      <v-card v-if="!mine.length" class="training-empty"><v-icon size="44" color="primary">mdi-school-outline</v-icon><h2>等待开始你的培养历程</h2><p>团队报名审核通过后，分组培养阶段与考核进度会显示在这里。</p><v-btn to="/teams" color="primary" variant="tonal" class="mt-5">前往团队报名</v-btn></v-card>
      <div v-else class="my-progress-list"><v-card v-for="item in mine" :key="item.id" class="progress-card">
        <div class="progress-header"><div><span class="progress-context">{{ teamName(item.teamId) }} · {{ groupLabel(item.direction) }} · 第 {{ item.cycle }} 次录取</span><h2>{{ item.planTitle }}</h2></div><v-chip :color="trainingStatusColors[item.status]" variant="tonal">{{ trainingStatusNames[item.status] }}</v-chip></div>
        <v-alert v-if="item.status === 'eliminated'" :type="mine[0]?.id === item.id ? 'error' : 'info'" variant="tonal" density="compact" class="mb-5">{{ mine[0]?.id === item.id ? '本次培养已淘汰，学习与提交资格已关闭。考核记录保留；如需重新参与，请联系管理员重新录取。' : '本次培养已淘汰，考核记录已保留。当前参与情况请查看最新录取周期。' }}</v-alert>
        <v-alert v-else-if="item.status === 'completed'" type="success" variant="tonal" density="compact" class="mb-5">已通过本次最终考核{{ item.decisions.at(-1)?.memberType ? `，晋升为${memberTypeNames[item.decisions.at(-1)!.memberType!]}` : '' }}。</v-alert>
        <p v-else class="current-caption">当前：{{ stageName(item) }}<span>每一阶段通过后再进入下一阶段，最终考核通过后确认成员身份。</span></p>
        <div class="personal-stages"><div v-for="(stage, index) in item.stages" :key="stage.id" class="personal-stage" :class="{ 'is-current': stage.id === item.currentStageId && item.status === 'training' }"><div class="personal-stage-icon"><v-icon :color="stageState(item, stage).color" size="19">{{ stageState(item, stage).icon }}</v-icon></div><div class="personal-stage-content"><div><strong>{{ index + 1 }}. {{ stage.title }}</strong><v-chip size="x-small" :color="stageState(item, stage).color" variant="tonal">{{ stageState(item, stage).label }}</v-chip></div><p v-if="stage.description">{{ stage.description }}</p><p v-if="stageDecision(item, stage.id)?.note" class="decision-note">反馈：{{ stageDecision(item, stage.id)?.note }}</p><small v-if="stageDecision(item, stage.id)">{{ stageDecision(item, stage.id)?.decidedByName }} · {{ dateTime(stageDecision(item, stage.id)?.decidedAt || '') }}</small></div></div></div>
        <div class="progress-footer"><span>录取于 {{ dateTime(item.createdAt) }}</span><v-btn variant="text" color="primary" size="small" @click="detail = item">完整考核记录<v-icon end>mdi-arrow-right</v-icon></v-btn></div>
      </v-card></div>
    </template>

    <v-dialog v-model="planDialog" max-width="800" scrollable :persistent="saving"><v-card><v-card-title class="dialog-heading">配置培养阶段<v-spacer /><v-btn icon="mdi-close" aria-label="关闭阶段设置" variant="text" :disabled="saving" @click="planDialog = false" /></v-card-title><v-card-text>
      <p class="dialog-context">{{ teamName(teamId) }} · {{ groupLabel(direction) }}</p><v-alert v-if="planError" type="error" variant="tonal" class="mb-4">{{ planError }}</v-alert><v-alert type="info" variant="tonal" density="compact" class="mb-5">阶段从上到下依次进行，最后一个阶段为最终考核。调整方案仅影响新的录取周期。</v-alert><v-text-field v-model="planDraft.title" label="方案名称" maxlength="200" :disabled="saving" />
      <div v-for="(stage, index) in planDraft.stages" :key="stage.id || `new-${index}`" class="stage-editor"><div class="stage-editor-heading"><strong>阶段 {{ index + 1 }}</strong><v-chip v-if="index === planDraft.stages.length - 1" size="x-small" color="primary">最终考核</v-chip><v-spacer /><v-btn icon="mdi-arrow-up" size="small" variant="text" aria-label="上移阶段" :disabled="saving || index === 0" @click="reorderStage(index, -1)" /><v-btn icon="mdi-arrow-down" size="small" variant="text" aria-label="下移阶段" :disabled="saving || index === planDraft.stages.length - 1" @click="reorderStage(index, 1)" /><v-btn icon="mdi-delete-outline" size="small" variant="text" aria-label="删除阶段" :disabled="saving || planDraft.stages.length === 1" @click="planDraft.stages.splice(index, 1)" /></div><v-text-field v-model="stage.title" label="阶段名称" maxlength="200" :disabled="saving" /><v-textarea v-model="stage.description" label="阶段说明（可选）" rows="2" maxlength="2000" hide-details :disabled="saving" /></div>
      <v-btn variant="tonal" prepend-icon="mdi-plus" :disabled="saving || planDraft.stages.length >= 12" @click="addStage">增加阶段</v-btn>
    </v-card-text><v-card-actions class="dialog-actions"><v-spacer /><v-btn :disabled="saving" @click="planDialog = false">取消</v-btn><v-btn color="primary" variant="flat" :loading="saving" @click="savePlan">保存阶段方案</v-btn></v-card-actions></v-card></v-dialog>

    <v-dialog v-model="decisionDialog" max-width="600" :persistent="saving"><v-card><v-card-title class="dialog-heading">{{ decisionStatus === 'passed' ? '确认阶段通过' : '确认淘汰' }}<v-spacer /><v-btn icon="mdi-close" variant="text" aria-label="关闭考核确认" :disabled="saving" @click="decisionDialog = false" /></v-card-title><v-card-text><v-alert v-if="decisionError" type="error" variant="tonal" class="mb-4">{{ decisionError }}</v-alert><p class="decision-summary">{{ groupLabel(decisionRecords[0]?.direction || '') }} · {{ decisionRecords[0] ? stageName(decisionRecords[0]) : '' }}<strong>{{ decisionRecords.length }} 人</strong></p><div class="decision-people"><v-chip v-for="item in decisionRecords" :key="item.id" size="small">{{ item.userName }}</v-chip></div><v-alert :type="decisionStatus === 'eliminated' ? 'warning' : 'info'" variant="tonal" density="compact" class="my-5">{{ decisionStatus === 'eliminated' ? '淘汰后关闭本次学习与提交资格，保留所有考核记录。再次录取需由管理员明确重新开启培养。' : finalDecision ? '通过最终考核后，按下方选定的身份完成晋升。' : '通过本阶段后进入下一阶段，成员身份仍为新生。' }}</v-alert><v-select v-if="decisionStatus === 'passed' && finalDecision" v-model="targetMemberType" :items="[{ title: '梯队队员', value: 'reserve' }, { title: '正式队员', value: 'formal' }]" label="晋升为" :disabled="saving" /><v-textarea v-model="decisionNote" label="考核反馈（成员可查看）" rows="3" maxlength="2000" hide-details :disabled="saving" /></v-card-text><v-card-actions class="dialog-actions"><v-spacer /><v-btn :disabled="saving" @click="decisionDialog = false">取消</v-btn><v-btn :color="decisionStatus === 'eliminated' ? 'error' : 'primary'" variant="flat" :loading="saving" @click="confirmDecision">{{ decisionStatus === 'eliminated' ? '确认淘汰' : finalDecision ? '通过并晋升' : '确认通过' }}</v-btn></v-card-actions></v-card></v-dialog>

    <v-dialog :model-value="Boolean(detail)" max-width="740" scrollable @update:model-value="!$event && (detail = null)"><v-card v-if="detail"><v-card-title class="dialog-heading">{{ detail.userName }} · 考核记录<v-spacer /><v-btn icon="mdi-close" aria-label="关闭考核记录" variant="text" @click="detail = null" /></v-card-title><v-card-text><div class="detail-context"><span>{{ teamName(detail.teamId) }} · {{ groupLabel(detail.direction) }} · 第 {{ detail.cycle }} 次录取</span><v-chip size="small" :color="trainingStatusColors[detail.status]">{{ trainingStatusNames[detail.status] }}</v-chip></div><h3 class="mt-5 mb-3">{{ detail.planTitle }}</h3><p v-if="!detail.decisions.length" class="text-body-2 text-medium-emphasis">尚未确认阶段结果，当前处于{{ stageName(detail) }}。</p><div v-for="decision in detail.decisions" :key="`${decision.stageId}-${decision.decidedAt}`" class="history-entry"><div class="d-flex align-center ga-3"><strong>{{ decision.stageTitle }}</strong><v-chip size="x-small" :color="decision.status === 'passed' ? 'success' : 'error'">{{ decision.status === 'passed' ? '通过' : '淘汰' }}</v-chip><v-chip v-if="decision.memberType" size="x-small" variant="outlined">{{ memberTypeNames[decision.memberType] }}</v-chip></div><p>{{ decision.note || '未填写考核反馈' }}</p><small>{{ decision.decidedByName }} · {{ dateTime(decision.decidedAt) }}</small></div></v-card-text><v-card-actions class="dialog-actions"><v-spacer /><v-btn color="primary" @click="detail = null">关闭</v-btn></v-card-actions></v-card></v-dialog>
    <v-snackbar :model-value="Boolean(notice)" @update:model-value="!$event && (notice = '')">{{ notice }}</v-snackbar>
  </div>
</template>

<style scoped>
.training-page { max-width: 1360px; }
.training-heading, .plan-heading, .roster-heading, .progress-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
.training-tabs { margin-bottom: 24px; border-bottom: 1px solid #e5e7eb; }
.scope-controls { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(220px, 2fr) auto; align-items: center; gap: 16px; margin-bottom: 22px; }
.training-stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 18px; margin-bottom: 22px; }
.stat-card { display: flex; align-items: center; gap: 18px; padding: 20px 24px; }
.stat-card div { display: flex; align-items: baseline; gap: 12px; }
.stat-card strong { font-size: 28px; font-weight: 600; }
.stat-card span { font-size: 13px; color: #757575; }
.plan-card { padding: 24px; margin-bottom: 22px; }
.plan-heading h2, .roster-heading h2, .progress-header h2 { font-size: 19px; font-weight: 500; }
.plan-heading p, .roster-heading p { color: #757575; font-size: 13px; line-height: 1.7; margin-top: 7px; }
.stage-overview { display: flex; flex-wrap: wrap; gap: 20px 34px; margin-top: 23px; padding-top: 22px; border-top: 1px solid #eeeeee; }
.overview-stage { display: flex; align-items: center; gap: 12px; }
.stage-number { display: flex; align-items: center; justify-content: center; width: 31px; height: 31px; border: 1px solid #dedede; border-radius: 50%; color: #757575; font-size: 13px; }
.overview-stage strong { display: block; font-size: 14px; font-weight: 500; }
.overview-stage small { display: block; margin-top: 4px; color: #8b8b8b; font-size: 11px; }
.roster-heading { padding: 24px 24px 18px; }
.roster-count { color: #8b8b8b; font-size: 12px; padding-top: 5px; }
.roster-filters { display: grid; grid-template-columns: 1.3fr 1fr 1fr; gap: 14px; padding: 0 24px 20px; }
.batch-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; padding: 10px 22px; background: #fafafa; border-block: 1px solid #eeeeee; }
.selected-count, .stage-tip { font-size: 12px; color: #757575; }
.stage-tip { padding: 14px 24px 0; }
.training-table th { font-size: 12px; color: #8b8b8b; white-space: nowrap; }
.training-table td { font-size: 13px; height: 76px !important; white-space: nowrap; }
.training-table td strong { font-weight: 500; }
.training-table td small { display: block; margin-top: 4px; font-size: 11px; color: #8b8b8b; }
.select-column { width: 60px; }
.roster-empty, .training-empty { padding: 48px 24px; text-align: center; color: #757575; }
.roster-empty p { margin-top: 13px; font-size: 14px; }
.training-empty h2 { font-size: 18px; font-weight: 500; color: #424242; margin: 20px 0 10px; }
.training-empty p { font-size: 14px; line-height: 1.8; }
.my-progress-list { display: flex; flex-direction: column; gap: 22px; }
.progress-card { padding: 27px; }
.progress-header { margin-bottom: 23px; }
.progress-context { display: block; font-size: 12px; color: #8b8b8b; margin-bottom: 9px; }
.current-caption { font-size: 14px; margin-bottom: 20px; }
.current-caption span { display: block; margin-top: 7px; color: #757575; font-size: 12px; }
.personal-stages { display: flex; flex-direction: column; gap: 10px; }
.personal-stage { display: flex; gap: 16px; border: 1px solid #eeeeee; border-radius: 6px; padding: 18px; }
.personal-stage.is-current { border-color: rgba(var(--v-theme-primary), 0.32); background: rgba(var(--v-theme-primary), 0.035); }
.personal-stage-icon { display: flex; align-items: center; justify-content: center; width: 30px; height: 30px; background: #fafafa; border-radius: 50%; flex-shrink: 0; }
.personal-stage-content { min-width: 0; flex: 1; }
.personal-stage-content > div { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.personal-stage-content strong { font-size: 14px; font-weight: 500; }
.personal-stage-content p { font-size: 13px; color: #757575; line-height: 1.75; margin-top: 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
.personal-stage-content .decision-note { color: #424242; }
.personal-stage-content small { display: block; color: #9e9e9e; font-size: 11px; margin-top: 9px; }
.progress-footer { display: flex; align-items: center; justify-content: space-between; margin-top: 22px; }
.progress-footer > span { color: #9e9e9e; font-size: 11px; }
.dialog-heading { display: flex; align-items: center; font-size: 20px; padding: 22px 24px 10px; }
.dialog-context { font-size: 13px; color: #757575; margin-bottom: 18px; }
.dialog-actions { padding: 16px 24px 24px; }
.stage-editor { padding: 18px; margin-bottom: 16px; border: 1px solid #e8e8e8; border-radius: 6px; }
.stage-editor-heading { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; }
.stage-editor-heading strong { font-size: 13px; font-weight: 500; }
.decision-summary { display: flex; justify-content: space-between; gap: 12px; font-size: 14px; }
.decision-summary strong { font-weight: 500; }
.decision-people { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 15px; }
.detail-context { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; font-size: 13px; color: #757575; }
.history-entry { padding: 18px 0; border-bottom: 1px solid #eeeeee; }
.history-entry strong { font-size: 14px; font-weight: 500; }
.history-entry p { font-size: 13px; color: #616161; line-height: 1.8; margin: 12px 0 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
.history-entry small { color: #9e9e9e; font-size: 11px; }
@media (max-width: 900px) {
  .scope-controls { grid-template-columns: 1fr 1fr; }
  .scope-controls > .v-btn { grid-column: 1 / -1; justify-self: start; }
  .stat-card { padding: 18px; gap: 12px; }
  .stat-card div { flex-direction: column; gap: 3px; }
  .roster-filters { grid-template-columns: 1fr 1fr; }
  .roster-filters > :first-child { grid-column: 1 / -1; }
}
@media (max-width: 600px) {
  .training-heading, .plan-heading { flex-wrap: wrap; gap: 12px; }
  .scope-controls { grid-template-columns: 1fr; }
  .training-stats { gap: 10px; }
  .stat-card { padding: 15px 10px; flex-direction: column; align-items: flex-start; gap: 8px; }
  .stat-card strong { font-size: 24px; }
  .stat-card span { font-size: 11px; }
  .plan-card, .progress-card { padding: 20px; }
  .roster-heading { padding: 20px; }
  .roster-filters { padding: 0 20px 18px; }
  .batch-toolbar { padding: 10px 16px; }
  .progress-header { flex-wrap: wrap; }
  .personal-stage { padding: 14px; gap: 10px; }
  .progress-footer { flex-wrap: wrap; gap: 10px; }
}
</style>
