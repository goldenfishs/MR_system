<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../lib/api";
import type { JudgeConfig, JudgeService } from "../lib/platform";
const props = defineProps<{ modelValue: JudgeConfig }>();
const emit = defineEmits<{ "update:modelValue": [JudgeConfig] }>();
const service = ref<JudgeService | null>(null),
  error = ref("");
function update<K extends keyof JudgeConfig>(key: K, value: JudgeConfig[K]) {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
function changeCase(
  index: number,
  key: "input" | "expectedOutput" | "hidden",
  value: string | boolean,
) {
  const testCases = props.modelValue.testCases.map((c, i) =>
    i === index ? { ...c, [key]: value } : c,
  );
  update("testCases", testCases);
}
async function load() {
  error.value = "";
  try {
    service.value = await api.get<JudgeService>("/judge/config");
  } catch (e) {
    error.value = (e as Error).message;
  }
}
onMounted(load);
</script>
<template>
  <section class="judge-fields">
    <v-switch
      :model-value="modelValue.enabled"
      @update:model-value="update('enabled', !!$event)"
      label="启用代码自动评测"
      :disabled="!service?.configured && !modelValue.enabled"
      color="primary"
      hide-details
    /><v-alert v-if="error" type="error" variant="tonal" density="compact"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    ><v-alert
      v-if="service && !service.configured"
      type="info"
      variant="tonal"
      density="compact"
      class="mt-3"
      >配置编译服务并加载语言列表后，才能设置自动评测。当前可以发布代码文件任务，由管理员人工评分。</v-alert
    ><template v-if="modelValue.enabled"
      ><v-select
        :model-value="modelValue.languageIds"
        @update:model-value="update('languageIds', $event)"
        :items="service?.languages || []"
        item-title="name"
        item-value="id"
        multiple
        chips
        label="允许的编程语言 *"
        class="mt-4"
      /><v-text-field
        :model-value="modelValue.timeLimitSeconds"
        @update:model-value="update('timeLimitSeconds', Number($event))"
        type="number"
        min="0.1"
        max="5"
        step="0.1"
        label="每个用例运行时限（秒）"
      />
      <div
        v-for="(item, index) in modelValue.testCases"
        :key="index"
        class="test-case"
      >
        <div class="d-flex align-center">
          <strong>测试用例 {{ index + 1 }}</strong
          ><v-spacer /><v-btn
            icon="mdi-close"
            :aria-label="`删除测试用例 ${index + 1}`"
            variant="text"
            size="small"
            @click="
              update(
                'testCases',
                modelValue.testCases.filter((_, i) => i !== index),
              )
            "
          />
        </div>
        <v-textarea
          :model-value="item.input"
          @update:model-value="changeCase(index, 'input', $event)"
          label="标准输入"
          rows="2"
        /><v-textarea
          :model-value="item.expectedOutput"
          @update:model-value="changeCase(index, 'expectedOutput', $event)"
          label="预期标准输出"
          rows="2"
        /><v-checkbox
          :model-value="item.hidden"
          @update:model-value="changeCase(index, 'hidden', !!$event)"
          label="隐藏用例（成员只看到通过状态）"
          hide-details
          density="compact"
        />
      </div>
      <v-btn
        :disabled="modelValue.testCases.length >= 10"
        variant="text"
        color="primary"
        prepend-icon="mdi-plus"
        @click="
          update('testCases', [
            ...modelValue.testCases,
            { input: '', expectedOutput: '', hidden: false },
          ])
        "
        >添加测试用例</v-btn
      ></template
    >
  </section>
</template>
<style scoped>
.judge-fields {
  border: 1px solid #e0e6ec;
  border-radius: 5px;
  padding: 16px;
  margin: 18px 0;
}
.test-case {
  background: #f7f9fb;
  padding: 16px;
  margin: 12px 0;
  border-radius: 4px;
}
.test-case strong {
  font-size: 14px;
  font-weight: 500;
}
</style>
