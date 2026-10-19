<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useDisplay } from "vuetify";
import { useRoute, useRouter } from "vue-router";
import { api } from "../lib/api";
import { hasPermission, isParticipant, groupLabel } from "../lib/permissions";
import { auth } from "../lib/auth";
import { canAccessLearning, learningAccess, refreshLearningAccess } from "../lib/learning-access";
import {
  directions,
  dateTime,
  downloadUrl,
  previewUrl,
  type TeamSpace,
  type TeamApplication,
  type Profile,
} from "../lib/platform";
import type { Settings } from "../lib/types";
import FileUpload from "../components/FileUpload.vue";
const { smAndDown } = useDisplay();
const route = useRoute(), router = useRouter();
const teams = ref<TeamSpace[]>([]),
  loading = ref(true),
  error = ref(""),
  notice = ref(""),
  saving = ref(false),
  uploading = ref(false),
  search = ref(""),
  selected = ref<TeamSpace | null>(null),
  profile = ref<Profile | null>(null),
  applications = ref<TeamApplication[]>([]),
  detailLoading = ref(false),
  detailError = ref(""),
  editor = ref(false),
  editing = ref("");
const isAdmin = computed(() => auth.user?.role === "admin");
const canApply = computed(() => isParticipant());
const canReviewApplications = computed(() => hasPermission("recruitment"));
const canReviewTeam = (team: TeamSpace) => team.directions.some(value => hasPermission("recruitment", value));
const canReviewSelected = computed(() => Boolean(selected.value && canReviewTeam(selected.value)));
const form = reactive({
  name: "",
  description: "",
  competitions: ["RoboCon", "RoboMaster"],
  directions: [...directions],
  status: "open",
  deadline: "",
});
const brand = ref<Settings | null>(null);
const competitionsOf = (t: TeamSpace) => t.competitions?.length ? t.competitions : [t.competition === "ROBOCON" ? "RoboCon" : t.competition].filter(Boolean);
const application = reactive({
  direction: "",
  resume: [] as string[],
  motivation: "",
});
const feedback = ref<Record<string, string>>({});
const reopening = ref<TeamApplication | null>(null);
const filtered = computed(() =>
  teams.value.filter((t) =>
    `${t.name} ${t.description} ${competitionsOf(t).join(" ")}`
      .toLowerCase()
      .includes(search.value.toLowerCase()),
  ),
);
const trainingEliminated = computed(() => teams.value.some(team => team.myApplication?.trainingStatus === "eliminated"));
const requestedDirection = computed(() => typeof route.query.direction === "string" && directions.includes(route.query.direction) ? route.query.direction : "");
const statusLabel = (item?: TeamApplication | null) => !item ? "未报名" : item.trainingStatus === "eliminated" ? "本轮已淘汰" :
  ({
    pending: item.interview?.status === "pending" ? "待面试" : "待审核",
    submitted: "待审核",
    accepted: item?.trainingStatus === "completed" ? "考核已通过" : "已录取",
    rejected: "未通过",
  })[item.status];
const statusColor = (item?: TeamApplication | null) =>
  item?.trainingStatus === "eliminated" ? "error" : item?.status === "accepted"
    ? "success"
    : item?.status === "rejected"
      ? "error"
      : "warning";
