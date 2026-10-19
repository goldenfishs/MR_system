<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { api, ApiError } from '../../lib/api';
import { auth } from '../../lib/auth';
import { groupLabel, hasPermission } from '../../lib/permissions';
import type { TeamApplication, TeamSpace } from '../../lib/platform';
import { beijingTimeISO, beijingDay, beijingClock, slotAvailable, slotDays, slotLabel, type InterviewSlot } from '../../lib/interview-slots';

const props = defineProps<{ applications: TeamApplication[]; teams: TeamSpace[]; manager: boolean; activeApplicationId?: string }>();
const emit = defineEmits<{ changed: []; updated: [application: TeamApplication] }>();
const slots = ref<InterviewSlot[]>([]), loading = ref(false), saving = ref(false), error = ref(''), notice = ref('');
const teamId = ref(''), direction = ref(''), applicationId = ref(''), selectedDay = ref(''), selectedSlotId = ref('');
const showCreate = ref(false), showPast = ref(false), cancelConfirm = ref(false);
const newDate = ref(''), newTime = ref('09:00'), interval = ref(10), count = ref(6), capacity = ref(1), room = ref('');
const editId = ref(''), editRoom = ref(''), editCapacity = ref(1), editClosed = ref(false);
const now = ref(Date.now()), serverOffset = ref(0);
const tick = setInterval(() => { now.value = Date.now() + serverOffset.value; }, 30000);
onBeforeUnmount(() => clearInterval(tick));
let requestId = 0;
let appliedActiveId = '';
const pending = computed(() => props.applications.filter(a => a.status === 'pending' && (!a.interview || a.interview.status === 'pending') && (a.userId === auth.user?.id || props.manager && hasPermission('recruitment', a.direction))));
const allowedTeams = computed(() => props.teams.filter(t => t.directions.some(d => props.manager && hasPermission('recruitment', d)) || pending.value.some(a => a.userId === auth.user?.id && a.teamId === t.id)));
const directions = computed(() => (props.teams.find(t => t.id === teamId.value)?.directions || []).filter(d => props.manager && hasPermission('recruitment', d) || pending.value.some(a => a.userId === auth.user?.id && a.teamId === teamId.value && a.direction === d)));
const scopeManager = computed(() => props.manager && !!direction.value && hasPermission('recruitment', direction.value));
const applicationsInScope = computed(() => pending.value.filter(a => a.teamId === teamId.value && a.direction === direction.value));
const selectedApplication = computed(() => pending.value.find(a => a.id === applicationId.value));
const activeBooking = computed(() => selectedApplication.value?.interview);
const currentSlot = computed(() => slots.value.find(s => s.id === activeBooking.value?.slotId));
const visibleSlots = computed(() => slots.value.filter(s => scopeManager.value && showPast.value || Date.parse(s.startAt) > now.value));
const days = computed(() => slotDays(visibleSlots.value));
const daySlots = computed(() => visibleSlots.value.filter(s => beijingDay(s.startAt) === selectedDay.value).sort((a,b) => a.startAt.localeCompare(b.startAt) || a.room.localeCompare(b.room)));
const selectedSlot = computed(() => slots.value.find(s => s.id === selectedSlotId.value));
const editedSlot = computed(() => slots.value.find(s => s.id === editId.value));
const newStart = computed(() => beijingTimeISO(newDate.value, newTime.value));
const batchEnd = computed(() => newStart.value && Number.isFinite(Number(count.value) * Number(interval.value)) ? new Date(Date.parse(newStart.value) + Number(count.value) * Number(interval.value) * 60000).toISOString() : '');
const canCreate = computed(() => scopeManager.value && !!teamId.value && !!direction.value && room.value.trim().length > 0 && Date.parse(newStart.value) > now.value && [interval.value, count.value, capacity.value].every(v => Number.isInteger(Number(v)) && Number(v) >= 1) && Number(interval.value) <= 120 && Number(count.value) <= 100 && Number(capacity.value) <= 100);
const canBook = computed(() => !!selectedApplication.value && !!selectedSlot.value && selectedSlot.value.id !== activeBooking.value?.slotId && slotAvailable(selectedSlot.value, now.value));
const teamName = (id: string) => props.teams.find(t => t.id === id)?.name || '报名团队';
function dateTitle(date: string) { const stamp = Date.parse(`${date}T12:00:00+08:00`); return new Intl.DateTimeFormat('zh-CN', { timeZone:'Asia/Shanghai', month:'numeric', day:'numeric', weekday:'short' }).format(stamp); }
function chooseApplication(id: string) {
  const item = pending.value.find(a => a.id === id);
  if (!item) return;
  teamId.value = item.teamId; direction.value = item.direction; applicationId.value = item.id; cancelConfirm.value = false; selectedSlotId.value = '';
}
function openApplication(id: string) { chooseApplication(id); document.getElementById('interview-slots')?.scrollIntoView({ behavior:'smooth', block:'start' }); }
defineExpose({ openApplication, refresh: load });
async function load() {
  const current = ++requestId;
  if (!teamId.value || !direction.value) { slots.value = []; loading.value = false; return; }
  loading.value = true;
  try {
    const query = new URLSearchParams({ teamId:teamId.value, direction:direction.value });
    if (!scopeManager.value && applicationId.value) query.set('applicationId', applicationId.value);
    const response = await api.get<{ items: InterviewSlot[]; serverTime: string }>(`/interview-slots?${query}`);
    if (current !== requestId) return;
    slots.value = response.items || [];
    const serverTime = Date.parse(response.serverTime);
    if (Number.isFinite(serverTime)) { serverOffset.value = serverTime - Date.now(); now.value = serverTime; }
    if (!slots.value.some(s => s.id === editId.value)) editId.value = '';
  } catch (e) { if (current === requestId) { error.value = (e as Error).message; slots.value = []; } }
  finally { if (current === requestId) loading.value = false; }
}
async function perform(action: () => Promise<void>) {
  if (saving.value) return;
  saving.value = true; error.value = ''; notice.value = '';
  try { await action(); } catch (e) { const message = (e as Error).message; if (e instanceof ApiError && e.status === 409) await load(); error.value = message; }
  finally { saving.value = false; }
}
function create() {
  if (!canCreate.value) { error.value = '请填写未来的日期、时间和教室；每段 1–120 分钟，时段数和每段名额均为 1–100。'; return; }
  void perform(async () => {
    const result = await api.post<{ items: InterviewSlot[] }>('/interview-slots/batch', { teamId:teamId.value, direction:direction.value, startAt:newStart.value, intervalMinutes:Number(interval.value), count:Number(count.value), capacity:Number(capacity.value), room:room.value.trim() });
    notice.value = `已开放 ${result.items.length} 个面试时段，同学现在可以选择预约。尚未发送邮件。`;
    selectedDay.value = newDate.value; showCreate.value = false; await load();
  });
}
function book() {
  if (!canBook.value || !selectedSlot.value || !selectedApplication.value) return;
  const slot = selectedSlot.value, candidate = selectedApplication.value;
  void perform(async () => {
    const response = await api.post<{ item: TeamApplication }>(`/interview-slots/${slot.id}/book`, { applicationId:candidate.id });
    emit('updated', response.item);
    notice.value = `${props.manager ? candidate.userName + '的面试' : '面试'}已预约：${slotLabel(slot)}（北京时间）。`;
    selectedSlotId.value = ''; cancelConfirm.value = false; await load(); emit('changed');
  });
}
function cancelBooking() {
  if (!selectedApplication.value?.interview?.slotId) return;
  const id = selectedApplication.value.id;
  void perform(async () => {
    const response = await api.delete<{ item: TeamApplication }>(`/interviews/${id}/booking`); emit('updated', response.item); cancelConfirm.value = false; selectedSlotId.value = ''; notice.value = '预约已取消，名额已释放，可重新选择面试时间。'; await load(); emit('changed');
  });
}
function startEdit(slot: InterviewSlot) {
  editId.value = slot.id; editRoom.value = slot.room; editCapacity.value = slot.capacity; editClosed.value = slot.closed;
  void nextTick(() => document.getElementById('interview-slot-edit')?.scrollIntoView({ behavior:'smooth', block:'center' }));
}
function selectSlot(slot: InterviewSlot) {
  selectedSlotId.value = slot.id; cancelConfirm.value = false;
  void nextTick(() => document.getElementById('interview-slot-confirm')?.scrollIntoView({ behavior:'smooth', block:'nearest' }));
}
function saveEdit() {
  if (!editedSlot.value) return;
  const slot = editedSlot.value;
  if (!editRoom.value.trim() || !Number.isInteger(Number(editCapacity.value)) || Number(editCapacity.value) < Math.max(1, slot.booked) || Number(editCapacity.value) > 100) { error.value = '请填写教室，名额需为 1–100 的整数，且不能少于已预约人数。'; return; }
  void perform(async () => {
    await api.patch(`/interview-slots/${slot.id}`, { room:editRoom.value.trim(), capacity:Number(editCapacity.value), closed:editClosed.value });
    editId.value = ''; notice.value = '时段设置已保存，已有预约保留。'; await load(); emit('changed');
  });
}
watch(() => [props.teams, props.applications], () => {
  if (!allowedTeams.value.some(t => t.id === teamId.value)) teamId.value = allowedTeams.value[0]?.id || '';
  if (!directions.value.includes(direction.value)) direction.value = directions.value[0] || '';
  if (!pending.value.some(a => a.id === applicationId.value)) applicationId.value = scopeManager.value ? '' : applicationsInScope.value[0]?.id || '';
  if (!scopeManager.value && !applicationId.value && pending.value[0]) chooseApplication(pending.value[0].id);
}, { immediate:true });
watch([() => props.activeApplicationId, () => props.applications], ([id]) => { if (id && id !== appliedActiveId && pending.value.some(a => a.id === id)) { chooseApplication(id); appliedActiveId = id; } }, { immediate:true });
watch([teamId, direction], () => {
  if (!directions.value.includes(direction.value)) direction.value = directions.value[0] || '';
  if (!applicationsInScope.value.some(a => a.id === applicationId.value)) applicationId.value = scopeManager.value ? '' : applicationsInScope.value[0]?.id || '';
  selectedSlotId.value = ''; editId.value = ''; cancelConfirm.value = false; error.value = ''; slots.value = []; void load();
}, { immediate:true });
watch(applicationId, () => { selectedSlotId.value = ''; cancelConfirm.value = false; if (!scopeManager.value) void load(); });
watch(days, values => { if (!values.includes(selectedDay.value)) selectedDay.value = values[0] || ''; }, { immediate:true });
</script>

