<template>
  <section class="home" aria-labelledby="home-title">
    <h1 id="home-title" class="home-title">欢迎使用番剧库</h1>
    <p class="home-desc">私人番剧管理与在线观看平台</p>
    <router-link class="enter-btn" :to="{ name: 'anime-list' }">进入番剧库</router-link>
  </section>
</template>

<style scoped>
.home {
  min-height: 60vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
}

.home-title {
  margin: 0 0 12px;
  color: var(--text-color);
  font-size: clamp(24px, 5vw, 28px);
  font-weight: 700;
}

.home-desc {
  color: var(--text-secondary);
  font-size: 16px;
  margin-bottom: 24px;
}

.enter-btn {
  padding: 10px 28px;
  border-radius: 4px;
  background: var(--primary-color);
  color: #fff;
  font-size: 15px;
  font-weight: 600;
  text-decoration: none;
}

.enter-btn:hover {
  background: var(--primary-hover-color);
}
</style>
