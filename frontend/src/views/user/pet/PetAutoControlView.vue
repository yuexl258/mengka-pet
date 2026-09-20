<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { createRequestId } from '../../../api/client'
import { getAutoPKConfig, getAutoPKLogs, getAutoPKState, getPetAccounts, getPetActivityOptions, getPetActivityOverview, getPetActivityStatus,
  getPetAutoControlConfig, getPetAutoControlInventory, getPetAutoControlLogs, getPetAutoControlState, savePetAutoControlConfig, startAutoPK, stopAutoPK, type AutoPKConfig, type AutoPKLog, type AutoPKState, type PetAccount, type PetActivityOption, type PetActivityStatus, type PetActivityType, type PetAutoControlConfig, type PetAutoControlInventory, type PetAutoControlLog, type PetAutoControlState } from '../../../api/pet'

type ExecutionLog = {
  key: string
  tag: string
  tagType: 'success' | 'warning' | 'info'
  label: string
  message: string
  created_at: string
}

type PlanItem = {
  id: string
  activity_type: PetActivityType
  control_mode: 'count' | 'duration'
  count: number
  hours: number
  learning_target: string
  work_career_type?: number
  work_option: string
  work_sub_event_type?: number
  work_options: PetActivityOption[]
  work_options_loading: boolean
}
const route = useRoute(); const router = useRouter()
const accounts = ref<PetAccount[]>([]); const activeQQ = ref(String(route.query.qq || ''))
const config = ref<PetAutoControlConfig | null>(null); const state = ref<PetAutoControlState | null>(null); const autoPKConfig = ref<AutoPKConfig>({ enabled: false, target_starts: 10, start_time: '01:00' }); const autoPKState = ref<AutoPKState | null>(null); const logs = ref<ExecutionLog[]>([]); const status = ref<PetActivityStatus | null>(null); const inventory = ref<PetAutoControlInventory | null>(null)
const saving = ref(false); const pkSaving = ref(false); const loading = ref(true); const labels: Record<PetActivityType, string> = { school: '学习', work: '打工', adventure: '冒险' }
const enabled = ref(false); const autoPKEnabled = ref(false); const order = ref<PetActivityType[]>(['school', 'work', 'adventure']); const plan = ref<PlanItem[]>([])
const workCareers = ref<{ career_type: number; name?: string; status_code?: number; message?: string }[]>([]); const countdown = ref(0); const feedEnabled = ref(false); const feedThreshold = ref(30); const batheEnabled = ref(false); const batheThreshold = ref(30); const probability = ref(100); const autoPokeEnabled = ref(false); const autoPokeStartTime = ref('00:00'); let timer: ReturnType<typeof setInterval> | null = null; let runtimeTimer: ReturnType<typeof setInterval> | null = null

function applyForm(x: PetAutoControlConfig) {
  enabled.value = x.enabled; order.value = x.activity_order || order.value
  plan.value = (x.plan || []).map((item, index) => ({ id: item.id || `legacy-plan-${index}`, activity_type: item.activity, control_mode: item.control_mode || 'duration', count: item.count || 1, hours: item.hours || 1, learning_target: item.learning_target || '力量', work_career_type: item.work_career_type, work_option: item.work_option || '', work_sub_event_type: item.work_sub_event_type, work_options: [], work_options_loading: false }))
  feedEnabled.value = x.auto_feed_enabled; feedThreshold.value = x.auto_feed_threshold ?? 30; batheEnabled.value = x.auto_bathe_enabled; batheThreshold.value = x.auto_bathe_threshold ?? 30; probability.value = x.encourage_probability ?? 100; autoPokeEnabled.value = x.auto_poke_enabled; autoPokeStartTime.value = '00:00'
}
async function loadWorkCareers(qq: string) {
  const overview = await getPetActivityOverview(qq, 'work')
  const entries = Array.isArray(overview.data?.entries) ? overview.data.entries : []
  workCareers.value = entries.filter(item => item && typeof item === 'object' && typeof (item as Record<string, unknown>).career_type === 'number' && (item as Record<string, unknown>).name !== '???').map(item => item as { career_type: number; name?: string; status_code?: number; message?: string })
}
async function fetchWorkOptions(qq: string, careerType: number) {
  return ((await getPetActivityOptions(qq, 'work', careerType)).data?.options || []).filter(item => item.name)
}
async function initializeWorkPlan(item: PlanItem, qq: string) {
  if (item.activity_type !== 'work') return
  item.work_options_loading = true
  try {
    if (item.work_career_type !== undefined) {
      item.work_options = await fetchWorkOptions(qq, item.work_career_type)
      return
    }
    if (!item.work_option) return
    for (const career of workCareers.value.filter(candidate => candidate.status_code === undefined || candidate.status_code === 0)) {
      const options = await fetchWorkOptions(qq, career.career_type)
      if (options.some(option => option.name === item.work_option && (item.work_sub_event_type === undefined || option.sub_event_type === item.work_sub_event_type))) {
        item.work_career_type = career.career_type
        item.work_options = options
        return
      }
    }
  } finally { item.work_options_loading = false }
}
async function changeWorkCareer(item: PlanItem) {
  item.work_option = ''
  item.work_sub_event_type = undefined
  item.work_options = []
  if (!activeQQ.value || item.work_career_type === undefined) return
  item.work_options_loading = true
  try { item.work_options = await fetchWorkOptions(activeQQ.value, item.work_career_type) } finally { item.work_options_loading = false }
}

