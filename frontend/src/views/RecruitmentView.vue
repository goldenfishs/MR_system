<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { api, ApiError } from "../lib/api";
import { auth } from "../lib/auth";
import type { Applicant, ApplicantStatus, User, Settings } from "../lib/types";

const { smAndDown } = useDisplay();
const route = useRoute();
const router = useRouter();
const items = ref<Applicant[]>([]);
const members = ref<User[]>([]);
const settings = ref<Settings | null>(null);
const loading = ref(true);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const search = ref("");

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
const statusFilter = computed(() =>
  typeof route.query.status === "string" &&
  statuses.some((item) => item.value === route.query.status)
    ? route.query.status
    : "all",
);
const directionFilter = computed({
  get: () =>
    typeof route.query.direction === "string" &&
    directions.includes(route.query.direction)
      ? route.query.direction
      : "全部方向",
  set: (value: string) => {
    router.replace({
      query: {
        ...route.query,
        direction: value === "全部方向" ? undefined : value,
      },
    });
  },
});
const teamFilter = computed(() =>
  typeof route.query.team === "string" &&
  ["RoboMaster", "ROBOCON"].includes(route.query.team)
    ? route.query.team
    : "全部赛队",
);
function clearFilters() {
  search.value = "";
  router.replace({
    query: {
      ...route.query,
      status: undefined,
      direction: undefined,
      team: undefined,
    },
  });
}
function clearQueryFilter(key: "status" | "team") {
  router.replace({ query: { ...route.query, [key]: undefined } });
}
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
      (teamFilter.value === "全部赛队" || a.team === teamFilter.value) &&
      (directionFilter.value === "全部方向" ||
        a.direction === directionFilter.value) &&
      `${a.name} ${a.email} ${a.university} ${a.major}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
  ),
);
// Statistics use all visible records for the selected team, independent of
// list status and search, so the chart scope stays explicit and consistent.
const chartItems = computed(() =>
  items.value.filter(
    (a) => teamFilter.value === "全部赛队" || a.team === teamFilter.value,
  ),
);
const chartScope = computed(() => `${teamFilter.value} · 全部状态与方向`);
const dailyCounts = computed(() => {
  const today = new Date();
  return Array.from({ length: 7 }, (_, index) => {
    const day = new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate() - 6 + index,
    );
    const next = new Date(day.getFullYear(), day.getMonth(), day.getDate() + 1);
    return {
      label: `${day.getMonth() + 1}/${day.getDate()}`,
      count: chartItems.value.filter((a) => {
        const created = new Date(a.createdAt).getTime();
        return created >= day.getTime() && created < next.getTime();
      }).length,
    };
  });
});
const trendMax = computed(() =>
  Math.max(1, ...dailyCounts.value.map((day) => day.count)),
);
const trendPoints = computed(() =>
  dailyCounts.value.map((day, index) => ({
    x: 24 + index * 42,
    y: 116 - (day.count / trendMax.value) * 82,
    ...day,
  })),
);
const trendLine = computed(() =>
  trendPoints.value.map((point) => `${point.x},${point.y}`).join(" "),
);
const trendLabel = computed(
  () =>
    `近七日新增报名，${dailyCounts.value.map((day) => `${day.label}：${day.count} 人`).join("，")}`,
);
const directionCounts = computed(() =>
  directions.map((direction) => ({
    direction,
    count: chartItems.value.filter((a) => a.direction === direction).length,
  })),
);
const directionMax = computed(() =>
  Math.max(1, ...directionCounts.value.map((item) => item.count)),
);
const directionLabel = computed(
  () =>
    `方向分布，${directionCounts.value.map((item) => `${item.direction}：${item.count} 人`).join("，")}`,
);
const avatarColors = ["#1976d2", "#7b1fa2", "#00897b", "#e65100", "#5c6bc0"];
function avatarColor(applicant: Applicant) {
  return avatarColors[Math.max(0, directions.indexOf(applicant.direction))];
}
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
        <h1>{{ isStudent ? "我的报名" : "招新工作台" }}</h1>
        <p>
          {{
            isStudent
              ? "填写报名资料，查看面试安排与选拔结果。"
              : `${settings?.season || "新赛季"} · 候选人资料与面试管理`
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
        rounded="sm"
        elevation="0"
        border
      >
        <div class="application-banner">
          <div>
            <span class="application-meta"
              >{{ own.team }} · {{ own.direction }}</span
            >
            <h2>
              {{ own.name }}，{{
                own.status === "accepted" ? "欢迎加入战队" : "你的报名资料"
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
        rounded="sm"
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
      <div class="recruitment-layout">
        <v-card elevation="0" border class="applicants-card">
          <div class="list-heading">
            <h2>
              候选人名单 <span>{{ filtered.length }} 人</span>
            </h2>
            <p>
              {{
                auth.user?.role === "interviewer"
                  ? "仅显示分配给你的候选人"
                  : "点击候选人查看报名资料与面试评价"
              }}
            </p>
          </div>
          <div class="list-filters">
            <v-text-field
              v-model="search"
              placeholder="搜索姓名、学校、邮箱"
              prepend-inner-icon="mdi-magnify"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="搜索候选人"
            />
            <v-select
              v-model="directionFilter"
              :items="['全部方向', ...directions]"
              variant="outlined"
              density="compact"
              hide-details
              aria-label="筛选方向"
            />
          </div>
          <div
            v-if="statusFilter !== 'all' || teamFilter !== '全部赛队'"
            class="active-filters"
          >
            <v-chip
              v-if="teamFilter !== '全部赛队'"
              size="small"
              closable
              color="primary"
              variant="outlined"
              @click:close="clearQueryFilter('team')"
              >{{ teamFilter }}</v-chip
            >
            <v-chip
              v-if="statusFilter !== 'all'"
              size="small"
              closable
              color="primary"
              variant="outlined"
              @click:close="clearQueryFilter('status')"
              >{{ statusOf(statusFilter).label }}</v-chip
            >
          </div>
          <div class="candidate-list" aria-label="候选人名单">
            <article v-for="a in filtered" :key="a.id" class="candidate-row">
              <button
                type="button"
                class="candidate-person"
                :aria-label="`查看 ${a.name} 的报名资料`"
                @click="openDetail(a)"
              >
                <span
                  class="candidate-avatar"
                  :style="{ backgroundColor: avatarColor(a) }"
                  >{{ a.name.slice(0, 1) }}</span
                >
                <span class="candidate-identity">
                  <span class="candidate-name"
                    >{{ a.name }}
                    <span class="candidate-direction">{{
                      a.direction
                    }}</span></span
                  >
                  <span class="candidate-school"
                    >{{ a.university }} · {{ a.major }} · {{ a.team }}</span
                  >
                </span>
              </button>
              <div class="candidate-schedule">
                <span><v-icon size="13">mdi-clock-outline</v-icon>面试安排</span
                ><time>{{
                  a.interviewAt ? formatDate(a.interviewAt) : "尚未安排"
                }}</time>
              </div>
              <div class="candidate-result">
                <v-chip
                  :color="statusOf(a.status).color"
                  size="small"
                  variant="outlined"
                  >{{ statusOf(a.status).label }}</v-chip
                >
                <span
                  class="candidate-score"
                  :class="{ 'has-score': a.score !== null }"
                  >{{ a.score === null ? "尚未评分" : `${a.score} 分` }}</span
                >
              </div>
              <v-btn
                class="candidate-open"
                variant="text"
                color="primary"
                size="small"
                icon="mdi-chevron-right"
                :aria-label="`查看 ${a.name}`"
                @click="openDetail(a)"
              />
            </article>
          </div>
          <div v-if="!filtered.length" class="table-empty">
            <v-icon size="38" color="grey">mdi-account-search-outline</v-icon>
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
              color="primary"
              @click="clearFilters"
              >清除筛选</v-btn
            >
          </div>
          <div v-if="filtered.length" class="list-footer">
            共 {{ filtered.length }} 位候选人<span
              >资料仅对有权限的成员可见</span
            >
          </div>
        </v-card>

        <aside class="statistics-column" aria-label="招新统计">
          <section class="material-chart-card">
            <div class="chart-surface trend-surface">
              <svg viewBox="0 0 300 150" role="img" :aria-label="trendLabel">
                <path d="M24 34H276M24 75H276M24 116H276" class="chart-grid" />
                <text x="4" y="38" class="chart-tick">{{ trendMax }}</text>
                <text x="9" y="120" class="chart-tick">0</text>
                <polyline :points="trendLine" class="trend-line" />
                <g v-for="point in trendPoints" :key="point.label">
                  <circle
                    :cx="point.x"
                    :cy="point.y"
                    r="3.5"
                    class="trend-point"
                  />
                  <text
                    :x="point.x"
                    y="140"
                    text-anchor="middle"
                    class="chart-tick"
                  >
                    {{ point.label }}
                  </text>
                </g>
              </svg>
            </div>
            <div class="chart-caption">
              <h2>每日新增报名</h2>
              <p>
                <v-icon size="16" color="success">mdi-arrow-up</v-icon>今日新增
                <strong>{{ dailyCounts[6]?.count || 0 }}</strong> 人<span
                  >近 7 天</span
                >
              </p>
            </div>
            <div class="chart-footer">
              <v-icon size="14">mdi-account-group-outline</v-icon
              >{{ chartScope }}
            </div>
          </section>
          <section class="material-chart-card">
            <div class="chart-surface direction-surface">
              <svg
                viewBox="0 0 300 166"
                role="img"
                :aria-label="directionLabel"
              >
                <path
                  d="M60 17V142M131 17V142M202 17V142M273 17V142"
                  class="chart-grid"
                />
                <g
                  v-for="(item, index) in directionCounts"
                  :key="item.direction"
                >
                  <text
                    x="48"
                    :y="31 + index * 26"
                    text-anchor="end"
                    class="direction-chart-label"
                  >
                    {{ item.direction }}
                  </text>
                  <rect
                    x="60"
                    :y="20 + index * 26"
                    :width="(item.count / directionMax) * 193"
                    height="14"
                    rx="1"
                    class="direction-bar"
                  />
                  <text
                    :x="68 + (item.count / directionMax) * 193"
                    :y="31 + index * 26"
                    class="direction-chart-value"
                  >
                    {{ item.count }}
                  </text>
                </g>
              </svg>
            </div>
            <div class="chart-caption">
              <h2>志愿方向分布</h2>
              <p>
                按第一志愿统计<span>共 {{ chartItems.length }} 人</span>
              </p>
            </div>
            <div class="chart-footer">
              <v-icon size="14">mdi-chart-bar</v-icon>{{ chartScope }}
            </div>
          </section>
          <div class="statistics-note">
            <v-icon size="15">mdi-information-outline</v-icon
            ><span>统计基于当前可见报名资料，名单筛选不会改变统计口径。</span>
          </div>
        </aside>
      </div>
    </template>

    <v-dialog
      v-model="formOpen"
      max-width="900"
      :fullscreen="smAndDown"
      :persistent="saving"
      scrollable
      ><v-card rounded="sm"
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
            ><h3 class="form-section-title">基础信息</h3>
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
            <h3 class="form-section-title">意向赛队与方向</h3>
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
            <h3 class="form-section-title">经历与报名动机</h3>
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
      ><v-card v-if="selected" rounded="sm"
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
      ><v-card rounded="sm" class="pa-4"
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
.recruitment-page {
  max-width: 1600px;
  padding: 24px;
}
.page-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 24px;
  margin-bottom: 22px;
}
.page-heading h1 {
  font-size: 24px;
  font-weight: 500;
  letter-spacing: 0;
  margin: 0 0 6px;
  color: #303030;
}
.page-heading p,
.dialog-title p {
  color: #757575;
  font-size: 14px;
  margin: 0;
}
.loading-panel {
  min-height: 300px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: #757575;
}
.recruitment-layout {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr);
  gap: 24px;
  align-items: start;
}
.applicants-card {
  border-radius: 6px !important;
  border-color: #e0e0e0 !important;
  overflow: hidden;
}
.list-heading {
  padding: 20px 20px 14px;
}
.list-heading h2 {
  margin: 0 0 6px;
  font-size: 18px;
  font-weight: 500;
  color: #303030;
}
.list-heading h2 span {
  margin-left: 8px;
  font-size: 13px;
  color: #757575;
  font-weight: 400;
}
.list-heading p {
  margin: 0;
  color: #757575;
  font-size: 12px;
}
.list-filters {
  display: flex;
  gap: 12px;
  padding: 0 20px 18px;
}
.list-filters > .v-text-field {
  flex: 1 1 auto;
  min-width: 0;
}
.list-filters > .v-select {
  flex: 0 0 144px;
}
.active-filters {
  padding: 0 20px 14px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.candidate-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 17px 14px 17px 20px;
  border-top: 1px solid #eeeeee;
  min-height: 88px;
  transition: background-color 0.15s ease;
}
.candidate-row:hover {
  background: #f5f8fc;
}
.candidate-person {
  appearance: none;
  display: flex;
  flex: 1 1 auto;
  align-items: center;
  gap: 14px;
  min-width: 0;
  color: inherit;
  text-align: left;
  cursor: pointer;
  border-radius: 4px;
}
.candidate-avatar {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 20px;
  font-weight: 400;
}
.candidate-identity {
  min-width: 0;
  display: block;
}
.candidate-name {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #303030;
  font-size: 16px;
  font-weight: 500;
}
.candidate-direction {
  font-weight: 400;
  color: #757575;
  font-size: 12px;
  white-space: nowrap;
}
.candidate-school {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: 6px;
  color: #757575;
  font-size: 12px;
}
.candidate-schedule {
  flex: 0 0 125px;
  display: grid;
  gap: 7px;
  font-size: 12px;
  color: #616161;
}
.candidate-schedule > span {
  display: flex;
  align-items: center;
  gap: 4px;
  color: #858585;
}
.candidate-schedule time {
  white-space: nowrap;
}
.candidate-result {
  display: flex;
  flex: 0 0 70px;
  align-items: flex-end;
  flex-direction: column;
  gap: 7px;
}
.candidate-result :deep(.v-chip) {
  font-size: 11px;
  height: 22px;
}
.candidate-score {
  font-size: 12px;
  color: #9e9e9e;
}
.candidate-score.has-score {
  font-size: 15px;
  color: #424242;
}
.candidate-open {
  flex-shrink: 0;
}
.list-footer {
  border-top: 1px solid #eeeeee;
  padding: 14px 20px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: #757575;
  font-size: 12px;
}
.list-footer span {
  color: #9e9e9e;
}
.statistics-column {
  display: grid;
  gap: 28px;
  padding-top: 18px;
  min-width: 0;
}
.material-chart-card {
  background: #fff;
  border: 1px solid #e8e8e8;
  border-radius: 6px;
  padding: 0 14px;
  box-shadow: 0 2px 5px #00000008;
}
.chart-surface {
  margin-top: -18px;
  border-radius: 5px;
  padding: 8px 10px 0;
  color: #fff;
  box-shadow: 0 4px 10px #00000024;
}
.trend-surface {
  background: #43a047;
}
.direction-surface {
  background: #e91e63;
}
.chart-surface svg {
  display: block;
  width: 100%;
  overflow: visible;
}
.chart-grid {
  stroke: #ffffff40;
  stroke-width: 1;
  stroke-dasharray: 3 3;
  fill: none;
}
.chart-tick {
  fill: #ffffffdc;
  font-size: 9px;
  font-family: inherit;
}
.trend-line {
  fill: none;
  stroke: #ffffffed;
  stroke-width: 2.5;
  stroke-linejoin: round;
}
.trend-point {
  fill: #fff;
}
.direction-chart-label {
  fill: #ffffffed;
  font-size: 10px;
  font-family: inherit;
}
.direction-chart-value {
  fill: #fff;
  font-size: 10px;
  font-family: inherit;
}
.direction-bar {
  fill: #ffffffe0;
}
.chart-caption {
  padding: 17px 2px 14px;
}
.chart-caption h2 {
  margin: 0 0 9px;
  font-size: 18px;
  font-weight: 400;
  color: #424242;
}
.chart-caption p {
  display: flex;
  align-items: center;
  gap: 4px;
  margin: 0;
  color: #757575;
  font-size: 12px;
}
.chart-caption strong {
  color: #43a047;
  font-weight: 500;
}
.chart-caption p > span {
  margin-left: auto;
  color: #9e9e9e;
}
.chart-footer {
  padding: 12px 0;
  display: flex;
  align-items: center;
  gap: 5px;
  border-top: 1px solid #eeeeee;
  font-size: 11px;
  color: #9e9e9e;
}
.statistics-note {
  display: flex;
  gap: 7px;
  font-size: 12px;
  color: #858585;
  line-height: 1.7;
  margin-top: -10px;
}
.statistics-note > .v-icon {
  margin-top: 3px;
  flex-shrink: 0;
}
.table-empty {
  padding: 60px 24px;
  text-align: center;
  color: #858585;
  border-top: 1px solid #eeeeee;
}
.table-empty h3 {
  font-size: 16px;
  font-weight: 500;
  color: #616161;
  margin: 16px 0 7px;
}
.table-empty p {
  font-size: 13px;
}
.application-card {
  max-width: 1060px;
}
.application-banner {
  padding: 22px 24px;
  border-bottom: 1px solid #e5e5e5;
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.application-meta {
  font-size: 13px;
  color: #757575;
}
.application-banner h2 {
  font-size: 21px;
  font-weight: 500;
  color: #303030;
  margin: 7px 0 0;
}
.application-body {
  padding: 24px;
}
.progress-track {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 28px;
  padding-bottom: 22px;
  border-bottom: 1px solid #eeeeee;
}
.progress-step {
  display: flex;
  gap: 9px;
  align-items: center;
  color: #9e9e9e;
  font-size: 13px;
}
.progress-step strong {
  font-weight: 400;
}
.progress-step span {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: #eeeeee;
}
.progress-step.active {
  color: #1976d2;
}
.progress-step.active span {
  background: #1976d2;
  color: #fff;
}
.detail-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 18px;
  margin-bottom: 22px;
}
.detail-grid label,
.prose-block label {
  font-size: 12px;
  color: #757575;
}
.detail-grid p {
  font-size: 14px;
  line-height: 1.7;
  margin: 5px 0 0;
  color: #424242;
  overflow-wrap: anywhere;
}
.prose-block {
  margin-bottom: 22px;
}
.prose-block p,
.preserve-lines {
  font-size: 14px;
  color: #424242;
  line-height: 1.8;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin-top: 6px;
}
.application-footer {
  border-top: 1px solid #eeeeee;
  padding-top: 18px;
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  color: #9e9e9e;
  font-size: 12px;
}
.empty-card {
  text-align: center;
  padding: 52px 24px;
  max-width: 1060px;
}
.empty-icon {
  width: 64px;
  height: 64px;
  background: #e3f2fd;
  color: #1976d2;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: auto auto 20px;
}
.empty-card h2 {
  font-size: 21px;
  font-weight: 500;
  color: #424242;
}
.empty-card p {
  font-size: 14px;
  line-height: 1.8;
  color: #757575;
  margin: 14px 0 22px;
}
.deadline {
  display: block;
  margin-top: 16px;
  font-size: 12px;
  color: #9e9e9e;
}
.dialog-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 18px 24px !important;
  white-space: normal !important;
  border-bottom: 1px solid #eeeeee;
}
.dialog-title h2 {
  font-size: 21px;
  font-weight: 500;
  color: #303030;
  margin: 0 0 5px;
}
.dialog-title p {
  font-size: 12px;
}
.form-section-title {
  font-size: 16px;
  font-weight: 500;
  color: #424242;
  margin: 22px 0 18px;
}
.form-submit {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin: 10px 0 16px;
}
.form-submit > span {
  font-size: 12px;
  color: #757575;
}
.review-panel {
  background: #fafafa;
  border: 1px solid #e5e5e5;
  border-radius: 6px;
  padding: 20px;
}
.review-panel h3 {
  font-size: 16px;
  font-weight: 500;
  color: #424242;
}
.review-panel > p {
  font-size: 13px;
  color: #757575;
}
.private-note {
  font-size: 11px !important;
  margin-top: 13px !important;
  color: #858585 !important;
}
.decision-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin: 18px 0;
}
.decision-actions > .v-btn {
  flex-grow: 1;
}
@media (max-width: 1250px) {
  .candidate-row {
    gap: 10px;
    padding-left: 16px;
  }
  .candidate-person {
    gap: 10px;
  }
  .candidate-schedule {
    flex-basis: 112px;
    font-size: 11px;
  }
  .candidate-open {
    display: none;
  }
}
@media (max-width: 1100px) {
  .recruitment-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .statistics-column {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 24px;
  }
  .statistics-note {
    grid-column: 1 / -1;
  }
  .candidate-open {
    display: inline-grid;
  }
  .candidate-schedule {
    flex-basis: 150px;
  }
}
@media (max-width: 600px) {
  .recruitment-page {
    padding: 18px 14px;
  }
  .page-heading {
    align-items: flex-start;
    gap: 12px;
    margin-bottom: 18px;
  }
  .page-heading h1 {
    font-size: 22px;
  }
  .page-heading p {
    font-size: 12px;
    line-height: 1.6;
  }
  .page-heading > .v-btn {
    min-width: auto;
  }
  .list-heading {
    padding: 18px 16px 14px;
  }
  .list-filters {
    padding: 0 16px 16px;
    gap: 8px;
  }
  .list-filters > .v-select {
    flex-basis: 112px;
  }
  .candidate-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 64px;
    gap: 8px 10px;
    padding: 15px 16px;
  }
  .candidate-person {
    grid-column: 1;
    grid-row: 1;
  }
  .candidate-avatar {
    width: 36px;
    height: 36px;
    flex-basis: 36px;
    font-size: 18px;
  }
  .candidate-name {
    font-size: 15px;
    gap: 6px;
  }
  .candidate-school {
    font-size: 11px;
  }
  .candidate-result {
    grid-column: 2;
    grid-row: 1 / 3;
    align-self: center;
    gap: 8px;
  }
  .candidate-schedule {
    grid-column: 1;
    grid-row: 2;
    display: flex;
    gap: 6px;
    padding-left: 46px;
    font-size: 11px;
  }
  .candidate-schedule > span {
    font-size: 0;
  }
  .candidate-schedule .v-icon {
    font-size: 13px !important;
  }
  .candidate-open {
    display: none;
  }
  .list-footer {
    padding: 13px 16px;
  }
  .list-footer span {
    display: none;
  }
  .statistics-column {
    grid-template-columns: minmax(0, 1fr);
    gap: 30px;
  }
  .chart-surface svg {
    max-height: 180px;
  }
  .application-banner {
    padding: 20px;
    align-items: flex-start;
  }
  .application-banner h2 {
    font-size: 18px;
    line-height: 1.5;
  }
  .application-body {
    padding: 20px;
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
}
</style>