function closed(t: TeamSpace) {
  return (
    t.status === "closed" ||
    Boolean(t.deadline && Date.parse(t.deadline) < Date.now())
  );
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [data, p, settings] = await Promise.all([
      api.get<{ items: TeamSpace[] }>("/teams"),
      api.get<{ profile: Profile }>("/profile"),
      api.get<{ settings: Settings }>("/settings"),
      refreshLearningAccess(),
    ]);
    teams.value = data.items;
    profile.value = p.profile;
    brand.value = settings.settings;
    if (selected.value) {
      selected.value = teams.value.find(team => team.id === selected.value?.id) || null;
      if (selected.value) await loadApplications(selected.value.id);
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
function edit(team?: TeamSpace) {
  editing.value = team?.id || "";
  Object.assign(form, {
    name: team?.name || `${brand.value?.teamName || "沧溟"} / ${brand.value?.secondaryTeamName || "MOVE"}`,
    description: team?.description || "",
    competitions: team ? competitionsOf(team) : [...(brand.value?.competitions?.length ? brand.value.competitions : ["RoboCon", "RoboMaster"])],
    directions: team ? [...team.directions] : [...directions],
    status: team?.status || "open",
    deadline: team?.deadline
      ? new Date(
          new Date(team.deadline).getTime() -
            new Date(team.deadline).getTimezoneOffset() * 60000,
        )
          .toISOString()
          .slice(0, 16)
      : "",
  });
  editor.value = true;
}
async function saveTeam() {
  if (
    !form.name.trim() ||
    !form.description.trim() ||
    !form.directions.length ||
    !form.competitions.length
  ) {
    notice.value = "请填写团队名称、介绍，并至少选择一个赛事和方向。";
    return;
  }
  saving.value = true;
  try {
    const body = {
      ...form,
      competition: form.competitions[0],
      deadline: form.deadline ? new Date(form.deadline).toISOString() : "",
    };
    if (editing.value) await api.patch(`/teams/${editing.value}`, body);
    else await api.post("/teams", body);
    editor.value = false;
    notice.value = "团队信息已保存。";
    await load();
  } catch (e) {
    notice.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
async function open(team: TeamSpace) {
  if (canReviewTeam(team)) {
    await router.push({ path: "/applications", query: { team: team.id } });
    return;
  }
  selected.value = team;
  application.direction =
    team.myApplication?.direction || team.directions[0] || "";
  application.resume = team.myApplication?.resumeFileId
    ? [team.myApplication.resumeFileId]
    : [];
  application.motivation = team.myApplication?.motivation || "";
  await loadApplications(team.id);
}
async function loadApplications(teamId: string) {
  detailLoading.value = true;
  detailError.value = "";
  try {
    const { items } = await api.get<{ items: TeamApplication[] }>(
      `/teams/${teamId}/applications`,
    );
    if (selected.value?.id !== teamId) return;
    applications.value = items.filter(item => hasPermission("recruitment", item.direction));
    for (const item of items) feedback.value[item.id] = item.feedback || "";
  } catch (e) {
    detailError.value = (e as Error).message;
  } finally {
    detailLoading.value = false;
  }
}
async function apply() {
  if (!selected.value || saving.value || uploading.value) return;
  if (!application.direction || !application.resume.length) {
    detailError.value = "请选择报名方向并上传 PDF 简历。";
    return;
  }
  saving.value = true;
  detailError.value = "";
  try {
    const id = selected.value.id;
    const { item } = await api.post<{ item: TeamApplication }>(
      `/teams/${id}/applications`,
      {
        direction: application.direction,
        resumeFileId: application.resume[0],
        motivation: application.motivation,
      },
    );
    selected.value = { ...selected.value, myApplication: item };
    teams.value = teams.value.map((t) => (t.id === id ? selected.value! : t));
    notice.value = `已提交${groupLabel(item.direction)}报名，侧栏已加 🌟。面试通过并录取后可完成该组作业和考试。`;
    await loadApplications(id);
    await refreshLearningAccess();
  } catch (e) {
    detailError.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
const canReopen = (item: TeamApplication) => item.trainingStatus === "eliminated" && hasPermission("assessment", item.direction) && hasPermission("recruitment", item.direction);
async function review(item: TeamApplication, status: "accepted" | "rejected", reopenTraining = false) {
  if (!hasPermission("recruitment", item.direction)) return;
  if (status === "accepted" && item.trainingStatus === "eliminated" && !reopenTraining) {
    if (canReopen(item)) reopening.value = item;
    return;
  }
  saving.value = true;
  detailError.value = "";
  try {
    await api.patch(`/team-applications/${item.id}`, {
      status,
      feedback: feedback.value[item.id] || "",
      ...(reopenTraining ? { reopenTraining: true } : {}),
    });
    reopening.value = null;
    await load();
    notice.value =
      status === "accepted"
        ? reopenTraining ? "已重新录取，以新生身份开启新一轮阶段考核；历史记录已保留。" : "已通过审核，以新生身份入队并开始阶段考核。"
        : "审核结果已保存。";
  } catch (e) {
    detailError.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
watch(() => JSON.stringify([canAccessLearning.value, learningAccess.appliedDirections, learningAccess.enrolledDirections]), () => {
  if (!loading.value) void load();
});
onMounted(load);
</script>
<template>
  <div class="page-content">
    <div class="page-head">
      <div>
        <h1 class="page-title">团队报名</h1>
        <p class="page-subtitle">
          {{ brand?.teamName || "沧溟" }} / {{ brand?.secondaryTeamName || "MOVE" }} 机器人战队 · {{ (brand?.competitions || ["RoboCon", "RoboMaster"]).join(" / ") }}。五个组别独立报名，可加入多个组；审核后完成对应组的作业与考试。
        </p>
      </div>
      <div class="d-flex flex-wrap ga-2">
      <v-btn v-if="canReviewApplications" to="/applications" color="primary" variant="tonal" prepend-icon="mdi-account-check-outline">报名审核</v-btn>
      <v-btn
        v-if="isAdmin"
        color="primary"
        prepend-icon="mdi-plus"
        @click="edit()"
        >发布团队</v-btn
      >
      </div>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    >
    <v-alert v-if="canApply && !canAccessLearning" :type="learningAccess.error || trainingEliminated ? 'warning' : 'info'" variant="tonal" class="mb-4">
      {{ learningAccess.error || (trainingEliminated ? "当前没有可用的成员学习资格。已淘汰组别的考核记录仍保留，可申请其它组，或由管理员重新录取。" : "侧栏展示全部五组。任一组审核通过后可浏览各组学习内容，作业提交与考试参加按各组录取结果开放。") }}
      <v-btn variant="text" :loading="loading" @click="load">刷新审核状态</v-btn>
    </v-alert>
    <v-alert v-if="requestedDirection && route.query.admissionRequired" type="info" variant="tonal" class="mb-4">{{ groupLabel(requestedDirection) }}的学习内容需取得成员学习资格后查看。请在下方选择组别报名并等待审核。</v-alert>
    <v-alert
      v-if="profile && !profile.complete && !isAdmin"
      type="info"
      variant="tonal"
      class="mb-4"
      >报名需要完整个人资料和本人证件照。<v-btn to="/profile" variant="text"
        >完善资料</v-btn
      ></v-alert
    ><v-text-field
      v-model="search"
      label="搜索团队"
      prepend-inner-icon="mdi-magnify"
      class="mb-4"
      hide-details
    /><v-progress-linear v-if="loading" indeterminate />
    <div v-else-if="filtered.length" class="team-grid">
      <v-card v-for="team in filtered" :key="team.id" class="team-card"
        ><div class="d-flex flex-wrap align-center ga-2 mb-4">
          <v-avatar color="primary" variant="tonal"
            ><v-icon>mdi-account-group</v-icon></v-avatar
          ><v-chip v-for="competition in competitionsOf(team)" :key="competition" size="small">{{ competition }}</v-chip
          ><v-chip
            :color="closed(team) ? 'grey' : 'success'"
            size="small"
            class="ml-auto"
            >{{ closed(team) ? "已截止" : "招募中" }}</v-chip
          >
        </div>
        <h2>{{ team.name }}</h2>
        <p class="description">{{ team.description }}</p>
        <div class="d-flex flex-wrap ga-2 mb-4">
          <v-chip
            v-for="d in team.directions"
            :key="d"
            size="x-small"
            variant="outlined"
            >{{ groupLabel(d) }}{{ team.myApplication?.direction === d && ['pending', 'submitted', 'accepted'].includes(team.myApplication.status) && team.myApplication.trainingStatus !== 'eliminated' ? ' 🌟' : '' }}</v-chip
          >
        </div>
        <div class="team-meta">
          <span>{{ team.memberCount }} 位成员</span
          ><span>{{
            team.deadline ? `${dateTime(team.deadline)} 截止` : "长期招募"
          }}</span>
        </div>
        <div class="card-footer">
          <v-chip
            v-if="team.myApplication"
            :color="statusColor(team.myApplication)"
            size="small"
            >{{ statusLabel(team.myApplication) }}</v-chip
          ><v-btn v-if="isAdmin" variant="text" @click="edit(team)">编辑</v-btn
          ><v-spacer /><v-btn
            color="primary"
            variant="text"
            @click="open(team)"
            >{{
              canReviewTeam(team)
                ? "报名审核"
                : team.myApplication
                  ? "查看报名"
                  : canApply
                    ? "查看并报名"
                    : "查看团队"
            }}</v-btn
          >
        </div></v-card
      >
    </div>
    <v-card v-else class="pa-10 text-center text-medium-emphasis">{{
      teams.length ? "没有匹配的团队。" : "还没有发布团队。"
    }}</v-card>
    <v-dialog
      :model-value="!!selected"
      max-width="850"
      :fullscreen="smAndDown"
      scrollable
      :persistent="saving || uploading"
      @update:model-value="!$event && (selected = null)"
      ><v-card v-if="selected"
        ><v-card-title class="dialog-head"
          ><span
            >{{ selected.name }} · {{ canReviewSelected ? "报名审核" : "团队报名" }}</span
          ><v-btn
            icon="mdi-close"
            variant="text"
            :disabled="saving || uploading"
            aria-label="关闭团队详情"
            @click="selected = null" /></v-card-title
        ><v-card-text
          ><p class="mb-4 pre-wrap">{{ selected.description }}</p>
          <v-alert v-if="detailError" type="error" variant="tonal" class="mb-4"
            >{{ detailError
            }}<v-btn
              v-if="canReviewSelected"
              variant="text"
              @click="loadApplications(selected.id)"
              >重试</v-btn
            ></v-alert
          ><v-progress-linear v-if="detailLoading" indeterminate />
          <template v-else-if="canReviewSelected"
            ><p class="mb-4 text-medium-emphasis">
              共
              {{ applications.length }}
              份报名；录取后以新生身份参加本组阶段考核，开放各组学习内容。各组独立审核与考核，淘汰只影响本组，最终通过后再晋升。
            </p>
            <v-card
              v-for="item in applications"
              :key="item.id"
              variant="outlined"
              class="application-card mb-4"
              ><div class="d-flex align-center ga-3">
                <v-avatar color="grey-lighten-3" size="48"
                  ><v-img
                    v-if="item.profile?.photoFileId"
                    :src="previewUrl(item.profile.photoFileId)"
                    alt="报名者本人证件照"
                  /><v-icon v-else>mdi-account</v-icon></v-avatar
                >
                <div>
                  <strong>{{ item.userName }}</strong>
                  <p class="text-caption">
                    {{ groupLabel(item.direction) }} · {{ dateTime(item.createdAt) }}
                  </p>
                </div>
                <v-chip
                  class="ml-auto"
                  :color="statusColor(item)"
                  size="small"
                  >{{ statusLabel(item) }}</v-chip
                >
              </div>
              <p v-if="item.trainingStatus" class="text-caption text-medium-emphasis mt-3">
                <template v-if="item.trainingCycle">第 {{ item.trainingCycle }} 轮考核 · </template>{{ item.trainingStageTitle || '阶段考核' }}{{ item.trainingStatus === 'eliminated' ? ' · 本组提交权限已关闭，其它组录取资格不受影响。重新录取将开启新周期。' : '' }}
                <v-btn v-if="hasPermission('assessment', item.direction)" to="/training" size="small" variant="text" color="primary">阶段考核</v-btn>
              </p>
              <dl v-if="item.profile" class="profile-detail">
                <div>
                  <dt>性别</dt>
                  <dd>{{ item.profile.gender }}</dd>
                </div>
                <div>
                  <dt>学校</dt>
                  <dd>{{ item.profile.university }}</dd>
                </div>
                <div>
                  <dt>专业 / 班级</dt>
                  <dd>
                    {{ item.profile.major }} / {{ item.profile.className }}
                  </dd>
                </div>
                <div>
                  <dt>学号</dt>
                  <dd>{{ item.profile.studentNumber }}</dd>
                </div>
                <div>
                  <dt>联系电话</dt>
                  <dd>{{ item.profile.phone }}</dd>
                </div>
                <div>
                  <dt>兴趣爱好</dt>
                  <dd>{{ item.profile.hobbies || "未填写" }}</dd>
                </div>
              </dl>
              <p v-if="item.motivation" class="pre-wrap my-3">
                {{ item.motivation }}
              </p>
              <v-btn
                :href="downloadUrl(item.resumeFileId)"
                color="primary"
                variant="tonal"
                size="small"
                prepend-icon="mdi-file-pdf-box"
                >下载 PDF 简历</v-btn
              ><v-textarea
                v-model="feedback[item.id]"
                label="审核意见"
                rows="2"
                class="mt-4"
                maxlength="2000"
              />
              <v-btn v-if="item.interview?.status === 'pending'" :to="'/recruitment?application=' + item.id" variant="text">进入面试管理</v-btn><div class="d-flex justify-end ga-2">
                <v-btn
                  color="error"
                  variant="text"
                  :disabled="saving || item.status === 'accepted'"
                  @click="review(item, 'rejected')"
                  >未通过</v-btn
                ><v-btn
                  color="primary"
                  :disabled="saving || item.interview?.status === 'pending' || (item.status === 'accepted' && !canReopen(item))"
                  @click="review(item, 'accepted')"
                  >{{
                    item.interview?.status === "pending" ? "请先确认面试结果" : canReopen(item) ? "重新录取" : item.status === "accepted" ? statusLabel(item) : "通过并加入团队"
                  }}</v-btn
                >
              </div></v-card
            >
            <p
              v-if="!applications.length"
              class="pa-8 text-center text-medium-emphasis"
            >
              尚未收到报名。
            </p></template
          >
          <template v-else
            ><v-alert
              v-if="selected.myApplication"
              :type="
                selected.myApplication.trainingStatus === 'eliminated' ? 'warning' : selected.myApplication.status === 'accepted'
                  ? 'success'
                  : 'info'
              "
              variant="tonal"
              class="mb-5"
              >报名状态：{{ statusLabel(selected.myApplication) }}
              <p v-if="selected.myApplication.trainingStatus === 'eliminated'" class="mt-2">本组本轮阶段考核已淘汰，不能继续提交本组作业或参加考试；其它组录取资格不受影响。历史记录仍保留，重新录取由管理员或获授权组管理员确认。</p>
              <p v-else-if="selected.myApplication.trainingStatus === 'training'" class="mt-2">当前考核阶段：{{ selected.myApplication.trainingStageTitle }}。完成最终考核后确认梯队或正式队员身份。</p>
              <p v-if="selected.myApplication.feedback" class="mt-2 pre-wrap">
                审核意见：{{ selected.myApplication.feedback }}
              </p></v-alert
            ><template
              v-if="
                canApply &&
                (!selected.myApplication ||
                  selected.myApplication.status === 'rejected')
              "
              ><v-alert v-if="closed(selected)" type="info" variant="tonal"
                >本团队当前已停止招募。</v-alert
              ><v-alert
                v-else-if="!profile?.complete"
                type="info"
                variant="tonal"
                >请先完善必需个人资料和本人证件照。<v-btn to="/profile" variant="text"
                  >完善资料</v-btn
                ></v-alert
              >
              <form v-else @submit.prevent="apply">
                <v-alert type="info" variant="tonal" class="mb-4" density="compact">{{ selected.directions.length === 1 ? `报名${groupLabel(selected.directions[0]!)}后` : '提交报名后' }}，侧栏会显示 🌟。审核通过后以新生身份入组，完成本组作业、考试和阶段考核；其它组学习内容也可浏览。</v-alert>
                <div v-if="selected.directions.length === 1" class="fixed-group mb-5"><span>报名组别</span><v-chip color="primary" variant="tonal">{{ groupLabel(selected.directions[0]!) }}</v-chip></div>
                <v-select
                  v-else
                  v-model="application.direction"
                  :items="selected.directions"
                  label="报名方向 *"
                  aria-required="true"
                /><FileUpload
                  v-model="application.resume"
                  label="个人简历（PDF）*"
                  accept=".pdf"
                  single
                  :disabled="saving"
                  @busy="uploading = $event"
                /><v-textarea
                  v-model="application.motivation"
                  label="报名说明（选填）"
                  rows="4"
                  maxlength="5000"
                />
                <div class="d-flex justify-end">
                  <v-btn
                    color="primary"
                    type="submit"
                    :loading="saving"
                    :disabled="uploading"
                    >提交报名</v-btn
                  >
                </div>
              </form></template
            >
            <div v-else-if="selected.myApplication">
              <p class="mb-3">
                报名方向：{{ groupLabel(selected.myApplication.direction) }}
              </p>
              <v-btn
                :href="downloadUrl(selected.myApplication.resumeFileId)"
                variant="tonal"
                color="primary"
                prepend-icon="mdi-file-pdf-box"
                >查看已提交简历</v-btn
              ><v-btn
                v-if="selected.myApplication.status === 'accepted' && canAccessLearning && selected.myApplication.trainingStatus !== 'eliminated'"
                :to="{ path: '/learning', query: { direction: selected.myApplication.direction, section: 'home' } }"
                color="primary"
                class="ml-3"
                >进入学习任务</v-btn
              ><v-btn v-if="selected.myApplication.trainingStatus" to="/training" variant="text" color="primary" class="ml-3">查看考核记录</v-btn>
            </div></template
          ></v-card-text
        ></v-card
      ></v-dialog
    >
    <v-dialog :model-value="!!reopening" max-width="500" :persistent="saving" @update:model-value="!$event && (reopening = null)">
      <v-card v-if="reopening">
        <v-card-title>确认重新录取</v-card-title>
        <v-card-text>「{{ reopening.userName }}」上一轮阶段考核已淘汰。重新录取后将以新生身份从第一阶段开始，恢复各组学习权限，并保留上一轮所有考核记录。</v-card-text>
        <v-card-actions><v-spacer /><v-btn :disabled="saving" @click="reopening = null">取消</v-btn><v-btn color="primary" :loading="saving" @click="review(reopening, 'accepted', true)">确认重新录取</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog
      v-model="editor"
      max-width="720"
      :fullscreen="smAndDown"
      scrollable
      :persistent="saving"
      ><v-card
        ><v-card-title class="dialog-head"
          ><span>{{ editing ? "编辑团队" : "发布团队" }}</span
          ><v-btn
            icon="mdi-close"
            variant="text"
            aria-label="关闭团队编辑"
            :disabled="saving"
            @click="editor = false" /></v-card-title
        ><v-card-text
          ><form @submit.prevent="saveTeam">
            <v-text-field
              v-model="form.name"
              label="团队名称 *"
              maxlength="120"
              required
            /><v-textarea
              v-model="form.description"
              label="团队介绍与招募要求 *"
              rows="5"
              maxlength="10000"
              required
            /><v-select
              v-model="form.competitions"
              :items="['RoboCon', 'RoboMaster']"
              multiple chips
              label="同时参加的赛事 *"
              aria-required="true"
            /><v-select
              v-model="form.directions"
              :items="directions"
              multiple
              chips
              label="招募方向 *"
              aria-required="true"
            /><v-row
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.deadline"
                  type="datetime-local"
                  label="报名截止时间（选填）" /></v-col
              ><v-col cols="12" sm="6"
                ><v-select
                  v-model="form.status"
                  :items="[
                    { title: '开放报名', value: 'open' },
                    { title: '停止报名', value: 'closed' },
                  ]"
                  label="报名状态" /></v-col
            ></v-row>
            <div class="d-flex justify-end">
              <v-btn color="primary" type="submit" :loading="saving">{{
                editing ? "保存修改" : "发布团队"
              }}</v-btn>
            </div>
          </form></v-card-text
        ></v-card
      ></v-dialog
    ><v-snackbar
      :model-value="!!notice"
      @update:model-value="!$event && (notice = '')"
      >{{ notice }}</v-snackbar
    >
  </div>
</template>
<style scoped>
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.fixed-group {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  font-size: 14px;
  color: #616161;
}
.team-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 18px;
}
.team-card {
  padding: 22px;
  display: flex;
  flex-direction: column;
}
.team-card h2 {
  font-size: 19px;
  font-weight: 500;
}
.description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  color: #616161;
  font-size: 14px;
  line-height: 1.8;
  margin: 12px 0 18px;
}
.team-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
  font-size: 12px;
  color: #757575;
  margin-top: auto;
}
.card-footer {
  display: flex;
  align-items: center;
  border-top: 1px solid #eee;
  padding-top: 10px;
  margin-top: 16px;
}
.dialog-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  white-space: normal;
  font-size: 19px;
}
.pre-wrap {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 14px;
  line-height: 1.8;
}
.application-card {
  padding: 20px;
}
.profile-detail {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 18px;
  font-size: 13px;
}
.profile-detail dt {
  font-size: 12px;
  color: #757575;
}
.profile-detail dd {
  margin-top: 3px;
  overflow-wrap: anywhere;
}
@media (max-width: 1200px) {
  .team-grid {
    grid-template-columns: 1fr 1fr;
  }
}
@media (max-width: 700px) {
  .team-grid {
    grid-template-columns: 1fr;
  }
  .page-head {
    flex-wrap: wrap;
    margin-bottom: 16px;
  }
  .profile-detail {
    grid-template-columns: 1fr;
  }
  .application-card {
    padding: 14px;
  }
}
</style>
