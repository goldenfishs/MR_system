<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue';
const available = ref(false);
const entry = document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src');
let timer: ReturnType<typeof setInterval> | undefined;
let checking = false;
async function check() {
  if (import.meta.env.DEV || checking || document.visibilityState !== 'visible' || !entry) return;
  checking = true;
  try {
    const response = await fetch('/?version-check=1', { cache: 'no-store', signal: AbortSignal.timeout(8000) });
    if (!response.ok) return;
    const html = new DOMParser().parseFromString(await response.text(), 'text/html');
    const latest = html.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src');
    available.value = Boolean(latest && latest !== entry);
  } catch { /* Update notices must not interrupt normal work. */ } finally { checking = false; }
}
function reload() { window.location.reload(); }
onMounted(() => { timer = setInterval(check, 60000); window.addEventListener('focus', check); void check(); });
onBeforeUnmount(() => { clearInterval(timer); window.removeEventListener('focus', check); });
</script>
<template><v-alert v-if="available" type="info" variant="tonal" class="ma-4">网站已有新版本，请保存当前编辑内容后更新页面。<v-btn class="ml-3" variant="tonal" @click="reload">更新页面</v-btn></v-alert></template>
