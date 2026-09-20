<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { usePetPKStore } from '../../../stores/petPK'

const store = usePetPKStore(); const loading = ref(true); const page = ref(1); const pageSize = ref(5)
const rows = computed(() => store.allStrangers)
const pageRows = computed(() => rows.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
async function load() { loading.value = true; try { await store.loadAll() } catch (error) { ElMessage.error(error instanceof Error ? error.message : '缓存数据加载失败') } finally { loading.value = false } }
function formatTime(value?: string | null) { return value ? new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value)) : '暂无' }
function typeLabel(value: number, updated: boolean) { if (!updated) return '未获取'; return ({ 1: '智力型', 2: '力量型', 3: '魅力型', 10: '综合型' } as Record<number, string>)[value] || String(value) }
watch(pageSize, () => { page.value = 1 })
onMounted(() => void load())
</script>
<template>
  <div class="page-heading"><div><h1>陌生人</h1><p>查看数据库中缓存的 PK 陌生人宠物数据。</p></div></div>
  <el-card v-loading="loading"><el-table :data="pageRows" stripe border><el-table-column prop="pet_name" label="宠物名称" width="110" show-overflow-tooltip /><el-table-column prop="nickname" label="昵称" width="150" show-overflow-tooltip /><el-table-column prop="user_id" label="用户 ID" width="120" /><el-table-column label="宠物类型" width="110"><template #default="scope">{{ typeLabel(scope.row.dominant_type, Boolean(scope.row.power_updated_at)) }}</template></el-table-column><el-table-column prop="power" label="战力" width="90" /><el-table-column label="数据更新时间" min-width="170"><template #default="scope">{{ formatTime(scope.row.power_updated_at || scope.row.imported_at) }}</template></el-table-column></el-table><div class="pet-pk-pagination"><span>共 {{ rows.length }} 条</span><el-select v-model="pageSize" style="width: 120px"><el-option v-for="size in [5, 10, 20, 50, 100]" :key="size" :label="`${size} 条/页`" :value="size" /></el-select><el-pagination v-model:current-page="page" :page-size="pageSize" :total="rows.length" layout="prev, pager, next, jumper" /></div></el-card>
</template>
