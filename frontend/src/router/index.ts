import { createRouter, createWebHistory } from 'vue-router'
import AuthLayout from '../layouts/AuthLayout.vue'
import UserLayout from '../layouts/UserLayout.vue'
import AdminLayout from '../layouts/AdminLayout.vue'
import SystemLayout from '../layouts/SystemLayout.vue'
import LoginView from '../views/auth/LoginView.vue'
import RegisterView from '../views/auth/RegisterView.vue'
import HomeView from '../views/user/HomeView.vue'
import WalletView from '../views/user/WalletView.vue'
import ProfileView from '../views/user/ProfileView.vue'
import QQAccountView from '../views/user/QQAccountView.vue'
import PetView from '../views/user/pet/PetView.vue'
import PetAutoControlView from '../views/user/pet/PetAutoControlView.vue'
import PetDailyStatsView from '../views/user/pet/PetDailyStatsView.vue'
import PetFriendsView from '../views/user/pet/PetFriendsView.vue'
import RankingView from '../views/user/RankingView.vue'
import PetActionsView from '../views/user/pet/PetActionsView.vue'
import PetHistoryView from '../views/user/pet/PetHistoryView.vue'
import PetTasksView from '../views/user/pet/PetTasksView.vue'
import AdminHomeView from '../views/admin/AdminHomeView.vue'
import UsersView from '../views/admin/UsersView.vue'
import SettingsView from '../views/admin/SettingsView.vue'
import PushSettingsView from '../views/admin/PushSettingsView.vue'
import MokantAdminView from '../views/admin/MokantView.vue'
import DebugView from '../views/admin/DebugView.vue'
import AuditView from '../views/admin/AuditView.vue'
import RedeemCodesView from '../views/admin/RedeemCodesView.vue'
import AdminAnnouncementsView from '../views/admin/AnnouncementsView.vue'
import QQAccountsView from '../views/admin/QQAccountsView.vue'
import QQPlansView from '../views/admin/QQPlansView.vue'
import StrangerPetsView from '../views/admin/StrangerPetsView.vue'
import InternalPetsView from '../views/admin/InternalPetsView.vue'
import AnnouncementsView from '../views/user/AnnouncementsView.vue'
import ForbiddenView from '../views/system/ForbiddenView.vue'
import NotFoundView from '../views/system/NotFoundView.vue'
import ErrorView from '../views/system/ErrorView.vue'
import { useAuthStore } from '../stores/auth'

const userChildren = [
  { path: '', component: HomeView, meta: { title: '首页', requiresAuth: true } },
  { path: 'wallet', component: WalletView, meta: { title: '我的钱包', requiresAuth: true } },
  { path: 'profile', component: ProfileView, meta: { title: '个人信息', requiresAuth: true } },
  { path: 'announcements', component: AnnouncementsView, meta: { title: '公告', requiresAuth: true } },
  { path: 'qq-account', component: QQAccountView, meta: { title: 'QQ 账号', requiresAuth: true } },
  { path: 'pet', component: PetView, meta: { title: 'QQ 宠物', requiresAuth: true } },
  { path: 'pet/auto-control', component: PetAutoControlView, meta: { title: '自动控制', requiresAuth: true } },
  { path: 'pet/daily-stats', component: PetDailyStatsView, meta: { title: '每日统计', requiresAuth: true } },
  { path: 'pet/friends', component: PetFriendsView, meta: { title: '好友管理', requiresAuth: true } },
  { path: 'ranking', component: RankingView, meta: { title: '战力排行榜', requiresAuth: true } },
  { path: 'pet/actions', component: PetActionsView, meta: { title: '宠物操作', requiresAuth: true } },
  { path: 'pet/history', component: PetHistoryView, meta: { title: '操作记录', requiresAuth: true } },
  { path: 'pet/tasks', component: PetTasksView, meta: { title: '宠物任务', requiresAuth: true } },
]

const adminChildren = [
  { path: '', component: AdminHomeView, meta: { title: '管理概览', requiresAuth: true, requiresAdmin: true } },
  { path: 'users', component: UsersView, meta: { title: '用户管理', requiresAuth: true, requiresAdmin: true } },
  { path: 'stranger-pets', component: StrangerPetsView, meta: { title: '陌生人管理', requiresAuth: true, requiresAdmin: true } },
  { path: 'internal-pets', component: InternalPetsView, meta: { title: '内部宠物', requiresAuth: true, requiresAdmin: true } },
  { path: 'settings', component: SettingsView, meta: { title: '系统设置', requiresAuth: true, requiresAdmin: true } },
  { path: 'push-settings', component: PushSettingsView, meta: { title: '推送设置', requiresAuth: true, requiresAdmin: true } },
  { path: 'mokant', component: MokantAdminView, meta: { title: '萌卡 NT', requiresAuth: true, requiresAdmin: true } },
  { path: 'debug', component: DebugView, meta: { title: 'WS 调试', requiresAuth: true, requiresAdmin: true } },
  { path: 'qq-accounts', component: QQAccountsView, meta: { title: 'QQ 管理', requiresAuth: true, requiresAdmin: true } },
  { path: 'qq-plans', component: QQPlansView, meta: { title: 'QQ 套餐', requiresAuth: true, requiresAdmin: true } },
  { path: 'audit', component: AuditView, meta: { title: '审计日志', requiresAuth: true, requiresAdmin: true } },
  { path: 'redeem-codes', component: RedeemCodesView, meta: { title: '卡密管理', requiresAuth: true, requiresAdmin: true } },
  { path: 'announcements', component: AdminAnnouncementsView, meta: { title: '公告管理', requiresAuth: true, requiresAdmin: true } },
]

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: UserLayout, children: userChildren },
    { path: '/admin', component: AdminLayout, children: adminChildren },
    { path: '/login', component: AuthLayout, children: [{ path: '', component: LoginView, meta: { title: '登录', guestOnly: true } }] },
    { path: '/register', component: AuthLayout, children: [{ path: '', component: RegisterView, meta: { title: '注册', guestOnly: true } }] },
    { path: '/403', component: SystemLayout, children: [{ path: '', component: ForbiddenView, meta: { title: '无权访问' } }] },
    { path: '/404', component: SystemLayout, children: [{ path: '', component: NotFoundView, meta: { title: '页面不存在' } }] },
    { path: '/error', component: SystemLayout, children: [{ path: '', component: ErrorView, meta: { title: '系统错误' } }] },
    { path: '/:pathMatch(.*)*', redirect: '/404' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.initialized) await auth.loadMe()
  if (to.meta.requiresAuth && !auth.isAuthenticated) return { path: '/login', query: { redirect: to.fullPath } }
  if (to.meta.requiresAdmin && auth.user?.role !== 'admin') return '/403'
  if (to.meta.guestOnly && auth.isAuthenticated) return '/'
  return true
})

export default router
