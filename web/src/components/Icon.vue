<script setup>
const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 16 },
})

const paths = {
  plus: ['M12 5v14', 'M5 12h14'],
  minus: ['M5 12h14'],
  back: ['M15 6l-6 6 6 6'],
  'chevron-down': ['M6 9l6 6 6-6'],
  'chevron-right': ['M9 6l6 6-6 6'],
  more: ['M6 12h.01', 'M12 12h.01', 'M18 12h.01'],
  edit: ['M4 20h4L18.5 9.5a2.12 2.12 0 0 0-3-3L5 17v3z', 'M13.5 6.5l3 3'],
  trash: ['M4 7h16', 'M9 7V5h6v2', 'M6 7l1 13h10l1-13', 'M10 11v6', 'M14 11v6'],
  pause: ['M9 5v14', 'M15 5v14'],
  play: ['M7 5l12 7-12 7z'],
  refresh: ['M20 11a8 8 0 1 0-2.4 6.4', 'M20 5v6h-6'],
  check: ['M5 12l5 5 9-10'],
  close: ['M6 6l12 12', 'M18 6L6 18'],
  logout: ['M10 4H5v16h5', 'M16 8l4 4-4 4', 'M20 12H10'],
  activity: ['M3 12h4l2-6 4 12 2-6h6'],
  grip: ['M9 5h.01', 'M15 5h.01', 'M9 12h.01', 'M15 12h.01', 'M9 19h.01', 'M15 19h.01'],
  info: ['M12 8h.01', 'M11 12h1v5h1'],
  alert: ['M12 8v5', 'M12 16h.01', 'M12 3l9 17H3z'],
}
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path v-for="(d, i) in paths[props.name] || []" :key="i" :d="d" />
  </svg>
</template>