async function load() {
  loading.value = true
  try {
    if (!accounts.value.length) accounts.value = (await getPetAccounts()).data || []
    if (!activeQQ.value) activeQQ.value = accounts.value.find(item => !item.expired)?.qq_number || ''
    if (!activeQQ.value) return
    const qq = activeQQ.value
    inventory.value = null
    void getPetAutoControlInventory(qq).then(response => {
      if (qq === activeQQ.value) inventory.value = response.data
    }).catch(() => {})
    const [c, s, pkc, pks, controlLogs, pkLogs, a] = await Promise.all([getPetAutoControlConfig(qq), getPetAutoControlState(qq), getAutoPKConfig(qq), getAutoPKState(qq), getPetAutoControlLogs(qq, 50), getAutoPKLogs(qq, 50), getPetActivityStatus(qq)])
    await loadWorkCareers(qq)
    if (qq !== activeQQ.value) return
    config.value = c.data; state.value = s.data; autoPKConfig.value = { enabled: Boolean(pkc.data?.enabled), target_starts: 10, start_time: '01:00' }; autoPKEnabled.value = autoPKConfig.value.enabled; autoPKState.value = pks.data; logs.value = mergeLogs(controlLogs.data || [], pkLogs.data || []); status.value = a.data
    countdown.value = Math.max(0, Number(a.data?.remaining_seconds) || 0)
    if (config.value) {
      applyForm(config.value)
      await Promise.all(plan.value.map(item => initializeWorkPlan(item, qq)))
    }
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '自动控制加载失败') } finally { loading.value = false }
}
async function refreshLogs() {
  const qq = activeQQ.value.trim()
  if (!qq) return
  try {
    const [controlLogs, pkLogs] = await Promise.all([getPetAutoControlLogs(qq, 50), getAutoPKLogs(qq, 50)])
    if (qq === activeQQ.value) logs.value = mergeLogs(controlLogs.data || [], pkLogs.data || [])
  } catch { /* 日志刷新失败时保留已有内容 */ }
}
function movePlan(index: number, offset: number) { const target = index + offset; if (target < 0 || target >= plan.value.length) return; const next = [...plan.value]; const [item] = next.splice(index, 1); next.splice(target, 0, item); plan.value = next }
function addPlan() { plan.value.push({ id: createRequestId(), activity_type: 'school', control_mode: 'count', count: 1, hours: 1, learning_target: '力量', work_career_type: undefined, work_option: '', work_sub_event_type: undefined, work_options: [], work_options_loading: false }) }
function removePlan(index: number) { plan.value.splice(index, 1) }
function changeActivity(item: PlanItem) {
  item.work_career_type = undefined
  item.work_option = ''
  item.work_sub_event_type = undefined
  item.work_options = []
}
function changeWorkOption(item: PlanItem) {
  const option = item.work_options.find(candidate => candidate.name === item.work_option)
  item.work_sub_event_type = option?.sub_event_type
}
async function save() {
  if (!activeQQ.value || !plan.value.length) return ElMessage.warning('请至少配置一项每日计划')
  if (plan.value.some(item => item.control_mode === 'count' && item.count < 1)) return ElMessage.warning('每条按次数计划的次数须大于 0')
  if (plan.value.some(item => item.control_mode === 'duration' && (item.hours < 1 || item.hours > 24))) return ElMessage.warning('每条按时间计划的小时数须为 1 到 24 的整数')
  if (plan.value.some(item => item.activity_type === 'work' && (item.work_career_type === undefined || !item.work_option || item.work_sub_event_type === undefined))) return ElMessage.warning('请为每条打工计划选择职业分类和具体项目')
  saving.value = true
  try {
    const old = config.value
    const oldPlan = new Map((old?.plan || []).map(item => [item.id, item]))
    const nextPlan = plan.value.map(item => { const previous = oldPlan.get(item.id); const count = item.control_mode === 'count' ? Math.trunc(item.count) : 0; const hours = item.control_mode === 'duration' ? Math.trunc(item.hours) : 0; const learningTarget = item.activity_type === 'school' ? item.learning_target : undefined; const workCareerType = item.activity_type === 'work' ? item.work_career_type : undefined; const workOption = item.activity_type === 'work' ? item.work_option : undefined; const workSubEventType = item.activity_type === 'work' ? item.work_sub_event_type : undefined; const unchanged = previous?.activity === item.activity_type && previous?.control_mode === item.control_mode && previous?.count === count && previous?.hours === hours && previous?.learning_target === learningTarget && previous?.work_career_type === workCareerType && previous?.work_option === workOption && previous?.work_sub_event_type === workSubEventType; return { id: item.id, activity: item.activity_type, control_mode: item.control_mode, count, hours, learning_target: learningTarget, work_career_type: workCareerType, work_option: workOption, work_sub_event_type: workSubEventType, executed_count: unchanged ? previous?.executed_count || 0 : 0, executed_seconds: unchanged ? previous?.executed_seconds || 0 : 0, completed: unchanged ? previous?.completed || false : false } })
    const next: PetAutoControlConfig = { enabled: enabled.value, plan: nextPlan, learning_target: '', activity_order: order.value, school_count: 0, work_count: 0, adventure_count: 0, school_remaining: 0, work_remaining: 0, adventure_remaining: 0, school_executed: 0, work_executed: 0, adventure_executed: 0, school_control_mode: 'count', work_control_mode: 'count', adventure_control_mode: 'count', school_duration_seconds: 0, work_duration_seconds: 0, adventure_duration_seconds: 0, school_duration_executed: 0, work_duration_executed: 0, adventure_duration_executed: 0, work_option: '', auto_feed_enabled: feedEnabled.value, auto_feed_threshold: feedThreshold.value, auto_feed_food: '饼干', auto_bathe_enabled: batheEnabled.value, auto_bathe_threshold: batheThreshold.value, auto_bathe_item: '香皂片', encourage_probability: probability.value, auto_poke_enabled: autoPokeEnabled.value, auto_poke_start_time: autoPokeStartTime.value, auto_poke_daily_count: old?.auto_poke_daily_count || 0, auto_poke_interval_seconds: 1, auto_poke_last_run_at: old?.auto_poke_last_run_at || '', auto_poke_last_friend_qq: old?.auto_poke_last_friend_qq || '', last_reset_date: old?.last_reset_date || '' }
    config.value = (await savePetAutoControlConfig(activeQQ.value, next)).data
    if (autoPKConfig.value) {
      if (autoPKEnabled.value) await startAutoPK(activeQQ.value)
      else await stopAutoPK(activeQQ.value)
      autoPKConfig.value = (await getAutoPKConfig(activeQQ.value)).data || autoPKConfig.value
      autoPKEnabled.value = autoPKConfig.value.enabled
      autoPKState.value = (await getAutoPKState(activeQQ.value)).data
    }
    ElMessage.success('自动控制配置已保存')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存失败') } finally { saving.value = false }
}
function formatDuration(seconds: number | undefined) { const hours = Math.max(0, Number(seconds) || 0) / 3600; return `${Number.isInteger(hours) ? hours : Number(hours.toFixed(2))} 小时` }
function inventoryStock(value: unknown, name: string): number { if (Array.isArray(value)) return value.reduce((total, item) => total || inventoryStock(item, name), 0); if (value && typeof value === 'object') { const item = value as Record<string, unknown>; const itemName = ['name', 'food_name', 'item_name', 'title'].map(key => item[key]).find(x => typeof x === 'string'); if (itemName === name) { const stock = ['stock', 'count', 'quantity', 'balance', 'amount', 'num'].map(key => item[key]).find(x => typeof x === 'number' || typeof x === 'string'); return Number(stock) || 0 } return Object.values(item).reduce<number>((total, child) => total || inventoryStock(child, name), 0) } return 0 }
function findItemID(value: unknown, name: string): string { if (Array.isArray(value)) return value.map(item => findItemID(item, name)).find(Boolean) || ''; if (value && typeof value === 'object') { const item = value as Record<string, unknown>; if (typeof item.name === 'string' && item.name.trim() === name.trim() && (typeof item.item_id === 'string' || typeof item.item_id === 'number')) return String(item.item_id).trim(); return Object.values(item).map(child => findItemID(child, name)).find(Boolean) || '' } return '' }
function inventoryStockByID(value: unknown, itemID: string): number { if (Array.isArray(value)) return value.reduce((total, item) => total || inventoryStockByID(item, itemID), 0); if (value && typeof value === 'object') { const item = value as Record<string, unknown>; if ((typeof item.item_id === 'string' || typeof item.item_id === 'number') && String(item.item_id).trim() === itemID) return Number(item.count) || 0; return Object.values(item).reduce<number>((total, child) => total || inventoryStockByID(child, itemID), 0) } return 0 }
function bathInventoryStock(name: string): number { const itemID = findItemID(inventory.value?.bath_catalog, name); return itemID ? inventoryStockByID(inventory.value?.bath_inventory, itemID) : inventoryStock(inventory.value?.bath_inventory, name) }
function executed(type: PetActivityType) { return config.value?.[`${type}_executed` as 'school_executed'] || 0 }
function durationExecuted(type: PetActivityType) { return config.value?.[`${type}_duration_executed` as 'school_duration_executed'] || 0 }
const currentProgress = computed(() => {
  const items = config.value?.plan || []
  const index = items.findIndex(item => !item.completed)
  if (index < 0) return null
  const item = items[index]
  if (item.control_mode === 'count') {
    const current = Math.min(item.executed_count || 0, item.count)
    return { index: index + 1, activity: item.activity, text: `${current}次 / ${item.count}次`, percentage: item.count ? Math.min(100, current / item.count * 100) : 0 }
  }
  const current = Math.min(item.executed_seconds || 0, item.hours * 3600)
  return { index: index + 1, activity: item.activity, text: `${formatDuration(current)} / ${item.hours} 小时`, percentage: item.hours ? Math.min(100, current / (item.hours * 3600) * 100) : 0 }
})
function logLabel(_log: AutoPKLog) { return 'PK已提交' }
function controlLogLabel(log: PetAutoControlLog) { const activity = log.activity ? labels[log.activity] : '自动任务'; const option = log.option_name ? ` · ${log.option_name}` : ''; return `${activity}${option}` }
function controlLogTag(log: PetAutoControlLog) { if (log.action === 'start') return { tag: '已启动', tagType: 'info' as const }; if (log.action === 'encourage') return { tag: log.message.includes('进行中') ? '进行中' : '鼓励成功', tagType: 'warning' as const }; return { tag: '已完成', tagType: 'success' as const } }
function mergeLogs(controlLogs: PetAutoControlLog[], pkLogs: AutoPKLog[]): ExecutionLog[] { const taskLogs = controlLogs.filter(log => ['start', 'encourage', 'complete', 'poke'].includes(log.action) && log.success).map(log => { if (log.action === 'poke') return { key: `control-${log.id}`, tag: '踩一踩', tagType: 'success' as const, label: log.option_name ? `好友 QQ ${log.option_name}` : '好友宠物', message: log.message, created_at: log.created_at }; const tag = controlLogTag(log); return { key: `control-${log.id}`, tag: tag.tag, tagType: tag.tagType, label: controlLogLabel(log), message: log.message, created_at: log.created_at } }); const battleLogs = pkLogs.filter(log => log.action === 'start' && log.success).map(log => ({ key: `pk-${log.id}`, tag: 'PK', tagType: 'success' as const, label: logLabel(log), message: '', created_at: log.created_at })); return [...taskLogs, ...battleLogs].sort((left, right) => right.created_at.localeCompare(left.created_at)).slice(0, 50) }
async function toggleAutoPK() {
  if (!activeQQ.value || !autoPKConfig.value) return
  pkSaving.value = true
  try {
    if (autoPKEnabled.value) await startAutoPK(activeQQ.value)
    else await stopAutoPK(activeQQ.value)
    autoPKConfig.value = (await getAutoPKConfig(activeQQ.value)).data || autoPKConfig.value
    autoPKEnabled.value = autoPKConfig.value.enabled
    autoPKState.value = (await getAutoPKState(activeQQ.value)).data
    ElMessage.success(autoPKEnabled.value ? '自动 PK 已启用' : '自动 PK 已停用')
  } catch (error) {
    autoPKEnabled.value = !autoPKEnabled.value
    ElMessage.error(error instanceof Error ? error.message : '自动 PK 状态更新失败')
  } finally { pkSaving.value = false }
}
onMounted(() => { void load(); timer = setInterval(() => { if (countdown.value > 0) countdown.value -= 1 }, 1000); runtimeTimer = setInterval(() => void refreshLogs(), 15000) }); onUnmounted(() => { if (timer) clearInterval(timer); if (runtimeTimer) clearInterval(runtimeTimer) })
</script>
<template>
  <div class="page-heading"><div><h1>自动控制</h1><p>按活动次数或累计时长依次执行自动任务。</p></div><el-button @click="router.push('/pet')">返回宠物</el-button></div>
  <el-card v-loading="loading"><div class="pet-auto-control-content">
    <div class="pet-account-switcher"><button v-for="account in accounts" :key="account.qq_number" type="button" class="pet-account-button" :class="{ 'is-active': activeQQ === account.qq_number }" @click="activeQQ = account.qq_number; load()"><span class="pet-account-button-name">{{ account.nickname || account.qq_number }}</span><span class="pet-account-button-qq">QQ {{ account.qq_number }}</span></button></div>
    <el-alert title="每日活动计划按列表顺序执行，每条计划可按次数或累计时长完成；活动成功启动后计入 1 次，并按实际运行时间累计。每天北京时间 00:00 重置当日进度。自动 PK 每天北京时间 01:00 开始，固定执行 10 次。" type="info" :closable="false" />
    <div class="pet-auto-control-overview"><div class="pet-auto-control-enable"><div><strong>自动控制配置</strong><span>配置状态：{{ enabled ? '已启用' : '已停用' }}</span><small>启用后按每日计划自动执行</small></div><el-switch v-model="enabled" /></div><div class="pet-auto-control-current-item"><span>运行状态</span><el-tag size="small" :type="state?.running ? 'success' : 'info'">{{ state?.running ? '运行中' : '等待中' }}</el-tag><small>{{ state?.message || '等待自动控制任务' }}</small></div><div class="pet-auto-control-current-item"><span>当前活动类型</span><strong>{{ state?.current_activity ? labels[state.current_activity] : '暂无任务' }}</strong><small>当前活动项目：{{ state?.current_option || (status?.story_id ? '活动执行中' : '暂无进行中的活动') }} · 倒计时：{{ countdown > 0 ? `${Math.floor(countdown / 60)}分${countdown % 60}秒` : '已结束/暂无活动' }}</small></div></div>
    <div class="pet-auto-control-section pet-auto-poke-section"><div class="pet-auto-poke-heading"><div><div class="pet-auto-control-section-title">自动踩一踩</div><small>每天北京时间 00:00 开始执行，每位好友当天只踩一次；当天完成后等待次日自动执行。</small></div><div class="pet-auto-poke-actions"><el-switch v-model="autoPokeEnabled" active-text="已启用" inactive-text="未启用" /><el-button type="primary" size="small" :loading="saving" @click="save">保存踩一踩设置</el-button></div></div><div class="pet-auto-poke-form"><label><span>每日开始时间</span><strong>00:00</strong><small>每天北京时间固定执行</small></label></div><div class="pet-auto-poke-stats"><div><span>今日已执行</span><strong>{{ config?.auto_poke_daily_count || 0 }} 次</strong></div><div><span>最近执行</span><strong>{{ config?.auto_poke_last_run_at || '暂无' }}</strong></div><div><span>最近好友</span><strong>{{ config?.auto_poke_last_friend_qq ? `QQ ${config.auto_poke_last_friend_qq}` : '暂无' }}</strong></div></div></div>
    <div class="pet-auto-pk-section"><div class="pet-auto-pk-heading"><div><div class="pet-auto-control-section-title">自动 PK</div><small>仅设置开关。每天北京时间 01:00 开始，固定执行 10 次；陌生人由管理员统一维护。</small></div><el-switch v-model="autoPKEnabled" :loading="pkSaving" active-text="已启用" inactive-text="未启用" @change="toggleAutoPK" /></div><div class="pet-auto-control-stats"><div><span>开始时间</span><strong>01:00</strong><small>每天北京时间</small></div><div><span>每日目标</span><strong>10 次</strong><small>系统固定</small></div><div><span>今日已执行</span><strong>{{ autoPKState?.today_starts || 0 }} 次</strong><small>成功开始数量</small></div></div></div><div class="pet-auto-control-section"><div class="pet-auto-control-section-title">今日执行统计</div><div class="pet-auto-control-stats"><div v-for="type in (['school', 'work', 'adventure'] as PetActivityType[])" :key="type"><span>{{ labels[type] }}</span><strong>{{ executed(type) }} 次</strong><small>累计 {{ formatDuration(durationExecuted(type)) }}</small></div></div><div class="pet-auto-current-progress"><div class="pet-auto-current-progress-head"><span>当前条目执行进度</span><strong v-if="currentProgress">第 {{ currentProgress.index }} 项 · {{ labels[currentProgress.activity] }} · {{ currentProgress.text }}</strong><strong v-else>今日计划已完成</strong></div><div class="pet-auto-rainbow-progress"><div :style="{ width: `${currentProgress?.percentage || 100}%` }"></div></div></div></div>
    <div class="pet-auto-control-section pet-auto-control-control-section"><div class="pet-auto-control-section-title pet-auto-control-section-title-row"><span>每日活动计划</span><small>按列表顺序执行，可按次数或累计时长完成</small><el-button size="small" type="primary" @click="addPlan">新增条目</el-button></div><el-empty v-if="!plan.length" description="暂无计划条目" :image-size="48" /><div v-for="(item,index) in plan" :key="item.id" class="pet-auto-control-plan-item"><span class="pet-auto-control-plan-index">{{ index + 1 }}</span><div class="pet-auto-control-plan-main"><div class="pet-auto-control-plan-fields"><el-select v-model="item.activity_type" placeholder="活动类型" @change="changeActivity(item)"><el-option v-for="(label,type) in labels" :key="type" :label="label" :value="type" /></el-select><el-radio-group v-model="item.control_mode"><el-radio-button value="count">按次数</el-radio-button><el-radio-button value="duration">按时长</el-radio-button></el-radio-group><el-input-number v-if="item.control_mode === 'count'" v-model="item.count" :min="1" :precision="0" /><el-input-number v-else v-model="item.hours" :min="1" :max="24" :precision="0" /><span class="pet-auto-control-plan-unit">{{ item.control_mode === 'count' ? '次' : '小时' }}</span><el-select v-if="item.activity_type === 'school'" v-model="item.learning_target" placeholder="学习属性"><el-option label="力量" value="力量" /><el-option label="智力" value="智力" /><el-option label="魅力" value="魅力" /><el-option label="随机" value="随机" /></el-select><el-select v-if="item.activity_type === 'work'" v-model="item.work_career_type" placeholder="选择职业分类" @change="changeWorkCareer(item)"><el-option v-for="career in workCareers" :key="career.career_type" :label="career.name || `职业 ${career.career_type}`" :value="career.career_type" :disabled="career.status_code !== undefined && career.status_code !== 0"><template #default><span>{{ career.name || `职业 ${career.career_type}` }}</span><small v-if="career.status_code !== undefined && career.status_code !== 0"> · {{ career.message || '暂不可用' }}</small></template></el-option></el-select><el-select v-if="item.activity_type === 'work'" v-model="item.work_option" placeholder="选择打工项目" :disabled="item.work_career_type === undefined" :loading="item.work_options_loading" @change="changeWorkOption(item)"><el-option v-for="option in item.work_options" :key="`${option.name}-${option.sub_event_type}`" :label="`${option.name}（${option.duration || option.duration_seconds || 0}）`" :value="option.name" /></el-select></div><div class="pet-auto-control-plan-actions"><el-button text :disabled="index === 0" @click="movePlan(index,-1)">上移</el-button><el-button text :disabled="index === plan.length - 1" @click="movePlan(index,1)">下移</el-button><el-button text type="danger" @click="removePlan(index)">删除</el-button></div></div></div></div>
    <div class="pet-auto-control-section"><div class="pet-auto-control-section-title pet-auto-control-section-title-row"><span>活动鼓励概率</span><small>活动进行期间，每次检查时触发鼓励的概率</small><strong>{{ probability }}%</strong></div><el-slider v-model="probability" :min="0" :max="100" show-input /></div>
    <div class="pet-auto-control-section pet-auto-care-section"><div class="pet-auto-control-section-title"><span>自动喂食 / 洗护</span><small>低于设定阈值时自动照顾宠物</small></div><div class="pet-auto-care-grid"><div class="pet-auto-care-card pet-auto-care-card-food"><div class="pet-auto-care-head"><span class="pet-auto-care-icon">食</span><div><strong>自动喂食</strong><small>保持宠物饱腹状态</small></div><el-switch v-model="feedEnabled" /></div><div class="pet-auto-care-stock"><span>当前库存</span><strong>{{ inventoryStock(inventory?.food_inventory, '饼干') }}<small> 份饼干</small></strong></div><div class="pet-auto-care-threshold"><label>饥饿值低于</label><el-input-number v-model="feedThreshold" :min="0" :max="100" :precision="0" controls-position="right" /><span>时喂食</span></div></div><div class="pet-auto-care-card pet-auto-care-card-bath"><div class="pet-auto-care-head"><span class="pet-auto-care-icon pet-auto-care-icon-image"><img src="https://tianquan.gtimg.cn/pet355/item/ff236ba3f3e615b0/preview.png" alt="香皂片"></span><div><strong>自动洗护</strong><small>维持宠物清洁状态</small></div><el-switch v-model="batheEnabled" /></div><div class="pet-auto-care-stock"><span>当前库存</span><strong>{{ bathInventoryStock('香皂片') }}<small> 片香皂</small></strong></div><div class="pet-auto-care-threshold"><label>清洁值低于</label><el-input-number v-model="batheThreshold" :min="0" :max="100" :precision="0" controls-position="right" /><span>时洗护</span></div></div></div></div>
    <div class="pet-auto-control-status"><span>运行状态</span><strong>{{ state?.message || '未启动自动控制' }}</strong></div><el-button type="primary" :loading="saving" @click="save">保存配置</el-button>
    <div class="pet-auto-control-section"><div class="pet-auto-control-section-title">执行日志</div><el-empty v-if="!logs.length" description="暂无执行日志" :image-size="48" /><div v-else class="pet-auto-control-logs"><div v-for="log in logs" :key="log.key" class="pet-auto-control-log"><el-tag :type="log.tagType" size="small">{{ log.tag }}</el-tag><span>{{ log.label }}<template v-if="log.message"> · {{ log.message }}</template></span><small>{{ log.created_at }}</small></div></div></div>
  </div></el-card>
</template>
