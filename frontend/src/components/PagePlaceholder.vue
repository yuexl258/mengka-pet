<script setup lang="ts">
import { Tools } from '@element-plus/icons-vue'
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <el-empty :image-size="120" :description="description || '该功能尚未开放，页面将在对应开发阶段逐步接入'">
    <template #image><el-icon :size="72" color="#94a3b8"><Tools /></el-icon></template>
    <el-tag type="info">功能开发中</el-tag>
  </el-empty>
</template>
