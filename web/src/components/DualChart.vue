<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import echarts, { cssVar, hatchPattern } from '../charts'
import { fmtLoss, fmtMs, pad } from '../format'

const props = defineProps({
  points: { type: Array, default: () => [] },
  from: { type: Number, required: true },
  to: { type: Number, required: true },
  res: { type: Number, default: 300 },
})
const emit = defineEmits(['brush'])

const latencyEl = ref(null)
const lossEl = ref(null)
const band = ref(true)
const brushing = ref(false)

let latencyChart = null
let lossChart = null
let observer = null
let zoomHandler = null
let zoomTimer = null
let themeMedia = null

const span = computed(() => Math.max(1, props.to - props.from))

const buckets = computed(() => {
  const res = props.res || 60
  const start = Math.floor(props.from / res) * res
  const map = new Map()
  for (const p of props.points) map.set(p.ts, p)
  const out = []
  for (let ts = start; ts <= props.to; ts += res) {
    const p = map.get(ts)
    out.push({
      ts,
      p: p && p.sent > 0 ? p : null,
    })
  }
  return out
})

const hasData = computed(() => buckets.value.some((b) => b.p))
const pointCount = computed(() => buckets.value.reduce((n, b) => n + (b.p ? 1 : 0), 0))

const labelFmt = (ms) => {
  const d = new Date(ms)
  const s = span.value
  if (s <= 6 * 3600) return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (s <= 3 * 86400) return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function withAlpha(color, alpha) {
  const c = color.trim()
  if (c.startsWith('#') && (c.length === 7 || c.length === 4)) {
    let hex = c.slice(1)
    if (hex.length === 3) hex = hex.split('').map((x) => x + x).join('')
    const r = parseInt(hex.slice(0, 2), 16)
    const g = parseInt(hex.slice(2, 4), 16)
    const b = parseInt(hex.slice(4, 6), 16)
    return `rgba(${r}, ${g}, ${b}, ${alpha})`
  }
  return c
}

function axisCommon() {
  const ink3 = cssVar('--ink-3')
  const line = cssVar('--line')
  return {
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { color: ink3, fontSize: 10.5, hideOverlap: true },
    splitLine: { show: true, lineStyle: { color: line, width: 1 } },
  }
}

function xAxis(showLabels) {
  const ink3 = cssVar('--ink-3')
  return {
    type: 'value',
    min: props.from * 1000,
    max: props.to * 1000,
    axisLine: { show: false },
    axisTick: { show: false },
    splitLine: { show: false },
    axisLabel: {
      show: showLabels,
      color: ink3,
      fontSize: 10.5,
      hideOverlap: true,
      formatter: (v) => labelFmt(v),
    },
    axisPointer: { show: true, lineStyle: { color: ink3, width: 1, type: 'solid' } },
  }
}

function gapRanges() {
  const ranges = []
  let start = null
  buckets.value.forEach((b, i) => {
    if (!b.p && start === null) start = b.ts
    if (b.p && start !== null) {
      ranges.push([
        { xAxis: start * 1000 },
        { xAxis: buckets.value[i - 1].ts * 1000 + props.res * 1000 },
      ])
      start = null
    }
  })
  if (start !== null) {
    const last = buckets.value[buckets.value.length - 1]
    ranges.push([{ xAxis: start * 1000 }, { xAxis: last.ts * 1000 + props.res * 1000 }])
  }
  return ranges
}

function barWidth() {
  const width = lossEl.value?.clientWidth || 800
  const count = Math.max(1, buckets.value.length)
  return Math.max(1, Math.min(((width - 60) / count) * 0.62, 18))
}

function tooltipBase() {
  return {
    trigger: 'axis',
    backgroundColor: cssVar('--surface'),
    borderColor: cssVar('--line-strong'),
    borderWidth: 1,
    padding: [8, 10],
    textStyle: { color: cssVar('--ink'), fontSize: 12 },
    extraCssText: 'box-shadow: 0 8px 24px rgba(0,0,0,.08); border-radius: 6px;',
  }
}

function latencyOption() {
  const accent = cssVar('--accent')
  const ink3 = cssVar('--ink-3')
  const line = cssVar('--line')

  const lower = buckets.value.map((b) => [b.ts * 1000, b.p ? b.p.rtt_min_ms : null])
  const delta = buckets.value.map((b) => [
    b.ts * 1000,
    b.p && b.p.rtt_max_ms !== null ? Math.max(0, b.p.rtt_max_ms - (b.p.rtt_min_ms || 0)) : null,
  ])
  const avg = buckets.value.map((b) => [b.ts * 1000, b.p ? b.p.rtt_avg_ms : null])

  return {
    animation: false,
    grid: { left: 46, right: 14, top: 12, bottom: 24 },
    tooltip: {
      ...tooltipBase(),
      axisPointer: { type: 'line', lineStyle: { color: ink3, width: 1 } },
      formatter: (params) => {
        if (!params?.length) return ''
        const ts = params[0].axisValue
        const b = buckets.value.find((x) => x.ts * 1000 === ts)
        if (!b) return ''
        const time = new Date(ts)
        const head = `${pad(time.getMonth() + 1)}-${pad(time.getDate())} ${pad(
          time.getHours(),
        )}:${pad(time.getMinutes())}`
        if (!b.p) return `<div style="color:${ink3}">${head} · 无数据</div>`
        const p = b.p
        return [
          `<div style="color:${ink3};margin-bottom:4px">${head}</div>`,
          `<div>平均 <b style="font-variant-numeric:tabular-nums">${fmtMs(p.rtt_avg_ms)}</b> ms</div>`,
          `<div style="color:${ink3}">最小 ${fmtMs(p.rtt_min_ms)} · 最大 ${fmtMs(p.rtt_max_ms)}</div>`,
          `<div style="color:${ink3}">样本 ${p.recv}/${p.sent} · 丢包 ${fmtLoss(p.loss)}</div>`,
        ].join('')
      },
    },
    xAxis: xAxis(false),
    yAxis: {
      type: 'value',
      min: 0,
      splitNumber: 3,
      ...axisCommon(),
      axisLabel: {
        color: ink3,
        fontSize: 10.5,
        formatter: (v) => (v >= 1000 ? (v / 1000).toFixed(1) + 's' : v + 'ms'),
      },
    },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, disabled: !brushing.value, filterMode: 'none', zoomOnMouseWheel: true },
    ],
    series: [
      {
        name: 'min',
        type: 'line',
        stack: 'band',
        data: band.value ? lower : [],
        symbol: 'none',
        lineStyle: { opacity: 0 },
        areaStyle: { opacity: 0 },
        silent: true,
        z: 1,
      },
      {
        name: 'band',
        type: 'line',
        stack: 'band',
        data: band.value ? delta : [],
        symbol: 'none',
        lineStyle: { opacity: 0 },
        areaStyle: { color: withAlpha(accent, 0.1) },
        silent: true,
        z: 1,
      },
      {
        name: '延迟',
        type: 'line',
        data: avg,
        // A line with symbol "none" is invisible when a range only holds one
        // or two buckets, which is exactly what a fresh install looks like.
        showSymbol: pointCount.value <= 60,
        symbol: 'circle',
        symbolSize: 3,
        connectNulls: false,
        lineStyle: { color: accent, width: 1.5 },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: withAlpha(accent, 0.09) },
            { offset: 1, color: withAlpha(accent, 0) },
          ]),
        },
        itemStyle: { color: accent },
        z: 3,
        markArea: gapRanges().length
          ? {
              silent: true,
              itemStyle: { color: hatchPattern(), borderColor: 'transparent' },
              data: gapRanges(),
              z: 0,
            }
          : undefined,
      },
    ],
  }
}

