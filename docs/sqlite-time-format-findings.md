# SQLite time format: mattn vs modernc compatibility findings

**Date:** 2026-03-22
**Context:** Migration of ory/cloud services from `mattn/go-sqlite3` to `modernc.org/sqlite`

---

## Background

`mattn/go-sqlite3` and `modernc.org/sqlite` store `time.Time` values in different
on-disk formats by default. This matters for any application that:

- Uses keyset (cursor-based) pagination on timestamp columns, or
- Migrates from one driver to the other on an existing database.

---

## On-disk format comparison

### mattn/go-sqlite3

Uses `SQLiteTimestampFormats[0]`:

```
2006-01-02 15:04:05.999999999-07:00
```

Example output for a UTC timestamp: `"2026-03-21 12:34:56+00:00"` (space separator,
numeric offset).

### modernc.org/sqlite — default (no `_time_format`)

Uses RFC3339Nano when writing:

```
2006-01-02T15:04:05.999999999Z07:00
```

Example output: `"2026-03-21T12:34:56Z"` (T separator, Z for UTC).

### modernc.org/sqlite — with `_time_format=sqlite`

Uses the same format string as mattn (`parseTimeFormats[0]`):

```
2006-01-02 15:04:05.999999999-07:00
```

Example output: `"2026-03-21 12:34:56+00:00"` — **byte-for-byte identical to mattn**.

---

## Key finding: cursor-based pagination breaks without `_time_format=sqlite`

When a DATETIME column is scanned into a `*string` in modernc (default mode),
`convertAssign` parses the stored string as `time.Time` and re-formats it as
RFC3339Nano. The returned string is therefore `"2026-03-21T12:34:56Z"` regardless
of what is stored on disk.

If this string is then used as a cursor in `WHERE ts > ?`:

```
'T' (0x54) > ' ' (0x20)
```

Every RFC3339 cursor is lexicographically greater than all space-separated stored
values. The query returns no rows.

**This bug affects any application that:**

1. Stores rows with mattn (space-separated format on disk), then
2. Reads a cursor by scanning into `*string` with modernc (default mode), then
3. Uses that cursor in a `WHERE ts > ?` predicate.

---

## How pop is affected

Pop passes timestamp cursor values to `WHERE` predicates as `time.Time` (via
keyset pagination), not as pre-formatted strings. The driver then serializes the
`time.Time` when binding the query parameter.

With `_time_format=sqlite`:

- modernc serializes a `time.Time` query parameter as `"2026-03-21 12:34:56+00:00"`
- This is identical to the stored format
- `WHERE commit_time > ?` works correctly

Without `_time_format=sqlite`:

- modernc serializes the parameter as `"2026-03-21T12:34:56Z"`
- `'T' > ' '` breaks all comparisons against mattn-written rows
- Pagination silently returns no results

**The DSN must include `_time_format=sqlite` when using modernc with any database
that was written by mattn, or when lexicographic timestamp ordering is required.**

---

## UTC normalization requirement

Both mattn and modernc with `_time_format=sqlite` preserve the timezone offset of
the input `time.Time`. A non-UTC value stored as `"2026-03-21 13:34:56+01:00"` and
a UTC value stored as `"2026-03-21 12:34:56+00:00"` represent the same instant but
compare differently as strings.

For correct cursor-based pagination, all timestamps must be stored and used in UTC:

- **On write:** call `.UTC()` before passing to the driver — e.g., `time.Now().UTC()`
- **On read:** normalize after scanning — e.g., `t = t.UTC()`
- **Cursor binding:** pass the `.UTC()` value to `WHERE ts > ?`

Mixed-timezone storage is not safe for lexicographic ordering.

---

## Empirically verified behavior

All findings below were verified with cross-driver compat tests
(`test/sqlite-compat/compat_test.go`).

| Scenario | Result |
|----------|--------|
| mattn write → modernc (`_time_format=sqlite`) read | Correct `time.Time` round-trip |
| modernc (`_time_format=sqlite`) write → mattn read | Correct `time.Time` round-trip |
| mattn on-disk format | `"2026-03-21 12:34:56+00:00"` |
| modernc `_time_format=sqlite` on-disk format | `"2026-03-21 12:34:56+00:00"` (identical) |
| modernc default on-disk format | `"2026-03-21T12:34:56Z"` (different) |
| Cursor as `*string` (modernc default) in `WHERE ts > ?` | **Broken** — returns no rows |
| Cursor as `time.Time` (pop behavior) with `_time_format=sqlite` | **Correct** |
| Cursor as `time.Time` without `_time_format=sqlite` | **Broken** — RFC3339Nano parameter |
| Mixed mattn + modernc rows, `time.Time` cursor | Correct when UTC-normalized |

---

## Recommended DSN for modernc.org/sqlite

```
sqlite://file:db.sqlite?_fk=true&_time_format=sqlite&_busy_timeout=100000&_pragma=journal_mode(WAL)
```

Note: `_journal_mode=WAL` is a mattn DSN parameter. modernc uses `_pragma=journal_mode(WAL)`.

---

## Notes on scanning behavior

When scanning a DATETIME column with modernc:

| Destination type | Behavior with `_time_format=sqlite` |
|-----------------|--------------------------------------|
| `*time.Time` | Parses stored string correctly; returns correct instant |
| `*string` | Re-formats the parsed `time.Time` as RFC3339Nano — **not the raw stored bytes** |
| `CAST(col AS TEXT)` | Returns raw stored bytes — use this to inspect on-disk format |

To read the raw stored format (e.g., for debugging), use `SELECT CAST(col AS TEXT)`.
