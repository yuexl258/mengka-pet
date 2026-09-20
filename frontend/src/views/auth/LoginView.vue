<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const loading = ref(false)
const form = reactive({ username: '', password: '' })

async function submit() {
  if (form.username.trim().length < 3 || form.password.length < 8) { ElMessage.warning('请输入有效的用户名和密码'); return }
  loading.value = true
  try { await auth.login(form.username.trim(), form.password); const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : auth.user?.role === 'admin' ? '/admin' : '/'; await router.push(redirect) } catch (error) { ElMessage.error(error instanceof Error ? error.message : '登录失败') } finally { loading.value = false }
}
</script>

<template>
  <section class="auth-form-panel" aria-labelledby="login-title">
    <header class="auth-form-header">
      <span class="auth-form-badge">欢迎回来</span>
      <h2 id="login-title">登录账号</h2>
      <p>继续照顾你的宠物伙伴</p>
    </header>
    <el-form class="auth-form" label-position="top" @submit.prevent="submit">
      <el-form-item label="用户名">
        <el-input v-model="form.username" autocomplete="username" placeholder="请输入用户名" maxlength="24" autofocus />
      </el-form-item>
      <el-form-item label="密码">
        <el-input v-model="form.password" type="password" autocomplete="current-password" placeholder="请输入密码" show-password />
      </el-form-item>
      <el-button type="primary" class="full-button auth-submit" :loading="loading" native-type="submit">登录</el-button>
    </el-form>
    <p class="auth-link">还没有账号？<router-link to="/register">立即注册</router-link></p>
  </section>
</template>