<template>
<section v-if="manager || pending.length" id="interview-slots" class="slot-panel" aria-labelledby="slot-panel-title">
  <header class="slot-heading">
    <div><h2 id="slot-panel-title">{{ manager ? '面试时段与预约' : '选择面试时间' }}</h2><p>{{ manager ? '先开放日期、教室和名额，同学可自主预约；也可为候选人指定时段。' : '选择报名组别和日期，再选择有剩余名额的时段。' }}所有时间均为北京时间（UTC+8）。</p></div>
    <div class="slot-actions"><v-btn prepend-icon="mdi-refresh" variant="text" :loading="loading" :disabled="saving" @click="load">刷新名额</v-btn><v-btn v-if="scopeManager" prepend-icon="mdi-calendar-plus" color="primary" :disabled="saving || !allowedTeams.length" @click="showCreate = !showCreate">{{ showCreate ? '收起设置' : '开放面试时段' }}</v-btn></div>
  </header>
  <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>
  <v-alert v-if="notice" type="success" variant="tonal" class="mb-4" closable @click:close="notice = ''">{{ notice }}</v-alert>
  <div v-if="manager" class="slot-filters">
    <v-select v-model="teamId" label="报名团队" :items="allowedTeams.map(t => ({ title:t.name, value:t.id }))" :disabled="saving" hide-details variant="outlined" density="compact"/>
    <v-select v-model="direction" label="面试组别" :items="directions.map(d => ({ title:groupLabel(d), value:d }))" :disabled="saving" hide-details variant="outlined" density="compact"/>
    <v-autocomplete v-model="applicationId" :label="scopeManager ? '为同学指定时段（可选）' : '我的待面试报名'" :items="[...(scopeManager ? [{ title:'仅管理时段', value:'' }] : []), ...applicationsInScope.map(a => ({ title:a.userName + ' · ' + (a.profile?.studentNumber || '未填学号'), value:a.id }))]" :disabled="saving" hide-details variant="outlined" density="compact" no-data-text="本组没有待面试报名"/>
  </div>
  <v-select v-else :model-value="applicationId" label="选择待面试的报名" :items="pending.map(a => ({ title:groupLabel(a.direction) + ' · ' + teamName(a.teamId), value:a.id }))" :disabled="saving" variant="outlined" density="comfortable" @update:model-value="chooseApplication"/>
  <section v-if="scopeManager && showCreate" class="slot-create" aria-label="批量开放面试时段">
    <h3>为 {{ groupLabel(direction) }} 开放时段</h3><p class="slot-hint">同一天连续安排，自动按每段时长生成；不同日期或教室可以分批添加。</p>
    <div class="slot-form-grid">
      <v-text-field v-model="newDate" type="date" label="面试日期 *" :min="beijingDay(new Date(now).toISOString())" :disabled="saving"/>
      <v-text-field v-model="newTime" type="time" label="开始时间（北京时间）*" :disabled="saving"/>
      <v-text-field v-model.number="interval" type="number" min="1" max="120" label="每段时长（分钟）" :disabled="saving"/>
      <v-text-field v-model.number="count" type="number" min="1" max="100" label="连续生成几段" :disabled="saving"/>
      <v-text-field v-model.number="capacity" type="number" min="1" max="100" label="每段名额（人）" :disabled="saving"/>
      <v-text-field v-model="room" label="教室 / 地点 *" placeholder="例如：主教学楼 A101" maxlength="100" :disabled="saving"/>
    </div>
    <div class="slot-actions"><v-btn color="primary" :disabled="!canCreate" :loading="saving" @click="create">确认开放 {{ count }} 个时段</v-btn><span v-if="canCreate && batchEnd" class="slot-hint">{{ newDate }} {{ newTime }} — {{ beijingDay(batchEnd) !== newDate ? beijingDay(batchEnd) + ' ' : '' }}{{ beijingClock(batchEnd) }}，共 {{ Number(count) * Number(capacity) }} 个名额</span></div>
  </section>
  <div v-if="selectedApplication && activeBooking?.at" class="current-booking">
    <div><strong>{{ manager ? selectedApplication.userName + '的当前安排' : '你的当前安排' }}</strong><p>{{ beijingDay(activeBooking.at) }} {{ beijingClock(activeBooking.at) }} · {{ activeBooking.room }}<span v-if="activeBooking.order"> · 第 {{ activeBooking.order }} 位</span></p><small v-if="!activeBooking.slotId">此安排由管理员设置；选择新时段并确认后将替换当前安排。</small><small v-else-if="currentSlot?.closed">此时段已停止新预约，你的现有预约仍有效。</small></div>
    <v-btn v-if="activeBooking.slotId && Date.parse(activeBooking.at) > now" variant="text" color="error" :disabled="saving" @click="cancelConfirm = !cancelConfirm">取消预约</v-btn>
  </div>
  <v-alert v-if="cancelConfirm" type="warning" variant="tonal" class="my-4">确定取消当前预约？名额会释放给其他同学。<div class="slot-actions mt-3"><v-btn color="error" :loading="saving" @click="cancelBooking">确认取消预约</v-btn><v-btn variant="text" :disabled="saving" @click="cancelConfirm = false">保留预约</v-btn></div></v-alert>
  <div class="slot-browse-heading"><p>{{ manager && !selectedApplication ? '点击“设置”调整时段；选择上方候选人后可指派。' : activeBooking?.slotId ? '重新选择其他时段，可以更改预约。' : '选择日期后，点击一个可预约时段。' }}</p><v-checkbox v-if="scopeManager" v-model="showPast" label="显示已开始时段" density="compact" hide-details/></div>
  <v-progress-linear v-if="loading" indeterminate color="primary"/>
  <div v-if="days.length" class="slot-days" role="tablist" aria-label="面试日期"><button v-for="day in days" :id="'slot-date-' + day" :key="day" type="button" role="tab" :aria-selected="selectedDay === day" aria-controls="slot-time-panel" :class="{ active:selectedDay === day }" @click="selectedDay = day; selectedSlotId = ''">{{ dateTitle(day) }}</button></div>
  <div v-if="daySlots.length" id="slot-time-panel" class="slot-grid" role="tabpanel" :aria-labelledby="'slot-date-' + selectedDay">
    <article v-for="slot in daySlots" :key="slot.id" class="slot-card" :class="{ selected:selectedSlotId === slot.id, current:activeBooking?.slotId === slot.id, unavailable:!slotAvailable(slot, now) }">
      <button type="button" class="slot-choice" :disabled="saving || !selectedApplication || !slotAvailable(slot, now) || activeBooking?.slotId === slot.id" :aria-pressed="selectedSlotId === slot.id" :aria-label="slotLabel(slot) + '，剩余 ' + slot.remaining + ' 位'" @click="selectSlot(slot)"><strong>{{ beijingClock(slot.startAt) }}</strong><span class="slot-end">至 {{ beijingClock(slot.endAt) }}</span><span class="slot-room">{{ slot.room }}</span><span class="slot-state">{{ activeBooking?.slotId === slot.id ? '已预约' : Date.parse(slot.startAt) <= now ? '已开始' : slot.closed ? '暂停预约' : slot.remaining < 1 ? '已约满' : '剩余 ' + slot.remaining + ' 位' }}</span></button>
      <div v-if="scopeManager" class="slot-card-manage"><small>{{ slot.booked }} / {{ slot.capacity }} 人</small><v-btn variant="text" size="small" :disabled="saving" :aria-label="'设置 ' + slotLabel(slot)" @click="startEdit(slot)">设置</v-btn></div>
    </article>
  </div>
  <div v-else-if="!loading" class="slot-empty"><v-icon icon="mdi-calendar-clock-outline" size="34"/><p>{{ scopeManager ? '当前组别还没有可预约时段，点击“开放面试时段”设置。' : '本组暂未开放可预约时段，请等待管理员安排。' }}</p></div>
  <div v-if="selectedSlot && selectedApplication" id="interview-slot-confirm" class="slot-confirm"><div><strong>{{ activeBooking?.at ? '确认更改面试安排' : manager ? '确认指派面试时段' : '确认预约面试时段' }}</strong><p>{{ selectedApplication.userName }} · {{ groupLabel(selectedApplication.direction) }}</p><p>{{ slotLabel(selectedSlot) }}（北京时间）</p><small v-if="activeBooking?.at">确认成功后释放原预约；若新时段已满，原安排保留。</small></div><div class="slot-actions"><v-btn color="primary" :loading="saving" :disabled="!canBook" @click="book">{{ activeBooking?.at ? '确认改约' : '确认预约' }}</v-btn><v-btn variant="text" :disabled="saving" @click="selectedSlotId = ''">取消选择</v-btn></div></div>
  <section v-if="scopeManager && editedSlot" id="interview-slot-edit" class="slot-edit" aria-label="编辑面试时段"><header class="slot-heading"><div><h3>时段设置 · {{ beijingDay(editedSlot.startAt) }} {{ beijingClock(editedSlot.startAt) }}</h3><p>已有 {{ editedSlot.booked }} 人预约。暂停后保留现有预约，停止接受新的预约。</p></div><v-btn icon="mdi-close" aria-label="收起时段设置" variant="text" :disabled="saving" @click="editId = ''"/></header><div class="slot-edit-fields"><v-text-field v-model="editRoom" label="教室 / 地点" maxlength="100" :disabled="saving || editedSlot.booked > 0" :hint="editedSlot.booked > 0 ? '已有预约，不能直接修改地点；请先为同学改约。' : ''" persistent-hint/><v-text-field v-model.number="editCapacity" type="number" :min="Math.max(1, editedSlot.booked)" max="100" label="名额" :disabled="saving"/><v-switch v-model="editClosed" color="warning" label="暂停接受预约" :disabled="saving" hide-details/></div><v-btn color="primary" :loading="saving" @click="saveEdit">保存时段设置</v-btn></section>
  <p v-if="manager" class="slot-hint slot-footer">面试安排保存后，可在下方候选人名单中选择同学，预览并发送通知邮件。</p>
