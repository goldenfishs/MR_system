<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import { groupLabel, hasPermission, manageableGroups } from "../lib/permissions";
import { dateTime, downloadUrl, previewUrl, type TeamApplication, type TeamSpace } from "../lib/platform";
const route = useRoute();
const { smAndDown } = useDisplay();
const teams = ref<TeamSpace[]>([]), applications = ref<TeamApplication[]>([]);
const loading = ref(true), saving = ref(false), error = ref(""), detailError = ref(""), notice = ref("");
const search = ref("");
const direction = ref(typeof route.query.direction === "string" ? route.query.direction : "");
const teamId = ref(typeof route.query.team === "string" ? route.query.team : "");
const status = ref(typeof route.query.status === "string" ? route.query.status : "pending");
const selected = ref<TeamApplication | null>(null), feedback = ref("");
const reopen = ref(false);
let generation = 0;
const groups = computed(() => manageableGroups("recruitment"));
const statusOptions = [
  { title: "待审核", value: "pending" }, { title: "全部状态", value: "all" },
  { title: "已录取", value: "accepted" }, { title: "未通过", value: "rejected" }, { title: "本轮已淘汰", value: "eliminated" },
];
const teamName = (id: string) => teams.value.find(t => t.id === id)?.name || "报名团队";
const itemStatus = (item: TeamApplication) => item.trainingStatus === "eliminated" ? "eliminated" : item.status;
const statusLabel = (item: TeamApplication) => item.trainingStatus === "eliminated" ? "本轮已淘汰" : item.status === "accepted" ? item.trainingStatus === "completed" ? "考核已通过" : "已录取" : item.status === "rejected" ? "未通过" : needsInterview(item) ? "待面试" : "待审核";
const statusColor = (item: TeamApplication) => ({ accepted: "success", rejected: "error", eliminated: "grey", pending: "warning" })[itemStatus(item)];
const scoped = computed(() => applications.value.filter(item => (!teamId.value || item.teamId === teamId.value) && (!direction.value || item.direction === direction.value)));
const counts = computed(() => ({ all: scoped.value.length, pending: scoped.value.filter(i => itemStatus(i) === "pending").length, accepted: scoped.value.filter(i => itemStatus(i) === "accepted").length, rejected: scoped.value.filter(i => itemStatus(i) === "rejected").length, eliminated: scoped.value.filter(i => itemStatus(i) === "eliminated").length }));
const filtered = computed(() => scoped.value.filter(item => (status.value === "all" || itemStatus(item) === status.value) && `${item.userName} ${item.profile?.studentNumber || ''} ${item.profile?.major || ''} ${item.profile?.className || ''} ${teamName(item.teamId)}`.toLowerCase().includes((search.value || "").trim().toLowerCase())).sort((a,b) => a.createdAt.localeCompare(b.createdAt)));
const needsInterview = (item: TeamApplication) => item.status === "pending" && item.interview?.status === "pending";
const canReview = (item: TeamApplication) => hasPermission("recruitment", item.direction) && (auth.user?.role === "admin" || item.userId !== auth.user?.id);
const canReopen = (item: TeamApplication) => canReview(item) && item.trainingStatus === "eliminated" && hasPermission("assessment", item.direction);
const nextPending = computed(() => filtered.value.find(item => item.id !== selected.value?.id && item.status === "pending" && canReview(item)));
async function load() {
  const version = ++generation;
  loading.value = true; error.value = "";
  try {
    const response = await api.get<{ items: TeamSpace[] }>("/teams");
    if (version !== generation) return;
    const visibleTeams = (response.items || []).filter(team => team.directions.some(value => hasPermission("recruitment", value)));
    teams.value = visibleTeams;
    const results = await Promise.allSettled(visibleTeams.map(team => api.get<{ items: TeamApplication[] }>(`/teams/${encodeURIComponent(team.id)}/applications`)));
    if (version !== generation) return;
    const failed: string[] = [];
    const entries: TeamApplication[] = [];
    for (const [index, result] of results.entries()) {
      if (result.status === "fulfilled") entries.push(...(result.value.items || []).filter(item => hasPermission("recruitment", item.direction)));
      else failed.push(visibleTeams[index]!.name);
    }
    applications.value = entries;
    if (failed.length) error.value = `以下团队暂时加载失败，当前列表和计数不包含这些报名：${failed.join("、")}。请重试。`;
    if (selected.value) selected.value = entries.find(item => item.id === selected.value?.id) || null;
    if (route.query.application && !selected.value) {
      const item = entries.find(item => item.id === route.query.application);
      if (item) view(item);
    }
  } catch (e) { if (version === generation) error.value = (e as Error).message; }
  finally { if (version === generation) loading.value = false; }
}
function view(item: TeamApplication) { selected.value = item; feedback.value = item.feedback || ""; detailError.value = ""; }
async function review(result: "accepted" | "rejected", reopenTraining = false) {
  const item = selected.value;
  if (!item || saving.value || !canReview(item)) return;
  if (result === "accepted" && needsInterview(item)) { detailError.value = "请先到面试管理确认结果"; return; }
  if (result === "accepted" && item.trainingStatus === "eliminated" && !reopenTraining) { if (canReopen(item)) reopen.value = true; return; }
  saving.value = true; detailError.value = "";
  try {
    const response = await api.patch<{ item: TeamApplication }>(`/team-applications/${encodeURIComponent(item.id)}`, { status: result, feedback: feedback.value.trim(), ...(reopenTraining ? { reopenTraining: true } : {}) });
    // Keep the profile from the review list: mutation responses only contain application fields.
    const updated = { ...item, ...response.item, profile: response.item.profile || item.profile };
    applications.value = applications.value.map(entry => entry.id === item.id ? updated : entry);
    selected.value = updated;
    reopen.value = false;
    notice.value = result === "accepted" ? reopenTraining ? "已重新录取，开启本组新一轮考核，历史记录已保留。" : "已通过报名，以新生身份加入本组。" : "已保存未通过结果与审核意见。";
  } catch (e) { detailError.value = (e as Error).message; reopen.value = false; }
  finally { saving.value = false; }
}
watch(() => route.query, (query) => {
  teamId.value = typeof query.team === "string" ? query.team : "";
  direction.value = typeof query.direction === "string" ? query.direction : "";
  status.value = typeof query.status === "string" && statusOptions.some(item => item.value === query.status) ? query.status : "pending";
});
onMounted(load);
onUnmounted(() => { generation++; });
</script>
<template>
  <div class="page-content applications-page">
    <div class="application-head"><div><h1 class="page-title">报名审核</h1><p class="page-subtitle">集中预览证件照、个人资料和 PDF 简历；每个组独立审核与考核。</p></div><div class="d-flex ga-2"><v-btn to="/teams" variant="text" prepend-icon="mdi-account-multiple-outline">团队报名</v-btn><v-btn prepend-icon="mdi-refresh" variant="tonal" :loading="loading" @click="load">刷新</v-btn></div></div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <div class="review-counts"><button v-for="item in statusOptions" :key="item.value" :class="{ active: status === item.value }" @click="status = item.value"><strong>{{ counts[item.value as keyof typeof counts] }}</strong><span>{{ item.title }}</span></button></div>
    <div class="review-filters"><v-text-field v-model="search" label="搜索姓名、学号、专业或班级" prepend-inner-icon="mdi-magnify" hide-details clearable /><v-select v-model="direction" :items="[{ title: '全部授权组别', value: '' }, ...groups]" label="报名组别" hide-details /><v-select v-model="teamId" :items="[{ title: '全部团队', value: '' }, ...teams.map(t => ({ title: t.name, value: t.id }))]" label="报名团队" hide-details /></div>
    <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-5" />
    <v-card v-if="filtered.length" class="review-list"><div class="review-list-caption">共 {{ filtered.length }} 份报名 · 按报名时间从早到晚</div><button v-for="item in filtered" :key="item.id" class="review-row" @click="view(item)"><v-avatar size="45" color="grey-lighten-3"><v-img v-if="item.profile?.photoFileId" :src="previewUrl(item.profile.photoFileId)" alt="本人证件照" /><v-icon v-else>mdi-account-outline</v-icon></v-avatar><div class="review-person"><strong>{{ item.userName }}</strong><span>{{ item.profile?.major || '专业未填写' }} · {{ item.profile?.className || '班级未填写' }}</span><small>{{ item.profile?.studentNumber || '学号未填写' }} · {{ dateTime(item.createdAt) }}</small></div><div class="review-team"><v-chip color="primary" variant="tonal" size="small">{{ groupLabel(item.direction) }}</v-chip><span>{{ teamName(item.teamId) }}</span></div><v-chip :color="statusColor(item)" variant="tonal" size="small">{{ statusLabel(item) }}</v-chip><span class="review-action">快速预览<v-icon size="18">mdi-chevron-right</v-icon></span></button></v-card>
    <v-card v-else-if="!loading" class="pa-10 text-center text-medium-emphasis"><v-icon size="42" class="mb-3">mdi-clipboard-check-outline</v-icon><p>{{ status === 'pending' && !search ? '当前筛选范围内没有待审核报名。' : '没有符合条件的报名。' }}</p><v-btn v-if="status !== 'all'" variant="text" color="primary" class="mt-3" @click="status = 'all'">查看全部状态</v-btn></v-card>
    <v-dialog :model-value="!!selected" max-width="1180" :fullscreen="smAndDown" scrollable :persistent="saving" @update:model-value="!$event && (selected = null)"><v-card v-if="selected"><v-card-title class="review-dialog-head"><div><span>{{ selected.userName }} · 报名预览</span><small>{{ teamName(selected.teamId) }} · {{ groupLabel(selected.direction) }}</small></div><v-btn icon="mdi-close" variant="text" aria-label="关闭报名预览" :disabled="saving" @click="selected = null" /></v-card-title><v-card-text class="review-dialog-body"><v-alert v-if="detailError" type="error" variant="tonal" class="mb-4">{{ detailError }}</v-alert><div class="review-detail"><section class="review-info"><div class="review-profile-top"><img v-if="selected.profile?.photoFileId" :src="previewUrl(selected.profile.photoFileId)" alt="报名者本人证件照" /><div v-else class="photo-placeholder"><v-icon size="36">mdi-account-box-outline</v-icon></div><div><h2>{{ selected.userName }}</h2><v-chip :color="statusColor(selected)" variant="tonal" class="my-2">{{ statusLabel(selected) }}</v-chip><p>{{ dateTime(selected.createdAt) }} 报名</p></div></div><dl v-if="selected.profile" class="review-profile"><div><dt>性别</dt><dd>{{ selected.profile.gender }}</dd></div><div><dt>学校</dt><dd>{{ selected.profile.university }}</dd></div><div><dt>专业</dt><dd>{{ selected.profile.major }}</dd></div><div><dt>班级</dt><dd>{{ selected.profile.className }}</dd></div><div><dt>学号</dt><dd>{{ selected.profile.studentNumber }}</dd></div><div><dt>电话</dt><dd>{{ selected.profile.phone }}</dd></div><div><dt>兴趣爱好</dt><dd>{{ selected.profile.hobbies || '未填写' }}</dd></div></dl><p v-else class="text-medium-emphasis my-4">个人资料暂不可用，请刷新后重试。</p><v-alert v-if="needsInterview(selected)" type="info" variant="tonal" class="my-3">报名已进入面试环节，面试通过后自动录取。<v-btn :to="'/recruitment?application=' + selected.id" variant="text">安排 / 确认面试</v-btn></v-alert><v-alert v-if="selected.interview?.status === 'exempt'" type="info" variant="tonal" class="my-3">本轮已完成线下面试，可直接确认录取。</v-alert><h3>报名说明</h3><p class="review-motivation">{{ selected.motivation || '未填写' }}</p><v-alert v-if="selected.trainingStatus" :type="selected.trainingStatus === 'eliminated' ? 'warning' : 'info'" variant="tonal" density="compact" class="my-4"><template v-if="selected.trainingCycle">第 {{ selected.trainingCycle }} 轮 · </template>{{ selected.trainingStageTitle || '阶段考核' }}<p v-if="selected.trainingStatus === 'eliminated'" class="mt-2">本组本轮考核已淘汰。重新录取需要报名审核与考核管理权限，将开启新周期并保留历史记录。</p><v-btn v-if="hasPermission('assessment', selected.direction)" to="/training" variant="text" size="small">查看阶段考核</v-btn></v-alert><v-textarea v-model="feedback" label="审核意见（随审核结果保存）" rows="3" maxlength="2000" :disabled="saving || !canReview(selected) || selected.status === 'accepted' && !canReopen(selected)" counter /><p v-if="!canReview(selected)" class="text-caption text-medium-emphasis">组管理员不能审核自己的报名。</p><p v-else-if="selected.status === 'accepted' && !canReopen(selected)" class="text-caption text-medium-emphasis">已录取。后续阶段通过与淘汰请在新生考核中处理。</p></section><section class="review-resume"><div class="resume-head"><h3>PDF 简历</h3><div class="d-flex ga-1"><v-btn :href="previewUrl(selected.resumeFileId)" target="_blank" rel="noopener" size="small" variant="text">新窗口预览</v-btn><v-btn :href="downloadUrl(selected.resumeFileId)" size="small" variant="tonal" prepend-icon="mdi-download">下载</v-btn></div></div><iframe :key="selected.resumeFileId" :src="previewUrl(selected.resumeFileId)" title="报名者 PDF 简历预览" /><p class="text-caption text-medium-emphasis mt-2">若浏览器无法显示 PDF，可在新窗口打开或下载查看。</p></section></div></v-card-text><v-divider /><v-card-actions class="review-actions"><v-btn v-if="nextPending" :disabled="saving" prepend-icon="mdi-skip-next-outline" variant="text" @click="view(nextPending!)">下一份待审核</v-btn><v-spacer /><v-btn :disabled="saving" variant="text" @click="selected = null">关闭</v-btn><v-btn v-if="canReview(selected) && selected.status !== 'accepted'" :disabled="saving" color="error" variant="text" @click="review('rejected')">未通过</v-btn><v-btn v-if="!needsInterview(selected) && canReview(selected) && (selected.status !== 'accepted' && selected.trainingStatus !== 'eliminated' || canReopen(selected))" :loading="saving" color="primary" variant="flat" @click="review('accepted')">{{ canReopen(selected) ? '重新录取' : '通过并加入本组' }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-dialog v-model="reopen" max-width="520" :persistent="saving"><v-card><v-card-title>确认重新录取</v-card-title><v-card-text>「{{ selected?.userName }}」在本组上一轮考核已淘汰。重新录取将以新生身份开启本组新一轮阶段考核，历史记录和其他组资格不受影响。</v-card-text><v-card-actions><v-spacer /><v-btn :disabled="saving" @click="reopen = false">取消</v-btn><v-btn color="primary" :loading="saving" @click="review('accepted', true)">确认重新录取</v-btn></v-card-actions></v-card></v-dialog>
    <v-snackbar :model-value="!!notice" @update:model-value="!$event && (notice = '')">{{ notice }}</v-snackbar>
  </div>
</template>
<style scoped>
.application-head{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:24px}.review-counts{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px;margin-bottom:22px}.review-counts button{background:white;border:1px solid #e3e7ec;padding:16px 12px;display:flex;align-items:center;gap:12px;border-radius:4px;text-align:left}.review-counts button.active{border-color:rgb(var(--v-theme-primary));background:#eef6ff}.review-counts strong{font-size:26px;font-weight:400;color:rgb(var(--v-theme-primary))}.review-counts span{font-size:13px;color:#6d7780}.review-filters{display:grid;grid-template-columns:1.6fr 1fr 1.3fr;gap:20px;margin-bottom:24px}.review-list-caption{padding:15px 20px;font-size:12px;color:#7b848d;border-bottom:1px solid #edf0f3}.review-row{display:flex;align-items:center;width:100%;gap:18px;padding:20px;text-align:left;border-bottom:1px solid #edf0f3}.review-row:last-child{border-bottom:0}.review-row:hover{background:#f6faff}.review-row:focus-visible{outline:2px solid rgb(var(--v-theme-primary));outline-offset:-2px}.review-person{min-width:0;flex:1;display:flex;flex-direction:column;gap:4px}.review-person strong{font-weight:500;font-size:16px}.review-person span{font-size:13px;color:#69737c;overflow-wrap:anywhere}.review-person small{font-size:11px;color:#9099a1}.review-team{display:flex;flex-direction:column;align-items:flex-start;gap:5px;max-width:220px}.review-team>span{font-size:11px;color:#8a949d}.review-action{font-size:12px;color:rgb(var(--v-theme-primary));white-space:nowrap}.review-dialog-head{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;white-space:normal}.review-dialog-head>div{min-width:0}.review-dialog-head small{display:block;font-size:12px;font-weight:400;color:#87929b;margin-top:5px}.review-dialog-head>.v-btn{flex-shrink:0}.review-dialog-body{padding-top:12px!important}.review-detail{display:grid;grid-template-columns:minmax(0,.9fr) minmax(0,1.2fr);gap:28px}.review-profile-top{display:flex;gap:18px;align-items:center;margin-bottom:18px}.review-profile-top img,.photo-placeholder{width:84px;height:112px;object-fit:contain;background:#f2f4f6;border-radius:4px}.photo-placeholder{display:grid;place-items:center}.review-profile-top h2{font-size:19px;font-weight:500}.review-profile-top p{font-size:11px;color:#7c8892}.review-profile{display:grid;gap:12px;font-size:13px;margin:20px 0}.review-profile>div{display:grid;grid-template-columns:68px minmax(0,1fr);gap:8px}.review-profile dt{color:#89939b}.review-profile dd{margin:0;overflow-wrap:anywhere}.review-info h3,.review-resume h3{font-size:14px;font-weight:500}.review-motivation{font-size:13px;line-height:1.9;white-space:pre-wrap;overflow-wrap:anywhere;margin:9px 0 20px}.resume-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:12px;flex-wrap:wrap}.review-resume iframe{border:1px solid #e4e8ed;background:#f6f7f8;width:100%;height:620px;border-radius:3px}.review-actions{padding:12px 20px!important;flex-wrap:wrap}.review-actions .v-btn{margin-inline-start:0!important}@media(max-width:1050px){.review-counts button{flex-direction:column;gap:3px;text-align:center}.review-team{max-width:160px}.review-action{display:none}}@media(max-width:700px){.application-head{flex-wrap:wrap}.review-filters{grid-template-columns:1fr 1fr;gap:12px}.review-filters>.v-input:first-child{grid-column:1/-1}.review-counts{gap:5px}.review-counts button{padding:10px 2px}.review-counts strong{font-size:22px}.review-counts span{font-size:11px}.review-row{gap:10px;padding:15px 12px;flex-wrap:wrap}.review-team{margin-left:55px;max-width:calc(100% - 55px)}.review-row>.v-chip{margin-left:auto}.review-person{flex-basis:calc(100% - 65px)}.review-detail{grid-template-columns:1fr;gap:20px}.review-resume iframe{height:480px}.review-actions{padding:10px!important}.review-actions .v-spacer{display:none}.review-dialog-head{font-size:18px}}
</style>
