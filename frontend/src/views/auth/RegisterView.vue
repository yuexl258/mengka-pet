<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'
const auth = useAuthStore(); const router = useRouter(); const loading = ref(false)
const form = reactive({ username: '', password: '', confirm: '' })
async function submit() {
  if (form.username.trim().length < 3 || form.password.length < 8 || form.password !== form.confirm) { ElMessage.warning('请检查用户名、密码和确认密码'); return }
  loading.value = true
  try { await auth.register(form.username.trim(), form.password); ElMessage.success('注册成功，请登录'); await router.push('/login') } catch (error) { ElMessage.error(error instanceof Error ? error.message : '注册失败') } finally { loading.value = false }
}
</script>

<template><el-card class="auth-card"><template #header><div class="auth-card-title"><span>注册</span><el-tag type="success">已开放</el-tag></div></template><el-form label-position="top" @submit.prevent="submit"><el-form-item label="用户名"><el-input v-model="form.username" autocomplete="username" placeholder="3-24 个字符" maxlength="24" /></el-form-item><el-form-item label="密码"><el-input v-model="form.password" type="password" autocomplete="new-password" placeholder="至少 8 个字符" show-password /></el-form-item><el-form-item label="确认密码"><el-input v-model="form.confirm" type="password" autocomplete="new-password" placeholder="再次输入密码" show-password @keyup.enter="submit" /></el-form-item><el-button type="primary" class="full-button" :loading="loading" native-type="submit">注册</el-button></el-form><div class="auth-link"><router-link to="/login">已有账号？返回登录</router-link></div></el-card></template>
