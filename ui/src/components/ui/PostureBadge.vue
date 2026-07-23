<script setup lang="ts">
import { computed } from 'vue'
import type { SoftTone } from '@/utils/badges'

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

type Look = { label: string; icon: string; tone: SoftTone; emphatic?: boolean }

const LOOKS: Record<string, Look> = {
  override: { label: 'Manual', icon: '⚠', tone: 'warning', emphatic: true },
  scheduled: { label: 'Scheduled', icon: '🕐', tone: 'info' },
  standing: { label: 'Standing', icon: '⚙', tone: 'neutral' },
}

const look = computed(() => LOOKS[props.source || 'standing'] ?? LOOKS.standing)
const value = computed(() => props.posture || '—')
</script>

<template>
  <span
    class="badge-soft gap-1"
    :class="`badge-soft-${look.tone}`"
    :title="`${look.label} posture: ${value}`"
  >
    <span aria-hidden="true">{{ look.icon }}</span>
    <span :class="{ 'uppercase tracking-wide': look.emphatic }">{{ look.label }}</span>
    <span class="opacity-50">·</span>
    <span>{{ value }}</span>
  </span>
</template>
