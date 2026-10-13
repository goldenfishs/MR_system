<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { api } from "../lib/api";
import { auth, roleNames } from "../lib/auth";
import { dateTime } from "../lib/format";
import { groupOptions, groupLabel, memberTypeNames, permissionOptions } from "../lib/permissions";
import { refreshLearningAccess } from "../lib/learning-access";
import type { Direction, GroupGrant, MemberType, Role, Settings, User } from "../lib/types";
type Member = User & { teams: { id: string; name: string; direction: string }[] };
const members = ref<Member[]>([]), brand = ref<Settings | null>(null);
const loading = ref(true), saving = ref(false), error = ref(""), formError = ref("");
const search = ref(""), direction = ref(""), dialog = ref(false), notice = ref(""), editing = ref<Member | null>(null);
const originalDirection = ref("");
const isAdmin = computed(() => auth.user?.role === "admin");
const draft = reactive({ name: "", email: "", password: "", role: "freshman" as Role, memberType: "freshman" as MemberType, direction: "", groupGrants: [] as GroupGrant[], assessmentPassed: false, assessmentNote: "" });
const needsAssessmentConfirmation = computed(() => Boolean(editing.value && (editing.value.memberType || (editing.value.role === "freshman" ? "freshman" : "")) === "freshman" && draft.memberType !== "freshman"));
const roleOptions = Object.entries(roleNames).map(([value, title]) => ({ value, title }));
const typeOptions = Object.entries(memberTypeNames).map(([value, title]) => ({ value, title }));
const filtered = computed(() => members.value.filter(u => (!direction.value || u.direction === direction.value || u.teams?.some(t => t.direction === direction.value) || u.groupGrants?.some(g => g.direction === direction.value)) && `${u.name} ${u.email}`.toLowerCase().includes(search.value.toLowerCase())));
const teamName = computed(() => [brand.value?.teamName || "沧溟", brand.value?.secondaryTeamName || "MOVE"].join(" / "));
async function load() {
  loading.value = true; error.value = "";
  try {
    const [directory, settings] = await Promise.all([api.get<{ items: Member[] }>("/members"), api.get<{ settings: Settings }>("/settings")]);
    members.value = directory.items || []; brand.value = settings.settings;
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function openEditor(member?: Member) {
  editing.value = member || null;
  originalDirection.value = member?.teams?.[0]?.direction || member?.direction || "";
  Object.assign(draft, { assessmentPassed: false, assessmentNote: "", name: member?.name || "", email: member?.email || "", password: "", role: member?.role || "freshman", memberType: member?.memberType || (member?.role === "member" ? "formal" : member?.role === "reserve" ? "reserve" : "freshman"), direction: originalDirection.value, groupGrants: (member?.groupGrants || []).map(grant => ({ direction: grant.direction, permissions: [...grant.permissions] })) });
  formError.value = ""; dialog.value = true;
}
function changeRole(value: Role) {
  draft.role = value;
  if (["member", "reserve", "freshman"].includes(value)) draft.memberType = value === "member" ? "formal" : value === "reserve" ? "reserve" : "freshman";
}
const grantFor = (value: Direction) => draft.groupGrants.find(g => g.direction === value);
function selectGroup(value: Direction, active: unknown) {
  if (active && !grantFor(value)) draft.groupGrants.push({ direction: value, permissions: permissionOptions.map(p => p.value) });
  if (!active) draft.groupGrants = draft.groupGrants.filter(g => g.direction !== value);
}
async function save() {
  if (!isAdmin.value || saving.value) return;
  if (!editing.value && (!draft.name.trim() || !draft.email.trim() || draft.password.length < 12)) { formError.value = "请填写姓名、邮箱和至少 12 位的初始密码。"; return; }
  if (draft.role === "group_admin" && !draft.groupGrants.length) { formError.value = "请至少分配一个管理组别。"; return; }
  if (draft.role === "group_admin" && draft.groupGrants.some(g => !g.permissions.length)) { formError.value = "每个管理组别至少选择一项权限，或取消该组。"; return; }
  if (needsAssessmentConfirmation.value && !draft.assessmentPassed) { formError.value = "请确认该成员已通过最终新生考核后再晋升。"; return; }
  const body = { role: draft.role, memberType: draft.memberType, ...(!editing.value || draft.direction !== originalDirection.value ? { direction: draft.direction } : {}), groupGrants: draft.role === "group_admin" ? draft.groupGrants : [] };
  saving.value = true; formError.value = "";
  try {
    if (editing.value) await api.patch(`/members/${editing.value.id}`, { ...body, assessmentPassed: needsAssessmentConfirmation.value && draft.assessmentPassed, assessmentNote: draft.assessmentNote });
    else await api.post("/members", { ...body, name: draft.name, email: draft.email, password: draft.password });
    notice.value = editing.value ? "身份与分组权限已更新。" : "账号已创建。";
    dialog.value = false; draft.password = "";
    await refreshLearningAccess(); await load();
  } catch (e) { formError.value = (e as Error).message; }
  finally { saving.value = false; }
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <div class="d-flex justify-space-between align-start ga-3 mb-5">
      <div><h1 class="page-title">战队成员</h1><p class="page-subtitle">{{ isAdmin ? '管理账号身份，为各组分别分配权限。' : '查看授权组别的成员。' }}</p></div>
      <v-btn v-if="isAdmin" color="primary" prepend-icon="mdi-account-plus-outline" @click="openEditor()">创建账号</v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <v-card class="mb-6"><div class="member-intro"><div class="intro-icon"><v-icon size="28">mdi-account-group-outline</v-icon></div><div><h2>成员与身份</h2><p>{{ isAdmin ? '包括新生、梯队队员、正式队员和管理员' : '仅展示已授权组别的成员' }}，共 {{ members.length }} 人</p></div></div></v-card>
    <div class="filters"><v-text-field v-model="search" placeholder="搜索姓名或邮箱" prepend-inner-icon="mdi-magnify" hide-details clearable @click:clear="search = ''" /><v-select v-model="direction" :items="[{ title: '全部组别', value: '' }, ...groupOptions]" label="组别" hide-details /></div>
    <v-progress-linear v-if="loading" indeterminate color="primary" class="my-6" />
    <div v-else-if="!filtered.length" class="empty-state"><v-icon size="46">mdi-account-search-outline</v-icon><p>没有找到符合条件的成员</p></div>
    <div v-else class="members-grid"><v-card v-for="u in filtered" :key="u.id" class="member-card">
      <div class="member-top"><div class="member-avatar">{{ u.name.slice(-2) }}</div><v-chip size="x-small" color="primary" variant="tonal">{{ roleNames[u.role] }}</v-chip></div>
      <h3>{{ u.name }}</h3><p class="member-email">{{ u.email }}</p>
      <p v-if="u.assessmentPassedAt" class="text-caption mt-2">已通过新生考核 · {{ dateTime(u.assessmentPassedAt) }}</p>
      <p v-if="u.role === 'group_admin' && u.memberType" class="text-caption mt-2">成员身份：{{ memberTypeNames[u.memberType] }}</p>
      <div class="d-flex flex-wrap ga-2 mt-4"><template v-if="u.groupGrants?.length"><v-chip v-for="grant in u.groupGrants" :key="grant.direction" size="small" variant="tonal" color="primary">{{ groupLabel(grant.direction) }}管理 · {{ grant.permissions.length }} 项权限</v-chip></template><template v-else-if="u.teams?.length"><v-chip v-for="membership in u.teams" :key="membership.id" size="small" variant="tonal" color="primary">{{ membership.name }} · {{ groupLabel(membership.direction) }}</v-chip></template><template v-else><v-chip size="small" variant="outlined">{{ teamName }}</v-chip><v-chip v-if="u.direction" size="small">{{ groupLabel(u.direction) }}</v-chip></template></div>
      <div class="joined"><v-icon size="13">mdi-calendar-check-outline</v-icon>{{ dateTime(u.joinedAt) }} 注册</div>
      <v-btn v-if="isAdmin" class="mt-4" color="primary" variant="tonal" size="small" prepend-icon="mdi-account-key-outline" @click="openEditor(u)">身份与权限</v-btn>
    </v-card></div>
    <v-dialog v-model="dialog" max-width="820" scrollable :persistent="saving"><v-card>
      <v-card-title class="d-flex align-center pa-6 pb-2">{{ editing ? `${editing.name} · 身份与权限` : '创建战队账号' }}<v-spacer /><v-btn icon="mdi-close" variant="text" aria-label="关闭身份设置" :disabled="saving" @click="dialog = false" /></v-card-title>
      <v-card-text>
        <v-alert v-if="formError" type="error" variant="tonal" class="mb-4">{{ formError }}</v-alert>
        <template v-if="!editing"><v-text-field v-model="draft.name" label="姓名" maxlength="60" /><v-text-field v-model="draft.email" label="邮箱" type="email" /><v-text-field v-model="draft.password" label="初始密码（至少 12 位）" type="password" autocomplete="new-password" /></template>
        <v-row><v-col cols="12" sm="6"><v-select :model-value="draft.role" :items="roleOptions" label="账号身份" @update:model-value="changeRole" /></v-col><v-col cols="12" sm="6"><v-select v-model="draft.direction" :items="[{ title: '未指定', value: '' }, ...groupOptions]" label="所属组别" /></v-col></v-row>
        <v-select v-if="draft.role === 'group_admin' || draft.role === 'admin'" v-model="draft.memberType" :items="typeOptions" label="成员身份" />
        <div v-if="needsAssessmentConfirmation" class="mb-4"><p class="text-caption text-medium-emphasis mb-2">晋升成员身份需要确认最终考核结果，授予管理员权限也需确认。已有培养周期的成员请前往<v-btn to="/training" size="small" variant="text" color="primary">新生考核</v-btn>逐阶段完成晋升；此处用于补记尚未建立培养周期的新生考核。</p><v-checkbox v-model="draft.assessmentPassed" label="已通过最终新生考核，确认晋升成员身份" hide-details color="primary" /><v-textarea v-model="draft.assessmentNote" label="考核说明（可选）" rows="2" maxlength="2000" /></div>
        <v-alert v-if="draft.role === 'admin'" type="info" variant="tonal" class="mb-4">管理员拥有全系统权限，包括账号授权、团队发布和空间设置。</v-alert>
        <template v-if="draft.role === 'group_admin'">
          <p class="text-body-2 text-medium-emphasis mb-4">可兼任多个组管理员。每个组分别勾选权限；勾选组别时默认开放该组全部管理模块，可按需取消。</p>
          <div v-for="group in groupOptions" :key="group.value" class="grant-card">
            <v-checkbox :model-value="Boolean(grantFor(group.value))" :label="group.title" hide-details color="primary" @update:model-value="selectGroup(group.value, $event)" />
            <div v-if="grantFor(group.value)" class="permission-grid"><v-checkbox v-for="permission in permissionOptions" :key="permission.value" v-model="grantFor(group.value)!.permissions" :value="permission.value" :label="permission.title" hide-details density="compact" color="primary" /></div>
          </div>
          <p class="text-caption text-medium-emphasis mt-4">组管理员仅管理授权组别和模块；获得“新生阶段考核”权限后，可在新生考核页确认同组阶段结果与最终晋升。账号授权、账号创建、团队发布及全局设置由管理员负责。</p>
        </template>
        <p v-else-if="draft.role !== 'admin'" class="text-body-2 text-medium-emphasis">报名通过后开放各组学习内容，身份仍为新生；各阶段考核在新生考核页依次确认，最终通过后晋升为梯队或正式队员。</p>
      </v-card-text>
      <v-card-actions class="px-6 pb-6"><v-spacer /><v-btn :disabled="saving" @click="dialog = false; draft.password = ''">取消</v-btn><v-btn color="primary" variant="flat" :loading="saving" @click="save">{{ editing ? '保存身份与权限' : '创建账号' }}</v-btn></v-card-actions>
    </v-card></v-dialog>
    <v-snackbar :model-value="Boolean(notice)" @update:model-value="!$event && (notice = '')">{{ notice }}</v-snackbar>
  </div>
</template>
<style scoped>
.dot {
  color: #7eb6a4;
  margin-left: 3px;
}
.member-intro {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 27px;
}
.intro-icon {
  width: 55px;
  height: 55px;
  border-radius: 50%;
  background: #edf2fa;
  color: #1976d2;
  display: grid;
  place-items: center;
}
.member-intro h2 {
  font-size: 17px;
  margin: 0 0 7px;
}
.member-intro p {
  font-size: 12px;
  margin: 0;
  color: #757575;
}
.member-intro .eyebrow {
  font-size: 8px;
  margin-left: auto;
  color: #a9b3c8;
}
.filters {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 16px;
  margin-bottom: 24px;
}
.members-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
}
.member-card {
  padding: 24px;
}
.member-top {
  display: flex;
  justify-content: space-between;
  align-items: start;
}
.member-avatar {
  width: 49px;
  height: 49px;
  background: #edf0fb;
  border-radius: 50%;
  color: #1976d2;
  font-size: 17px;
  display: grid;
  place-items: center;
}
.member-card h3 {
  margin: 18px 0 7px;
  font-size: 17px;
}
.member-email {
  font-size: 12px;
  color: #757575;
  word-break: break-all;
}
.joined {
  border-top: 1px solid #eef0f6;
  margin-top: 20px;
  padding-top: 16px;
  font-size: 12px;
  color: #757575;
  display: flex;
  align-items: center;
  gap: 7px;
}
@media (max-width: 1200px) {
  .members-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 600px) {
  .members-grid {
    grid-template-columns: 1fr;
  }
  .filters {
    grid-template-columns: 1fr 1fr;
  }
  .filters > *:first-child {
    grid-column: 1/-1;
  }
  .member-intro {
    padding: 20px;
  }
  .member-intro h2 {
    font-size: 14px;
  }
  .page-content > .d-flex {
    flex-wrap: wrap;
    margin-bottom: 15px;
  }
}
</style>

<style scoped>
.grant-card { border: 1px solid #e5e7eb; border-radius: 6px; padding: 5px 14px 14px; margin-top: 12px; }
.permission-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 3px 14px; margin-left: 12px; }
@media (max-width: 600px) { .permission-grid { grid-template-columns: 1fr; } }
</style>
