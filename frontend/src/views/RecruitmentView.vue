<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useDisplay } from "vuetify";
import { api, ApiError } from "../lib/api";
import { auth } from "../lib/auth";
import type { Applicant, ApplicantStatus, User, Settings } from "../lib/types";

const { smAndDown } = useDisplay();
const route = useRoute();
const items = ref<Applicant[]>([]);
const members = ref<User[]>([]);
const settings = ref<Settings | null>(null);
const loading = ref(true);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const search = ref("");
const statusFilter = ref("all");
const directionFilter = ref("全部方向");
const formOpen = ref(false);
const selected = ref<Applicant | null>(null);
const decision = ref<"accepted" | "rejected" | null>(null);
const draftSaved = ref(false);
const draftReady = ref(false);
const form = reactive({
  name: "",
  email: "",
  phone: "",
  university: "",
  major: "",
  year: "",
  team: "RoboMaster",
  direction: "电控",
  secondDirection: "",
  motivation: "",
  experience: "",
  portfolioUrl: "",
});
const review = reactive({
  interviewAt: "",
  interviewerId: "",
  score: null as number | null,
  notes: "",
});
const directions = ["机械", "电控", "视觉算法", "硬件", "运营"];
const statuses: { value: ApplicantStatus; label: string; color: string }[] = [
  { value: "submitted", label: "待筛选", color: "blue-grey" },
  { value: "screening", label: "筛选中", color: "indigo" },
  { value: "interview", label: "待面试", color: "amber-darken-3" },
  { value: "accepted", label: "已录取", color: "teal" },
  { value: "rejected", label: "未录取", color: "grey" },
];
watch(
  () => route.query.status,
  (status) => {
    statusFilter.value =
      typeof status === "string" &&
      statuses.some((item) => item.value === status)
        ? status
        : "all";
  },
  { immediate: true },
);
const isStudent = computed(() => auth.user?.role === "student");
const isAdmin = computed(() => auth.user?.role === "admin");
const canReview = computed(
  () =>
    isAdmin.value ||
    (auth.user?.role === "interviewer" &&
      selected.value?.interviewerId === auth.user?.id),
);
const own = computed(() => items.value.find((a) => a.userId === auth.user?.id));
const deadlinePassed = computed(() =>
  Boolean(
    settings.value?.applicationDeadline &&
      new Date(settings.value.applicationDeadline).getTime() < Date.now(),
  ),
);
const canApply = computed(
  () =>
    settings.value?.applicationOpen &&
    !deadlinePassed.value &&
    (!own.value || ["submitted", "screening"].includes(own.value.status)),
);
const filtered = computed(() =>
  items.value.filter(
    (a) =>
      (statusFilter.value === "all" || a.status === statusFilter.value) &&
      (directionFilter.value === "全部方向" ||
        a.direction === directionFilter.value) &&
      `${a.name} ${a.email} ${a.university} ${a.major}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
  ),
);
const interviewers = computed(() =>
  members.value.filter((u) => ["admin", "interviewer"].includes(u.role)),
);
const draftKey = () => `robotlab:application:${auth.user?.id}`;
const statusOf = (value: string) =>
  statuses.find((s) => s.value === value) || statuses[0]!;
const formatDate = (value: string) =>
  value
    ? new Date(value).toLocaleString("zh-CN", {
        month: "long",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "尚未安排";
const toLocalDate = (value: string) => {
  if (!value) return "";
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
};
const messageOf = (e: unknown) =>
  e instanceof ApiError || e instanceof Error
    ? e.message
    : "操作失败，请稍后重试";
function setNotice(value: string) {
  notice.value = value;
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const requests = await Promise.all([
      api.get<{ items: Applicant[] }>("/applicants"),
      api.get<{ settings: Settings }>("/settings"),
    ]);
    items.value = requests[0].items;
    settings.value = requests[1].settings;
    if (isAdmin.value)
      members.value = (await api.get<{ items: User[] }>("/members")).items;
  } catch (e) {
    error.value = messageOf(e);
  } finally {
    loading.value = false;
  }
}

function openForm() {
  draftReady.value = false;
  const applicant = own.value;
  for (const key of Object.keys(form) as (keyof typeof form)[]) {
    form[key] =
      applicant?.[key] ??
      (key === "name"
        ? auth.user?.name || ""
        : key === "email"
          ? auth.user?.email || ""
          : key === "team"
            ? "RoboMaster"
            : key === "direction"
              ? "电控"
              : "");
  }
  try {
    const saved = localStorage.getItem(draftKey());
    if (saved) {
      const parsed: unknown = JSON.parse(saved);
      if (parsed && typeof parsed === "object")
        for (const key of Object.keys(form) as (keyof typeof form)[]) {
          const value = (parsed as Record<string, unknown>)[key];
          if (typeof value === "string") form[key] = value;
        }
      draftSaved.value = true;
    } else draftSaved.value = false;
  } catch {
    draftSaved.value = false;
  }
  formOpen.value = true;
  draftReady.value = true;
}
watch(form, () => {
  if (!draftReady.value || !formOpen.value) return;
  try {
    localStorage.setItem(draftKey(), JSON.stringify(form));
    draftSaved.value = true;
  } catch {
    draftSaved.value = false;
  }
});

async function saveApplication() {
  if (
    !form.name.trim() ||
    !form.email.trim() ||
    !form.phone.trim() ||
    !form.university.trim() ||
    !form.major.trim() ||
    !form.year ||
    !form.motivation.trim()
  )
    return setNotice("请填写姓名、邮箱、电话、学校、专业、年级和报名动机");
  if (form.secondDirection && form.secondDirection === form.direction)
    return setNotice("第二志愿需要与第一志愿不同");
  saving.value = true;
  try {
    const { item } = await api.post<{ item: Applicant }>("/applicants", {
      ...form,
    });
    const index = items.value.findIndex((a) => a.id === item.id);
    if (index < 0) items.value.push(item);
    else items.value[index] = item;
    draftReady.value = false;
    try {
      localStorage.removeItem(draftKey());
    } catch {
      /* Draft storage can be unavailable. */
    }
    formOpen.value = false;
    setNotice("报名资料已提交，可在此查看后续进度");
  } catch (e) {
    setNotice(messageOf(e));
  } finally {
    saving.value = false;
  }
}

function openDetail(applicant: Applicant) {
  selected.value = applicant;
  review.interviewAt = toLocalDate(applicant.interviewAt);
  review.interviewerId = applicant.interviewerId;
  review.score = applicant.score;
  review.notes = applicant.notes;
}
let appliedApplicantLink = "";
watch(
  [() => route.query.applicant, loading, saving],
  ([query]) => {
    const id = typeof query === "string" ? query : "";
    if (loading.value || saving.value || error.value) return;
    if (!id) {
      if (selected.value?.id === appliedApplicantLink) selected.value = null;
      appliedApplicantLink = "";
      return;
    }
    // Consuming each query change once lets users close the dialog or refresh
    // the list without immediately opening the same candidate again.
    if (id === appliedApplicantLink) return;
    appliedApplicantLink = id;
    const applicant = items.value.find((item) => item.id === id);
    if (applicant) openDetail(applicant);
    else {
      selected.value = null;
      setNotice("未找到该候选人，或你没有查看权限");
    }
  },
  { immediate: true },
);
async function updateApplicant(
  patch: Record<string, unknown>,
  success: string,
) {
  if (!selected.value) return;
  saving.value = true;
  try {
    const { item } = await api.patch<{ item: Applicant }>(
      `/applicants/${selected.value.id}`,
      patch,
    );
    items.value = items.value.map((a) => (a.id === item.id ? item : a));
    openDetail(item);
    decision.value = null;
    setNotice(success);
  } catch (e) {
    setNotice(messageOf(e));
  } finally {
    saving.value = false;
  }
}
function saveReview() {
  if (
    selected.value?.score !== null &&
    (review.score === null || (review.score as unknown) === "")
  )
    return setNotice("请填写 0–100 之间的面试成绩");
  if (
    review.score !== null &&
    review.score !== undefined &&
    (review.score as unknown) !== "" &&
    (!Number.isFinite(Number(review.score)) ||
      Number(review.score) < 0 ||
      Number(review.score) > 100)
  )
    return setNotice("面试成绩应在 0–100 之间");
  updateApplicant(
    {
      score:
        review.score === null || (review.score as unknown) === ""
          ? null
          : Number(review.score),
      notes: review.notes,
    },
    "评价已保存",
  );
}
function saveSchedule() {
  if (!review.interviewAt || !review.interviewerId)
    return setNotice("请选择面试时间与面试官");
  const date = new Date(review.interviewAt);
  if (!Number.isFinite(date.getTime())) return setNotice("面试时间无效");
  updateApplicant(
    {
      interviewAt: date.toISOString(),
      interviewerId: review.interviewerId,
      status: "interview",
    },
    "面试安排已保存",
  );
}
onMounted(load);
</script>

<template>
  <div class="page-content recruitment-page">
    <div class="page-heading">
      <div>
        <div class="eyebrow">
          RECRUITMENT · {{ settings?.season || "新赛季" }}
        </div>
        <h1>{{ isStudent ? "我的报名" : "招新工作台" }}</h1>
        <p>
          {{
            isStudent
              ? "从这里开始，把你的热爱变成机器人的下一次突破。"
              : "把每一次热爱，交给认真而清晰的选拔。"
          }}
        </p>
      </div>
      <v-btn
        v-if="isStudent && canApply"
        color="primary"
        prepend-icon="mdi-pencil-outline"
        size="large"
        @click="openForm"
        >{{ own ? "编辑报名资料" : "开始报名" }}</v-btn
      >
      <v-btn
        v-else
        variant="outlined"
        prepend-icon="mdi-refresh"
        :loading="loading"
        @click="load"
        >刷新</v-btn
      >
    </div>

    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      class="mb-5"
      closable
      @click:close="error = ''"
      >{{ error
      }}<template #append
        ><v-btn variant="text" @click="load">重试</v-btn></template
      ></v-alert
    >
    <div v-if="loading" class="loading-panel">
      <v-progress-circular indeterminate color="primary" /><span
        >正在读取招新资料…</span
      >
    </div>

    <template v-else-if="isStudent">
      <v-alert
        v-if="!settings?.applicationOpen || deadlinePassed"
        type="info"
        variant="tonal"
        class="mb-5"
        >{{
          deadlinePassed ? "本轮报名已截止。" : "本轮报名尚未开放。"
        }}已提交的资料仍可在此查看。</v-alert
      >
      <v-card
        v-if="own"
        class="application-card"
        rounded="xl"
        elevation="0"
        border
      >
        <div class="application-banner">
          <div>
            <span class="eyebrow">{{ own.team }} / {{ own.direction }}</span>
            <h2>
              {{ own.name }}，{{
                own.status === "accepted"
                  ? "欢迎加入战队"
                  : "你的申请正在这里成长"
              }}
            </h2>
          </div>
          <v-chip :color="statusOf(own.status).color" variant="flat">{{
            statusOf(own.status).label
          }}</v-chip>
        </div>
        <div class="application-body">
          <div class="progress-track">
            <div
              v-for="(step, index) in [
                '提交报名',
                '资料筛选',
                '面试交流',
                '结果公布',
              ]"
              :key="step"
              :class="[
                'progress-step',
                {
                  active:
                    index <=
                    {
                      submitted: 0,
                      screening: 1,
                      interview: 2,
                      accepted: 3,
                      rejected: 3,
                    }[own.status],
                },
              ]"
            >
              <span>{{ index + 1 }}</span
              ><strong>{{ step }}</strong>
            </div>
          </div>
          <v-alert
            v-if="own.status === 'accepted'"
            type="success"
            variant="tonal"
            class="mb-6"
            >录取已确认！前往「学习任务」，开启你的第一项训练。</v-alert
          >
          <v-alert
            v-if="own.status === 'rejected'"
            type="info"
            variant="tonal"
            class="mb-6"
            >本轮选拔未通过。感谢你的认真投入，也欢迎继续学习与探索。</v-alert
          >
          <div class="detail-grid">
            <div>
              <label>学校 / 专业</label>
              <p>{{ own.university }} · {{ own.major }}</p>
            </div>
            <div>
              <label>志愿方向</label>
              <p>
                {{ own.direction
                }}{{ own.secondDirection ? ` / ${own.secondDirection}` : "" }}
              </p>
            </div>
            <div>
              <label>联系方式</label>
              <p>{{ own.email }}<br />{{ own.phone }}</p>
            </div>
            <div>
              <label>面试时间</label>
              <p>{{ formatDate(own.interviewAt) }}</p>
            </div>
          </div>
          <div class="prose-block">
            <label>为什么想加入我们</label>
            <p>{{ own.motivation }}</p>
          </div>
          <div v-if="own.experience" class="prose-block">
            <label>经历与技能</label>
            <p>{{ own.experience }}</p>
          </div>
          <div class="application-footer">
            <span>提交于 {{ formatDate(own.createdAt) }}</span
            ><v-btn
              v-if="own.status === 'accepted'"
              color="primary"
              variant="tonal"
              to="/learning"
              append-icon="mdi-arrow-right"
              >查看学习任务</v-btn
            >
          </div>
        </div>
      </v-card>
      <v-card
        v-else-if="!error"
        class="empty-card"
        rounded="xl"
        elevation="0"
        border
        ><div class="empty-icon">
          <v-icon size="38">mdi-rocket-launch-outline</v-icon>
        </div>
        <h2>机器人之旅，从一次报名开始</h2>
        <p>
          选择喜欢的赛队与方向，介绍你的经历和想做的事情。<br />无需完美的履历，我们也期待认真学习的新伙伴。
        </p>
        <v-btn v-if="canApply" color="primary" size="large" @click="openForm"
          >填写报名表</v-btn
        ><span v-if="settings?.applicationDeadline" class="deadline"
          >报名截止 {{ formatDate(settings.applicationDeadline) }}</span
        ></v-card
      >
    </template>

    <template v-else-if="!loading">
      <div class="pipeline-grid">
        <button
          :class="['pipeline-card', { selected: statusFilter === s.value }]"
          v-for="s in statuses"
          :key="s.value"
          @click="statusFilter = statusFilter === s.value ? 'all' : s.value"
        >
          <span><i :class="`status-dot dot-${s.value}`" />{{ s.label }}</span
          ><strong>{{
            items.filter((a) => a.status === s.value).length
          }}</strong
          ><v-icon size="18">mdi-arrow-top-right</v-icon>
        </button>
      </div>
      <v-card rounded="xl" elevation="0" border class="applicants-card">
        <div class="table-toolbar">
          <div>
            <h2>
              候选人名单 <span>{{ filtered.length }}</span>
            </h2>
            <p>
              {{
                auth.user?.role === "interviewer"
                  ? "仅显示分配给你的候选人"
                  : "根据方向和当前进度找到需要关注的候选人"
              }}
            </p>
          </div>
          <div class="table-filters">
            <v-text-field
              v-model="search"
              placeholder="搜索姓名、学校、邮箱"
              prepend-inner-icon="mdi-magnify"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="搜索候选人"
            /><v-select
              v-model="directionFilter"
              :items="['全部方向', ...directions]"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="筛选方向"
            />
          </div>
        </div>
        <v-chip
          v-if="statusFilter !== 'all'"
          class="mx-6 mb-4"
          closable
          color="primary"
          size="small"
          @click:close="statusFilter = 'all'"
          >{{ statusOf(statusFilter).label }}</v-chip
        >
        <v-table hover
          ><thead>
            <tr>
              <th>候选人</th>
              <th>赛队 / 志愿</th>
              <th>状态</th>
              <th>面试安排</th>
              <th>评分</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in filtered" :key="a.id">
              <td>
                <div class="person-cell">
                  <v-avatar color="indigo-lighten-5" size="38"
                    ><span class="avatar-letter">{{
                      a.name.slice(0, 1)
                    }}</span></v-avatar
                  >
                  <div>
                    <strong>{{ a.name }}</strong
                    ><small>{{ a.university }} · {{ a.major }}</small>
                  </div>
                </div>
              </td>
              <td>
                <strong class="direction-label">{{ a.direction }}</strong
                ><small>{{ a.team }}</small>
              </td>
              <td>
                <v-chip
                  :color="statusOf(a.status).color"
                  size="small"
                  variant="tonal"
                  >{{ statusOf(a.status).label }}</v-chip
                >
              </td>
              <td class="nowrap">{{ formatDate(a.interviewAt) }}</td>
              <td>
                <strong>{{ a.score ?? "—" }}</strong
                ><span v-if="a.score !== null" class="score-unit"> / 100</span>
              </td>
              <td>
                <v-btn
                  variant="text"
                  color="primary"
                  append-icon="mdi-chevron-right"
                  @click="openDetail(a)"
                  >查看</v-btn
                >
              </td>
            </tr>
          </tbody></v-table
        >
        <div v-if="!filtered.length" class="table-empty">
          <v-icon size="34" color="grey">mdi-account-search-outline</v-icon>
          <h3>{{ items.length ? "没有匹配的候选人" : "还没有候选人" }}</h3>
          <p>
            {{
              items.length
                ? "试试其他关键词或筛选条件。"
                : "报名提交后，候选人将在这里出现。"
            }}
          </p>
          <v-btn
            v-if="items.length"
            variant="text"
            @click="
              search = '';
              statusFilter = 'all';
              directionFilter = '全部方向';
            "
            >清除筛选</v-btn
          >
        </div>
      </v-card>
    </template>

    <v-dialog
      v-model="formOpen"
      max-width="900"
      :fullscreen="smAndDown"
      :persistent="saving"
      scrollable
      ><v-card rounded="xl"
        ><v-card-title class="dialog-title"
          ><div>
            <h2>我的报名资料</h2>
            <p>认真介绍自己，比一份完美的履历更重要。</p>
          </div>
          <v-btn
            icon="mdi-close"
            variant="text"
            aria-label="关闭报名表"
            :disabled="saving"
            @click="formOpen = false" /></v-card-title
        ><v-card-text
          ><v-form @submit.prevent="saveApplication"
            ><h3 class="form-section-title">01 / 基础信息</h3>
            <v-row
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.name"
                  label="姓名 *"
                  maxlength="80"
                  variant="outlined"
                  hide-details="auto"
                  required /></v-col
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.email"
                  label="邮箱 *"
                  type="email"
                  variant="outlined"
                  hide-details="auto"
                  required /></v-col
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.phone"
                  label="联系电话 *"
                  type="tel"
                  variant="outlined"
                  hide-details="auto"
                  required /></v-col
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.university"
                  label="学校 *"
                  variant="outlined"
                  hide-details="auto"
                  required /></v-col
              ><v-col cols="12" sm="6"
                ><v-text-field
                  v-model="form.major"
                  label="专业 *"
                  variant="outlined"
                  hide-details="auto"
                  required /></v-col
              ><v-col cols="12" sm="6"
                ><v-select
                  v-model="form.year"
                  label="年级 *"
                  :items="['大一', '大二', '大三', '大四', '研究生', '其他']"
                  variant="outlined"
                  hide-details="auto"
                  required /></v-col
            ></v-row>
            <h3 class="form-section-title">02 / 选择你的方向</h3>
            <v-row
              ><v-col cols="12" sm="4"
                ><v-select
                  v-model="form.team"
                  :items="['RoboMaster', 'ROBOCON']"
                  label="意向赛队"
                  variant="outlined"
                  hide-details="auto" /></v-col
              ><v-col cols="12" sm="4"
                ><v-select
                  v-model="form.direction"
                  :items="directions"
                  label="第一志愿"
                  variant="outlined"
                  hide-details="auto" /></v-col
              ><v-col cols="12" sm="4"
                ><v-select
                  v-model="form.secondDirection"
                  :items="[
                    { title: '暂不选择', value: '' },
                    ...directions
                      .filter((d) => d !== form.direction)
                      .map((d) => ({ title: d, value: d })),
                  ]"
                  label="第二志愿（选填）"
                  variant="outlined"
                  hide-details="auto" /></v-col
            ></v-row>
            <h3 class="form-section-title">03 / 让我们认识你</h3>
            <v-textarea
              v-model="form.motivation"
              label="为什么想加入我们？ *"
              placeholder="你对机器人最感兴趣的是什么？希望在战队完成什么？"
              variant="outlined"
              rows="4"
              maxlength="10000"
              counter
              required
            /><v-textarea
              v-model="form.experience"
              label="相关经历与技能（选填）"
              placeholder="课程项目、个人作品、动手经历，或正在学习的内容。"
              variant="outlined"
              rows="3"
              maxlength="10000"
              counter
            /><v-text-field
              v-model="form.portfolioUrl"
              label="作品或代码仓库链接（选填）"
              type="url"
              placeholder="https://"
              variant="outlined"
            />
            <div class="form-submit">
              <span
                ><v-icon size="16" class="mr-1">mdi-cloud-check-outline</v-icon
                >{{
                  draftSaved
                    ? "草稿已保存在当前浏览器"
                    : "填写后会尝试自动保存本地草稿"
                }}</span
              ><v-btn
                color="primary"
                type="submit"
                :loading="saving"
                size="large"
                >提交报名</v-btn
              >
            </div></v-form
          ></v-card-text
        ></v-card
      ></v-dialog
    >

    <v-dialog
      :model-value="!!selected"
      max-width="1050"
      :fullscreen="smAndDown"
      :persistent="saving"
      scrollable
      @update:model-value="!$event && (selected = null)"
      ><v-card v-if="selected" rounded="xl"
        ><v-card-title class="dialog-title"
          ><div>
            <h2>
              {{ selected.name }}
              <v-chip
                :color="statusOf(selected.status).color"
                size="small"
                class="ml-2"
                >{{ statusOf(selected.status).label }}</v-chip
              >
            </h2>
            <p>
              {{ selected.team }} · {{ selected.direction }} ·
              {{ selected.university }}
            </p>
          </div>
          <v-btn
            icon="mdi-close"
            variant="text"
            aria-label="关闭候选人详情"
            :disabled="saving"
            @click="selected = null" /></v-card-title
        ><v-card-text
          ><v-row
            ><v-col cols="12" :md="isStudent ? 12 : 7"
              ><h3 class="form-section-title">报名资料</h3>
              <div class="detail-grid">
                <div>
                  <label>专业 / 年级</label>
                  <p>{{ selected.major }} · {{ selected.year }}</p>
                </div>
                <div>
                  <label>第二志愿</label>
                  <p>{{ selected.secondDirection || "未填写" }}</p>
                </div>
                <div>
                  <label>邮箱</label>
                  <p>{{ selected.email }}</p>
                </div>
                <div>
                  <label>电话</label>
                  <p>{{ selected.phone }}</p>
                </div>
                <div v-if="isStudent">
                  <label>面试时间</label>
                  <p>{{ formatDate(selected.interviewAt) }}</p>
                </div>
              </div>
              <div class="prose-block">
                <label>报名动机</label>
                <p>{{ selected.motivation }}</p>
              </div>
              <div class="prose-block">
                <label>经历与技能</label>
                <p>{{ selected.experience || "未填写" }}</p>
              </div>
              <v-btn
                v-if="/^https?:\/\//i.test(selected.portfolioUrl)"
                :href="selected.portfolioUrl"
                target="_blank"
                rel="noopener noreferrer"
                variant="tonal"
                prepend-icon="mdi-open-in-new"
                color="primary"
                >查看作品</v-btn
              ></v-col
            ><v-col v-if="!isStudent" cols="12" md="5"
              ><div class="review-panel">
                <h3>面试安排</h3>
                <template
                  v-if="
                    isAdmin &&
                    !['accepted', 'rejected'].includes(selected.status)
                  "
                  ><v-text-field
                    v-model="review.interviewAt"
                    type="datetime-local"
                    label="面试时间"
                    variant="outlined"
                    density="comfortable"
                    class="mt-4"
                  /><v-select
                    v-model="review.interviewerId"
                    :items="interviewers"
                    item-title="name"
                    item-value="id"
                    label="负责面试官"
                    variant="outlined"
                    density="comfortable"
                  /><v-btn
                    block
                    color="primary"
                    variant="tonal"
                    :loading="saving"
                    @click="saveSchedule"
                    >保存面试安排</v-btn
                  ></template
                >
                <p v-else class="mt-3">
                  {{ formatDate(selected.interviewAt) }}
                </p>
                <v-divider class="my-6" />
                <h3 class="mb-4">内部评价</h3>
                <template v-if="canReview"
                  ><v-text-field
                    v-model.number="review.score"
                    label="面试成绩（0–100）"
                    type="number"
                    min="0"
                    max="100"
                    variant="outlined"
                  /><v-textarea
                    v-model="review.notes"
                    label="面试记录与评价"
                    variant="outlined"
                    rows="4"
                    maxlength="10000"
                    counter
                  /><v-btn
                    block
                    color="primary"
                    :loading="saving"
                    @click="saveReview"
                    >保存评价</v-btn
                  ></template
                ><template v-else
                  ><p>成绩：{{ selected.score ?? "尚未评分" }}</p>
                  <p class="preserve-lines">
                    {{ selected.notes || "暂无内部评价" }}
                  </p></template
                >
                <p class="private-note">
                  <v-icon size="14">mdi-lock-outline</v-icon
                  >内部评价仅对有权限的工作人员可见
                </p>
              </div>
              <div v-if="isAdmin" class="decision-actions">
                <v-btn
                  v-if="selected.status === 'submitted'"
                  variant="outlined"
                  color="primary"
                  block
                  :loading="saving"
                  @click="
                    updateApplicant({ status: 'screening' }, '已进入资料筛选')
                  "
                  >进入筛选</v-btn
                ><template
                  v-if="!['accepted', 'rejected'].includes(selected.status)"
                  ><v-btn
                    color="teal"
                    :disabled="saving"
                    @click="decision = 'accepted'"
                    >确认录取</v-btn
                  ><v-btn
                    variant="outlined"
                    color="grey-darken-1"
                    :disabled="saving"
                    @click="decision = 'rejected'"
                    >未录取</v-btn
                  ></template
                >
              </div></v-col
            ></v-row
          ></v-card-text
        ></v-card
      ></v-dialog
    >
    <v-dialog
      :model-value="!!decision"
      max-width="440"
      :persistent="saving"
      @update:model-value="!$event && (decision = null)"
      ><v-card rounded="xl" class="pa-4"
        ><v-card-title>{{
          decision === "accepted" ? "确认录取这位候选人？" : "确认本轮未录取？"
        }}</v-card-title
        ><v-card-text
          >{{ selected?.name }} 将看到更新后的选拔结果。{{
            decision === "accepted"
              ? "录取后即可参与对应赛队和方向的学习任务。"
              : "请确认已经完成必要的评价与讨论。"
          }}</v-card-text
        ><v-card-actions
          ><v-spacer /><v-btn :disabled="saving" @click="decision = null"
            >返回</v-btn
          ><v-btn
            :color="decision === 'accepted' ? 'teal' : 'primary'"
            :loading="saving"
            @click="updateApplicant({ status: decision }, '录取决定已保存')"
            >确认结果</v-btn
          ></v-card-actions
        ></v-card
      ></v-dialog
    >
    <v-snackbar
      :model-value="!!notice"
      timeout="5000"
      @update:model-value="!$event && (notice = '')"
      >{{ notice
      }}<template #actions
        ><v-btn variant="text" @click="notice = ''">关闭</v-btn></template
      ></v-snackbar
    >
  </div>
</template>

<style scoped>
.page-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 24px;
  margin-bottom: 28px;
}
.eyebrow {
  font-size: 11px;
  letter-spacing: 1.8px;
  font-weight: 700;
  color: #737a98;
}
.page-heading h1 {
  font-size: 30px;
  letter-spacing: -1px;
  margin: 8px 0;
  color: #232941;
}
.page-heading p,
.dialog-title p {
  color: #7b8193;
  font-size: 14px;
  margin-top: 6px;
}
.loading-panel {
  min-height: 300px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: #7b8193;
}
.pipeline-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 16px;
  margin-bottom: 24px;
}
.pipeline-card {
  position: relative;
  text-align: left;
  background: #fff;
  border: 1px solid #e9eaf1;
  border-radius: 16px;
  padding: 20px;
  transition: 0.2s;
}
.pipeline-card:hover,
.pipeline-card.selected {
  border-color: #7784d4;
  background: #f7f8ff;
}
.pipeline-card > span {
  font-size: 13px;
  color: #71798e;
  display: flex;
  align-items: center;
  gap: 8px;
}
.pipeline-card strong {
  font-size: 30px;
  color: #262d44;
  display: block;
  margin-top: 12px;
  line-height: 1.2;
}
.pipeline-card > .v-icon {
  position: absolute;
  right: 20px;
  bottom: 22px;
  color: #939aae;
}
.status-dot {
  height: 7px;
  width: 7px;
  border-radius: 50%;
  display: inline-block;
  background: #607d8b;
}
.dot-screening {
  background: #6366c9;
}
.dot-interview {
  background: #d99a35;
}
.dot-accepted {
  background: #26a69a;
}
.dot-rejected {
  background: #9e9e9e;
}
.table-toolbar {
  padding: 26px;
  display: flex;
  justify-content: space-between;
  gap: 24px;
  align-items: center;
}
.table-toolbar h2 {
  font-size: 17px;
  font-weight: 700;
}
.table-toolbar h2 span {
  font-size: 12px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 10px;
  background: #f0f1f8;
  color: #6b7291;
  margin-left: 6px;
}
.table-toolbar p {
  font-size: 12px;
  color: #9095a3;
  margin-top: 7px;
}
.table-filters {
  display: flex;
  gap: 12px;
  min-width: 420px;
}
.table-filters > .v-text-field {
  width: 255px;
}
.table-filters > .v-select {
  width: 155px;
}
.applicants-card :deep(th) {
  background: #fafbfe !important;
  font-size: 12px !important;
  color: #8c93a3 !important;
}
.applicants-card :deep(td) {
  height: 86px !important;
  font-size: 13px;
  color: #596276;
  border-bottom-color: #f0f1f5 !important;
}
.person-cell {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 185px;
}
.person-cell strong {
  font-size: 14px;
  color: #343c55;
}
.person-cell small,
td > small {
  display: block;
  font-size: 11px;
  color: #959baa;
  margin-top: 5px;
}
.avatar-letter {
  font-weight: 600;
  color: #6874b4;
}
.direction-label {
  font-size: 13px;
  font-weight: 500;
  color: #566079;
}
.nowrap {
  white-space: nowrap;
}
.score-unit {
  font-size: 11px;
  color: #a1a7b7;
}
.table-empty {
  padding: 70px 24px;
  text-align: center;
  color: #8a92a5;
}
.table-empty h3 {
  font-size: 16px;
  color: #5b657b;
  margin: 16px 0 7px;
}
.table-empty p {
  font-size: 13px;
}
.application-banner {
  padding: 30px 32px;
  background: linear-gradient(120deg, #eff0fe, #f2f9fb);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.application-banner h2 {
  font-size: 23px;
  color: #343e65;
  margin-top: 10px;
}
.application-body {
  padding: 32px;
}
.progress-track {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 35px;
}
.progress-step {
  display: flex;
  gap: 10px;
  align-items: center;
  color: #a4abbc;
  font-size: 13px;
}
.progress-step span {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: #f1f3f7;
}
.progress-step.active {
  color: #5b64b9;
}
.progress-step.active span {
  background: #e8eafe;
}
.detail-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 22px;
  margin-bottom: 26px;
}
.detail-grid label,
.prose-block label {
  font-size: 12px;
  color: #969cac;
}
.detail-grid p {
  font-size: 14px;
  line-height: 1.8;
  margin-top: 6px;
  color: #48516b;
  overflow-wrap: anywhere;
}
.prose-block {
  margin-bottom: 24px;
}
.prose-block p,
.preserve-lines {
  font-size: 14px;
  color: #536079;
  line-height: 1.9;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin-top: 8px;
}
.application-footer {
  border-top: 1px solid #f0f1f6;
  padding-top: 22px;
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  color: #99a0b0;
  font-size: 12px;
}
.empty-card {
  text-align: center;
  padding: 64px 24px;
}
.empty-icon {
  width: 80px;
  height: 80px;
  background: #eeeffe;
  color: #747cc6;
  border-radius: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: auto auto 24px;
}
.empty-card h2 {
  font-size: 23px;
  color: #3a435e;
}
.empty-card p {
  font-size: 14px;
  line-height: 1.9;
  color: #8c94a6;
  margin: 16px 0 24px;
}
.deadline {
  display: block;
  margin-top: 18px;
  font-size: 12px;
  color: #969eaf;
}
.dialog-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 22px 28px !important;
  white-space: normal !important;
  border-bottom: 1px solid #eff0f6;
}
.dialog-title h2 {
  font-size: 21px;
  color: #353e58;
}
.dialog-title p {
  font-size: 12px;
}
.form-section-title {
  font-size: 14px;
  color: #515c7d;
  margin: 24px 0 20px;
}
.form-submit {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin: 10px 0 20px;
}
.form-submit > span {
  font-size: 12px;
  color: #8895a7;
}
.review-panel {
  background: #f7f8fc;
  border: 1px solid #eef0f6;
  border-radius: 16px;
  padding: 22px;
}
.review-panel h3 {
  font-size: 15px;
  color: #46506d;
}
.review-panel > p {
  font-size: 13px;
  color: #788198;
}
.private-note {
  font-size: 11px !important;
  margin-top: 13px !important;
  color: #969fb2 !important;
}
.decision-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin: 20px 0;
}
.decision-actions > .v-btn {
  flex-grow: 1;
}
@media (max-width: 1100px) {
  .table-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .table-filters {
    min-width: 0;
  }
  .pipeline-card {
    padding: 16px;
  }
  .pipeline-card > span {
    font-size: 12px;
  }
}
@media (max-width: 600px) {
  .page-heading {
    align-items: flex-start;
    gap: 16px;
  }
  .page-heading h1 {
    font-size: 25px;
  }
  .page-heading p {
    font-size: 12px;
    line-height: 1.8;
  }
  .page-heading > .v-btn {
    margin-top: 22px;
  }
  .pipeline-grid {
    grid-template-columns: repeat(3, 1fr);
    gap: 10px;
  }
  .pipeline-card {
    padding: 13px;
  }
  .pipeline-card strong {
    font-size: 26px;
  }
  .pipeline-card > .v-icon {
    display: none;
  }
  .table-toolbar {
    padding: 20px;
  }
  .table-filters {
    flex-direction: column;
    gap: 12px;
  }
  .table-filters > .v-text-field,
  .table-filters > .v-select {
    width: 100%;
  }
  .application-banner {
    padding: 24px;
    align-items: flex-start;
  }
  .application-banner h2 {
    font-size: 18px;
    line-height: 1.6;
  }
  .application-body {
    padding: 22px;
  }
  .progress-track {
    gap: 8px;
  }
  .progress-step {
    flex-direction: column;
    font-size: 11px;
  }
  .detail-grid {
    grid-template-columns: 1fr;
  }
  .empty-card h2 {
    font-size: 19px;
  }
  .form-submit {
    align-items: flex-start;
    flex-direction: column;
  }
  .form-submit > .v-btn {
    width: 100%;
  }
  .dialog-title {
    padding: 18px 20px !important;
  }
  .application-footer {
    align-items: flex-start;
    flex-direction: column;
  }
  .eyebrow {
    font-size: 10px;
  }
}
</style>
