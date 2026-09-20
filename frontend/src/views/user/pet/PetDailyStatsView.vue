<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getPetAccounts, getPetDailyStats, type PetAccount, type PetDailyStat } from '../../../api/pet'

const route = useRoute()
const router = useRouter()
const accounts = ref<PetAccount[]>([])
const activeQQ = ref(String(route.query.qq || ''))
const range = ref<'7d' | '30d'>('7d')
const stats = ref<PetDailyStat[]>([])
const selectedPoints = ref<number[]>([])
const loading = ref(true)
const width = 480
const height = 210
const colors = ['#7c93c8', '#79b8a5', '#c88a9b', '#d9ad72', '#8c82c5']
const names = ['力量', '智力', '魅力', '金币', '战力']

function number(value: unknown) { return typeof value === 'number' ? value : null }
function metric(item: PetDailyStat, index: number) {
  if (index === 3) return number(item.gold)
  if (index === 4) return number(item.power)
  return number(item.attributes[names[index]])
}
function bounds(index: number) {
  const values = stats.value.map(item => metric(item, index)).filter((value): value is number => value !== null)
  if (!values.length) return { min: 0, max: 1 }
  const min = Math.min(...values); const max = Math.max(...values)
  return { min: min === max ? min - 1 : min, max: max === min ? max + 1 : max }
}
function y(value: number, index: number) { const bound = bounds(index); return height - ((value - bound.min) / (bound.max - bound.min)) * height }
function path(index: number) { return stats.value.map((item, itemIndex) => { const value = metric(item, index); return value === null ? '' : `${(itemIndex / Math.max(1, stats.value.length - 1)) * width},${y(value, index)}` }).filter(Boolean).join(' ') }
function selectPoint(metricIndex: number, pointIndex: number) {
  selectedPoints.value[metricIndex] = pointIndex
}
function pointX(index: number) { return (index / Math.max(1, stats.value.length - 1)) * width }
function labelX(index: number) { return Math.min(width - 48, Math.max(48, pointX(index))) }
function labelY(item: PetDailyStat, metricIndex: number) {
  const value = metric(item, metricIndex)
  if (value === null) return 0
  return Math.max(26, y(value, metricIndex) - 12)
}
function selectedPoint(metricIndex: number) {
  const index = selectedPoints.value[metricIndex]
  return index === undefined ? undefined : stats.value[index]
}
const series = computed(() => names.map((name, index) => ({ name, color: colors[index], points: path(index), min: bounds(index).min, max: bounds(index).max })))

async function load() {
  loading.value = true
  selectedPoints.value = []
  try {
    if (!accounts.value.length) accounts.value = (await getPetAccounts()).data || []
    if (!activeQQ.value) activeQQ.value = accounts.value.find(item => !item.expired)?.qq_number || ''
    if (activeQQ.value) stats.value = (await getPetDailyStats(activeQQ.value, range.value)).data || []
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '每日数据加载失败') } finally { loading.value = false }
}
function selectQQ(qq: string) { activeQQ.value = qq; void load() }
function changeRange(value: '7d' | '30d') { range.value = value; void load() }
onMounted(() => { void load() })
</script>

<template>
  <div class="page-heading"><div><h1>每日统计</h1><p>查看宠物力量、智力、魅力、金币与战力的每日变化。</p></div><el-button @click="router.push('/pet')">返回宠物</el-button></div>
  <el-card v-loading="loading">
    <div class="pet-auto-control-content">
      <div class="pet-account-switcher" role="group" aria-label="切换 QQ 账号"><button v-for="account in accounts" :key="account.qq_number" type="button" class="pet-account-button" :class="{ 'is-active': activeQQ === account.qq_number }" @click="selectQQ(account.qq_number)"><span class="pet-account-button-name">{{ account.nickname || account.qq_number }}</span><span class="pet-account-button-qq">QQ {{ account.qq_number }}</span><span class="pet-account-button-status" :class="account.online ? 'is-online' : 'is-offline'">{{ account.online ? '在线' : '离线' }}</span></button></div>
      <div class="pet-stats-toolbar"><span>北京时间每日快照</span><el-button-group><el-button size="small" :type="range === '7d' ? 'primary' : 'default'" @click="changeRange('7d')">7天</el-button><el-button size="small" :type="range === '30d' ? 'primary' : 'default'" @click="changeRange('30d')">30天</el-button></el-button-group></div>
      <el-empty v-if="!stats.length && !loading" description="暂无每日快照" />
      <div v-else class="daily-stats-chart-wrap">
        <div v-for="(line, metricIndex) in series" :key="line.name" class="daily-stats-chart-card">
          <div class="daily-stats-chart-title"><strong>{{ line.name }}</strong><span>范围：{{ line.min }} 至 {{ line.max }}</span></div>
          <svg :viewBox="`0 0 ${width + 55} ${height + 45}`" class="daily-stats-chart" role="img" :aria-label="`${line.name}每日变化折线图`">
            <g transform="translate(42,0)">
              <line x1="0" y1="0" :x2="width" y2="0" stroke="#e5e7eb" /><line x1="0" :y1="height / 2" :x2="width" :y2="height / 2" stroke="#e5e7eb" /><line x1="0" :y1="height" :x2="width" :y2="height" stroke="#d9dee8" />
              <polyline :points="line.points" fill="none" :stroke="line.color" stroke-width="3" stroke-linejoin="round" />
              <g v-for="(item, index) in stats" :key="item.date"><circle v-if="metric(item, metricIndex) !== null" class="daily-stats-chart-point-hit" :cx="pointX(index)" :cy="y(metric(item, metricIndex)!, metricIndex)" r="12" fill="transparent" tabindex="0" role="button" :aria-label="`${item.date}，${line.name}：${metric(item, metricIndex)}`" @click="selectPoint(metricIndex, index)" @keydown.enter="selectPoint(metricIndex, index)" @keydown.space.prevent="selectPoint(metricIndex, index)" /><circle v-if="metric(item, metricIndex) !== null" class="daily-stats-chart-point" :class="{ 'is-selected': selectedPoints[metricIndex] === index }" :cx="pointX(index)" :cy="y(metric(item, metricIndex)!, metricIndex)" r="4" :fill="line.color" pointer-events="none" /><text :x="pointX(index)" :y="height + 22" text-anchor="middle" font-size="11" fill="#7d8798">{{ item.date.slice(5) }}</text></g>
              <g v-if="selectedPoint(metricIndex)" class="daily-stats-chart-value" pointer-events="none"><rect :x="labelX(selectedPoints[metricIndex]!) - 47" :y="labelY(selectedPoint(metricIndex)!, metricIndex) - 24" width="94" height="24" rx="4" :fill="line.color" /><text :x="labelX(selectedPoints[metricIndex]!)" :y="labelY(selectedPoint(metricIndex)!, metricIndex) - 8" text-anchor="middle" fill="#fff" font-size="11" font-weight="700">{{ selectedPoint(metricIndex)!.date.slice(5) }} · {{ metric(selectedPoint(metricIndex)!, metricIndex) }}</text></g>
              <text x="-8" y="12" text-anchor="end" font-size="11" :fill="line.color">{{ line.max }}</text><text x="-8" :y="height" text-anchor="end" font-size="11" :fill="line.color">{{ line.min }}</text>
            </g>
          </svg>
        </div>
        <div class="pet-stats-legend"><span v-for="line in series" :key="line.name"><i :style="{ backgroundColor: line.color }" />{{ line.name }}</span><small>每项单独显示折线图，点击数据点查看具体数值</small></div>
      </div>
    </div>
  </el-card>
</template>
