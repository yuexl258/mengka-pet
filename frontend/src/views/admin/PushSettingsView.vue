<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getOfficialBotConfig, testOfficialBot, updateOfficialBotConfig } from '../../api/admin'

const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const form = reactive({
  app_id: '', client_secret: '', client_secret_set: false, group_openid: '',
  offline_enabled: false, offline_template: '## QQ 掉线提醒\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 时间：{{time}}\n- 原因：{{reason}}',
  activity_started_enabled: false, activity_started_template: '## 计划任务开始\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 时间：{{time}}',
  activity_completed_enabled: false, activity_completed_template: '## 计划任务完成\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 今日执行：{{today_summary}}\n- 时间：{{time}}'
})

async function load() {
  try {
    const result = await getOfficialBotConfig()
    if (result.data) Object.assign(form, result.data)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '推送设置加载失败')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (![form.offline_template, form.activity_started_template, form.activity_completed_template].every((value) => value.trim())) {
    ElMessage.warning('消息模板不能为空')
    return
  }
  saving.value = true
  try {
    const result = await updateOfficialBotConfig({ ...form })
    if (result.data) Object.assign(form, result.data)
    form.client_secret = ''
    ElMessage.success('推送设置已保存')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '推送设置保存失败')
  } finally {
    saving.value = false
  }
}

async function test() {
  testing.value = true
  try {
    await testOfficialBot()
    ElMessage.success('测试消息已发送')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '测试消息发送失败')
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>推送设置</h1></div></div>
  <el-card v-loading="loading">
    <template #header><strong>官方机器人</strong></template>
    <el-form label-position="top" class="settings-form">
      <el-form-item label="AppID"><el-input v-model="form.app_id" placeholder="机器人 AppID" /></el-form-item>
      <el-form-item label="ClientSecret"><el-input v-model="form.client_secret" type="password" show-password :placeholder="form.client_secret_set ? '已设置，留空则不修改' : '请输入 ClientSecret'" /></el-form-item>
      <el-form-item label="群 OpenID"><el-input v-model="form.group_openid" placeholder="接收提醒的群 OpenID" /></el-form-item>

      <el-divider content-position="left">自动控制掉线</el-divider>
      <el-form-item label="掉线提醒"><el-switch v-model="form.offline_enabled" active-text="开启" inactive-text="关闭" /></el-form-item>
      <el-form-item label="掉线模板">
        <el-input v-model="form.offline_template" type="textarea" :rows="8" />
        <div v-pre class="form-help">可用变量：{{qq}}、{{time}}、{{reason}}、{{sys_name}}、{{sys_version}}</div>
      </el-form-item>

      <el-divider content-position="left">计划任务开始</el-divider>
      <el-form-item label="计划开始推送"><el-switch v-model="form.activity_started_enabled" active-text="开启" inactive-text="关闭" /></el-form-item>
      <el-form-item label="开始模板">
        <el-input v-model="form.activity_started_template" type="textarea" :rows="9" />
        <div v-pre class="form-help">可用变量：{{qq}}、{{time}}、{{activity}}、{{option}}、{{plan_expectation}}、{{duration}}、{{sys_name}}、{{sys_version}}</div>
      </el-form-item>

      <el-divider content-position="left">计划任务完成</el-divider>
      <el-form-item label="计划完成推送"><el-switch v-model="form.activity_completed_enabled" active-text="开启" inactive-text="关闭" /></el-form-item>
      <el-form-item label="完成模板">
        <el-input v-model="form.activity_completed_template" type="textarea" :rows="10" />
        <div v-pre class="form-help">可用变量：{{qq}}、{{time}}、{{activity}}、{{option}}、{{plan_expectation}}、{{duration}}、{{today_summary}}、{{sys_name}}、{{sys_version}}</div>
      </el-form-item>

      <el-button type="primary" :loading="saving" @click="save">保存推送设置</el-button>
      <el-button :loading="testing" @click="test">发送测试消息</el-button>
    </el-form>
  </el-card>
</template>
