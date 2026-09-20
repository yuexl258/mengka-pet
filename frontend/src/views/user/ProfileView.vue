<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { changePassword } from '../../api/auth'
import { useAuthStore } from '../../stores/auth'
import { formatDateTime } from '../../utils/format'
import { useRouter } from 'vue-router'
const auth = useAuthStore(); const router = useRouter(); const saving = ref(false)
const form = reactive({ current_password: '', new_password: '', confirm_password: '' })
async function submit() {
  if (form.new_password.length < 8 || form.new_password !== form.confirm_password) { ElMessage.warning('请检查新密码，长度至少 8 位且两次输入一致'); return }
  saving.value = true
  try { await changePassword(form); await auth.logout(); ElMessage.success('密码修改成功，请重新登录'); await router.push('/login') } catch (error) { ElMessage.error(error instanceof Error ? error.message : '密码修改失败') } finally { saving.value = false }
}
</script>
<template><div class="page-heading"><div><h1>个人信息</h1></div></div><el-row :gutter="16" class="equal-row"><el-col :xs="24" :md="10"><el-card><el-descriptions v-if="auth.user" :column="1" border><el-descriptions-item label="用户 ID">{{ auth.user.id }}</el-descriptions-item><el-descriptions-item label="用户名">{{ auth.user.username }}</el-descriptions-item><el-descriptions-item label="角色">{{ auth.user.role === 'admin' ? '管理员' : '普通用户' }}</el-descriptions-item><el-descriptions-item label="状态">{{ auth.user.status === 'active' ? '正常' : auth.user.status }}</el-descriptions-item><el-descriptions-item label="注册时间">{{ formatDateTime(auth.user.created_at) }}</el-descriptions-item></el-descriptions></el-card></el-col><el-col :xs="24" :md="14"><el-card><template #header>修改密码</template><el-form label-position="top" @submit.prevent="submit"><el-form-item label="原密码"><el-input v-model="form.current_password" type="password" show-password autocomplete="current-password" /></el-form-item><el-form-item label="新密码"><el-input v-model="form.new_password" type="password" show-password autocomplete="new-password" /></el-form-item><el-form-item label="确认新密码"><el-input v-model="form.confirm_password" type="password" show-password autocomplete="new-password" /></el-form-item><el-button type="primary" :loading="saving" native-type="submit">修改密码</el-button></el-form><div class="form-help">修改成功后，所有已登录设备都会退出，需要重新登录。</div></el-card></el-col></el-row></template>