function lossOption() {
  const ink3 = cssVar('--ink-3')
  const warn = cssVar('--warn')
  const bad = cssVar('--bad')
  const accent = cssVar('--accent')

  const data = buckets.value.map((b) => {
    if (!b.p) return [b.ts * 1000, null]
    const v = b.p.loss * 100
    let color = withAlpha(accent, 0.35)
    if (v >= 5) color = withAlpha(bad, 0.85)
    else if (v >= 1) color = withAlpha(warn, 0.6)
    return { value: [b.ts * 1000, v], itemStyle: { color } }
  })

  return {
    animation: false,
    grid: { left: 46, right: 14, top: 10, bottom: 26 },
    tooltip: {
      ...tooltipBase(),
      axisPointer: { type: 'line', lineStyle: { color: ink3, width: 1 } },
      formatter: (params) => {
        if (!params?.length) return ''
        const ts = params[0].axisValue
        const b = buckets.value.find((x) => x.ts * 1000 === ts)
        if (!b || !b.p) {
          const d = new Date(ts)
          return `<div style="color:${ink3}">${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
            d.getHours(),
          )}:${pad(d.getMinutes())} · 无数据</div>`
        }
        const d = new Date(ts)
        return [
          `<div style="color:${ink3};margin-bottom:4px">${pad(d.getMonth() + 1)}-${pad(
            d.getDate(),
          )} ${pad(d.getHours())}:${pad(d.getMinutes())}</div>`,
          `<div>丢包 <b>${fmtLoss(b.p.loss)}</b> · 样本 ${b.p.recv}/${b.p.sent}</div>`,
        ].join('')
      },
    },
    xAxis: xAxis(true),
    yAxis: {
      type: 'value',
      min: 0,
      max: 100,
      splitNumber: 2,
      ...axisCommon(),
      axisLabel: { color: ink3, fontSize: 10.5, formatter: '{value}%' },
    },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, disabled: !brushing.value, filterMode: 'none', zoomOnMouseWheel: true },
    ],
    series: [
      {
        name: '丢包',
        type: 'bar',
        data: data,
        barWidth: barWidth(),
        itemStyle: { borderRadius: 0 },
        z: 2,
        markArea: gapRanges().length
          ? {
              silent: true,
              itemStyle: {
                color: hatchPattern(),
                borderColor: 'transparent',
              },
              data: gapRanges(),
              z: 1,
            }
          : undefined,
      },
    ],
  }
}

