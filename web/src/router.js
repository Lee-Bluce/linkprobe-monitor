import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    name: 'overview',
    component: () => import('./views/OverviewView.vue'),
  },
  {
    path: '/node/:id',
    name: 'node',
    component: () => import('./views/NodeView.vue'),
    props: (route) => ({ id: Number(route.params.id) }),
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export default createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})
