// Tiny dependency-free CSV utilities for the bulk-import view.
//
// parseCsv covers the RFC-4180 essentials: quoted fields, embedded commas and
// newlines, escaped "" inside quotes, a leading UTF-8 BOM, and both CRLF and LF
// line endings. toRecords maps the header row onto each data row as a plain
// object keyed by normalized (lowercased, trimmed) column names. A bundled CSV
// library would be overkill for this — the UI's dependency set is deliberately
// minimal — so we keep it small and local.

/** Parse CSV text into rows of raw cell strings. */
export function parseCsv(text: string): string[][] {
  // Strip a leading BOM so the first header cell isn't "﻿name".
  if (text.charCodeAt(0) === 0xfeff) text = text.slice(1)

  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let inQuotes = false

  const endField = () => {
    row.push(field)
    field = ''
  }
  const endRow = () => {
    endField()
    rows.push(row)
    row = []
  }

  const n = text.length
  for (let i = 0; i < n; i++) {
    const c = text[i]
    if (inQuotes) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"' // escaped quote
          i++
        } else {
          inQuotes = false
        }
      } else {
        field += c
      }
      continue
    }
    if (c === '"') {
      inQuotes = true
    } else if (c === ',') {
      endField()
    } else if (c === '\r') {
      endRow()
      if (text[i + 1] === '\n') i++ // swallow the LF of a CRLF
    } else if (c === '\n') {
      endRow()
    } else {
      field += c
    }
  }
  // Flush a final field/row unless the text ended exactly on a line break.
  if (field !== '' || row.length > 0) endRow()
  return rows
}

export interface CsvRecords {
  /** Normalized header names, in column order. */
  headers: string[]
  /** One object per non-empty data row, keyed by normalized header. */
  records: Record<string, string>[]
}

/**
 * Map a parsed CSV (header row + data rows) onto records keyed by normalized
 * header names. Fully blank rows are dropped; cell values are trimmed.
 */
export function toRecords(rows: string[][]): CsvRecords {
  if (rows.length === 0) return { headers: [], records: [] }
  const headers = rows[0].map((h) => h.trim().toLowerCase())
  const records: Record<string, string>[] = []
  for (let r = 1; r < rows.length; r++) {
    const cells = rows[r]
    if (cells.every((c) => c.trim() === '')) continue // skip blank lines
    const rec: Record<string, string> = {}
    headers.forEach((h, idx) => {
      rec[h] = (cells[idx] ?? '').trim()
    })
    records.push(rec)
  }
  return { headers, records }
}
