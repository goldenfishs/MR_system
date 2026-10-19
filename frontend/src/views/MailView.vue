<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useDisplay } from "vuetify";
import { api } from "../lib/api";
import {
  dateTime,
  type MailCampaign,
  type UserWithProfile,
} from "../lib/platform";
const { smAndDown } = useDisplay();
const campaigns = ref<MailCampaign[]>([]),
  users = ref<UserWithProfile[]>([]),
  loading = ref(true),
  configured = ref(false),
  error = ref(""),
  notice = ref(""),
  saving = ref(false),
  compose = ref(false),
  selected = ref<MailCampaign | null>(null),
  detailError = ref(""),
  confirmSend = ref(false),
  filter = ref("");
const form = reactive({ subject: "", body: "", userIds: [] as string[] });
const matchingUsers = computed(() =>
  users.value.filter((u) =>
    `${u.name} ${u.email} ${u.direction}`
      .toLowerCase()
      .includes(filter.value.toLowerCase()),
  ),
);
const statusLabel = (value: string) =>
  ({
    draft: "草稿",
    sending: "发送中",
    completed: "发送完成",
    partial: "部分失败",
    pending: "待发送",
    sent: "已送出",
    failed: "发送失败",
    unknown: "状态待核实",
  })[value] || value;
const count = (status: string) =>
  selected.value?.recipients.filter((r) => r.status === status).length || 0;
