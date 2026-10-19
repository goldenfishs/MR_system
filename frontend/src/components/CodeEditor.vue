<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { EditorView, basicSetup } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { cpp } from '@codemirror/lang-cpp';
import { python } from '@codemirror/lang-python';
import { oneDark } from '@codemirror/theme-one-dark';
const props = defineProps<{ modelValue: string; python: boolean; disabled?: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [string] }>();
const host = ref<HTMLElement>();
const syntax = new Compartment(), editable = new Compartment();
let view: EditorView | undefined;
onMounted(() => {
  view = new EditorView({ parent: host.value, state: EditorState.create({ doc: props.modelValue, extensions: [basicSetup, oneDark, syntax.of(props.python ? python() : cpp()), editable.of(EditorState.readOnly.of(Boolean(props.disabled))), EditorView.contentAttributes.of({ 'aria-label': '在线代码编辑器' }), EditorView.updateListener.of(update => { if (update.docChanged) emit('update:modelValue', update.state.doc.toString()); }), EditorView.theme({ '&': { fontSize: '14px' }, '.cm-scroller': { minHeight: '360px', maxHeight: '640px', overflow: 'auto', fontFamily: 'Menlo, Consolas, monospace' } })] }) });
});
watch(() => props.python, value => view?.dispatch({ effects: syntax.reconfigure(value ? python() : cpp()) }));
watch(() => props.disabled, value => view?.dispatch({ effects: editable.reconfigure(EditorState.readOnly.of(Boolean(value))) }));
watch(() => props.modelValue, value => { if (view && view.state.doc.toString() !== value) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } }); });
onBeforeUnmount(() => view?.destroy());
</script>
<template><div ref="host" class="code-editor" /></template>
<style scoped>.code-editor { overflow: hidden; border-radius: 6px; margin-bottom: 16px; text-align: left; }</style>
