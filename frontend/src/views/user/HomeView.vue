<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Wallet as WalletIcon } from '@element-plus/icons-vue'
import { getWallet, type Wallet as WalletData } from '../../api/wallet'
import { getAnnouncements, type PublicAnnouncement } from '../../api/announcement'
import { formatDateTime } from '../../utils/format'
import { useAuthStore } from '../../stores/auth'
const loading = ref(true)
const auth = useAuthStore()
const wallet = ref<WalletData | null>(null)
const announcements = ref<PublicAnnouncement[]>([])
onMounted(async () => { try { const [walletResult, announcementResult] = await Promise.all([getWallet(), getAnnouncements()]); wallet.value = walletResult.data; announcements.value = announcementResult.data || [] } catch (error) { ElMessage.error(error instanceof Error ? error.message : '首页数据加载失败') } finally { loading.value = false } })
</script>
<template><div class="page-heading"><div><h1>欢迎回来，{{ auth.user?.username }}</h1></div></div><el-row :gutter="16" class="equal-row"><el-col :xs="24" :md="10"><el-card v-loading="loading" class="wallet-summary"><div class="overview-label">当前金币</div><div class="wallet-coins">{{ wallet?.coins ?? 0 }}</div><router-link to="/wallet" class="wallet-entry"><el-icon><WalletIcon /></el-icon><span>查看钱包流水</span><span class="wallet-entry-arrow">→</span></router-link></el-card></el-col><el-col :xs="24" :md="14"><el-card v-loading="loading" class="home-announcement-card"><el-empty v-if="!announcements.length" description="暂无公告" /><div v-else class="home-latest-announcement"><div class="home-latest-announcement-head"><strong>{{ announcements[0].title }}</strong><span>{{ formatDateTime(announcements[0].created_at) }}</span></div><div class="announcement-content">{{ announcements[0].content }}</div></div><router-link to="/announcements" class="home-announcement-more">查看全部公告<span>→</span></router-link></el-card></el-col></el-row></template>
