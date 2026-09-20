<script setup lang="ts">
import { onMounted, watch } from 'vue'
import { RouterView } from 'vue-router'
import { useSystemStore } from '../stores/system'
const system = useSystemStore()
onMounted(() => system.load())
watch(() => system.name, value => { document.title = value }, { immediate: true })
</script>
<template>
  <div class="auth-layout">
    <section class="auth-shell" aria-label="账号入口">
      <aside class="auth-showcase">
        <div class="auth-brand">
          <span class="brand-mark">Q</span>
          <span>{{ system.name }}</span>
        </div>
        <div class="auth-showcase-copy">
          <p class="auth-eyebrow">QQ PET HOME</p>
          <h1>陪伴每一天，<br>一起慢慢长大</h1>
          <p>照顾你的专属伙伴，记录成长、活力与每一次温暖互动。</p>
        </div>
        <div class="auth-pet-scene" aria-hidden="true">
          <span class="auth-scene-glow"></span>
          <span class="auth-scene-orbit"></span>
          <i class="auth-scene-star auth-scene-star-one"></i>
          <i class="auth-scene-star auth-scene-star-two"></i>
          <i class="auth-scene-star auth-scene-star-three"></i>
          <span class="auth-paw auth-paw-one"><i></i></span>
          <span class="auth-paw auth-paw-two"><i></i></span>
          <span class="auth-pet-shadow"></span>
          <div class="auth-pet">
            <span class="auth-pet-ear auth-pet-ear-left"></span>
            <span class="auth-pet-ear auth-pet-ear-right"></span>
            <span class="auth-pet-face">
              <i class="auth-pet-eye auth-pet-eye-left"></i>
              <i class="auth-pet-eye auth-pet-eye-right"></i>
              <i class="auth-pet-nose"></i>
            </span>
            <span class="auth-pet-body"></span>
          </div>
        </div>
      </aside>
      <main class="auth-content"><RouterView /></main>
    </section>
    <p class="auth-footer">{{ system.name }}</p>
  </div>
</template>
