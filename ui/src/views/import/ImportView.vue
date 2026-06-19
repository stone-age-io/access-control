<script setup lang="ts">
import { ref, computed } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { parseCsv, toRecords } from '@/utils/csv'
import type {
  Cardholder,
  Credential,
  Role,
  CardholderStatus,
  CredentialStatus,
  CredentialType,
} from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import RailCard from '@/components/ui/RailCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import type { Column } from '@/components/ui/ResponsiveList.vue'

const toast = useToast()
const { confirm } = useConfirm()

// ---- enum domains (mirror pbmigrations/1750000000_collections.go) ----
const CH_STATUSES = ['active', 'suspended'] as const
const CRED_TYPES = ['nkey', 'wiegand', 'pin', 'mobile'] as const
const CRED_STATUSES = ['active', 'revoked', 'suspended'] as const

// Recognized header names (canonical + a few friendly aliases). Used to flag
// columns we'll ignore so the operator isn't surprised.
const RECOGNIZED = new Set([
  'name', 'email', 'external_id', 'externalid', 'external id', 'status',
  'roles', 'role', 'credential_value', 'value', 'card_number', 'card',
  'credential_type', 'type', 'credential_status', 'credential_label', 'label',
])

// The example template — its header row is the single source of truth for the
// columns the parser accepts. Shows two badges for one cardholder (repeat the
// external_id), a multi-role cell, and a credential-less cardholder.
const TEMPLATE_CSV = [
  'name,email,external_id,status,roles,credential_value,credential_type,credential_status,credential_label',
  'Alice Smith,alice@example.com,EMP-1001,active,staff;managers,CARD-0001,wiegand,active,Alice badge',
  'Alice Smith,alice@example.com,EMP-1001,active,,MOBILE-ALICE,mobile,active,Alice phone',
  'Bob Jones,bob@example.com,EMP-1002,active,staff,CARD-0002,wiegand,active,Bob badge',
  'Carol Vance,carol@example.com,EMP-1003,suspended,,,,,',
].join('\n')

// ---- view state ----
type Step = 'input' | 'preview' | 'result'
type Mode = 'skip' | 'update'
type Action = 'create' | 'update' | 'skip' | 'error'

const step = ref<Step>('input')
const mode = ref<Mode>('skip')
const csvText = ref('')
const fileName = ref('')
const busy = ref(false)

// ---- planning model ----
interface CardholderOp {
  key: string
  firstLine: number
  action: Action
  existingId?: string
  createData?: Record<string, unknown>
  patch?: Record<string, unknown>
  error?: string
  resolvedId?: string // filled during execution
}
interface CredentialOp {
  line: number
  chKey: string
  value: string
  action: Action
  existingId?: string
  createData?: Record<string, unknown>
  patch?: Record<string, unknown>
  error?: string
}
interface PreviewRow {
  id: string
  line: number
  chLabel: string
  chKey: string
  chAction: Action | 'reuse'
  credValue: string
  credType: string
  credAction: Action | 'none'
  errors: string[]
  warnings: string[]
}
interface Plan {
  rows: PreviewRow[]
  cardholderOps: Map<string, CardholderOp>
  credentialOps: CredentialOp[]
  unknownColumns: string[]
  counts: {
    chCreate: number; chUpdate: number; chSkip: number; chError: number
    credCreate: number; credUpdate: number; credSkip: number; credError: number
    rowErrors: number
  }
}

const plan = ref<Plan | null>(null)

const writeTotal = computed(() => {
  if (!plan.value) return 0
  const c = plan.value.counts
  return c.chCreate + c.chUpdate + c.credCreate + c.credUpdate
})

// ---- execution progress / result ----
const processed = ref(0)
const result = ref({
  chCreated: 0, chUpdated: 0, chSkipped: 0,
  credCreated: 0, credUpdated: 0, credSkipped: 0,
  failed: 0,
  errors: [] as { line: number; message: string }[],
})

// ---- helpers ----
function getAlias(rec: Record<string, string>, names: string[]): string {
  for (const n of names) {
    const v = rec[n]
    if (v !== undefined && v !== '') return v
  }
  return ''
}

