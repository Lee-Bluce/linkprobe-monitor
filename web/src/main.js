import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './styles.css'

const app = createApp(App)

app.directive('focus', {
  mounted(el) {
    el.focus()
    if (typeof el.select === 'function') el.select()
  },
})

app.use(router)
app.mount('#app')
