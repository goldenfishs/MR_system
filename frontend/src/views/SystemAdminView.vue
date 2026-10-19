<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api } from "../lib/api";
import type { Settings } from "../lib/types";
const settings = ref<Settings | null>(null);
const error = ref("");
const loading = ref(true);
const stats = ref<Record<string, number>>({});
const cards = [
  { title: "AI 答疑", description: "设置聊天助手的服务地址、密钥、模型与回答规则。", icon: "mdi-robot-outline", to: "/settings#ai", color: "primary" },
  { title: "网站外观", description: "修改网站标志、简洁名称、战队名称与赛季信息。", icon: "mdi-palette-outline", to: "/settings#branding", color: "primary" },
  { title: "邮箱设置", description: "配置发件邮箱与邮件服务，用于注册验证和通知。", icon: "mdi-email-edit-outline", to: "/settings#email", color: "teal" },
  { title: "用户管理", description: "管理用户账号、成员身份，按组分配管理权限。", icon: "mdi-account-key-outline", to: "/members", color: "purple" },
  { title: "报名审核", description: "集中查看各组报名，预览证件照与简历，快速审核。", icon: "mdi-account-check-outline", to: "/applications", color: "primary" },
  { title: "团队与招募", description: "维护各组报名入口、介绍、招募状态与截止时间。", icon: "mdi-account-multiple-plus-outline", to: "/teams", color: "teal" },
  { title: "考核阶段配置", description: "按组设置阶段数量、名称、说明与顺序，同步尚未考核的新生。", icon: "mdi-account-school-outline", to: "/training?section=plans", color: "purple" },
  { title: "荣誉与证书", description: "管理证书与荣誉，按用户资料生成并批量发放。", icon: "mdi-certificate-outline", to: "/records", color: "orange-darken-2" },
  { title: "邮件管理", description: "选择收件人、编辑通知，查看发送进度与结果。", icon: "mdi-email-multiple-outline", to: "/mail", color: "primary" },
  { title: "学习内容", description: "发布知识库、作业、考试和题库，查看作答与评阅。", icon: "mdi-book-open-page-variant-outline", to: "/learning", color: "teal" },
  { title: "讨论区管理", description: "解答各组问题，将重要解答公开给全队查阅。", icon: "mdi-forum-outline", to: "/discussions", color: "purple" },
  { title: "面试管理", description: "报名自动进入面试，安排教室和顺序，发送通知并确认录取。", icon: "mdi-account-clock-outline", to: "/recruitment", color: "orange-darken-2" },
  { title: "通用设置", description: "管理网站公告、报名设置和在线测评服务入口。", icon: "mdi-cog-outline", to: "/settings#general", color: "blue-grey" },
];
const summary = computed(() => [
  { label: "待审核报名", value: stats.value.pendingApplications, to: "/applications" },
  { label: "开放报名团队", value: stats.value.openTeams, to: "/teams" },
  { label: "在队成员", value: stats.value.members, to: "/members" },
]);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [brand, summary] = await Promise.all([api.get<{ settings: Settings }>("/settings"), api.get<{ stats: Record<string, number> }>("/workspace-summary")]);
    settings.value = brand.settings;
    stats.value = summary.stats;
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
onMounted(load);
</script>
<template>
  <div class="page-content admin-page">
    <div class="admin-head"><div><h1 class="page-title">系统管理</h1><p class="page-subtitle">网站配置、用户权限和日常管理，在这里统一处理。</p></div><v-btn prepend-icon="mdi-refresh" variant="text" :loading="loading" @click="load">刷新</v-btn></div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}<v-btn variant="text" @click="load">重试</v-btn></v-alert>
    <v-card class="admin-overview mb-6">
      <div class="admin-brand"><img :src="settings?.logoUrl || '/mark.png'" alt="网站标志" @error="($event.target as HTMLImageElement).src = '/mark.png'" /><div><h2>{{ settings?.brandName || 'QUT机器人战队' }}</h2><p>{{ settings?.organization || '青岛理工大学机器人战队' }} · {{ settings?.season || '当前赛季' }}</p><v-chip color="primary" variant="tonal" size="x-small">管理员 · 全部权限</v-chip></div></div>
      <div class="admin-summary"><router-link v-for="item in summary" :key="item.label" :to="item.to"><strong>{{ item.value ?? '—' }}</strong><span>{{ item.label }}</span></router-link></div>
    </v-card>
    <div class="admin-grid"><v-card v-for="item in cards" :key="item.title" :to="item.to" class="admin-card"><v-avatar :color="item.color" variant="tonal" size="44"><v-icon :icon="item.icon" /></v-avatar><div class="admin-card-content"><h2>{{ item.title }}</h2><p>{{ item.description }}</p></div><v-icon color="grey" size="20">mdi-chevron-right</v-icon></v-card></div>
  </div>
</template>
<style scoped>
.admin-head{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin-bottom:24px}.admin-overview{display:flex;justify-content:space-between;align-items:center;gap:30px;padding:28px}.admin-brand{display:flex;align-items:center;gap:18px;min-width:0}.admin-brand img{width:54px;height:54px;object-fit:contain}.admin-brand h2{font-size:20px;font-weight:500;overflow-wrap:anywhere}.admin-brand p{font-size:13px;color:#7a8087;margin:5px 0 9px}.admin-summary{display:flex;gap:24px;flex-shrink:0}.admin-summary a{display:flex;flex-direction:column;text-align:center;color:inherit;text-decoration:none}.admin-summary strong{font-weight:400;font-size:27px;color:rgb(var(--v-theme-primary))}.admin-summary span{color:#7a8087;font-size:12px;margin-top:5px}.admin-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:18px}.admin-card{display:flex;align-items:flex-start;gap:14px;padding:23px 19px}.admin-card .v-avatar{flex-shrink:0}.admin-card-content{flex:1;min-width:0}.admin-card h2{font-size:16px;font-weight:500;margin:2px 0 8px}.admin-card p{font-size:13px;color:#7a8087;line-height:1.8}.admin-card>.v-icon{margin-top:12px;flex-shrink:0}@media(max-width:1250px){.admin-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.admin-overview{align-items:flex-start;flex-direction:column;width:100%}.admin-summary{justify-content:space-between;width:100%}}@media(max-width:600px){.admin-grid{grid-template-columns:1fr}.admin-overview{padding:20px}.admin-head{flex-wrap:wrap}.admin-brand img{width:40px;height:40px}.admin-summary{gap:12px}.admin-summary span{font-size:11px}}
</style>