function normalizeEnum<T extends string>(
  raw: string,
  allowed: readonly T[],
  dflt: T,
): { value: T; error?: string } {
  const s = raw.trim().toLowerCase()
  if (!s) return { value: dflt }
  if ((allowed as readonly string[]).includes(s)) return { value: s as T }
  return { value: dflt, error: `"${raw}" is not one of ${allowed.join('/')}` }
}

function errMsg(e: unknown): string {
  const anyE = e as any
  const data = anyE?.response?.data ?? anyE?.data
  if (data && typeof data === 'object') {
    const parts = Object.entries(data).map(
      ([k, v]: [string, any]) => `${k}: ${v?.message ?? v}`,
    )
    if (parts.length) return parts.join('; ')
  }
  return anyE?.message ?? String(e)
}

// ---- file / template ----
async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    csvText.value = await file.text()
    fileName.value = file.name
  } catch (err) {
    toast.error(errMsg(err))
  }
  input.value = '' // allow re-selecting the same file
}

function downloadTemplate() {
  const blob = new Blob([TEMPLATE_CSV + '\n'], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'cardholders-import-template.csv'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

// ---- build the dry-run plan ----
async function buildPlan() {
  if (!csvText.value.trim()) {
    toast.error('Paste CSV text or choose a file first')
    return
  }

  busy.value = true
  try {
    const { headers, records } = toRecords(parseCsv(csvText.value))
    if (records.length === 0) {
      toast.error('No data rows found (need a header row plus at least one row)')
      return
    }

    // Load reference data once; everything else is computed client-side.
    const [chs, creds, roles] = await Promise.all([
      pb.collection('cardholders').getFullList<Cardholder>({ fields: 'id,external_id,email' }),
      pb.collection('credentials').getFullList<Credential>({ fields: 'id,value' }),
      pb.collection('roles').getFullList<Role>({ fields: 'id,code', sort: 'code' }),
    ])

    const chByExternalId = new Map<string, Cardholder>()
    const chByEmail = new Map<string, Cardholder>()
    for (const c of chs) {
      if (c.external_id && !chByExternalId.has(c.external_id)) chByExternalId.set(c.external_id, c)
      if (c.email) {
        const k = c.email.toLowerCase()
        if (!chByEmail.has(k)) chByEmail.set(k, c)
      }
    }
    const credByValue = new Map<string, Credential>()
    for (const c of creds) credByValue.set(c.value, c)
    const roleByCode = new Map<string, Role>()
    for (const r of roles) roleByCode.set(r.code, r)

    const rows: PreviewRow[] = []
    const chOps = new Map<string, CardholderOp>()
    const credOps: CredentialOp[] = []
    const seenCredValues = new Map<string, number>()

    records.forEach((rec, idx) => {
      const line = idx + 2 // header is line 1
      const errors: string[] = []
      const warnings: string[] = []

      const name = (rec.name ?? '').trim()
      const email = (rec.email ?? '').trim()
      const externalId = getAlias(rec, ['external_id', 'externalid', 'external id'])
      const rolesCell = getAlias(rec, ['roles', 'role'])
      const rolesProvided = rolesCell.trim() !== ''
      const statusRaw = (rec.status ?? '').trim()
      const credValue = getAlias(rec, ['credential_value', 'value', 'card_number', 'card'])
      const credTypeRaw = getAlias(rec, ['credential_type', 'type'])
      const credStatusRaw = (rec.credential_status ?? '').trim()
      const credLabel = getAlias(rec, ['credential_label', 'label'])

      // Dedup key: external_id, else email, else a synthetic per-row key (those
      // rows can only ever create — they can't be matched or grouped).
      const key = externalId || (email ? `email:${email.toLowerCase()}` : `__line${line}`)
      const firstOccurrence = !chOps.has(key)

      if (firstOccurrence) {
        const roleCodes = rolesProvided
          ? rolesCell.split(/[;,]/).map((s) => s.trim()).filter(Boolean)
          : []
        const roleIds: string[] = []
        const unknownRoles: string[] = []
        for (const code of roleCodes) {
          const r = roleByCode.get(code)
          if (r) roleIds.push(r.id)
          else unknownRoles.push(code)
        }
        const st = normalizeEnum(statusRaw, CH_STATUSES, 'active' as CardholderStatus)

        let existing: Cardholder | undefined
        if (externalId) existing = chByExternalId.get(externalId)
        else if (email) existing = chByEmail.get(email.toLowerCase())

        let action: Action = existing ? (mode.value === 'skip' ? 'skip' : 'update') : 'create'
        const opErrors: string[] = []
        if (unknownRoles.length) opErrors.push(`unknown role code(s): ${unknownRoles.join(', ')}`)
        if (st.error) opErrors.push(`status ${st.error}`)
        if (action === 'create' && !name && !email) opErrors.push('cardholder needs a name or email')
        if (opErrors.length) action = 'error'

        let createData: Record<string, unknown> | undefined
        let patch: Record<string, unknown> | undefined
        if (action === 'create') {
          createData = { external_id: externalId, name, email, status: st.value, roles: roleIds }
        } else if (action === 'update') {
          // Patch only the columns the CSV actually provides, so blank cells
          // never clobber existing data.
          patch = {}
          if (name) patch.name = name
          if (email) patch.email = email
          if (externalId) patch.external_id = externalId
          if (statusRaw) patch.status = st.value
          if (rolesProvided) patch.roles = roleIds
        }
        chOps.set(key, { key, firstLine: line, action, existingId: existing?.id, createData, patch, error: opErrors[0] })
        opErrors.forEach((e) => errors.push(e))
      } else if (name || email || rolesProvided || statusRaw || externalId) {
        warnings.push(`repeat cardholder — fields here are ignored (set on line ${chOps.get(key)!.firstLine})`)
      }

      const chOp = chOps.get(key)!

      // ---- credential on this row (optional) ----
      let credAction: Action | 'none' = 'none'
      if (credValue) {
        const dupLine = seenCredValues.get(credValue)
        if (dupLine) {
          errors.push(`duplicate credential value "${credValue}" (also line ${dupLine})`)
          credAction = 'error'
        } else {
          seenCredValues.set(credValue, line)
          if (/\s/.test(credValue)) warnings.push('credential value contains spaces (it becomes the KV key)')
          const ty = normalizeEnum(credTypeRaw, CRED_TYPES, 'wiegand' as CredentialType)
          const cs = normalizeEnum(credStatusRaw, CRED_STATUSES, 'active' as CredentialStatus)
          const existingCred = credByValue.get(credValue)

          let action: Action = existingCred ? (mode.value === 'skip' ? 'skip' : 'update') : 'create'
          const cErrors: string[] = []
          if (ty.error) cErrors.push(`credential type ${ty.error}`)
          if (cs.error) cErrors.push(`credential status ${cs.error}`)
          if (chOp.action === 'error') cErrors.push('credential has no valid cardholder on this row')
          if (cErrors.length) action = 'error'
          cErrors.forEach((e) => errors.push(e))

          let createData: Record<string, unknown> | undefined
          let patch: Record<string, unknown> | undefined
          if (action === 'create') {
            createData = { value: credValue, type: ty.value, status: cs.value, label: credLabel } // user set at execute
          } else if (action === 'update') {
            patch = {}
            if (credTypeRaw) patch.type = ty.value
            if (credStatusRaw) patch.status = cs.value
            if (credLabel) patch.label = credLabel
          }
          credOps.push({ line, chKey: key, value: credValue, action, existingId: existingCred?.id, createData, patch, error: cErrors[0] })
          credAction = action
        }
      }

      rows.push({
        id: `r${line}`,
        line,
        chLabel: name || email || externalId || '(unnamed)',
        chKey: externalId || email || '',
        chAction: firstOccurrence ? chOp.action : 'reuse',
        credValue,
        credType: credValue ? credTypeRaw.trim().toLowerCase() || 'wiegand' : '',
        credAction,
        errors,
        warnings,
      })
    })

    const chArr = [...chOps.values()]
    plan.value = {
      rows,
      cardholderOps: chOps,
      credentialOps: credOps,
      unknownColumns: headers.filter((h) => h && !RECOGNIZED.has(h)),
      counts: {
        chCreate: chArr.filter((o) => o.action === 'create').length,
        chUpdate: chArr.filter((o) => o.action === 'update').length,
        chSkip: chArr.filter((o) => o.action === 'skip').length,
        chError: chArr.filter((o) => o.action === 'error').length,
        credCreate: credOps.filter((o) => o.action === 'create').length,
        credUpdate: credOps.filter((o) => o.action === 'update').length,
        credSkip: credOps.filter((o) => o.action === 'skip').length,
        credError: credOps.filter((o) => o.action === 'error').length,
        rowErrors: rows.filter((r) => r.errors.length > 0).length,
      },
    }
    step.value = 'preview'
  } catch (err) {
    toast.error(errMsg(err))
  } finally {
    busy.value = false
  }
}

// ---- execute the plan ----
async function runImport() {
  if (!plan.value) return
  const c = plan.value.counts
  const ok = await confirm({
    title: 'Run import',
    message: `Create ${c.chCreate} and update ${c.chUpdate} cardholder(s); create ${c.credCreate} and update ${c.credUpdate} credential(s).`,
    details: c.rowErrors > 0
      ? `${c.rowErrors} row(s) have errors and will be skipped. ${c.chSkip + c.credSkip} record(s) already exist and will be skipped.`
      : `${c.chSkip + c.credSkip} record(s) already exist and will be skipped.`,
    confirmText: 'Import',
    variant: 'info',
  })
  if (!ok) return

  busy.value = true
  processed.value = 0
  const res = {
    chCreated: 0, chUpdated: 0, chSkipped: 0,
    credCreated: 0, credUpdated: 0, credSkipped: 0,
    failed: 0,
    errors: [] as { line: number; message: string }[],
  }

  // Phase 1 — cardholders first, capturing the id each row's credential needs.
  for (const op of plan.value.cardholderOps.values()) {
    if (op.action === 'error') continue
    try {
      if (op.action === 'create') {
        const rec = await pb.collection('cardholders').create<Cardholder>(op.createData!)
        op.resolvedId = rec.id
        res.chCreated++
      } else if (op.action === 'update') {
        await pb.collection('cardholders').update(op.existingId!, op.patch!)
        op.resolvedId = op.existingId
        res.chUpdated++
      } else if (op.action === 'skip') {
        op.resolvedId = op.existingId
        res.chSkipped++
      }
    } catch (err) {
      res.failed++
      res.errors.push({ line: op.firstLine, message: `cardholder: ${errMsg(err)}` })
    }
    processed.value++
  }

  // Phase 2 — credentials, resolving the holder to its (now-created) id.
  for (const op of plan.value.credentialOps) {
    if (op.action === 'error') continue
    if (op.action === 'skip') {
      res.credSkipped++
      continue
    }
    const holder = plan.value.cardholderOps.get(op.chKey)
    if (!holder?.resolvedId) {
      res.failed++
      res.errors.push({ line: op.line, message: `credential "${op.value}": holder was not created` })
      processed.value++
      continue
    }
    try {
      if (op.action === 'create') {
        await pb.collection('credentials').create({ ...op.createData, user: holder.resolvedId })
        res.credCreated++
      } else if (op.action === 'update') {
        await pb.collection('credentials').update(op.existingId!, { ...op.patch, user: holder.resolvedId })
        res.credUpdated++
      }
    } catch (err) {
      res.failed++
      res.errors.push({ line: op.line, message: `credential "${op.value}": ${errMsg(err)}` })
    }
    processed.value++
  }

  result.value = res
  busy.value = false
  step.value = 'result'
  if (res.failed === 0) toast.success('Import complete')
  else toast.error(`Import finished with ${res.failed} error(s)`)
}

function reset() {
  step.value = 'input'
  csvText.value = ''
  fileName.value = ''
  plan.value = null
  processed.value = 0
}

// ---- preview table ----
const columns: Column<PreviewRow>[] = [
  { key: 'line', label: '#', class: 'w-12' },
  { key: 'chLabel', label: 'Cardholder' },
  { key: 'chAction', label: 'Cardholder action' },
  { key: 'credValue', label: 'Credential' },
  { key: 'credAction', label: 'Credential action' },
  { key: 'messages', label: 'Notes' },
]

const ACTION_BADGE: Record<string, string> = {
  create: 'badge-success',
  update: 'badge-info',
  skip: 'badge-ghost',
  error: 'badge-error',
  reuse: 'badge-ghost',
  none: '',
}
</script>

<template>
  <DetailLayout
    title="Import"
    subtitle="Bulk-create cardholders and their credentials from a single CSV."
    :breadcrumbs="[{ label: 'Cardholders', to: '/cardholders' }, { label: 'Import' }]"
  >
    <!-- MAIN COLUMN -->
    <template v-if="step === 'input'">
      <BaseCard title="1 · Source CSV">
        <div class="space-y-4">
          <div class="form-control">
            <label class="label"><span class="label-text">Upload a file</span></label>
            <input type="file" accept=".csv,text/csv" class="file-input file-input-bordered w-full" @change="onFile" />
            <label v-if="fileName" class="label">
              <span class="label-text-alt">Loaded <code class="font-mono">{{ fileName }}</code></span>
            </label>
          </div>

          <div class="divider text-xs opacity-50">or paste</div>

          <div class="form-control">
            <textarea
              v-model="csvText"
              rows="10"
              class="textarea textarea-bordered font-mono text-xs w-full"
              placeholder="name,email,external_id,status,roles,credential_value,credential_type,credential_status,credential_label&#10;Alice Smith,alice@example.com,EMP-1001,active,staff,CARD-0001,wiegand,active,Alice badge"
            ></textarea>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="2 · How to handle existing records">
        <div class="space-y-3">
          <label class="flex items-start gap-3 cursor-pointer p-3 rounded-box border border-base-300 hover:bg-base-200">
            <input type="radio" class="radio radio-sm mt-0.5" value="skip" v-model="mode" />
            <span>
              <span class="font-medium">Skip existing</span>
              <span class="block text-sm opacity-60">Only create new records. Matching cardholders/credentials are left untouched. (Safest.)</span>
            </span>
          </label>
          <label class="flex items-start gap-3 cursor-pointer p-3 rounded-box border border-base-300 hover:bg-base-200">
            <input type="radio" class="radio radio-sm mt-0.5" value="update" v-model="mode" />
            <span>
              <span class="font-medium">Update existing</span>
              <span class="block text-sm opacity-60">Overwrite matching records with the columns present in the CSV (blank cells are left as-is).</span>
            </span>
          </label>
        </div>
      </BaseCard>
    </template>

    <template v-else-if="step === 'preview' && plan">
      <BaseCard title="Summary">
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Cardholders</div>
            <div class="stat-value text-2xl">{{ plan.counts.chCreate + plan.counts.chUpdate }}</div>
            <div class="stat-desc">{{ plan.counts.chCreate }} new · {{ plan.counts.chUpdate }} update · {{ plan.counts.chSkip }} skip</div>
          </div>
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Credentials</div>
            <div class="stat-value text-2xl">{{ plan.counts.credCreate + plan.counts.credUpdate }}</div>
            <div class="stat-desc">{{ plan.counts.credCreate }} new · {{ plan.counts.credUpdate }} update · {{ plan.counts.credSkip }} skip</div>
          </div>
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Mode</div>
            <div class="stat-value text-2xl capitalize">{{ mode }}</div>
            <div class="stat-desc">existing records</div>
          </div>
          <div class="stat rounded-box py-3 px-4" :class="plan.counts.rowErrors ? 'bg-error/10' : 'bg-base-200/50'">
            <div class="stat-title text-xs">Rows with errors</div>
            <div class="stat-value text-2xl" :class="plan.counts.rowErrors ? 'text-error' : ''">{{ plan.counts.rowErrors }}</div>
            <div class="stat-desc">skipped on import</div>
          </div>
        </div>

        <div v-if="plan.unknownColumns.length" class="alert alert-warning mt-4 text-sm">
          <span>⚠ Ignored unrecognized column(s): <code>{{ plan.unknownColumns.join(', ') }}</code></span>
        </div>
      </BaseCard>

      <BaseCard title="Row-by-row preview" :no-padding="true">
        <div class="p-4">
          <ResponsiveList :items="plan.rows" :columns="columns" :clickable="false">
            <template #cell-chLabel="{ item }">
              <div class="font-medium">{{ item.chLabel }}</div>
              <code v-if="item.chKey" class="text-[11px] opacity-50">{{ item.chKey }}</code>
            </template>
            <template #cell-chAction="{ item }">
              <span class="badge badge-sm" :class="ACTION_BADGE[item.chAction]">{{ item.chAction }}</span>
            </template>
            <template #cell-credValue="{ item }">
              <code v-if="item.credValue" class="text-sm">{{ item.credValue }}</code>
              <span v-else class="opacity-40">—</span>
              <span v-if="item.credType" class="block text-[11px] opacity-50">{{ item.credType }}</span>
            </template>
            <template #cell-credAction="{ item }">
              <span v-if="item.credAction !== 'none'" class="badge badge-sm" :class="ACTION_BADGE[item.credAction]">{{ item.credAction }}</span>
              <span v-else class="opacity-40">—</span>
            </template>
            <template #cell-messages="{ item }">
              <div v-if="item.errors.length || item.warnings.length" class="space-y-1">
                <div v-for="(e, i) in item.errors" :key="`e${i}`" class="text-xs text-error">{{ e }}</div>
                <div v-for="(w, i) in item.warnings" :key="`w${i}`" class="text-xs text-warning">{{ w }}</div>
              </div>
              <span v-else class="opacity-40">—</span>
            </template>

            <template #card-chLabel="{ item }">
              <div class="text-sm font-bold text-primary truncate">{{ item.chLabel }}</div>
            </template>
            <template #card-chAction="{ item }">
              <span class="badge badge-xs" :class="ACTION_BADGE[item.chAction]">{{ item.chAction }}</span>
            </template>
            <template #card-credValue="{ item }">
              <code v-if="item.credValue" class="text-xs">{{ item.credValue }}</code>
              <span v-else>—</span>
            </template>
            <template #card-credAction="{ item }">
              <span v-if="item.credAction !== 'none'" class="badge badge-xs" :class="ACTION_BADGE[item.credAction]">{{ item.credAction }}</span>
              <span v-else>—</span>
            </template>
            <template #card-messages="{ item }">
              <div v-if="item.errors.length || item.warnings.length" class="text-right">
                <div v-for="(e, i) in item.errors" :key="`e${i}`" class="text-[11px] text-error">{{ e }}</div>
                <div v-for="(w, i) in item.warnings" :key="`w${i}`" class="text-[11px] text-warning">{{ w }}</div>
              </div>
              <span v-else>—</span>
            </template>
          </ResponsiveList>
        </div>
      </BaseCard>
    </template>

    <template v-else-if="step === 'result'">
      <BaseCard :title="result.failed === 0 ? '✅ Import complete' : '⚠ Import finished with errors'">
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Cardholders created</div>
            <div class="stat-value text-2xl text-success">{{ result.chCreated }}</div>
          </div>
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Cardholders updated</div>
            <div class="stat-value text-2xl text-info">{{ result.chUpdated }}</div>
          </div>
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Cardholders skipped</div>
            <div class="stat-value text-2xl">{{ result.chSkipped }}</div>
          </div>
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Credentials created</div>
            <div class="stat-value text-2xl text-success">{{ result.credCreated }}</div>
          </div>
          <div class="stat bg-base-200/50 rounded-box py-3 px-4">
            <div class="stat-title text-xs">Credentials updated</div>
            <div class="stat-value text-2xl text-info">{{ result.credUpdated }}</div>
          </div>
          <div class="stat rounded-box py-3 px-4" :class="result.failed ? 'bg-error/10' : 'bg-base-200/50'">
            <div class="stat-title text-xs">Failed</div>
            <div class="stat-value text-2xl" :class="result.failed ? 'text-error' : ''">{{ result.failed }}</div>
          </div>
        </div>

        <div v-if="result.errors.length" class="mt-4">
          <h3 class="text-sm font-bold mb-2">Errors</h3>
          <div class="border border-base-300 rounded-box divide-y divide-base-200 max-h-72 overflow-y-auto">
            <div v-for="(e, i) in result.errors" :key="i" class="flex gap-3 px-3 py-2 text-sm">
              <span class="badge badge-ghost badge-sm shrink-0">line {{ e.line }}</span>
              <span class="text-error">{{ e.message }}</span>
            </div>
          </div>
        </div>
      </BaseCard>
    </template>

    <div v-if="busy && step === 'preview'" class="fixed inset-0 bg-base-300/40 backdrop-blur-sm z-30 flex items-center justify-center">
      <div class="card bg-base-100 shadow-xl p-6 w-80 text-center space-y-3">
        <span class="loading loading-spinner loading-lg mx-auto"></span>
        <p class="font-medium">Importing…</p>
        <progress class="progress progress-primary w-full" :value="processed" :max="writeTotal || 1"></progress>
        <p class="text-sm opacity-60">{{ processed }} / {{ writeTotal }}</p>
      </div>
    </div>

    <!-- RAIL -->
    <template #rail>
      <RailCard v-if="step === 'input'" title="CSV format" icon="📄">
        <p class="text-xs opacity-60 leading-relaxed">
          One row per credential. Columns: <code class="text-[11px]">name</code>,
          <code class="text-[11px]">email</code>, <code class="text-[11px]">external_id</code>,
          <code class="text-[11px]">status</code>, <code class="text-[11px]">roles</code>,
          <code class="text-[11px]">credential_value</code>, <code class="text-[11px]">credential_type</code>,
          <code class="text-[11px]">credential_status</code>, <code class="text-[11px]">credential_label</code>.
        </p>
        <ul class="text-xs opacity-60 leading-relaxed list-disc pl-4 space-y-1">
          <li>Repeat a cardholder's <code class="text-[11px]">external_id</code> (or email) on more rows to add more badges.</li>
          <li>List multiple <code class="text-[11px]">roles</code> in one cell, separated by <code class="text-[11px]">;</code>.</li>
          <li>Leave the credential columns blank for a cardholder with no badge yet.</li>
          <li>Cardholders match on <code class="text-[11px]">external_id</code> then email; credentials on their value.</li>
        </ul>
        <button type="button" class="btn btn-sm btn-outline w-full" @click="downloadTemplate">
          ⬇ Download example CSV
        </button>
      </RailCard>

      <RailCard v-else-if="step === 'preview'" title="Before you import" icon="✅">
        <p class="text-xs opacity-60 leading-relaxed">
          This is a dry run — nothing has been written yet. <span class="font-medium">{{ writeTotal }}</span>
          record(s) will be created or updated when you confirm. Rows marked
          <span class="badge badge-error badge-xs">error</span> are skipped.
        </p>
      </RailCard>
    </template>

    <!-- FOOTER -->
    <template #footer>
      <template v-if="step === 'input'">
        <router-link to="/cardholders" class="btn btn-ghost">Cancel</router-link>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="buildPlan">
          <span v-if="busy" class="loading loading-spinner"></span>
          <span v-else>Preview import →</span>
        </button>
      </template>
      <template v-else-if="step === 'preview'">
        <button type="button" class="btn btn-ghost" :disabled="busy" @click="step = 'input'">← Back</button>
        <button type="button" class="btn btn-primary" :disabled="busy || writeTotal === 0" @click="runImport">
          <span v-if="busy" class="loading loading-spinner"></span>
          <span v-else>Import {{ writeTotal }} record(s)</span>
        </button>
      </template>
      <template v-else-if="step === 'result'">
        <button type="button" class="btn btn-ghost" @click="reset">Import another file</button>
        <router-link to="/cardholders" class="btn btn-primary">View cardholders</router-link>
      </template>
    </template>
  </DetailLayout>
</template>
