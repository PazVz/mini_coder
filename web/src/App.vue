<script setup lang="ts">
import { watch } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'

const route = useRoute()
watch(
  () => route.meta.title,
  (title) => {
    document.title = String(title || '账号') + ' · mini_coder'
  },
  { immediate: true },
)
</script>

<template>
  <a class="skip-link" href="#main">跳到主要内容</a>
  <header class="site-header">
    <RouterLink to="/" class="brand" aria-label="mini_coder 首页">
      <span class="brand-mark" aria-hidden="true">m_</span><span>mini_coder</span>
    </RouterLink>
    <nav aria-label="主导航"><RouterLink to="/account">我的账号</RouterLink></nav>
  </header>
  <main id="main" class="page-layout">
    <aside class="intro-panel">
      <p class="eyebrow">YOUR SPACE TO BUILD</p>
      <h1>从一个账号，<br />开始你的开发旅程。</h1>
      <p class="intro-copy">管理你的身份与邮箱，为每一次专注创造做好准备。</p>
      <div class="workspace-sketch" aria-hidden="true">
        <div class="sketch-toolbar"><i></i><i></i><i></i><span>mini_coder / account</span></div>
        <div class="sketch-code">
          <span class="code-muted">01</span> <span class="code-teal">hello</span>, developer<br /><span
            class="code-muted"
            >02</span
          >
          your next idea starts here<span class="cursor">_</span>
        </div>
      </div>
      <p class="intro-foot">一个轻量的开发工作区管理平台</p>
    </aside>
    <section class="content-panel"><RouterView /></section>
  </main>
  <footer class="site-footer"><span>mini_coder</span><span>让想法，有处落地。</span></footer>
</template>
