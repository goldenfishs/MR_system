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
    <h1 class="page-title">个人资料</h1>
    <p class="page-subtitle">
      一次完善基本信息，报名与个人档案自动关联到你的账号。
    </p>
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
  </div>
</template>
<style scoped>
.profile-page {
  max-width: 900px;
}
</style>
