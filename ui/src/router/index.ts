import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import MainLayout from '@/components/layout/MainLayout.vue'

// Role gates for write routes (the API collection rules are the real boundary;
// these block navigation to forms an operator's role can't submit). Reads
// (list/detail) stay open to any authenticated operator.
const WRITE = ['operator', 'admin'] // daily ops: people, credentials, holidays
const ADMIN = ['admin'] // topology/config + operator + audit

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

      // Monitor (live operational map: geo overview → per-location floor plan + commands)
      { path: 'monitor', name: 'Monitor', component: () => import('@/views/monitor/MonitorView.vue'), meta: { title: 'Live Map' } },
      { path: 'monitor/:locationId', name: 'MonitorLocation', component: () => import('@/views/monitor/MonitorView.vue'), meta: { title: 'Live Map' } },

      // Locations
      { path: 'locations', name: 'Locations', component: () => import('@/views/locations/LocationListView.vue'), meta: { title: 'Locations' } },
      { path: 'locations/new', name: 'LocationNew', component: () => import('@/views/locations/LocationFormView.vue'), meta: { title: 'New Location', roles: ADMIN } },
      { path: 'locations/:id', name: 'Location', component: () => import('@/views/locations/LocationDetailView.vue'), meta: { title: 'Location' } },
      { path: 'locations/:id/edit', name: 'LocationEdit', component: () => import('@/views/locations/LocationFormView.vue'), meta: { title: 'Edit Location', roles: ADMIN } },

      // Schedules
      { path: 'schedules', name: 'Schedules', component: () => import('@/views/schedules/ScheduleListView.vue'), meta: { title: 'Schedules' } },
      { path: 'schedules/new', name: 'ScheduleNew', component: () => import('@/views/schedules/ScheduleFormView.vue'), meta: { title: 'New Schedule', roles: ADMIN } },
      { path: 'schedules/:id', name: 'Schedule', component: () => import('@/views/schedules/ScheduleDetailView.vue'), meta: { title: 'Schedule' } },
      { path: 'schedules/:id/edit', name: 'ScheduleEdit', component: () => import('@/views/schedules/ScheduleFormView.vue'), meta: { title: 'Edit Schedule', roles: ADMIN } },

      // Holidays
      { path: 'holidays', name: 'Holidays', component: () => import('@/views/holidays/HolidayListView.vue'), meta: { title: 'Holidays' } },
      { path: 'holidays/new', name: 'HolidayNew', component: () => import('@/views/holidays/HolidayFormView.vue'), meta: { title: 'New Holiday', roles: WRITE } },
      { path: 'holidays/:id', name: 'Holiday', component: () => import('@/views/holidays/HolidayDetailView.vue'), meta: { title: 'Holiday' } },
      { path: 'holidays/:id/edit', name: 'HolidayEdit', component: () => import('@/views/holidays/HolidayFormView.vue'), meta: { title: 'Edit Holiday', roles: WRITE } },

      // Portals
      { path: 'portals', name: 'Portals', component: () => import('@/views/portals/PortalListView.vue'), meta: { title: 'Portals' } },
      { path: 'portals/new', name: 'PortalNew', component: () => import('@/views/portals/PortalFormView.vue'), meta: { title: 'New Portal', roles: ADMIN } },
      { path: 'portals/:id', name: 'Portal', component: () => import('@/views/portals/PortalDetailView.vue'), meta: { title: 'Portal' } },
      { path: 'portals/:id/edit', name: 'PortalEdit', component: () => import('@/views/portals/PortalFormView.vue'), meta: { title: 'Edit Portal', roles: ADMIN } },

      // Aux inputs (named observe-only digital inputs)
      { path: 'aux-inputs', name: 'AuxInputs', component: () => import('@/views/aux/AuxInputListView.vue'), meta: { title: 'Aux Inputs' } },
      { path: 'aux-inputs/new', name: 'AuxInputNew', component: () => import('@/views/aux/AuxInputFormView.vue'), meta: { title: 'New Aux Input', roles: ADMIN } },
      { path: 'aux-inputs/:id', name: 'AuxInput', component: () => import('@/views/aux/AuxInputDetailView.vue'), meta: { title: 'Aux Input' } },
      { path: 'aux-inputs/:id/edit', name: 'AuxInputEdit', component: () => import('@/views/aux/AuxInputFormView.vue'), meta: { title: 'Edit Aux Input', roles: ADMIN } },

      // Aux outputs (named relays driven by cmd.output)
      { path: 'aux-outputs', name: 'AuxOutputs', component: () => import('@/views/aux/AuxOutputListView.vue'), meta: { title: 'Aux Outputs' } },
      { path: 'aux-outputs/new', name: 'AuxOutputNew', component: () => import('@/views/aux/AuxOutputFormView.vue'), meta: { title: 'New Aux Output', roles: ADMIN } },
      { path: 'aux-outputs/:id', name: 'AuxOutput', component: () => import('@/views/aux/AuxOutputDetailView.vue'), meta: { title: 'Aux Output' } },
      { path: 'aux-outputs/:id/edit', name: 'AuxOutputEdit', component: () => import('@/views/aux/AuxOutputFormView.vue'), meta: { title: 'Edit Aux Output', roles: ADMIN } },

      // Controllers
      { path: 'controllers', name: 'Controllers', component: () => import('@/views/controllers/ControllerListView.vue'), meta: { title: 'Controllers' } },
      { path: 'controllers/new', name: 'ControllerNew', component: () => import('@/views/controllers/ControllerFormView.vue'), meta: { title: 'New Controller', roles: ADMIN } },
      { path: 'controllers/:id', name: 'Controller', component: () => import('@/views/controllers/ControllerDetailView.vue'), meta: { title: 'Controller' } },
      { path: 'controllers/:id/edit', name: 'ControllerEdit', component: () => import('@/views/controllers/ControllerFormView.vue'), meta: { title: 'Edit Controller', roles: ADMIN } },

      // Access groups
      { path: 'access-groups', name: 'AccessGroups', component: () => import('@/views/access_groups/AccessGroupListView.vue'), meta: { title: 'Access Groups' } },
      { path: 'access-groups/new', name: 'AccessGroupNew', component: () => import('@/views/access_groups/AccessGroupFormView.vue'), meta: { title: 'New Access Group', roles: ADMIN } },
      { path: 'access-groups/:id', name: 'AccessGroup', component: () => import('@/views/access_groups/AccessGroupDetailView.vue'), meta: { title: 'Access Group' } },
      { path: 'access-groups/:id/edit', name: 'AccessGroupEdit', component: () => import('@/views/access_groups/AccessGroupFormView.vue'), meta: { title: 'Edit Access Group', roles: ADMIN } },

      // Roles
      { path: 'roles', name: 'Roles', component: () => import('@/views/roles/RoleListView.vue'), meta: { title: 'Roles' } },
      { path: 'roles/new', name: 'RoleNew', component: () => import('@/views/roles/RoleFormView.vue'), meta: { title: 'New Role', roles: ADMIN } },
      { path: 'roles/:id', name: 'Role', component: () => import('@/views/roles/RoleDetailView.vue'), meta: { title: 'Role' } },
      { path: 'roles/:id/edit', name: 'RoleEdit', component: () => import('@/views/roles/RoleFormView.vue'), meta: { title: 'Edit Role', roles: ADMIN } },

      // Cardholders
      { path: 'cardholders', name: 'Cardholders', component: () => import('@/views/cardholders/CardholderListView.vue'), meta: { title: 'Cardholders' } },
      { path: 'cardholders/new', name: 'CardholderNew', component: () => import('@/views/cardholders/CardholderFormView.vue'), meta: { title: 'New Cardholder', roles: WRITE } },
      { path: 'cardholders/:id', name: 'Cardholder', component: () => import('@/views/cardholders/CardholderDetailView.vue'), meta: { title: 'Cardholder' } },
      { path: 'cardholders/:id/edit', name: 'CardholderEdit', component: () => import('@/views/cardholders/CardholderFormView.vue'), meta: { title: 'Edit Cardholder', roles: WRITE } },

      // Credentials (a credential belongs to one cardholder)
      { path: 'credentials', name: 'Credentials', component: () => import('@/views/credentials/CredentialListView.vue'), meta: { title: 'Credentials' } },
      { path: 'credentials/new', name: 'CredentialNew', component: () => import('@/views/credentials/CredentialFormView.vue'), meta: { title: 'New Credential', roles: WRITE } },
      { path: 'credentials/:id', name: 'Credential', component: () => import('@/views/credentials/CredentialDetailView.vue'), meta: { title: 'Credential' } },
      { path: 'credentials/:id/edit', name: 'CredentialEdit', component: () => import('@/views/credentials/CredentialFormView.vue'), meta: { title: 'Edit Credential', roles: WRITE } },

      // Operators (the users auth collection — admin manages accounts/roles)
      { path: 'operators', name: 'Operators', component: () => import('@/views/operators/OperatorListView.vue'), meta: { title: 'Operators', roles: ADMIN } },
      { path: 'operators/new', name: 'OperatorNew', component: () => import('@/views/operators/OperatorFormView.vue'), meta: { title: 'New Operator', roles: ADMIN } },
      { path: 'operators/:id/edit', name: 'OperatorEdit', component: () => import('@/views/operators/OperatorFormView.vue'), meta: { title: 'Edit Operator', roles: ADMIN } },

      // Import (bulk-create cardholders + credentials from CSV)
      { path: 'import', name: 'Import', component: () => import('@/views/import/ImportView.vue'), meta: { title: 'Import', roles: WRITE } },

      // Events (read-only audit timeline)
      { path: 'events', name: 'Events', component: () => import('@/views/events/EventListView.vue'), meta: { title: 'Events' } },

      // Audit log (control-plane change history — admin only)
      { path: 'audit-log', name: 'AuditLog', component: () => import('@/views/audit/AuditLogListView.vue'), meta: { title: 'Audit Log', roles: ADMIN } },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to, from, next) => {
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
  // Role gate: a route may restrict itself to specific operator roles. On an
  // in-app navigation we stay put (just toast); on a direct URL load (no prior
  // route) we fall back to Overview, which every role can see.
  const roles = to.meta.roles as string[] | undefined
  if (roles && authStore.isAuthenticated && !roles.includes(authStore.role)) {
    useToast().error('You do not have permission to access that page.')
    if (from.name) {
      next(false)
    } else {
      next('/')
    }
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
