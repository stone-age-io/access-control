import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import MainLayout from '@/components/layout/MainLayout.vue'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/LoginView.vue'),
    meta: { requiresAuth: false, title: 'Sign in' },
  },
  {
    path: '/',
    component: MainLayout,
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'Overview', component: () => import('@/views/OverviewView.vue'), meta: { title: 'Overview' } },

      // Sites
      { path: 'sites', name: 'Sites', component: () => import('@/views/sites/SiteListView.vue'), meta: { title: 'Sites' } },
      { path: 'sites/new', name: 'SiteNew', component: () => import('@/views/sites/SiteFormView.vue'), meta: { title: 'New Site' } },
      { path: 'sites/:id', name: 'Site', component: () => import('@/views/sites/SiteDetailView.vue'), meta: { title: 'Site' } },
      { path: 'sites/:id/edit', name: 'SiteEdit', component: () => import('@/views/sites/SiteFormView.vue'), meta: { title: 'Edit Site' } },

      // Schedules
      { path: 'schedules', name: 'Schedules', component: () => import('@/views/schedules/ScheduleListView.vue'), meta: { title: 'Schedules' } },
      { path: 'schedules/new', name: 'ScheduleNew', component: () => import('@/views/schedules/ScheduleFormView.vue'), meta: { title: 'New Schedule' } },
      { path: 'schedules/:id', name: 'Schedule', component: () => import('@/views/schedules/ScheduleDetailView.vue'), meta: { title: 'Schedule' } },
      { path: 'schedules/:id/edit', name: 'ScheduleEdit', component: () => import('@/views/schedules/ScheduleFormView.vue'), meta: { title: 'Edit Schedule' } },

      // Access points
      { path: 'access-points', name: 'AccessPoints', component: () => import('@/views/access_points/AccessPointListView.vue'), meta: { title: 'Access Points' } },
      { path: 'access-points/new', name: 'AccessPointNew', component: () => import('@/views/access_points/AccessPointFormView.vue'), meta: { title: 'New Access Point' } },
      { path: 'access-points/:id', name: 'AccessPoint', component: () => import('@/views/access_points/AccessPointDetailView.vue'), meta: { title: 'Access Point' } },
      { path: 'access-points/:id/edit', name: 'AccessPointEdit', component: () => import('@/views/access_points/AccessPointFormView.vue'), meta: { title: 'Edit Access Point' } },

      // Access groups
      { path: 'access-groups', name: 'AccessGroups', component: () => import('@/views/access_groups/AccessGroupListView.vue'), meta: { title: 'Access Groups' } },
      { path: 'access-groups/new', name: 'AccessGroupNew', component: () => import('@/views/access_groups/AccessGroupFormView.vue'), meta: { title: 'New Access Group' } },
      { path: 'access-groups/:id', name: 'AccessGroup', component: () => import('@/views/access_groups/AccessGroupDetailView.vue'), meta: { title: 'Access Group' } },
      { path: 'access-groups/:id/edit', name: 'AccessGroupEdit', component: () => import('@/views/access_groups/AccessGroupFormView.vue'), meta: { title: 'Edit Access Group' } },

      // Roles
      { path: 'roles', name: 'Roles', component: () => import('@/views/roles/RoleListView.vue'), meta: { title: 'Roles' } },
      { path: 'roles/new', name: 'RoleNew', component: () => import('@/views/roles/RoleFormView.vue'), meta: { title: 'New Role' } },
      { path: 'roles/:id', name: 'Role', component: () => import('@/views/roles/RoleDetailView.vue'), meta: { title: 'Role' } },
      { path: 'roles/:id/edit', name: 'RoleEdit', component: () => import('@/views/roles/RoleFormView.vue'), meta: { title: 'Edit Role' } },

      // Cardholders
      { path: 'cardholders', name: 'Cardholders', component: () => import('@/views/cardholders/CardholderListView.vue'), meta: { title: 'Cardholders' } },
      { path: 'cardholders/new', name: 'CardholderNew', component: () => import('@/views/cardholders/CardholderFormView.vue'), meta: { title: 'New Cardholder' } },
      { path: 'cardholders/:id', name: 'Cardholder', component: () => import('@/views/cardholders/CardholderDetailView.vue'), meta: { title: 'Cardholder' } },
      { path: 'cardholders/:id/edit', name: 'CardholderEdit', component: () => import('@/views/cardholders/CardholderFormView.vue'), meta: { title: 'Edit Cardholder' } },

      // Credentials (a credential belongs to one cardholder)
      { path: 'credentials', name: 'Credentials', component: () => import('@/views/credentials/CredentialListView.vue'), meta: { title: 'Credentials' } },
      { path: 'credentials/new', name: 'CredentialNew', component: () => import('@/views/credentials/CredentialFormView.vue'), meta: { title: 'New Credential' } },
      { path: 'credentials/:id', name: 'Credential', component: () => import('@/views/credentials/CredentialDetailView.vue'), meta: { title: 'Credential' } },
      { path: 'credentials/:id/edit', name: 'CredentialEdit', component: () => import('@/views/credentials/CredentialFormView.vue'), meta: { title: 'Edit Credential' } },

      // Events (read-only audit timeline)
      { path: 'events', name: 'Events', component: () => import('@/views/events/EventListView.vue'), meta: { title: 'Events' } },
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

// Keep the browser tab title in sync with the active route.
router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} · Stone Access` : 'Stone Access'
})

export default router
