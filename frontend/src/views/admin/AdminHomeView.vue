<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getSettings, getSystemStatus, getUserTrend, getUsers, type AdminSettings, type PageResult, type AdminUser, type SystemStatus, type UserTrendItem } from '../../api/admin'
import { formatDateTime } from '../../utils/format'

const loading = ref(true)
const users = ref<PageResult<AdminUser> | null>(null)
const settings = ref<AdminSettings | null>(null)
const trend = ref<UserTrendItem[]>([])
const systemStatus = ref<SystemStatus | null>(null)
const range = ref('30d')
const chartPoints = computed(() => {
  if (!trend.value.length) return ''
  const max = Math.max(...trend.value.map(item => item.total), 1)
  return trend.value.map((item, index) => `${(index / Math.max(trend.value.length - 1, 1)) * 100},${100 - (item.total / max) * 86 - 7}`).join(' ')
})
const chartLabels = computed(() => trend.value.filter((_, index) => index === 0 || index === trend.value.length - 1 || index % Math.max(Math.floor(trend.value.length / 4), 1) === 0))
async function loadTrend() { try { trend.value = (await getUserTrend(range.value)).data?.items || [] } catch (error) { ElMessage.error(error instanceof Error ? error.message : '用户趋势加载失败') } }
onMounted(async () => {
  try { const [userResult, settingResult, statusResult] = await Promise.all([getUsers({ page: 1, page_size: 1 }), getSettings(), getSystemStatus()]); users.value = userResult.data; settings.value = settingResult.data; systemStatus.value = statusResult.data; await loadTrend() } catch (error) { ElMessage.error(error instanceof Error ? error.message : '管理数据加载失败') } finally { loading.value = false }
})
</script>

<template><div class="page-heading"><div><h1>管理概览</h1></div><el-tag type="success">管理员</el-tag></div><el-skeleton v-if="loading" :rows="4" animated /><template v-else><el-row :gutter="16" class="equal-row"><el-col :xs="24" :sm="8"><el-card><el-statistic title="用户总数" :value="users?.total || 0" /></el-card></el-col><el-col :xs="24" :sm="8"><el-card><div class="overview-label">注册状态</div><div class="overview-value">{{ settings?.registration_enabled ? '开放' : '关闭' }}</div></el-card></el-col><el-col :xs="24" :sm="8"><el-card><el-statistic title="注册赠送金币" :value="settings?.register_gift_coins || 0" /></el-card></el-col></el-row><el-card class="dashboard-card"><div class="dashboard-toolbar"><div><div class="section-title">用户注册趋势</div><div class="form-help">累计注册用户数量</div></div><el-radio-group v-model="range" size="small" @change="loadTrend"><el-radio-button label="7d">7 天</el-radio-button><el-radio-button label="30d">30 天</el-radio-button><el-radio-button label="90d">90 天</el-radio-button></el-radio-group></div><div class="line-chart" v-if="trend.length"><svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="用户注册趋势折线图"><line x1="0" y1="93" x2="100" y2="93" /><polyline :points="chartPoints" /></svg><div class="chart-labels"><span v-for="item in chartLabels" :key="item.date">{{ item.date.slice(5) }}</span></div></div><el-empty v-else description="暂无用户趋势数据" /></el-card><el-card class="dashboard-card"><div class="dashboard-toolbar"><div><div class="section-title">系统运行状态</div><div class="form-help">仅管理员可见</div></div><el-tag :type="systemStatus?.status === 'ok' ? 'success' : 'danger'">{{ systemStatus?.status === 'ok' ? '运行正常' : '异常' }}</el-tag></div><el-descriptions v-if="systemStatus" :column="3" border><el-descriptions-item label="数据库">{{ systemStatus.database === 'ok' ? '正常' : '异常' }}</el-descriptions-item><el-descriptions-item label="数据库延迟">{{ systemStatus.database_latency_ms }} ms</el-descriptions-item><el-descriptions-item label="数据库占用">{{ systemStatus.database_size_mb }} MB</el-descriptions-item><el-descriptions-item label="内存占用">{{ systemStatus.memory_alloc_mb }} MB</el-descriptions-item><el-descriptions-item label="运行环境">{{ systemStatus.environment }}</el-descriptions-item><el-descriptions-item label="检查时间">{{ formatDateTime(systemStatus.checked_at) }}</el-descriptions-item></el-descriptions></el-card></template></template>
