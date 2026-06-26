// Tiny dependency-free CSV utilities for bulk import and report export.
//
// parseCsv covers the RFC-4180 essentials: quoted fields, embedded commas and
// newlines, escaped "" inside quotes, a leading UTF-8 BOM, and both CRLF and LF
// line endings. toRecords maps the header row onto each data row as a plain
// object keyed by normalized (lowercased, trimmed) column names. toCsv/downloadCsv
// are the inverse, used by the report exports. A bundled CSV library would be
// overkill for this — the UI's dependency set is deliberately minimal — so we
// keep it small and local.

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

/** A CSV export column: which key to read from each row, and its header label. */
export interface CsvColumn<T> {
  key: keyof T & string
  label: string
}

/** Quote a cell only when it contains a comma, quote, or line break (RFC 4180). */
function escapeCell(value: unknown): string {
  const s = value == null ? '' : String(value)
  return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

/**
 * Serialize rows of objects to RFC-4180 CSV text. `columns` defines both the
 * header row and the column order; missing keys render as empty cells.
 */
export function toCsv<T>(rows: T[], columns: CsvColumn<T>[]): string {
  const header = columns.map((c) => escapeCell(c.label)).join(',')
  const body = rows.map((r) => columns.map((c) => escapeCell(r[c.key])).join(',')).join('\r\n')
  return body ? `${header}\r\n${body}` : header
}

/** Trigger a browser download of CSV text as a file. */
export function downloadCsv(filename: string, csv: string): void {
  // A leading BOM makes Excel read the file as UTF-8 (parseCsv strips it back off).
  const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

/** Compact local timestamp (YYYYMMDD-HHmm) for export filenames. */
export function fileStamp(d = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}`
}
