<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { api } from "../lib/api";
import { auth, roleNames } from "../lib/auth";
import { dateTime } from "../lib/format";
import type { User } from "../lib/types";
const members = ref<User[]>([]),
  loading = ref(true),
  saving = ref(false),
  error = ref(""),
  formError = ref(""),
  search = ref(""),
  team = ref("全部赛队"),
  direction = ref("全部方向"),
  dialog = ref(false),
  success = ref(false);
const draft = reactive({
  name: "",
  email: "",
  password: "",
  role: "mentor",
  team: "RoboMaster",
  direction: "电控",
});
const filtered = computed(() =>
  members.value.filter(
    (u) =>
      (team.value === "全部赛队" || u.team === team.value) &&
      (direction.value === "全部方向" || u.direction === direction.value) &&
      `${u.name} ${u.email}`.toLowerCase().includes(search.value.toLowerCase()),
  ),
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    members.value = (await api.get<{ items: User[] }>("/members")).items || [];
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
async function create() {
  if (!draft.name.trim() || !draft.email.trim() || draft.password.length < 12) {
    formError.value = "请填写姓名、邮箱和至少 12 位的初始密码。";
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await api.post("/members", draft);
    dialog.value = false;
    draft.password = "";
    success.value = true;
    await load();
  } catch (e) {
    formError.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <div class="d-flex justify-space-between align-start ga-3">
      <div>
        <h1 class="page-title">战队成员<span class="dot">.</span></h1>
        <p class="page-subtitle">不同的专长，共同的目标。</p>
      </div>
      <v-btn
        v-if="auth.user?.role === 'admin'"
        color="primary"
        prepend-icon="mdi-account-plus-outline"
        @click="
          dialog = true;
          formError = '';
        "
        >添加导师 / 面试官</v-btn
      >
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    >
    <v-card class="mb-6"
      ><div class="member-intro">
        <div class="intro-icon">
          <v-icon size="28">mdi-account-group-outline</v-icon>
        </div>
        <div>
          <h2>成员名册</h2>
          <p>已录取学员与团队工作人员，共 {{ members.length }} 人</p>
        </div>
      </div></v-card
    >
    <div class="filters">
      <v-text-field
        v-model="search"
        placeholder="搜索姓名或邮箱"
        prepend-inner-icon="mdi-magnify"
        hide-details
        clearable
        @click:clear="search = ''"
      /><v-select
        v-model="team"
        :items="['全部赛队', 'RoboMaster', 'ROBOCON']"
        label="赛队"
        hide-details
      /><v-select
        v-model="direction"
        :items="['全部方向', '机械', '电控', '视觉算法', '硬件', '运营']"
        label="方向"
        hide-details
      />
    </div>
    <v-progress-linear
      v-if="loading"
      indeterminate
      color="primary"
      class="my-6"
    />
    <div v-else-if="!filtered.length" class="empty-state">
      <v-icon size="46">mdi-account-search-outline</v-icon>
      <p>没有找到符合条件的成员</p>
      <small>调整筛选条件，或在招新管理中录取新成员。</small>
    </div>
    <div v-else class="members-grid">
      <v-card v-for="u in filtered" :key="u.id" class="member-card"
        ><div class="member-top">
          <div class="member-avatar" :class="{ green: u.team === 'ROBOCON' }">
            {{ u.name.slice(-2) }}
          </div>
          <v-chip
            size="x-small"
            :color="u.role === 'student' ? 'secondary' : 'primary'"
            variant="tonal"
            >{{ roleNames[u.role] }}</v-chip
          >
        </div>
        <h3>{{ u.name }}</h3>
        <p class="member-email">{{ u.email }}</p>
        <div class="d-flex ga-2 mt-4">
          <v-chip size="small" variant="outlined" color="primary">{{
            u.team || "跨赛队"
          }}</v-chip
          ><v-chip size="small" variant="tonal">{{
            u.direction || "团队协作"
          }}</v-chip>
        </div>
        <div class="joined">
          <v-icon size="13">mdi-calendar-check-outline</v-icon
          >{{ dateTime(u.joinedAt) }} 加入
        </div></v-card
      >
    </div>
    <v-dialog v-model="dialog" max-width="560" :persistent="saving"
      ><v-card
        ><v-card-title class="pa-6 pb-2">添加团队工作人员</v-card-title
        ><v-card-text
          ><p class="text-body-2 text-medium-emphasis mb-5">
            为导师或面试官创建独立账号。请通过安全渠道告知对方初始密码。
          </p>
          <v-alert v-if="formError" type="error" variant="tonal" class="mb-4">{{
            formError
          }}</v-alert
          ><v-text-field
            v-model="draft.name"
            label="姓名"
            maxlength="60" /><v-text-field
            v-model="draft.email"
            label="邮箱"
            type="email" /><v-text-field
            v-model="draft.password"
            label="初始密码（至少 12 位）"
            type="password"
            autocomplete="new-password" /><v-select
            v-model="draft.role"
            :items="[
              { title: '导师', value: 'mentor' },
              { title: '面试官', value: 'interviewer' },
            ]"
            label="角色" /><v-row
            ><v-col
              ><v-select
                v-model="draft.team"
                :items="['RoboMaster', 'ROBOCON']"
                label="赛队" /></v-col
            ><v-col
              ><v-select
                v-model="draft.direction"
                :items="['机械', '电控', '视觉算法', '硬件', '运营']"
                label="方向" /></v-col></v-row></v-card-text
        ><v-card-actions class="px-6 pb-6"
          ><v-spacer /><v-btn
            :disabled="saving"
            @click="
              dialog = false;
              draft.password = '';
            "
            >取消</v-btn
          ><v-btn
            color="primary"
            variant="flat"
            :loading="saving"
            @click="create"
            >创建账号</v-btn
          ></v-card-actions
        ></v-card
      ></v-dialog
    >
    <v-snackbar v-model="success" color="success"
      >工作人员账号已创建</v-snackbar
    >
  </div>
</template>
<style scoped>
.dot {
  color: #7eb6a4;
  margin-left: 3px;
}
.member-intro {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 27px;
}
.intro-icon {
  width: 55px;
  height: 55px;
  border-radius: 50%;
  background: #edf2fa;
  color: #1976d2;
  display: grid;
  place-items: center;
}
.member-intro h2 {
  font-size: 17px;
  margin: 0 0 7px;
}
.member-intro p {
  font-size: 12px;
  margin: 0;
  color: #757575;
}
.member-intro .eyebrow {
  font-size: 8px;
  margin-left: auto;
  color: #a9b3c8;
}
.filters {
  display: grid;
  grid-template-columns: 2fr 1fr 1fr;
  gap: 16px;
  margin-bottom: 24px;
}
.members-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
}
.member-card {
  padding: 24px;
}
.member-top {
  display: flex;
  justify-content: space-between;
  align-items: start;
}
.member-avatar {
  width: 49px;
  height: 49px;
  background: #edf0fb;
  border-radius: 50%;
  color: #1976d2;
  font-size: 17px;
  display: grid;
  place-items: center;
}
.member-avatar.green {
  background: #eaf5ef;
  color: #388e3c;
}
.member-card h3 {
  margin: 18px 0 7px;
  font-size: 17px;
}
.member-email {
  font-size: 12px;
  color: #757575;
  word-break: break-all;
}
.joined {
  border-top: 1px solid #eef0f6;
  margin-top: 20px;
  padding-top: 16px;
  font-size: 12px;
  color: #757575;
  display: flex;
  align-items: center;
  gap: 7px;
}
@media (max-width: 1200px) {
  .members-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 600px) {
  .members-grid {
    grid-template-columns: 1fr;
  }
  .filters {
    grid-template-columns: 1fr 1fr;
  }
  .filters > *:first-child {
    grid-column: 1/-1;
  }
  .member-intro {
    padding: 20px;
  }
  .member-intro h2 {
    font-size: 14px;
  }
  .page-content > .d-flex {
    flex-wrap: wrap;
    margin-bottom: 15px;
  }
}
</style>
