<template>
  <div class="relative h-screen w-full overflow-hidden bg-black">
    <!-- Back / Return Button (Floating on top left) -->
    <div class="fixed top-4 left-4 z-50 flex items-center gap-3">
      <button
        @click="goBack"
        class="group flex items-center gap-2 rounded-xl border border-cyan-500/30 bg-black/60 px-4 py-2 text-xs font-mono font-semibold text-cyan-400 backdrop-blur-xl transition-all duration-300 hover:border-cyan-400 hover:bg-cyan-500/20 hover:text-white hover:shadow-[0_0_20px_rgba(0,240,255,0.4)]"
      >
        <svg class="h-4 w-4 transition-transform group-hover:-translate-x-1" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M10.5 19.5L3 12m0 0l7.5-7.5M3 12h18" />
        </svg>
        <span>返回控制台 (DASHBOARD)</span>
      </button>

      <a
        href="/mushoku-tensei.html"
        target="_blank"
        class="flex items-center gap-1.5 rounded-xl border border-white/10 bg-black/60 px-3 py-2 text-xs font-mono text-gray-300 backdrop-blur-xl transition-all duration-300 hover:border-white/30 hover:bg-white/10 hover:text-white"
        title="在新窗口独立全屏打开"
      >
        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
        </svg>
        <span>独立全屏</span>
      </a>
    </div>

    <!-- Fullscreen Custom MAD Landing Frame -->
    <iframe
      src="/mushoku-tensei.html"
      class="h-full w-full border-0"
      allow="autoplay; fullscreen"
    ></iframe>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores'

const router = useRouter()
const authStore = useAuthStore()

function goBack() {
  if (authStore.isAuthenticated) {
    router.push(authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
  } else {
    router.push('/home')
  }
}
</script>
