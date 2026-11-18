<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>
<template>
  <div class="card">
    <p class="eyebrow">404</p>
    <h2>页面不存在</h2>
    <p class="subtitle">这个地址没有对应的页面。</p>
    <RouterLink class="button primary" to="/">返回首页</RouterLink>
  </div>
</template>
