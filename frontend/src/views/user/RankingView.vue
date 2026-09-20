<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getRankings, type PetRankingItem } from '../../api/pet'
import { formatDateTime } from '../../utils/format'

const loading = ref(false)
const rankings = ref<PetRankingItem[]>([])
const petTypeNames: Record<number, string> = {
  1: '智力型',
  2: '力量型',
  3: '魅力型',
  10: '综合型',
}

async function load() {
  loading.value = true
  try {
    const result = await getRankings()
    rankings.value = result.data || []
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '战力排行榜加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-heading">
    <div><h1>战力排行榜</h1></div>
  </div>
  <el-card v-loading="loading" class="ranking-card">
    <el-empty v-if="!rankings.length && !loading" description="暂无战力数据" />
    <el-table v-else :data="rankings" stripe>
      <el-table-column type="index" label="排名" width="80" />
      <el-table-column prop="pet_name" label="宠物名称" min-width="150">
        <template #default="scope">{{ scope.row.pet_name || '未命名宠物' }}</template>
      </el-table-column>
      <el-table-column label="宠物类型" width="120">
        <template #default="scope">{{ scope.row.dominant_type ? petTypeNames[scope.row.dominant_type] || '未知类型' : '未设置' }}</template>
      </el-table-column>
      <el-table-column prop="power" label="最新战力" width="130" sortable />
      <el-table-column prop="qq_number" label="来源 QQ" width="150" />
      <el-table-column label="战力更新时间" min-width="190">
        <template #default="scope">{{ formatDateTime(scope.row.updated_at) }}</template>
      </el-table-column>
    </el-table>
  </el-card>
</template>
