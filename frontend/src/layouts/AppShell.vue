<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Monitor, Wallet, User, Connection, House, Setting, UserFilled, List, SwitchButton, Back, Bell, Lock, ArrowDown, Menu } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { useSystemStore } from '../stores/system'
import { getVersion } from '../api/system'

const props = defineProps<{ admin?: boolean }>()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const system = useSystemStore()
const collapsed = ref(false)
const mobileNavVisible = ref(false)
const version = ref('v0.1.3')
const title = computed(() => String(route.meta.title || system.name))
const links = computed(() => props.admin ? [
  { to: '/admin', label: '管理概览', icon: Monitor },
  { to: '/admin/users', label: '用户管理', icon: UserFilled },
  { to: '/admin/stranger-pets', label: '陌生人管理', icon: UserFilled },
  { to: '/admin/internal-pets', label: '内部宠物', icon: Monitor },
  { to: '/admin/settings', label: '系统设置', icon: Setting },
  { to: '/admin/push-settings', label: '推送设置', icon: Bell },
  { to: '/admin/mokant', label: '萌卡 NT', icon: Connection },
  { to: '/admin/debug', label: 'WS调试', icon: Connection },
  { to: '/admin/qq-accounts', label: 'QQ 管理', icon: User },
  { to: '/admin/qq-plans', label: 'QQ 套餐', icon: List },
  { to: '/admin/redeem-codes', label: '卡密管理', icon: List },
  { to: '/admin/announcements', label: '公告管理', icon: Bell },
  { to: '/admin/audit', label: '审计日志', icon: List },
] : [
  { to: '/', label: '首页', icon: House },
  { to: '/wallet', label: '我的钱包', icon: Wallet },
  { to: '/qq-account', label: '我的 QQ', icon: Connection },
  { to: '/profile', label: '个人信息', icon: User },
  { to: '/pet', label: 'QQ 宠物', icon: Monitor },
  { to: '/pet/auto-control', label: '自动控制', icon: Setting },
  { to: '/pet/friends', label: '好友管理', icon: UserFilled },
  { to: '/ranking', label: '战力排行', icon: List },
])
const activeMenu = computed(() => links.value
  .filter(link => route.path === link.to || (link.to !== '/' && route.path.startsWith(`${link.to}/`)))
  .sort((a, b) => b.to.length - a.to.length)[0]?.to || route.path)

async function logout() {
  try { await auth.logout(); await router.push('/login'); ElMessage.success('已退出登录') } catch (error) { ElMessage.error(error instanceof Error ? error.message : '退出失败') }
}
onMounted(() => system.load())
onMounted(async () => { try { const result = await getVersion(); if (result.data?.version) version.value = `v${result.data.version}` } catch {} })
watch(() => system.name, value => { document.title = `${title.value} - ${value}` }, { immediate: true })
watch(() => route.fullPath, () => { mobileNavVisible.value = false })

async function switchPortal() {
  mobileNavVisible.value = false
  await router.push(props.admin ? '/' : '/admin')
}
</script>

<template>
  <el-container class="app-shell">
    <el-aside :width="collapsed ? '64px' : '232px'" class="app-aside">
      <div class="brand"><span class="brand-mark">Q</span><span v-if="!collapsed">{{ system.name }}</span></div>
      <el-menu :default-active="activeMenu" :collapse="collapsed" router class="app-menu">
        <el-menu-item v-for="link in links" :key="link.to" :index="link.to">
          <el-icon><component :is="link.icon" /></el-icon><template #title>{{ link.label }}</template>
        </el-menu-item>
      </el-menu>
      <div class="aside-bottom">
        <el-button text :icon="SwitchButton" @click="collapsed = !collapsed" />
        <span v-if="!collapsed">{{ version }}</span>
      </div>
    </el-aside>
    <el-container>
      <el-header class="app-header">
        <div class="header-leading"><el-button class="collapse-button" text :aria-label="collapsed ? '展开侧边栏' : '收起侧边栏'" @click="collapsed = !collapsed">{{ collapsed ? '>>' : '<<' }}</el-button><el-button class="mobile-menu-button" text :icon="Menu" aria-label="打开导航" @click="mobileNavVisible = true" /><span class="header-title">{{ title }}</span></div>
        <div class="header-right"><el-button v-if="auth.user?.role === 'admin'" text :icon="Back" @click="switchPortal">{{ props.admin ? '用户端' : '管理后台' }}</el-button><el-dropdown><span class="user-menu-trigger"><el-icon><User /></el-icon>{{ auth.user?.username }}<el-icon><ArrowDown /></el-icon></span><template #dropdown><el-dropdown-menu><el-dropdown-item :icon="User" @click="router.push('/profile')">个人信息</el-dropdown-item><el-dropdown-item :icon="Lock" @click="router.push('/profile')">修改密码</el-dropdown-item><el-dropdown-item divided :icon="SwitchButton" @click="logout">退出登录</el-dropdown-item></el-dropdown-menu></template></el-dropdown></div>
      </el-header>
      <el-main class="app-main"><div class="page-wrap"><el-breadcrumb class="breadcrumb"><el-breadcrumb-item>{{ system.name }}</el-breadcrumb-item><el-breadcrumb-item>{{ title }}</el-breadcrumb-item></el-breadcrumb><RouterView /></div></el-main>
    </el-container>
    <el-drawer v-model="mobileNavVisible" direction="ltr" size="min(84vw, 320px)" :with-header="false" class="mobile-nav-drawer">
      <div class="mobile-nav-head"><span class="brand-mark">Q</span><div><strong>{{ system.name }}</strong><span>{{ props.admin ? '管理后台' : '用户中心' }}</span></div></div>
      <el-menu :default-active="activeMenu" router class="mobile-nav-menu" @select="mobileNavVisible = false">
        <el-menu-item v-for="link in links" :key="link.to" :index="link.to">
          <el-icon><component :is="link.icon" /></el-icon><template #title>{{ link.label }}</template>
        </el-menu-item>
      </el-menu>
      <div class="mobile-nav-footer"><el-button v-if="auth.user?.role === 'admin'" text :icon="Back" @click="switchPortal">{{ props.admin ? '返回用户端' : '进入管理后台' }}</el-button><span>{{ version }}</span></div>
    </el-drawer>
  </el-container>
</template>
