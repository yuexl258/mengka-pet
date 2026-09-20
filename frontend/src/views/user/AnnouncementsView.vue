<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getAnnouncements, type PublicAnnouncement } from '../../api/announcement'
import { formatDateTime } from '../../utils/format'
const loading = ref(false); const items = ref<PublicAnnouncement[]>([])
onMounted(async () => { loading.value = true; try { items.value = (await getAnnouncements()).data || [] } catch (error) { ElMessage.error(error instanceof Error ? error.message : '公告加载失败') } finally { loading.value = false } })
</script>
<template><div class="page-heading"><div><h1>公告</h1></div></div><el-card v-loading="loading"><el-empty v-if="!items.length" description="暂无公告" /><div v-for="item in items" :key="item.id" class="announcement-item"><div class="announcement-head"><h2>{{ item.title }}</h2><span>{{ formatDateTime(item.created_at) }}</span></div><div class="announcement-content">{{ item.content }}</div></div></el-card></template>
