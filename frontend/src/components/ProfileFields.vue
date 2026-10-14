<script setup lang="ts">
import { computed, ref } from "vue";
import type { PhotoInput, ProfileInput } from "../lib/platform";
import { previewUrl } from "../lib/platform";
const props = defineProps<{
  modelValue: ProfileInput;
  avatar?: PhotoInput | null;
  avatarFileId?: string;
  photo?: PhotoInput | null;
  photoFileId?: string;
  disabled?: boolean;
  hideName?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [ProfileInput];
  "update:avatar": [PhotoInput | null];
  "update:photo": [PhotoInput | null];
  saveAvatar: [];
}>();
const error = ref("");
const avatarSrc = computed(() =>
  props.avatar
    ? `data:image/${props.avatar.name.toLowerCase().endsWith(".png") ? "png" : "jpeg"};base64,${props.avatar.contentBase64}`
    : props.avatarFileId
      ? previewUrl(props.avatarFileId)
      : "",
);
const photoSrc = computed(() =>
  props.photo
    ? `data:image/${props.photo.name.toLowerCase().endsWith(".png") ? "png" : "jpeg"};base64,${props.photo.contentBase64}`
    : props.photoFileId
      ? previewUrl(props.photoFileId)
      : "",
);
function update(key: keyof ProfileInput, value: string | null) {
  emit("update:modelValue", { ...props.modelValue, [key]: value || "" });
}
async function chooseImage(event: Event, kind: "avatar" | "photo") {
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
    const image = { name: file.name, contentBase64: content };
    if (kind === "avatar") emit("update:avatar", image);
    else emit("update:photo", image);
  } catch {
    error.value = "照片读取失败，请重新选择。";
  }
}
</script>
<template>
  <div class="profile-fields">
    <div class="image-fields">
      <section class="image-field avatar-field" aria-labelledby="avatar-label">
        <v-avatar size="64" color="grey-lighten-3">
          <v-img v-if="avatarSrc" :src="avatarSrc" alt="个人头像预览" />
          <v-icon v-else size="34">mdi-account-outline</v-icon>
        </v-avatar>
        <div class="image-copy">
          <strong id="avatar-label">个人头像 <span>选填</span></strong>
          <p>用于系统账号展示，与报名审核照片分开保存。</p>
          <input
            class="image-input"
            type="file"
            accept="image/jpeg,image/png"
            aria-label="选择个人头像"
            :disabled="disabled"
            @change="chooseImage($event, 'avatar')"
          />
          <v-btn v-if="avatar" type="button" size="small" variant="tonal" color="primary" :disabled="disabled" class="ml-2" @click="emit('saveAvatar')">单独保存头像</v-btn>
          <small>JPEG / PNG，最多 2 MB</small>
        </div>
      </section>
      <section class="image-field identity-field" aria-labelledby="identity-label">
        <div class="identity-preview">
          <v-img v-if="photoSrc" :src="photoSrc" alt="本人证件照预览" />
          <v-icon v-else size="34">mdi-card-account-details-outline</v-icon>
        </div>
        <div class="image-copy">
          <strong id="identity-label">本人证件照 <span class="required-mark">必填</span></strong>
          <p>仅用于团队报名身份审核，不会作为个人头像展示。</p>
          <input
            class="image-input"
            type="file"
            accept="image/jpeg,image/png"
            aria-label="选择本人证件照"
            :disabled="disabled"
            @change="chooseImage($event, 'photo')"
          />
          <small>请上传清晰的本人正面照；JPEG / PNG，最多 2 MB</small>
        </div>
      </section>
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
.image-fields {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin: 8px 0 28px;
}
.image-field {
  flex: 1 1 310px;
  min-width: 0;
  min-height: 122px;
  display: flex;
  align-items: center;
  gap: 16px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 16px;
  background: #fff;
}
.identity-field {
  border-color: #f0d9bc;
  background: #fffdfa;
}
.image-copy {
  min-width: 0;
}
.image-copy strong {
  display: block;
  font-size: 14px;
}
.image-copy strong span {
  color: #758195;
  font-size: 12px;
  font-weight: 400;
  margin-left: 4px;
}
.image-copy strong .required-mark {
  color: #c2410c;
  font-weight: 600;
}
.image-copy p {
  margin: 4px 0 8px;
  color: #697586;
  font-size: 12px;
  line-height: 1.5;
}
.image-copy small {
  display: block;
  margin-top: 5px;
  color: #758195;
  font-size: 11px;
  line-height: 1.45;
}
.identity-preview {
  flex: 0 0 68px;
  width: 68px;
  height: 88px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: #f3f4f6;
  color: #8993a2;
}
.identity-preview :deep(.v-img) {
  width: 100%;
  height: 100%;
}
.image-input {
  display: inline-block;
  max-width: 100%;
  color: #4b5563;
  font-size: 12px;
}
.image-input::file-selector-button {
  border: 1px solid #d6dee8;
  border-radius: 6px;
  background: #fff;
  color: #24568a;
  padding: 6px 10px;
  margin-right: 8px;
  cursor: pointer;
}
@media (max-width: 520px) {
  .image-field { padding: 12px; gap: 12px; }
  .image-copy { flex: 1; }
}
</style>