let timer: ReturnType<typeof setTimeout> | undefined;
let pollToken = 0;
function stopPolling() {
  clearTimeout(timer);
  pollToken++;
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [c, u] = await Promise.all([
      api.get<{ items: MailCampaign[]; configured: boolean }>(
        "/mail-campaigns",
      ),
      api.get<{ items: UserWithProfile[] }>("/users?permission=mail"),
    ]);
    campaigns.value = c.items;
    configured.value = c.configured;
    users.value = u.items;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
function open(c: MailCampaign) {
  selected.value = c;
  detailError.value = "";
  if (c.status === "sending") poll();
}
async function refreshDetail() {
  if (!selected.value) return;
  const id = selected.value.id;
  const { item } = await api.get<{ item: MailCampaign }>(
    `/mail-campaigns/${id}`,
  );
  if (selected.value?.id === id) {
    detailError.value = "";
    selected.value = item;
    campaigns.value = campaigns.value.map((c) => (c.id === id ? item : c));
  }
  return item;
}
function poll() {
  stopPolling();
  const token = pollToken;
  const tick = async () => {
    try {
      const item = await refreshDetail();
      if (token === pollToken && item?.status === "sending")
        timer = setTimeout(tick, 2000);
    } catch (e) {
      if (token === pollToken) detailError.value = (e as Error).message;
    }
  };
  timer = setTimeout(tick, 1500);
}
watch(
  () => selected.value?.id,
  () => {
    stopPolling();
    if (selected.value?.status === "sending") poll();
  },
);
onUnmounted(stopPolling);
function create() {
  Object.assign(form, { subject: "", body: "", userIds: [] });
  filter.value = "";
  compose.value = true;
}
function selectMatching() {
  form.userIds = [
    ...new Set([...form.userIds, ...matchingUsers.value.map((u) => u.id)]),
  ];
}
async function draft() {
  if (!form.subject.trim() || !form.body.trim() || !form.userIds.length) {
    notice.value = "请填写主题、正文，并选择至少一位收件人。";
    return;
  }
  saving.value = true;
  try {
    const { item } = await api.post<{ item: MailCampaign }>("/mail-campaigns", {
      ...form,
    });
    campaigns.value = [item, ...campaigns.value];
    compose.value = false;
    open(item);
    notice.value = "草稿已保存，请核对收件人后确认发送。";
  } catch (e) {
    notice.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
async function send(retry = false) {
  if (!selected.value) return;
  saving.value = true;
  detailError.value = "";
  try {
    const { item } = await api.post<{ item: MailCampaign }>(
      `/mail-campaigns/${selected.value.id}/${retry ? "retry" : "send"}`,
    );
    detailError.value = "";
    selected.value = item;
    campaigns.value = campaigns.value.map((c) => (c.id === item.id ? item : c));
    confirmSend.value = false;
    poll();
  } catch (e) {
    detailError.value = (e as Error).message;
    confirmSend.value = false;
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <div class="mail-heading">
      <div>
        <h1 class="page-title">邮件管理</h1>
        <p class="page-subtitle">
          选择系统用户、预览邮件，再逐人发送并记录结果。
        </p>
      </div>
      <v-btn color="primary" prepend-icon="mdi-pencil-outline" @click="create"
        >撰写邮件</v-btn
      >
    </div>
    <v-alert
      v-if="!loading && !configured && !error"
      type="info"
      variant="tonal"
      class="mb-5"
      >邮件服务尚未配置，可以先保存草稿。配置 SMTP 后才能发送。</v-alert
    ><v-alert v-if="error" type="error" variant="tonal" class="mb-4"
      >{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert
    ><v-progress-linear v-if="loading" indeterminate /><v-card v-else
      ><v-list v-if="campaigns.length" lines="two"
        ><v-list-item
          v-for="c in campaigns"
          :key="c.id"
          :title="c.subject"
          :subtitle="`${c.recipients.length} 位收件人 · ${dateTime(c.createdAt)}`"
          prepend-icon="mdi-email-outline"
          @click="open(c)"
          ><template #append
            ><v-chip
              size="small"
              :color="
                c.status === 'completed'
                  ? 'success'
                  : c.status === 'partial'
                    ? 'warning'
                    : 'primary'
              "
              >{{ statusLabel(c.status) }}</v-chip
            ></template
          ></v-list-item
        ></v-list
      >
      <div v-else class="pa-10 text-center text-medium-emphasis">
        还没有邮件记录，撰写第一封团队通知。
      </div></v-card
    >
    <v-dialog
      v-model="compose"
      max-width="800"
      :fullscreen="smAndDown"
      scrollable
      :persistent="saving"
      ><v-card
        ><v-card-title class="d-flex align-center"
          >撰写邮件<v-spacer /><v-btn
            icon="mdi-close"
            aria-label="关闭邮件草稿"
            variant="text"
            :disabled="saving"
            @click="compose = false" /></v-card-title
        ><v-card-text
          ><form @submit.prevent="draft">
            <v-text-field
              v-model="form.subject"
              label="邮件主题 *"
              maxlength="200"
              required
            /><v-textarea
              v-model="form.body"
              label="邮件正文（纯文本）*"
              rows="7"
              maxlength="20000"
              counter
              required
            /><v-autocomplete
              v-model="form.userIds"
              :items="
                users.map((u) => ({
                  title: `${u.name} · ${u.email}`,
                  value: u.id,
                }))
              "
              multiple
              chips
              closable-chips
              label="收件人 *"
              aria-required="true"
            />
            <div class="recipient-tools">
              <v-text-field
                v-model="filter"
                label="按姓名、邮箱、方向筛选"
                density="compact"
                hide-details
              /><v-btn variant="text" color="primary" @click="selectMatching"
                >选择匹配的 {{ matchingUsers.length }} 人</v-btn
              >
            </div>
            <p class="text-caption text-medium-emphasis my-4">
              已选
              {{ form.userIds.length }}
              人。每位用户单独收信，不会公开其他收件人的邮箱。
            </p>
            <div class="d-flex justify-end">
              <v-btn color="primary" type="submit" :loading="saving"
                >保存并预览</v-btn
              >
            </div>
          </form></v-card-text
        ></v-card
      ></v-dialog
    >
    <v-dialog
      :model-value="!!selected"
      max-width="850"
      :fullscreen="smAndDown"
      scrollable
      :persistent="saving"
      @update:model-value="!$event && (selected = null)"
      ><v-card v-if="selected"
        ><v-card-title class="d-flex align-center"
          >邮件预览与发送记录<v-spacer /><v-btn
            icon="mdi-close"
            aria-label="关闭邮件预览"
            variant="text"
            :disabled="saving"
            @click="selected = null" /></v-card-title
        ><v-card-text
          ><v-alert v-if="detailError" type="error" variant="tonal" class="mb-4"
            >{{ detailError
            }}<v-btn variant="text" @click="poll">重试</v-btn></v-alert
          >
          <div class="d-flex flex-wrap ga-2 mb-4">
            <v-chip size="small" color="primary">{{
              statusLabel(selected.status)
            }}</v-chip
            ><v-chip size="small"
              >{{ selected.recipients.length }} 位收件人</v-chip
            ><v-chip v-if="count('sent')" size="small" color="success"
              >已送出 {{ count("sent") }}</v-chip
            ><v-chip v-if="count('failed')" size="small" color="error"
              >失败 {{ count("failed") }}</v-chip
            >
          </div>
          <h2 class="mail-subject">{{ selected.subject }}</h2>
          <div class="mail-body">{{ selected.body }}</div>
          <h3 class="text-subtitle-1 mb-2">收件人清单</h3>
          <v-list class="recipient-list" lines="two"
            ><v-list-item
              v-for="r in selected.recipients"
              :key="r.userId"
              :title="r.name || r.userName || r.email"
              :subtitle="`${r.email}${r.error ? ' · ' + r.error : ''}`"
              ><template #append
                ><v-chip
                  size="x-small"
                  :color="
                    r.status === 'sent'
                      ? 'success'
                      : r.status === 'failed'
                        ? 'error'
                        : 'grey'
                  "
                  >{{ statusLabel(r.status) }}</v-chip
                ></template
              ></v-list-item
            ></v-list
          ><v-alert
            v-if="count('unknown')"
            type="warning"
            variant="tonal"
            class="mt-4"
            >有邮件发送状态不明确，需要管理员核查邮箱服务记录；系统不会自动重发这部分邮件。</v-alert
          ><v-progress-linear
            v-if="selected.status === 'sending'"
            indeterminate
            class="mt-4" /></v-card-text
        ><v-card-actions class="px-6 pb-5"
          ><span class="text-caption text-medium-emphasis">{{
            configured ? "邮件发出后无法撤回。" : "尚未配置 SMTP。"
          }}</span
          ><v-spacer /><v-btn
            v-if="selected.status === 'draft'"
            color="primary"
            variant="flat"
            :disabled="!configured"
            @click="confirmSend = true"
            >确认收件人并发送</v-btn
          ><v-btn
            v-if="selected.status !== 'sending' && count('failed')"
            color="primary"
            variant="tonal"
            :disabled="!configured"
            :loading="saving"
            @click="send(true)"
            >重试失败的 {{ count("failed") }} 封</v-btn
          ></v-card-actions
        ></v-card
      ></v-dialog
    >
    <v-dialog v-model="confirmSend" max-width="460" :persistent="saving"
      ><v-card v-if="selected"
        ><v-card-title>发送邮件</v-card-title
        ><v-card-text
          >将以已配置的发件邮箱向
          {{ selected.recipients.length }} 位用户发送「{{
            selected.subject
          }}」。</v-card-text
        ><v-card-actions
          ><v-spacer /><v-btn :disabled="saving" @click="confirmSend = false"
            >返回检查</v-btn
          ><v-btn color="primary" :loading="saving" @click="send()"
            >立即发送</v-btn
          ></v-card-actions
        ></v-card
      ></v-dialog
    ><v-snackbar
      :model-value="!!notice"
      @update:model-value="!$event && (notice = '')"
      >{{ notice }}</v-snackbar
    >
  </div>
</template>
<style scoped>
.mail-heading {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}
.recipient-tools {
  display: flex;
  gap: 12px;
  align-items: center;
}
.recipient-tools > .v-input {
  min-width: 0;
}
.mail-subject {
  font-size: 21px;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.mail-body {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.8;
  padding: 20px;
  background: #f7f8fa;
  border: 1px solid #eee;
  border-radius: 4px;
  margin: 18px 0 24px;
  font-size: 14px;
}
.recipient-list {
  max-height: 300px;
  overflow: auto;
  border: 1px solid #eee;
  border-radius: 4px;
}
@media (max-width: 650px) {
  .mail-heading {
    flex-wrap: wrap;
    margin-bottom: 18px;
  }
  .recipient-tools {
    flex-wrap: wrap;
  }
  .recipient-tools > .v-input {
    flex-basis: 100%;
  }
}
</style>
