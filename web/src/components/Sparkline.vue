<script setup>
import { computed } from 'vue'

const props = defineProps({
  points: { type: Array, default: () => [] },
  from: { type: Number, default: 0 },
  to: { type: Number, default: 0 },
})

const W = 100
const H = 40
const LOSS_H = 7

const span = computed(() => Math.max(1, props.to - props.from))

const hasData = computed(() => props.points.some((p) => p.rtt_avg_ms !== null || p.loss > 0))

const xOf = (ts) => ((ts - props.from) / span.value) * W

const maxRtt = computed(() => {
  let m = 0
  for (const p of props.points) {
    if (p.rtt_avg_ms !== null && p.rtt_avg_ms > m) m = p.rtt_avg_ms
  }
  return m > 0 ? m : 1
})

const yOf = (p) => H - LOSS_H - 2 - (p.rtt_avg_ms / maxRtt.value) * (H - LOSS_H - 8)

// A lone sample produces a move-only path that paints nothing, so draw it as
// a dot instead of leaving the card blank.
const singlePoint = computed(() => {
  const pts = props.points.filter((p) => p.rtt_avg_ms !== null)
  if (pts.length !== 1) return null
  return { x: xOf(pts[0].ts), y: yOf(pts[0]) }
})

const linePath = computed(() => {
  const pts = []
  let prevMissing = true
  for (const p of props.points) {
    if (p.rtt_avg_ms === null) {
      prevMissing = true
      continue
    }
    const x = xOf(p.ts)
    const y = yOf(p)
    pts.push(`${prevMissing ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`)
    prevMissing = false
  }
  return pts.join(' ')
})

const areaPath = computed(() => {
  if (!linePath.value) return ''
  const first = props.points.find((p) => p.rtt_avg_ms !== null)
  const last = [...props.points].reverse().find((p) => p.rtt_avg_ms !== null)
  if (!first || !last) return ''
  const base = H - LOSS_H - 1
  return `${linePath.value} L${xOf(last.ts).toFixed(2)},${base} L${xOf(first.ts).toFixed(
    2,
  )},${base} Z`
})

const lossBars = computed(() => {
  const bars = []
  const width = Math.max(0.6, Math.min(2.4, (W / span.value) * 30 * 0.8))
  for (const p of props.points) {
    if (!p.loss) continue
    const h = Math.max(1, p.loss * LOSS_H)
    bars.push({
      x: xOf(p.ts) - width / 2,
      y: H - h,
      w: width,
      h,
      opacity: p.loss < 0.01 ? 0.35 : p.loss < 0.05 ? 0.6 : 0.9,
    })
  }
  return bars
})
</script>

<template>
  <svg
    class="spark"
    :viewBox="`0 0 ${W} ${H}`"
    preserveAspectRatio="none"
    aria-hidden="true"
  >
    <line
      v-if="!hasData"
      x1="0"
      :y1="H - LOSS_H - 1"
      :x2="W"
      :y2="H - LOSS_H - 1"
      stroke="var(--line)"
      stroke-width="0.8"
      stroke-dasharray="1.5 2"
    />
    <path v-if="areaPath" :d="areaPath" fill="var(--accent)" opacity="0.08" />
    <circle
      v-if="singlePoint"
      :cx="singlePoint.x"
      :cy="singlePoint.y"
      r="1.3"
      fill="var(--accent)"
    />
    <path
      v-if="linePath"
      :d="linePath"
      fill="none"
      stroke="var(--accent)"
      stroke-width="1.1"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
    <rect
      v-for="(b, i) in lossBars"
      :key="i"
      :x="b.x"
      :y="b.y"
      :width="b.w"
      :height="b.h"
      fill="var(--warn)"
      :opacity="b.opacity"
    />
    <line
      x1="0"
      :y1="H - 0.5"
      :x2="W"
      :y2="H - 0.5"
      stroke="var(--line)"
      stroke-width="0.6"
    />
  </svg>
</template>
