<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
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
  try {
    profile.value = (await api.get<{ profile: Profile }>("/profile")).profile;
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
.profile-page {
  max-width: 900px;
}
</style>
