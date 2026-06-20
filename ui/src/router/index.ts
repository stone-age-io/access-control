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

      // Locations
      { path: 'locations', name: 'Locations', component: () => import('@/views/locations/LocationListView.vue'), meta: { title: 'Locations' } },
      { path: 'locations/new', name: 'LocationNew', component: () => import('@/views/locations/LocationFormView.vue'), meta: { title: 'New Location' } },
      { path: 'locations/:id', name: 'Location', component: () => import('@/views/locations/LocationDetailView.vue'), meta: { title: 'Location' } },
      { path: 'locations/:id/edit', name: 'LocationEdit', component: () => import('@/views/locations/LocationFormView.vue'), meta: { title: 'Edit Location' } },

      // Schedules
      { path: 'schedules', name: 'Schedules', component: () => import('@/views/schedules/ScheduleListView.vue'), meta: { title: 'Schedules' } },
      { path: 'schedules/new', name: 'ScheduleNew', component: () => import('@/views/schedules/ScheduleFormView.vue'), meta: { title: 'New Schedule' } },
      { path: 'schedules/:id', name: 'Schedule', component: () => import('@/views/schedules/ScheduleDetailView.vue'), meta: { title: 'Schedule' } },
      { path: 'schedules/:id/edit', name: 'ScheduleEdit', component: () => import('@/views/schedules/ScheduleFormView.vue'), meta: { title: 'Edit Schedule' } },

      // Holidays
      { path: 'holidays', name: 'Holidays', component: () => import('@/views/holidays/HolidayListView.vue'), meta: { title: 'Holidays' } },
      { path: 'holidays/new', name: 'HolidayNew', component: () => import('@/views/holidays/HolidayFormView.vue'), meta: { title: 'New Holiday' } },
      { path: 'holidays/:id', name: 'Holiday', component: () => import('@/views/holidays/HolidayDetailView.vue'), meta: { title: 'Holiday' } },
      { path: 'holidays/:id/edit', name: 'HolidayEdit', component: () => import('@/views/holidays/HolidayFormView.vue'), meta: { title: 'Edit Holiday' } },

      // Portals
      { path: 'portals', name: 'Portals', component: () => import('@/views/portals/PortalListView.vue'), meta: { title: 'Portals' } },
      { path: 'portals/new', name: 'PortalNew', component: () => import('@/views/portals/PortalFormView.vue'), meta: { title: 'New Portal' } },
      { path: 'portals/:id', name: 'Portal', component: () => import('@/views/portals/PortalDetailView.vue'), meta: { title: 'Portal' } },
      { path: 'portals/:id/edit', name: 'PortalEdit', component: () => import('@/views/portals/PortalFormView.vue'), meta: { title: 'Edit Portal' } },

      // Controllers
      { path: 'controllers', name: 'Controllers', component: () => import('@/views/controllers/ControllerListView.vue'), meta: { title: 'Controllers' } },
      { path: 'controllers/new', name: 'ControllerNew', component: () => import('@/views/controllers/ControllerFormView.vue'), meta: { title: 'New Controller' } },
      { path: 'controllers/:id', name: 'Controller', component: () => import('@/views/controllers/ControllerDetailView.vue'), meta: { title: 'Controller' } },
      { path: 'controllers/:id/edit', name: 'ControllerEdit', component: () => import('@/views/controllers/ControllerFormView.vue'), meta: { title: 'Edit Controller' } },

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

      // Import (bulk-create cardholders + credentials from CSV)
      { path: 'import', name: 'Import', component: () => import('@/views/import/ImportView.vue'), meta: { title: 'Import' } },

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