</section>
</template>

<style scoped>
.slot-panel{min-width:0;background:rgb(var(--v-theme-surface));border:1px solid #e1e8f0;border-radius:8px;padding:24px;margin-bottom:24px;scroll-margin-top:100px}.slot-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;flex-wrap:wrap;margin-bottom:20px}.slot-heading>div:first-child{flex:1 1 280px;min-width:0}.slot-heading h2{font-size:20px;font-weight:600}.slot-heading p,.slot-hint,.slot-browse-heading p{font-size:13px;line-height:1.7;color:#637588}.slot-heading p{margin-top:6px}.slot-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.slot-actions :deep(.v-btn){max-width:100%}.slot-filters{display:grid;grid-template-columns:1fr .8fr 1.3fr;gap:14px;margin-bottom:20px}.slot-filters>*{min-width:0}.slot-create,.slot-edit{padding:20px;background:#f6f9fd;border:1px solid #dce6f2;border-radius:6px;margin:20px 0}.slot-create h3,.slot-edit h3{font-size:16px}.slot-form-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px 16px;margin-top:18px}.slot-edit-fields{display:grid;grid-template-columns:1.4fr .6fr 1fr;gap:16px}.slot-browse-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin:20px 0 8px}.slot-browse-heading :deep(.v-input){flex:0 0 auto;width:auto}.slot-browse-heading :deep(.v-selection-control){flex:0 0 auto;min-width:max-content}.slot-browse-heading :deep(.v-label){white-space:nowrap}.slot-days{display:flex;gap:4px;overflow-x:auto;max-width:100%;border-bottom:1px solid #e1e8f0;margin:12px 0 20px;padding-bottom:1px}.slot-days button{flex:0 0 auto;white-space:nowrap;padding:13px 18px;font-size:14px;color:#586d82;border-bottom:3px solid transparent;border-radius:4px 4px 0 0}.slot-days button.active{background:#eaf3ff;color:#086bc9;border-bottom-color:#0876df}.slot-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:14px}.slot-card{background:#f7fafc;border:1px solid #dae4ee;border-radius:7px;min-width:0;overflow:hidden}.slot-card.selected{background:#e8f2ff;border:2px solid #1376d2}.slot-card.current{background:#ecf8f2;border-color:#70b18d}.slot-card.unavailable{background:#f3f4f6;color:#89919b}.slot-choice{display:flex;flex-direction:column;align-items:center;text-align:center;padding:18px 12px 14px;width:100%;color:inherit;min-height:160px}.slot-choice:not(:disabled):hover{background:#e8f2ff}.slot-choice:disabled{cursor:default}.slot-choice strong{font-size:29px;font-weight:500;line-height:1.25;color:#24425f}.slot-end{font-size:11px;color:#78899a;margin-top:2px}.slot-room{font-size:13px;margin:10px 0 5px;overflow-wrap:anywhere;line-height:1.4}.slot-state{font-size:13px;font-weight:500;color:#1472b7}.unavailable .slot-state{color:#7a8490}.current .slot-state{color:#20834a}.slot-card-manage{display:flex;align-items:center;justify-content:space-between;border-top:1px solid #e3eaf1;padding:3px 9px}.slot-card-manage small{font-size:11px;color:#708194}.current-booking,.slot-confirm{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;border:1px solid #bed8ef;background:#f0f7ff;border-radius:6px;padding:18px;margin-top:18px}.current-booking p,.slot-confirm p{font-size:14px;margin-top:6px}.current-booking small,.slot-confirm small{display:block;font-size:12px;line-height:1.7;color:#6c7c89;margin-top:6px}.slot-confirm,.slot-edit{scroll-margin-top:110px}.slot-confirm{border-color:#91bde8}.slot-empty{text-align:center;color:#8291a0;padding:32px 12px}.slot-empty p{font-size:14px;margin-top:10px}.slot-footer{margin-top:20px}.slot-panel button:focus-visible{outline:3px solid #1d76cf;outline-offset:2px}@media(max-width:1000px){.slot-filters{grid-template-columns:1fr 1fr}.slot-filters>:last-child{grid-column:1/-1}.slot-form-grid{grid-template-columns:1fr 1fr}.slot-edit-fields{grid-template-columns:1fr 1fr}.slot-edit-fields>:last-child{grid-column:1/-1}}@media(max-width:600px){.slot-panel{padding:16px}.slot-filters,.slot-form-grid,.slot-edit-fields{grid-template-columns:minmax(0,1fr)}.slot-create,.slot-edit{padding:14px}.slot-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.slot-choice{padding:15px 8px;min-height:150px}.slot-choice strong{font-size:26px}.slot-days button{padding:12px}.slot-heading h2{font-size:18px}}
</style>
