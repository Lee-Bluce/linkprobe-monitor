import * as echarts from 'echarts/core'
import { LineChart, BarChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
  AxisPointerComponent,
  DataZoomComponent,
  MarkAreaComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

echarts.use([
  LineChart,
  BarChart,
  GridComponent,
  TooltipComponent,
  AxisPointerComponent,
  DataZoomComponent,
  MarkAreaComponent,
  CanvasRenderer,
])

export function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

/** Diagonal hatch used for "no data" stretches, readable in both themes. */
export function hatchPattern() {
  const size = 8
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d')
  ctx.strokeStyle = cssVar('--line-strong')
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.moveTo(-2, size + 2)
  ctx.lineTo(size + 2, -2)
  ctx.stroke()
  return { image: canvas, repeat: 'repeat' }
}

export default echarts
