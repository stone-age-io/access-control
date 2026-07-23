/**
 * Semantic tint for the soft-badge system — the shared vocabulary between the
 * `SoftBadge` component's `tone` prop and the `.badge-soft-*` classes in
 * assets/main.css. Tone-returning helpers (eventKindTone, alarmTone, armTone, …)
 * produce one of these so a badge's colour has a single source of truth.
 */
export type SoftTone = 'info' | 'primary' | 'warning' | 'success' | 'neutral' | 'error'