function build(notMerge = true) {
  if (!latencyEl.value || !lossEl.value) return
  if (!latencyChart) {
    latencyChart = echarts.init(latencyEl.value, null, { renderer: 'canvas' })
    lossChart = echarts.init(lossEl.value, null, { renderer: 'canvas' })
    echarts.connect([latencyChart, lossChart])
    zoomHandler = () => {
      if (!brushing.value) return
      const idx = latencyChart.getOption().dataZoom?.[0]
      if (!idx) return
      const startPct = idx.start ?? 0
      const endPct = idx.end ?? 100
      window.clearTimeout(zoomTimer)
      zoomTimer = window.setTimeout(() => {
        const from = Math.round(props.from + (span.value * startPct) / 100)
        const to = Math.round(props.from + (span.value * endPct) / 100)
        if (to - from >= 60) emit('brush', { from, to })
      }, 450)
    }
    latencyChart.on('datazoom', zoomHandler)
  }
  latencyChart.setOption(latencyOption(), notMerge)
  lossChart.setOption(lossOption(), notMerge)
}

function resize() {
  latencyChart?.resize()
  lossChart?.resize()
}

function toggleBrush() {
  brushing.value = !brushing.value
  build()
}

function toggleBand() {
  band.value = !band.value
  build()
}

onMounted(() => {
  build()
  observer = new ResizeObserver(() => resize())
  observer.observe(latencyEl.value)
  observer.observe(lossEl.value)
  themeMedia = window.matchMedia('(prefers-color-scheme: dark)')
  themeMedia.addEventListener('change', onThemeChange)
})

function onThemeChange() {
  build()
}

onBeforeUnmount(() => {
  observer?.disconnect()
  themeMedia?.removeEventListener('change', onThemeChange)
  window.clearTimeout(zoomTimer)
  latencyChart?.dispose()
  lossChart?.dispose()
})

watch(
  () => [props.points, props.from, props.to, props.res],
  () => build(),
)
</script>

<template>
  <section class="panel chart-block">
    <div class="chart-head">
      <span class="chart-title">延迟与丢包</span>
      <div class="chart-legend">
        <span><i></i>平均延迟</span>
        <span>柱 · 丢包率</span>
      </div>
      <button
        class="btn btn-sm btn-ghost"
        :aria-pressed="band"
        :style="{ color: band ? 'var(--accent-ink)' : '' }"
        @click="toggleBand"
      >
        波动带
      </button>
      <button
        class="btn btn-sm btn-ghost"
        :aria-pressed="brushing"
        :style="{ color: brushing ? 'var(--accent-ink)' : '' }"
        @click="toggleBrush"
      >
        框选
      </button>
    </div>
    <div style="position: relative">
      <div ref="latencyEl" class="chart-box latency"></div>
      <div ref="lossEl" class="chart-box loss"></div>
      <div v-if="!hasData" class="chart-empty" style="position: absolute; inset: 0">
        该范围内没有数据
      </div>
    </div>
  </section>
</template>
