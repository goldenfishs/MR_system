<script setup lang="ts">
import { ref, watch } from "vue";
import { api } from "../lib/api";
import { downloadUrl, fileSize, type FileAsset } from "../lib/platform";
const props = defineProps<{ ids?: string[]; removable?: boolean }>();
const emit = defineEmits<{ remove: [string] }>();
const files = ref<Record<string, FileAsset>>({});
watch(
  () => props.ids,
  async (ids) => {
    for (const id of ids || [])
      if (!files.value[id])
        try {
          const data = await api.get<{ item: FileAsset }>(
            `/files/${encodeURIComponent(id)}`,
          );
          files.value[id] = data.item;
        } catch {
          /* Download remains available even when metadata cannot be read. */
        }
  },
  { immediate: true, deep: true },
);
</script>
<template>
  <ul v-if="ids?.length" class="attachments">
    <li v-for="(id, index) in ids" :key="id">
      <v-icon size="19" color="primary">mdi-paperclip</v-icon
      ><a :href="downloadUrl(id)">{{
        files[id]?.name || `附件 ${index + 1}`
      }}</a
      ><span v-if="files[id]">{{ fileSize(files[id]!.size) }}</span
      ><v-btn
        v-if="removable"
        icon="mdi-close"
        size="x-small"
        variant="text"
        :aria-label="`移除${files[id]?.name || '附件'}`"
        @click="emit('remove', id)"
      />
    </li>
  </ul>
</template>
<style scoped>
.attachments {
  list-style: none;
  padding: 0;
  margin: 10px 0;
}
.attachments li {
  display: flex;
  align-items: center;
  gap: 8px;
  background: #f6f8fa;
  padding: 9px 12px;
  border-radius: 4px;
  margin-bottom: 6px;
  font-size: 13px;
}
.attachments a {
  color: #1976d2;
  overflow-wrap: anywhere;
  min-width: 0;
}
.attachments span {
  margin-left: auto;
  white-space: nowrap;
  color: #757575;
  font-size: 12px;
}
</style>
