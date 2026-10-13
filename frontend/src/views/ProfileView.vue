<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api } from "../lib/api";
import { auth, roleNames } from "../lib/auth";
import { memberTypeNames, groupLabel } from "../lib/permissions";
import { dateTime } from "../lib/format";
import { currentTrainingStage, trainingStatusNames, trainingStatusColors, type TrainingProgress } from "../lib/training";
import {
  blankProfile,
  profileError,
  type Profile,
  type PhotoInput,
} from "../lib/platform";
import ProfileFields from "../components/ProfileFields.vue";
import RecordsView from "./RecordsView.vue";
import FilesView from "./FilesView.vue";
const activeTab = ref("profile");
const training = ref<TrainingProgress[]>([]);
const trainingError = ref("");
const latestTraining = computed(() => [...training.value].sort((a, b) => b.cycle - a.cycle)[0]);
const trainingStage = computed(() => latestTraining.value ? currentTrainingStage(latestTraining.value) : undefined);
const form = ref(blankProfile()),
  photo = ref<PhotoInput | null>(null),
  profile = ref<Profile | null>(null),
  loading = ref(true),
  saving = ref(false),
  error = ref(""),
  notice = ref("");
async function load() {
  loading.value = true;
  error.value = "";
  trainingError.value = "";
  try {
    const [profileResult, trainingResult] = await Promise.allSettled([
      api.get<{ profile: Profile }>("/profile"),
      api.get<{ items: TrainingProgress[] }>("/training-progress"),
    ]);
    if (profileResult.status === "rejected") throw profileResult.reason;
    profile.value = profileResult.value.profile;
    if (trainingResult.status === "fulfilled") training.value = trainingResult.value.items.filter(item => item.userId === auth.user?.id);
    else trainingError.value = (trainingResult.reason as Error).message;
    form.value = {
      name: profile.value.name || auth.user?.name || "",
      gender: profile.value.gender,
      university: profile.value.university,
      major: profile.value.major,
      className: profile.value.className,
      studentNumber: profile.value.studentNumber,
      phone: profile.value.phone,
      hobbies: profile.value.hobbies,
    };
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (saving.value || loading.value || !profile.value) return;
  error.value = profileError(
    form.value,
    Boolean(photo.value || profile.value?.photoFileId),
  );
  if (error.value) return;
  saving.value = true;
  notice.value = "";
  try {
    profile.value = (
      await api.patch<{ profile: Profile }>("/profile", {
        ...form.value,
        ...(photo.value ? { photo: photo.value } : {}),
      })
    ).profile;
    photo.value = null;
    if (auth.user) auth.user.name = profile.value.name;
    notice.value = "个人资料已保存，后续报名会使用这些资料。";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content profile-page">
    <h1 class="page-title">个人中心</h1>
    <p class="page-subtitle">
      管理基本资料、报名身份与个人档案，所有记录都关联到你的账号。
    </p>
    <v-card v-if="auth.user" class="pa-4 mb-5">
      <div class="d-flex flex-wrap align-center ga-2"><v-chip color="primary" variant="tonal">{{ roleNames[auth.user.role] }}</v-chip><v-chip v-if="auth.user.role === 'group_admin' && auth.user.memberType" variant="outlined">{{ memberTypeNames[auth.user.memberType] }}</v-chip><v-chip v-for="grant in auth.user.groupGrants" :key="grant.direction" size="small">{{ groupLabel(grant.direction) }}管理</v-chip></div>
      <p class="text-caption text-medium-emphasis mt-3">{{ auth.user.assessmentPassedAt ? `新生考核已通过 · ${dateTime(auth.user.assessmentPassedAt)}` : auth.user.memberType === 'freshman' || auth.user.role === 'freshman' ? '报名通过后以新生身份参加阶段考核、学习各组内容；最终通过后，由管理员或获考核授权的组管理员确认晋升为梯队或正式队员。' : '成员身份由管理员维护。' }}</p>
      <div v-if="latestTraining" class="training-summary mt-4">
        <div class="d-flex flex-wrap align-center ga-2">
          <strong>新生阶段考核</strong>
          <v-chip :color="trainingStatusColors[latestTraining.status]" size="small" variant="tonal">{{ trainingStatusNames[latestTraining.status] }}</v-chip>
          <span class="text-caption text-medium-emphasis">第 {{ latestTraining.cycle }} 轮 · {{ groupLabel(latestTraining.direction) }}</span>
        </div>
        <p v-if="latestTraining.status === 'training'" class="text-body-2 mt-2">当前阶段：{{ trainingStage?.title || '等待阶段安排' }}。中间阶段通过后仍为新生，完成最终考核后确认成员身份。</p>
        <p v-else-if="latestTraining.status === 'eliminated'" class="text-body-2 mt-2">本轮已在「{{ latestTraining.decisions[latestTraining.decisions.length - 1]?.stageTitle || '阶段考核' }}」淘汰，学习和作业提交权限已关闭。报名、考核和荣誉历史仍可查看；重新录取后开启新一轮培养。</p>
        <p v-else class="text-body-2 mt-2">已完成本轮全部阶段考核，可查看每次考核结果和确认记录。</p>
        <v-btn to="/training" variant="text" color="primary" prepend-icon="mdi-clipboard-check-outline" class="mt-2">查看考核记录</v-btn>
      </div>
      <v-alert v-if="trainingError" type="warning" variant="tonal" class="mt-4">考核状态加载失败：{{ trainingError }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    </v-card>
    <v-tabs v-model="activeTab" color="primary" class="profile-tabs" show-arrows aria-label="个人中心分类"><v-tab value="profile" prepend-icon="mdi-account-outline">个人资料</v-tab><v-tab value="records" prepend-icon="mdi-card-account-details-outline">个人档案</v-tab><v-tab value="files" prepend-icon="mdi-folder-outline">我的附件</v-tab></v-tabs>
    <RecordsView v-if="activeTab === 'records'" personal-only embedded />
    <FilesView v-else-if="activeTab === 'files'" personal-only embedded />
    <template v-else>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4"
      >{{ error
      }}<v-btn v-if="!profile" variant="text" @click="load"
        >重试</v-btn
      ></v-alert
    ><v-alert v-if="notice" type="success" variant="tonal" class="mb-4">{{
      notice
    }}</v-alert
    ><v-progress-linear v-if="loading" indeterminate /><v-card
      v-else-if="profile"
      class="pa-6"
      ><v-alert
        v-if="!profile.complete"
        type="info"
        variant="tonal"
        class="mb-5"
        >报名团队前，请补全带 * 的资料和本人照片。</v-alert
      >
      <form @submit.prevent="save">
        <ProfileFields
          v-model="form"
          v-model:photo="photo"
          :photo-file-id="profile.photoFileId"
          :disabled="saving"
        />
        <div class="d-flex justify-end">
          <v-btn color="primary" type="submit" :loading="saving"
            >保存个人资料</v-btn
          >
        </div>
      </form></v-card
    >
    </template>
  </div>
</template>
<style scoped>
.profile-tabs { margin-bottom:24px; border-bottom:1px solid #e5e7eb; }
.training-summary { border-top: 1px solid #e5e7eb; padding-top: 16px; }
.profile-page {
  max-width: 900px;
}
</style>
