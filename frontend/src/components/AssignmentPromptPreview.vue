<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { api } from "../lib/api";
import AttachmentList from "./AttachmentList.vue";
import { previewUrl, type FileAsset } from "../lib/platform";

const props = defineProps<{ ids: string[]; removable?: boolean; showAttachments?: boolean }>();
const emit = defineEmits<{ remove: [id: string] }>();
const metadata = reactive<Record<string, FileAsset>>({});
const imageIds = computed(() => props.ids.filter(id => ["image/jpeg", "image/png"].includes(metadata[id]?.mime || "")));
const attachmentIds = computed(() => props.ids.filter(id => !imageIds.value.includes(id)));
watch(() => [...props.ids], async ids => {
  await Promise.allSettled(ids.map(async id => {
    if (metadata[id]) return;
    const result = await api.get<{ item: FileAsset }>(`/files/${encodeURIComponent(id)}`);
    metadata[id] = result.item;
  }));
}, { immediate: true });
</script>

<template>
  <div v-if="ids.length" class="prompt-previews">
    <figure v-for="id in imageIds" :key="id" class="prompt-image">
      <a :href="previewUrl(id)" target="_blank" rel="noopener noreferrer"><img :src="previewUrl(id)" :alt="metadata[id]?.name || '题目图片'" loading="lazy" /></a>
      <figcaption>{{ metadata[id]?.name }}<v-btn v-if="removable" size="small" variant="text" color="error" :aria-label="`移除题图 ${metadata[id]?.name || ''}`" @click="emit('remove', id)">移除题图</v-btn></figcaption>
    </figure>
    <figure v-for="id in ids.filter(id => metadata[id]?.mime === 'application/pdf')" :key="`pdf-${id}`" class="prompt-pdf">
      <figcaption>{{ metadata[id]?.name || '题目 PDF' }}</figcaption>
      <iframe :src="previewUrl(id)" :title="metadata[id]?.name || '题目 PDF 预览'" loading="lazy" />
    </figure>
    <AttachmentList v-if="showAttachments" :ids="attachmentIds" :removable="removable" @remove="emit('remove', $event)" />
  </div>
</template>

<style scoped>
.prompt-previews { display: grid; gap: 12px; margin: 12px 0; }
.prompt-image, .prompt-pdf { margin: 0; overflow: hidden; border: 1px solid #e0e6ee; border-radius: 9px; background: #f8fafc; }
.prompt-image { justify-self: start; max-width: 100%; }
.prompt-image img { display: block; max-width: min(100%, 720px); max-height: 440px; object-fit: contain; }
.prompt-image figcaption, .prompt-pdf figcaption { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 8px 11px; color: #5d6b7a; font-size: 12px; }
.prompt-pdf iframe { display: block; width: 100%; height: min(68vh, 720px); border: 0; background: white; }
</style>
