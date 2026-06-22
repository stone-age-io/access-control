<script setup lang="ts">
import { computed } from 'vue'

/**
 * Effective-posture badge that names its own provenance. The colour and prefix
 * tell an operator at a glance whether a posture is the door's normal standing
 * state, an active scheduled posture, or — the case this exists for — a manual
 * override someone set and may need to clear:
 *
 *   ⚙ Standing · secure      (neutral)   normal — nothing to do
 *   🕐 Scheduled · unlocked   (blue)      a schedule window is driving it
 *   ⚠ MANUAL · lockdown       (amber)     an operator override is in force
 */
const props = withDefaults(
  defineProps<{
    /** The effective posture value (secure, unlocked, lockdown, …). */
    posture?: string
    /** Provenance: 'standing' | 'scheduled' | 'override'. Empty → standing. */
    source?: string
  }>(),
  { posture: '', source: '' },
)

type Look = { label: string; icon: string; cls: string; emphatic?: boolean }

const LOOKS: Record<string, Look> = {
  override: { label: 'Manual', icon: '⚠', cls: 'badge-warning font-semibold', emphatic: true },
  scheduled: { label: 'Scheduled', icon: '🕐', cls: 'badge-info badge-outline' },
  standing: { label: 'Standing', icon: '⚙', cls: 'badge-ghost' },
}

const look = computed(() => LOOKS[props.source || 'standing'] ?? LOOKS.standing)
const value = computed(() => props.posture || '—')
</script>

<template>
  <span
    class="badge badge-sm gap-1 whitespace-nowrap"
    :class="look.cls"
    :title="`${look.label} posture: ${value}`"
  >
    <span aria-hidden="true">{{ look.icon }}</span>
    <span :class="{ 'uppercase tracking-wide': look.emphatic }">{{ look.label }}</span>
    <span class="opacity-50">·</span>
    <span>{{ value }}</span>
  </span>
</template>
