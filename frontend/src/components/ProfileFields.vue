<script setup lang="ts">
import { computed, ref } from "vue";
import type { PhotoInput, ProfileInput } from "../lib/platform";
import { previewUrl } from "../lib/platform";
const props = defineProps<{
  modelValue: ProfileInput;
  photo?: PhotoInput | null;
  photoFileId?: string;
  disabled?: boolean;
  hideName?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [ProfileInput];
  "update:photo": [PhotoInput | null];
}>();
const error = ref("");
const imageSrc = computed(() =>
  props.photo
    ? `data:image/${props.photo.name.toLowerCase().endsWith(".png") ? "png" : "jpeg"};base64,${props.photo.contentBase64}`
    : props.photoFileId
      ? previewUrl(props.photoFileId)
      : "",
);
function update(key: keyof ProfileInput, value: string | null) {
  emit("update:modelValue", { ...props.modelValue, [key]: value || "" });
}
async function choosePhoto(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  error.value = "";
  if (!file) return;
  if (
    !["image/jpeg", "image/png"].includes(file.type) ||
    file.size > 2 * 1024 * 1024
  ) {
    error.value = "请选择不超过 2 MB 的 JPEG 或 PNG 照片。";
    return;
  }
  try {
    const content = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result).split(",")[1] || "");
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
    emit("update:photo", { name: file.name, contentBase64: content });
  } catch {
    error.value = "照片读取失败，请重新选择。";
  }
}
</script>
<template>
  <div class="profile-fields">
    <div class="photo-row">
      <v-avatar size="68" color="grey-lighten-3"
        ><v-img v-if="imageSrc" :src="imageSrc" alt="本人照片" /><v-icon
          v-else
          size="34"
          >mdi-account-outline</v-icon
        ></v-avatar
      >
      <div>
        <label class="photo-label"
          >本人照片 *
          <input
            type="file"
            accept="image/jpeg,image/png"
            :disabled="disabled"
            @change="choosePhoto"
        /></label>
        <p>JPEG / PNG，最多 2 MB。用于个人资料与报名审核。</p>
      </div>
    </div>
    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      density="compact"
      class="mb-4"
      >{{ error }}</v-alert
    >
    <v-row dense>
      <v-col v-if="!hideName" cols="12" sm="6"
        ><v-text-field
          :model-value="modelValue.name"
          @update:model-value="update('name', $event)"
          label="姓名 *"
          autocomplete="name"
          maxlength="60"
          :disabled="disabled"
          required
      /></v-col>
      <v-col cols="12" sm="6"
        ><v-select
          :model-value="modelValue.gender"
          @update:model-value="update('gender', $event)"
          :items="['男', '女', '其他']"
          label="性别 *"
          :disabled="disabled"
          aria-required="true"
      /></v-col>
      <v-col cols="12" sm="6"
        ><v-text-field
          :model-value="modelValue.university"
          @update:model-value="update('university', $event)"
          label="学校 *"
          maxlength="120"
          :disabled="disabled"
          required
      /></v-col>
      <v-col cols="12" sm="6"
        ><v-text-field
          :model-value="modelValue.major"
          @update:model-value="update('major', $event)"
          label="专业 *"
          maxlength="120"
          :disabled="disabled"
          required
      /></v-col>
      <v-col cols="12" sm="6"
        ><v-text-field
          :model-value="modelValue.className"
          @update:model-value="update('className', $event)"
          label="班级 *"
          maxlength="80"
          :disabled="disabled"
          required
      /></v-col>
      <v-col cols="12" sm="6"
        ><v-text-field
          :model-value="modelValue.studentNumber"
          @update:model-value="update('studentNumber', $event)"
          label="学号 *"
          maxlength="60"
          :disabled="disabled"
          required
      /></v-col>
      <v-col cols="12" sm="6"
        ><v-text-field
          :model-value="modelValue.phone"
          @update:model-value="update('phone', $event)"
          label="联系电话 *"
          type="tel"
          autocomplete="tel"
          maxlength="32"
          :disabled="disabled"
          required
      /></v-col>
      <v-col cols="12"
        ><v-textarea
          :model-value="modelValue.hobbies"
          @update:model-value="update('hobbies', $event)"
          label="兴趣爱好（选填）"
          rows="2"
          maxlength="2000"
          :disabled="disabled"
      /></v-col>
    </v-row>
  </div>
</template>
<style scoped>
.photo-row {
  display: flex;
  gap: 16px;
  align-items: center;
  margin: 8px 0 24px;
}
.photo-row > div {
  min-width: 0;
}
.photo-label {
  font-size: 14px;
  display: block;
}
.photo-label input {
  display: block;
  max-width: 100%;
  font-size: 12px;
  margin-top: 8px;
}
.photo-row p {
  font-size: 12px;
  color: #757575;
  margin-top: 6px;
  line-height: 1.6;
}
</style>
