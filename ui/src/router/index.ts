import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import MainLayout from '@/components/layout/MainLayout.vue'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/LoginView.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/',
    component: MainLayout,
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'Overview', component: () => import('@/views/OverviewView.vue') },

      // Sites
      { path: 'sites', name: 'Sites', component: () => import('@/views/sites/SiteListView.vue') },
      { path: 'sites/new', name: 'SiteNew', component: () => import('@/views/sites/SiteFormView.vue') },
      { path: 'sites/:id/edit', name: 'SiteEdit', component: () => import('@/views/sites/SiteFormView.vue') },

      // Schedules
      { path: 'schedules', name: 'Schedules', component: () => import('@/views/schedules/ScheduleListView.vue') },
      { path: 'schedules/new', name: 'ScheduleNew', component: () => import('@/views/schedules/ScheduleFormView.vue') },
      { path: 'schedules/:id/edit', name: 'ScheduleEdit', component: () => import('@/views/schedules/ScheduleFormView.vue') },

      // Access points
      { path: 'access-points', name: 'AccessPoints', component: () => import('@/views/access_points/AccessPointListView.vue') },
      { path: 'access-points/new', name: 'AccessPointNew', component: () => import('@/views/access_points/AccessPointFormView.vue') },
      { path: 'access-points/:id/edit', name: 'AccessPointEdit', component: () => import('@/views/access_points/AccessPointFormView.vue') },

      // Access groups
      { path: 'access-groups', name: 'AccessGroups', component: () => import('@/views/access_groups/AccessGroupListView.vue') },
      { path: 'access-groups/new', name: 'AccessGroupNew', component: () => import('@/views/access_groups/AccessGroupFormView.vue') },
      { path: 'access-groups/:id/edit', name: 'AccessGroupEdit', component: () => import('@/views/access_groups/AccessGroupFormView.vue') },

      // Roles
      { path: 'roles', name: 'Roles', component: () => import('@/views/roles/RoleListView.vue') },
      { path: 'roles/new', name: 'RoleNew', component: () => import('@/views/roles/RoleFormView.vue') },
      { path: 'roles/:id/edit', name: 'RoleEdit', component: () => import('@/views/roles/RoleFormView.vue') },

      // Cardholders
      { path: 'cardholders', name: 'Cardholders', component: () => import('@/views/cardholders/CardholderListView.vue') },
      { path: 'cardholders/new', name: 'CardholderNew', component: () => import('@/views/cardholders/CardholderFormView.vue') },
      { path: 'cardholders/:id/edit', name: 'CardholderEdit', component: () => import('@/views/cardholders/CardholderFormView.vue') },

      // Credentials
      { path: 'credentials', name: 'Credentials', component: () => import('@/views/credentials/CredentialListView.vue') },
      { path: 'credentials/new', name: 'CredentialNew', component: () => import('@/views/credentials/CredentialFormView.vue') },
      { path: 'credentials/:id/edit', name: 'CredentialEdit', component: () => import('@/views/credentials/CredentialFormView.vue') },

      // Events (read-only audit timeline)
      { path: 'events', name: 'Events', component: () => import('@/views/events/EventListView.vue') },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to, _from, next) => {
  const authStore = useAuthStore()
  if (to.meta.requiresAuth !== false && !authStore.isAuthenticated) {
    next('/login')
    return
  }
  // Already signed in — skip the login page.
  if (to.path === '/login' && authStore.isAuthenticated) {
    next('/')
    return
  }
  next()
})

export default router
