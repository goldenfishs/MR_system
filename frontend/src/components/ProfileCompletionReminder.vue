<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../lib/api";
import { auth } from "../lib/auth";
import type { Profile } from "../lib/platform";

const route = useRoute(), router = useRouter();
const incomplete = ref(false), dismissed = ref(false);
let checkedUserId: string | undefined;
let checkingUserId: string | undefined;
let requestVersion = 0;

const visible = computed(() => Boolean(
  auth.user && !auth.signingOut && incomplete.value && !dismissed.value &&
  route.path !== "/login" && route.path !== "/profile",
));

async function checkProfile(force = false) {
  const userId = auth.user?.id;
  if (!userId || auth.signingOut || route.path === "/login") return;
  if (!force && (checkedUserId === userId || checkingUserId === userId)) return;
  const version = ++requestVersion;
  checkingUserId = userId;
  try {
    const { profile } = await api.get<{ profile: Profile }>("/profile");
    if (version !== requestVersion || auth.user?.id !== userId) return;
    checkedUserId = userId;
    incomplete.value = !profile.complete;
  } catch {
    // Retry on the next navigation or focus; a failed request is not a missing profile.
  } finally {
    if (version === requestVersion) checkingUserId = undefined;
  }
}

watch(() => auth.user?.id, () => {
  requestVersion++;
  checkedUserId = undefined;
  checkingUserId = undefined;
  incomplete.value = false;
  dismissed.value = false;
  void checkProfile();
}, { immediate: true });
watch(() => route.path, () => { void checkProfile(); });

function profileUpdated() { void checkProfile(true); }
function retryCheck() { void checkProfile(); }
async function completeProfile() {
  await router.push("/profile");
}
onMounted(() => {
  window.addEventListener("profile-updated", profileUpdated);
  window.addEventListener("focus", retryCheck);
});
onUnmounted(() => {
  requestVersion++;
  window.removeEventListener("profile-updated", profileUpdated);
  window.removeEventListener("focus", retryCheck);
});
</script>

<template>
  <v-dialog :model-value="visible" max-width="460" @update:model-value="!$event && (dismissed = true)">
    <v-card rounded="lg" aria-labelledby="profile-reminder-title" aria-describedby="profile-reminder-description">
      <v-card-text class="pt-7 pb-3 px-6">
        <v-avatar color="primary" variant="tonal" size="48" class="mb-4">
          <v-icon size="26">mdi-account-edit-outline</v-icon>
        </v-avatar>
        <h2 id="profile-reminder-title" class="text-h6 mb-2">完善个人资料</h2>
        <p id="profile-reminder-description" class="text-body-2 text-medium-emphasis">
          你的个人资料还未填写完整。请前往个人中心补全姓名、性别、学校、专业、班级、学号、联系电话和本人证件照，之后即可报名团队。
        </p>
        <p class="text-caption text-medium-emphasis mt-3">兴趣爱好选填，资料可在个人中心随时更新。</p>
      </v-card-text>
      <v-card-actions class="px-6 pb-5 pt-3 flex-wrap">
        <v-spacer />
        <v-btn variant="text" @click="dismissed = true">稍后填写</v-btn>
        <v-btn color="primary" variant="flat" @click="completeProfile">去填写资料</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
