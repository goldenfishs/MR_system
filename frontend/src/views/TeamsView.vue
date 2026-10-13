<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
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
const canApply = computed(() => auth.user?.role === "student");
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
const filtered = computed(() =>
  teams.value.filter((t) =>
    `${t.name} ${t.description} ${competitionsOf(t).join(" ")}`
      .toLowerCase()
      .includes(search.value.toLowerCase()),
  ),
);
const statusLabel = (status?: string) =>
  ({
    pending: "待审核",
    submitted: "待审核",
    accepted: "已加入",
    rejected: "未通过",
  })[status || ""] || "未报名";
const statusColor = (status?: string) =>
  status === "accepted"
    ? "success"
    : status === "rejected"
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
    applications.value = items;
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
    notice.value = "报名已提交，审核结果会显示在这里。";
    await loadApplications(id);
    await refreshLearningAccess();
  } catch (e) {
    detailError.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
async function review(item: TeamApplication, status: "accepted" | "rejected") {
  saving.value = true;
  detailError.value = "";
  try {
    await api.patch(`/team-applications/${item.id}`, {
      status,
      feedback: feedback.value[item.id] || "",
    });
    await loadApplications(item.teamId);
    await load();
    notice.value =
      status === "accepted"
        ? "已通过审核，该用户已加入团队。"
        : "审核结果已保存。";
  } catch (e) {
    detailError.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
watch(canAccessLearning, () => {
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
          {{ brand?.teamName || "沧溟" }} / {{ brand?.secondaryTeamName || "MOVE" }} 机器人战队 · {{ (brand?.competitions || ["RoboCon", "RoboMaster"]).join(" / ") }}。选择方向、提交简历，审核后入队。
        </p>
      </div>
      <v-btn
        v-if="isAdmin"
        color="primary"
        prepend-icon="mdi-plus"
        @click="edit()"
        >发布团队</v-btn
      >
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    >
    <v-alert v-if="canApply && !canAccessLearning" :type="learningAccess.error ? 'warning' : 'info'" variant="tonal" class="mb-4">
      {{ learningAccess.error || "报名审核通过后，侧栏将显示技术分组，并开放知识库、作业和考试。" }}
      <v-btn variant="text" :loading="loading" @click="load">刷新审核状态</v-btn>
    </v-alert>
    <v-alert
      v-if="profile && !profile.complete && !isAdmin"
      type="info"
      variant="tonal"
      class="mb-4"
      >报名需要完整个人资料和本人照片。<v-btn to="/profile" variant="text"
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
        <div v-if="canAccessLearning" class="d-flex flex-wrap ga-2 mb-4">
          <v-chip
            v-for="d in team.directions"
            :key="d"
            size="x-small"
            variant="outlined"
            >{{ d }}组</v-chip
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
            :color="statusColor(team.myApplication.status)"
            size="small"
            >{{ statusLabel(team.myApplication.status) }}</v-chip
          ><v-btn v-if="isAdmin" variant="text" @click="edit(team)">编辑</v-btn
          ><v-spacer /><v-btn
            color="primary"
            variant="text"
            @click="open(team)"
            >{{
              isAdmin
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
            >{{ selected.name }} · {{ isAdmin ? "报名审核" : "团队报名" }}</span
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
              v-if="isAdmin"
              variant="text"
              @click="loadApplications(selected.id)"
              >重试</v-btn
            ></v-alert
          ><v-progress-linear v-if="detailLoading" indeterminate />
          <template v-else-if="isAdmin"
            ><p class="mb-4 text-medium-emphasis">
              共
              {{ applications.length }}
              份报名；通过审核即加入团队并获得对应任务的提交权限。
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
                    alt="报名者照片"
                  /><v-icon v-else>mdi-account</v-icon></v-avatar
                >
                <div>
                  <strong>{{ item.userName }}</strong>
                  <p class="text-caption">
                    {{ item.direction }}组 · {{ dateTime(item.createdAt) }}
                  </p>
                </div>
                <v-chip
                  class="ml-auto"
                  :color="statusColor(item.status)"
                  size="small"
                  >{{ statusLabel(item.status) }}</v-chip
                >
              </div>
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
              <div class="d-flex justify-end ga-2">
                <v-btn
                  color="error"
                  variant="text"
                  :disabled="saving || item.status === 'accepted'"
                  @click="review(item, 'rejected')"
                  >未通过</v-btn
                ><v-btn
                  color="primary"
                  :disabled="saving || item.status === 'accepted'"
                  @click="review(item, 'accepted')"
                  >{{
                    item.status === "accepted" ? "已加入团队" : "通过并加入团队"
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
                selected.myApplication.status === 'accepted'
                  ? 'success'
                  : 'info'
              "
              variant="tonal"
              class="mb-5"
              >报名状态：{{ statusLabel(selected.myApplication.status) }}
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
                >请先完善必需个人资料和照片。<v-btn to="/profile" variant="text"
                  >完善资料</v-btn
                ></v-alert
              >
              <form v-else @submit.prevent="apply">
                <v-select
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
                报名方向：{{ selected.myApplication.direction }}组
              </p>
              <v-btn
                :href="downloadUrl(selected.myApplication.resumeFileId)"
                variant="tonal"
                color="primary"
                prepend-icon="mdi-file-pdf-box"
                >查看已提交简历</v-btn
              ><v-btn
                v-if="selected.myApplication.status === 'accepted'"
                to="/learning"
                color="primary"
                class="ml-3"
                >进入学习任务</v-btn
              >
            </div></template
          ></v-card-text
        ></v-card
      ></v-dialog
    >
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
