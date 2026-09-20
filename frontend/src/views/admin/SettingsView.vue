<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getSettings, updateSettings } from '../../api/admin'
import { useSystemStore } from '../../stores/system'

const loading = ref(true)
const saving = ref(false)
const form = reactive({ registration_enabled: true, register_gift_coins: 0, system_name: 'QQ 宠物' })
const system = useSystemStore()

async function load() {
  try {
    const result = await getSettings()
    if (result.data) Object.assign(form, result.data)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '设置加载失败')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!form.system_name.trim() || form.system_name.trim().length > 50) {
    ElMessage.warning('系统名称长度必须为 1 到 50 个字符')
    return
  }
  if (!Number.isInteger(form.register_gift_coins) || form.register_gift_coins < 0) {
    ElMessage.warning('金币配置必须是非负整数')
    return
  }
  saving.value = true
  try {
    const result = await updateSettings({ ...form, system_name: form.system_name.trim() })
    if (result.data) {
      Object.assign(form, result.data)
      system.name = result.data.system_name
      document.title = result.data.system_name
    }
    ElMessage.success('系统设置已保存')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '设置保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>系统设置</h1></div></div>
  <el-card v-loading="loading">
    <el-form label-position="top" class="settings-form">
      <el-form-item label="系统名称">
        <el-input v-model="form.system_name" maxlength="50" show-word-limit placeholder="请输入系统名称" />
        <div class="form-help">会同步显示在登录页、导航栏、面包屑和浏览器标题。</div>
      </el-form-item>
      <el-form-item label="开放用户注册">
        <el-switch v-model="form.registration_enabled" active-text="开放" inactive-text="关闭" />
      </el-form-item>
      <el-form-item label="注册赠送金币">
        <el-input-number v-model="form.register_gift_coins" :min="0" :max="2147483647" :step="1" step-strictly controls-position="right" />
        <div class="form-help">只能填写非负整数，注册成功后会自动发放到用户钱包，并记录金币流水。</div>
      </el-form-item>
      <el-button type="primary" :loading="saving" @click="save">保存设置</el-button>
    </el-form>
  </el-card>
</template>
