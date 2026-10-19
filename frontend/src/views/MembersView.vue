<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { api } from "../lib/api";
import { auth, roleNames } from "../lib/auth";
import { dateTime } from "../lib/format";
import { groupOptions, groupLabel, memberTypeNames, permissionOptions } from "../lib/permissions";
import { refreshLearningAccess } from "../lib/learning-access";
import type { Direction, GroupGrant, MemberType, Role, Settings, User } from "../lib/types";
type Member = User & { directions?: Direction[]; teams: { id: string; name: string; direction: string }[] };
const router = useRouter();
const members = ref<Member[]>([]), brand = ref<Settings | null>(null);
const loading = ref(true), saving = ref(false), error = ref(""), formError = ref("");
const accountStatus = ref("all");
const passwordTarget = ref<Member | null>(null), passwordDraft = ref(""), passwordConfirm = ref(""), currentPassword = ref(""), passwordError = ref(""), passwordBusy = ref(false);
function closePasswordEditor() {
  if (passwordBusy.value) return;
  passwordTarget.value = null; passwordDraft.value = ""; passwordConfirm.value = ""; currentPassword.value = ""; passwordError.value = "";
}
function openPasswordEditor(member: Member) {
  if (!isAdmin.value || passwordBusy.value) return;
  closePasswordEditor(); passwordTarget.value = member;
}
async function saveMemberPassword() {
  const target = passwordTarget.value;
  if (!isAdmin.value || !target || passwordBusy.value) return;
  if (Array.from(passwordDraft.value).length < 6) { passwordError.value = "密码至少需要 6 个字符。"; return; }
  if (passwordDraft.value !== passwordConfirm.value) { passwordError.value = "两次输入的密码不一致。"; return; }
  passwordBusy.value = true; passwordError.value = "";
  try {
    await api.put(`/members/${encodeURIComponent(target.id)}/password`, { password: passwordDraft.value, ...(target.id === auth.user?.id ? { currentPassword: currentPassword.value } : {}) });
    notice.value = `${target.name || target.email} 的密码已更新，原有登录会话已退出。`;
    passwordTarget.value = null; passwordDraft.value = ""; passwordConfirm.value = ""; currentPassword.value = "";
    if (target.id === auth.user?.id) { auth.user = null; await router.push({ path: "/login", query: { passwordChanged: "1" } }); }
  } catch (e) { passwordError.value = (e as Error).message; }
  finally { passwordBusy.value = false; }
}
const accountTarget = ref<Member | null>(null), accountReason = ref(""), accountError = ref(""), accountBusy = ref(false);
const search = ref(""), direction = ref(""), dialog = ref(false), notice = ref(""), editing = ref<Member | null>(null);
const isAdmin = computed(() => auth.user?.role === "admin");
const draft = reactive({ name: "", email: "", password: "", role: "freshman" as Role, memberType: "freshman" as MemberType, directions: [] as Direction[], groupGrants: [] as GroupGrant[], assessmentNote: "" });
const currentMemberType = computed<MemberType>(() => editing.value?.memberType || (editing.value?.role === "member" ? "formal" : editing.value?.role === "reserve" ? "reserve" : "freshman"));
const needsPromotionOverride = computed(() => Boolean(editing.value && (currentMemberType.value === "freshman" && draft.memberType !== "freshman" || currentMemberType.value === "reserve" && draft.memberType === "formal")));
const roleOptions = Object.entries(roleNames).map(([value, title]) => ({ value, title }));
const typeOptions = Object.entries(memberTypeNames).map(([value, title]) => ({ value, title }));
const activeAdminCount = computed(() => members.value.filter(member => member.role === "admin" && !member.deactivatedAt).length);
const activeCount = computed(() => members.value.filter(member => !member.deactivatedAt).length);
const filtered = computed(() => members.value.filter(u => (accountStatus.value === "all" || (accountStatus.value === "active" ? !u.deactivatedAt : Boolean(u.deactivatedAt))) && (!direction.value || assignedDirections(u).some(value => value === direction.value) || u.groupGrants?.some(g => g.direction === direction.value)) && `${u.name} ${u.email}`.toLowerCase().includes((search.value || "").toLowerCase())));
const teamName = computed(() => [brand.value?.teamName || "沧溟", brand.value?.secondaryTeamName || "MOVE"].join(" / "));
async function load() {
  loading.value = true; error.value = "";
  try {
    const [directory, settings] = await Promise.all([api.get<{ items: Member[] }>("/members"), api.get<{ settings: Settings }>("/settings")]);
    members.value = directory.items || []; brand.value = settings.settings;
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function assignedDirections(member?: Member): Direction[] {
  if (member?.directions !== undefined) return [...new Set(member.directions)];
  const directions = [...new Set((member?.teams?.map(team => team.direction as Direction) || []).filter(Boolean))];
  if (!directions.length && member?.direction) directions.push(member.direction as Direction);
  return directions;
}
function openEditor(member?: Member) {
  if (member?.deactivatedAt) { notice.value = "请先恢复账号，再修改身份与权限。"; return; }
  editing.value = member || null;
  Object.assign(draft, { assessmentNote: "", name: member?.name || "", email: member?.email || "", password: "", role: member?.role || "freshman", memberType: member?.memberType || (member?.role === "member" ? "formal" : member?.role === "reserve" ? "reserve" : "freshman"), directions: assignedDirections(member), groupGrants: (member?.groupGrants || []).map(grant => ({ direction: grant.direction, permissions: [...grant.permissions] })) });
  formError.value = ""; dialog.value = true;
}
const groupTarget = ref<Member | null>(null), groupDraft = ref<Direction[]>([]), groupError = ref(""), groupSaving = ref(false);
function openGroups(member: Member) {
  if (!isAdmin.value || member.deactivatedAt) return;
  groupTarget.value = member; groupDraft.value = assignedDirections(member); groupError.value = "";
}
async function saveGroups() {
  if (!groupTarget.value || !isAdmin.value || groupSaving.value) return;
  groupSaving.value = true; groupError.value = "";
  try {
    await api.patch(`/members/${groupTarget.value.id}`, { directions: groupDraft.value });
    groupTarget.value = null; notice.value = "所属组别已更新，历史作业和考核记录已保留。";
    await refreshLearningAccess(); await load();
  } catch (e) { groupError.value = (e as Error).message; }
  finally { groupSaving.value = false; }
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
  if (!editing.value && (!draft.name.trim() || !draft.email.trim() || Array.from(draft.password).length < 6)) { formError.value = "请填写姓名、邮箱和至少 6 个字符的初始密码。"; return; }
  if (draft.role === "group_admin" && !draft.groupGrants.length) { formError.value = "请至少分配一个管理组别。"; return; }
  if (draft.role === "group_admin" && draft.groupGrants.some(g => !g.permissions.length)) { formError.value = "每个管理组别至少选择一项权限，或取消该组。"; return; }
  if (needsPromotionOverride.value && !draft.assessmentNote.trim()) { formError.value = "管理员直接晋升需要填写操作说明；这不会记录为通过考核。"; return; }
  const body = { role: draft.role, memberType: draft.memberType, directions: draft.directions, groupGrants: draft.role === "group_admin" ? draft.groupGrants : [] };
  saving.value = true; formError.value = "";
  try {
    if (editing.value) await api.patch(`/members/${editing.value.id}`, { ...body, forcePromotion: needsPromotionOverride.value, assessmentNote: needsPromotionOverride.value ? draft.assessmentNote.trim() : "" });
    else await api.post("/members", { ...body, name: draft.name, email: draft.email, password: draft.password });
    notice.value = editing.value ? "身份与分组权限已更新。" : "账号已创建。";
    dialog.value = false; draft.password = "";
    await refreshLearningAccess(); await load();
  } catch (e) { formError.value = (e as Error).message; }
  finally { saving.value = false; }
}
function deactivateBlocked(member: Member) {
  if (member.id === auth.user?.id) return "不能注销当前登录账号";
  if (member.role === "admin" && !member.deactivatedAt && activeAdminCount.value <= 1) return "不能注销最后一个有效管理员";
  return "";
}
function openAccountAction(member: Member) {
  if (!isAdmin.value || (!member.deactivatedAt && deactivateBlocked(member))) return;
  accountTarget.value = member;
  accountReason.value = "";
  accountError.value = "";
}
async function confirmAccountAction() {
  const target = accountTarget.value;
  if (!isAdmin.value || !target || accountBusy.value) return;
  const restoring = Boolean(target.deactivatedAt);
  if (!restoring && !accountReason.value.trim()) { accountError.value = "请填写注销原因。"; return; }
  if (!restoring && new TextEncoder().encode(accountReason.value.trim()).length > 2000) { accountError.value = "注销原因过长，请控制在 2000 字节以内（中文约 660 字）。"; return; }
  if (!restoring && deactivateBlocked(target)) { accountError.value = deactivateBlocked(target); return; }
  accountBusy.value = true;
  accountError.value = "";
  try {
    await api.post(`/users/${encodeURIComponent(target.id)}/${restoring ? "reactivate" : "deactivate"}`, restoring ? {} : { reason: accountReason.value.trim() });
    notice.value = restoring ? `${target.name} 的账号已恢复，可重新登录。` : `${target.name} 的账号已注销，历史记录已保留。`;
    accountTarget.value = null;
    await load();
  } catch (e) { accountError.value = (e as Error).message; }
  finally { accountBusy.value = false; }
}
onMounted(load);
</script>
<template>
  <div class="page-content">
    <div class="d-flex justify-space-between align-start ga-3 mb-5">
      <div><h1 class="page-title">{{ isAdmin ? '用户管理' : '组内成员' }}</h1><p class="page-subtitle">{{ isAdmin ? '管理账号身份，为成员选择一个或多个组别，并分配各组管理权限。' : '查看授权组别的成员。' }}</p></div>
      <v-btn v-if="isAdmin" color="primary" prepend-icon="mdi-account-plus-outline" @click="openEditor()">创建账号</v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <div class="member-counts"><strong>{{ teamName }}</strong><span>共 {{ members.length }} 个账号</span><span>启用 {{ activeCount }}</span><span>已注销 {{ members.length - activeCount }}</span></div>
    <div class="filters">
      <v-text-field v-model="search" placeholder="搜索姓名或邮箱" prepend-inner-icon="mdi-magnify" density="compact" variant="outlined" hide-details clearable @click:clear="search = ''" />
      <v-select v-model="direction" :items="[{ title: '全部组别', value: '' }, ...groupOptions]" label="组别" density="compact" variant="outlined" hide-details />
      <v-select v-model="accountStatus" :items="[{ title: '全部状态', value: 'all' }, { title: '启用', value: 'active' }, { title: '已注销', value: 'inactive' }]" label="账号状态" density="compact" variant="outlined" hide-details />
    </div>
    <v-progress-linear v-if="loading" indeterminate color="primary" class="my-6" />
    <div v-else-if="!filtered.length" class="empty-state"><v-icon size="46">mdi-account-search-outline</v-icon><p>没有找到符合条件的成员</p></div>
    <v-card v-else class="member-table-card" variant="outlined">
      <div class="member-table-heading"><strong>账号列表</strong><span>当前 {{ filtered.length }} 人</span></div>
      <div class="member-table-scroll" tabindex="0" aria-label="用户列表，可横向滚动">
        <table class="member-table">
          <thead><tr><th>姓名</th><th>邮箱</th><th>身份</th><th>所属组别</th><th>管理组别</th><th>注册时间</th><th>状态</th><th v-if="isAdmin">操作</th></tr></thead>
          <tbody>
            <tr v-for="u in filtered" :key="u.id" :class="{ 'account-deactivated': u.deactivatedAt }">
              <td class="member-name"><strong>{{ u.name || '未填写姓名' }}</strong><span v-if="u.id === auth.user?.id" class="self-label">当前账号</span></td>
              <td class="member-email">{{ u.email }}</td>
              <td><v-chip size="x-small" color="primary" variant="tonal">{{ roleNames[u.role] }}</v-chip><span v-if="['admin', 'group_admin'].includes(u.role) && u.memberType" class="cell-note">{{ memberTypeNames[u.memberType] }}</span><span v-if="u.promotionOverrideAt" class="cell-note" :title="[dateTime(u.promotionOverrideAt), u.promotionOverrideBy, u.promotionOverrideNote].filter(Boolean).join(' · ')">管理员直接晋升</span><span v-else-if="u.assessmentPassedAt" class="cell-note" :title="dateTime(u.assessmentPassedAt)">已通过新生考核</span></td>
              <td><div class="group-tags"><v-chip v-for="group in assignedDirections(u)" :key="group" size="x-small" variant="tonal" color="primary">{{ groupLabel(group) }}</v-chip><span v-if="!assignedDirections(u).length" class="cell-muted">未分组</span></div><v-btn v-if="isAdmin" variant="text" size="small" color="primary" prepend-icon="mdi-account-switch-outline" :disabled="Boolean(u.deactivatedAt)" @click="openGroups(u)">调整组别</v-btn></td>
              <td><span v-if="u.role === 'admin'" class="cell-muted">全系统</span><div v-else-if="u.groupGrants?.length" class="group-tags"><v-chip v-for="grant in u.groupGrants" :key="grant.direction" size="x-small" variant="outlined" color="primary" :title="`${grant.permissions.length} 项管理权限`">{{ groupLabel(grant.direction) }}</v-chip></div><span v-else class="cell-muted">—</span></td>
              <td class="member-date">{{ dateTime(u.joinedAt) }}</td>
              <td><v-chip size="x-small" :color="u.deactivatedAt ? 'grey' : 'success'" variant="tonal">{{ u.deactivatedAt ? '已注销' : '启用' }}</v-chip><span v-if="u.deactivatedAt" class="cell-note" :title="u.deactivationReason">{{ dateTime(u.deactivatedAt) }}</span></td>
              <td v-if="isAdmin"><div class="member-actions"><v-btn color="primary" variant="text" size="small" @click="openPasswordEditor(u)">设置密码</v-btn><v-btn color="primary" variant="text" size="small" :disabled="Boolean(u.deactivatedAt)" :title="u.deactivatedAt ? '请先恢复账号' : '编辑身份与组别权限'" @click="openEditor(u)">身份与权限</v-btn><v-btn v-if="u.deactivatedAt" color="success" variant="text" size="small" @click="openAccountAction(u)">恢复账号</v-btn><v-btn v-else color="error" variant="text" size="small" :disabled="Boolean(deactivateBlocked(u))" :title="deactivateBlocked(u) || '注销账号并保留历史记录'" @click="openAccountAction(u)">注销账号</v-btn></div></td>
            </tr>
          </tbody>
        </table>
      </div>
    </v-card>
    <v-dialog :model-value="Boolean(groupTarget)" max-width="560" :persistent="groupSaving" @update:model-value="!$event && (groupTarget = null)">
      <v-card v-if="groupTarget">
        <v-card-title class="pa-5 pb-2">调整所属组别 · {{ groupTarget.name || groupTarget.email }}</v-card-title>
        <v-card-text>
          <v-alert v-if="groupError" type="error" variant="tonal" class="mb-4">{{ groupError }}</v-alert>
          <p class="mb-3">勾选要加入或保留的组别，可同时加入多个组。</p>
          <v-checkbox v-for="group in groupOptions" :key="group.value" v-model="groupDraft" :value="group.value" :label="group.title" :disabled="groupSaving" color="primary" hide-details density="compact" />
          <v-alert type="info" variant="tonal" class="mt-4">取消勾选即退出该组，暂停该组作业提交与考核；历史记录保留，重新加入可继续原进度。新加入的组独立开始培养，不影响其他组。成员身份和管理权限保持不变。</v-alert>
          <p v-if="!groupDraft.length" class="text-warning mt-3">保存后该用户将不属于任何组。</p>
          <p class="text-caption text-medium-emphasis mt-3">已被考核淘汰的组仍需在报名审核中明确重新录取。</p>
        </v-card-text>
        <v-card-actions class="px-5 pb-5"><v-spacer /><v-btn :disabled="groupSaving" @click="groupTarget = null">取消</v-btn><v-btn color="primary" variant="flat" :loading="groupSaving" @click="saveGroups">保存组别</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="dialog" max-width="820" scrollable :persistent="saving"><v-card>
      <v-card-title class="d-flex align-center pa-6 pb-2">{{ editing ? `${editing.name} · 身份与权限` : '创建战队账号' }}<v-spacer /><v-btn icon="mdi-close" variant="text" aria-label="关闭身份设置" :disabled="saving" @click="dialog = false" /></v-card-title>
      <v-card-text>
        <v-alert v-if="formError" type="error" variant="tonal" class="mb-4">{{ formError }}</v-alert>
        <v-alert v-if="editing?.deactivatedAt" type="warning" variant="tonal" class="mb-4">账号已注销，无法登录。请先使用列表中的“恢复账号”，再修改身份。<p v-if="editing.deactivationReason" class="mt-2">注销原因：{{ editing.deactivationReason }}</p></v-alert>
        <div v-if="editing?.promotionOverrideAt" class="promotion-override-note mb-4"><strong>管理员直接晋升</strong><span>{{ dateTime(editing.promotionOverrideAt) }}<template v-if="editing.promotionOverrideBy"> · 操作者 {{ editing.promotionOverrideBy }}</template></span><p v-if="editing.promotionOverrideNote">{{ editing.promotionOverrideNote }}</p></div>
        <template v-if="!editing"><v-text-field v-model="draft.name" label="姓名" maxlength="60" /><v-text-field v-model="draft.email" label="邮箱" type="email" /><v-text-field v-model="draft.password" label="初始密码（至少 6 个字符）" type="password" autocomplete="new-password" /></template>
        <v-row><v-col cols="12" sm="6"><v-select :model-value="draft.role" :items="roleOptions" label="账号身份" @update:model-value="changeRole" /></v-col><v-col cols="12" sm="6"><v-select v-model="draft.directions" :items="groupOptions" label="所属组别（可多选）" multiple chips closable-chips /></v-col></v-row>
        <v-select v-if="draft.role === 'group_admin' || draft.role === 'admin'" v-model="draft.memberType" :items="typeOptions" label="成员身份" />
        <div v-if="needsPromotionOverride" class="mb-4"><v-alert type="info" variant="tonal" class="mb-3">直接晋升只调整成员身份，各组阶段考核记录继续保留，不会自动记作通过考核。</v-alert><v-textarea v-model="draft.assessmentNote" label="直接晋升说明（必填）" placeholder="例如：经队务会议确认，直接调整为正式队员。" rows="2" maxlength="2000" /></div>
        <v-alert v-if="draft.role === 'admin'" type="info" variant="tonal" class="mb-4">管理员拥有全系统权限，包括账号授权、团队发布和空间设置。</v-alert>
        <template v-if="draft.role === 'group_admin'">
          <p class="text-body-2 text-medium-emphasis mb-4">可兼任多个组管理员。每个组分别勾选权限；勾选组别时默认开放该组全部管理模块，可按需取消。</p>
          <div v-for="group in groupOptions" :key="group.value" class="grant-card">
            <v-checkbox :model-value="Boolean(grantFor(group.value))" :label="group.title" hide-details color="primary" @update:model-value="selectGroup(group.value, $event)" />
            <div v-if="grantFor(group.value)" class="permission-grid"><v-checkbox v-for="permission in permissionOptions" :key="permission.value" v-model="grantFor(group.value)!.permissions" :value="permission.value" :label="permission.title" hide-details density="compact" color="primary" /></div>
          </div>
          <p class="text-caption text-medium-emphasis mt-4">组管理员仅管理授权组别和模块；获得“新生阶段考核”权限后，可在新生考核页确认同组阶段结果与最终晋升。账号授权、账号创建、团队发布及全局设置由管理员负责。</p>
        </template>
        <p v-else-if="draft.role !== 'admin'" class="text-body-2 text-medium-emphasis">报名通过后开放各组学习内容，身份仍为新生；组别可多选，各组阶段考核独立记录。</p>
      </v-card-text>
      <v-card-actions class="px-6 pb-6"><v-spacer /><v-btn :disabled="saving" @click="dialog = false; draft.password = ''">取消</v-btn><v-btn color="primary" variant="flat" :loading="saving" @click="save">{{ editing ? '保存身份与权限' : '创建账号' }}</v-btn></v-card-actions>
    </v-card></v-dialog>
    <v-dialog :model-value="Boolean(passwordTarget)" max-width="520" :persistent="passwordBusy" @update:model-value="!$event && closePasswordEditor()">
      <v-card v-if="passwordTarget">
        <v-card-title class="pa-5 pb-2">设置密码 · {{ passwordTarget.name || passwordTarget.email }}</v-card-title>
        <v-card-text>
          <p class="mb-4 text-medium-emphasis">{{ passwordTarget.email }}<br />保存后，该账号所有已登录设备都会退出。账号原有状态保持不变。</p>
          <v-alert v-if="passwordError" type="error" variant="tonal" class="mb-4">{{ passwordError }}</v-alert>
          <v-text-field v-if="passwordTarget.id === auth.user?.id" v-model="currentPassword" label="当前密码" type="password" autocomplete="current-password" :disabled="passwordBusy" />
          <v-text-field v-model="passwordDraft" label="新密码" type="password" autocomplete="new-password" hint="至少 6 个字符，无字符组合要求。" persistent-hint :disabled="passwordBusy" />
          <v-text-field v-model="passwordConfirm" label="再次输入新密码" type="password" autocomplete="new-password" :disabled="passwordBusy" @keyup.enter="saveMemberPassword" />
        </v-card-text>
        <v-card-actions class="px-5 pb-5"><v-spacer /><v-btn :disabled="passwordBusy" @click="closePasswordEditor">取消</v-btn><v-btn color="primary" variant="flat" :loading="passwordBusy" @click="saveMemberPassword">保存新密码</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog :model-value="Boolean(accountTarget)" max-width="540" :persistent="accountBusy" @update:model-value="!$event && (accountTarget = null)">
      <v-card v-if="accountTarget">
        <v-card-title class="pa-5 pb-2">{{ accountTarget.deactivatedAt ? '恢复账号' : '注销账号' }}</v-card-title>
        <v-card-text>
          <p class="mb-4"><strong>{{ accountTarget.name }}</strong><br /><span class="text-medium-emphasis">{{ accountTarget.email }}</span></p>
          <v-alert v-if="accountError" type="error" variant="tonal" class="mb-4">{{ accountError }}</v-alert>
          <v-alert :type="accountTarget.deactivatedAt ? 'info' : 'warning'" variant="tonal" class="mb-4">{{ accountTarget.deactivatedAt ? '恢复后，该用户可以重新登录，原有身份、所属组别和历史记录继续保留。' : '注销后将立即禁止登录，并退出已有登录会话。报名、作业、成绩、证书等历史记录保留，管理员之后可以恢复账号。' }}</v-alert>
          <p v-if="accountTarget.deactivatedAt && accountTarget.deactivationReason" class="mb-3">原注销原因：{{ accountTarget.deactivationReason }}</p>
          <v-textarea v-if="!accountTarget.deactivatedAt" v-model="accountReason" label="注销原因 *" placeholder="例如：本人申请退出战队" hint="最多 2000 字节，中文约 660 字。" persistent-hint rows="3" maxlength="2000" counter :disabled="accountBusy" variant="outlined" />
        </v-card-text>
        <v-card-actions class="px-5 pb-5"><v-spacer /><v-btn :disabled="accountBusy" @click="accountTarget = null">取消</v-btn><v-btn :color="accountTarget.deactivatedAt ? 'success' : 'error'" variant="flat" :loading="accountBusy" :disabled="!accountTarget.deactivatedAt && !accountReason.trim()" @click="confirmAccountAction">{{ accountTarget.deactivatedAt ? '确认恢复' : '确认注销' }}</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <v-snackbar :model-value="Boolean(notice)" @update:model-value="!$event && (notice = '')">{{ notice }}</v-snackbar>
  </div>
</template>
<style scoped>
.member-counts { display: flex; flex-wrap: wrap; gap: 8px 20px; align-items: center; margin-bottom: 20px; color: #6b7888; font-size: 13px; }
.member-counts strong { color: #30475d; font-size: 14px; }
.filters { display: grid; grid-template-columns: minmax(220px, 2fr) minmax(150px, 1fr) minmax(140px, 1fr); gap: 14px; margin-bottom: 20px; }
.member-table-card { border-color: #e4e9ef; }
.member-table-heading { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid #e8edf2; font-size: 14px; }
.member-table-heading span { color: #7b8898; font-size: 12px; }
.member-table-scroll { overflow-x: auto; }
.member-table { width: 100%; min-width: 1120px; border-collapse: collapse; text-align: left; }
.member-table th { color: #68798c; background: #f7f9fc; font-size: 12px; font-weight: 500; padding: 12px 14px; white-space: nowrap; }
.member-table td { padding: 12px 14px; border-top: 1px solid #edf0f4; font-size: 13px; vertical-align: middle; }
.member-table tbody tr:hover { background: #f8fbff; }
.member-table .account-deactivated { background: #fafafa; }
.member-name { min-width: 110px; color: #263d54; }
.self-label, .cell-note { display: block; margin-top: 5px; font-size: 11px; color: #7c8c9d; }
.self-label { color: #427cb3; }
.member-email { color: #5c6d7e; max-width: 230px; overflow-wrap: anywhere; }
.member-date { white-space: nowrap; font-size: 12px !important; color: #728297; }
.group-tags { display: flex; flex-wrap: wrap; gap: 5px; min-width: 100px; max-width: 190px; }
.cell-muted { color: #8693a1; font-size: 12px; }
.member-actions { display: flex; align-items: center; gap: 2px; white-space: nowrap; }
.member-actions .v-btn { min-width: 0; padding: 0 7px; }
.promotion-override-note { padding: 10px 12px; border-left: 3px solid #d99a31; background: #fff9ed; font-size: 12px; color: #70501e; }
.promotion-override-note strong, .promotion-override-note span { display: block; }
.promotion-override-note span { margin-top: 3px; color: #8a744e; }
.promotion-override-note p { margin: 6px 0 0; line-height: 1.6; }
@media (max-width: 600px) {
  .filters { grid-template-columns: 1fr 1fr; }
  .filters > *:first-child { grid-column: 1 / -1; }
  .page-content > .d-flex { flex-wrap: wrap; }
}
</style>

<style scoped>
.grant-card { border: 1px solid #e5e7eb; border-radius: 6px; padding: 5px 14px 14px; margin-top: 12px; }
.permission-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 3px 14px; margin-left: 12px; }
@media (max-width: 600px) { .permission-grid { grid-template-columns: 1fr; } }
</style>
