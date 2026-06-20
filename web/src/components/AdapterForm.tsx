import { useMemo, useState } from 'react'
import type { ConfigField, ConfigSchema } from '../lib/adaptersApi'
import { testAdapter } from '../lib/adaptersApi'

export interface AdapterFormProps {
  name: string
  schema: ConfigSchema
  initial?: Record<string, unknown>
  submitLabel?: string
  onSubmit: (config: Record<string, unknown>) => void | Promise<void>
}

type FieldValue = string | boolean

// initialValue derives the form value for a field from the (redacted) initial config.
// Secret fields always start blank (the real value is never sent to the browser).
function initialValue(f: ConfigField, initial?: Record<string, unknown>): FieldValue {
  if (f.type === 'bool') return Boolean(initial?.[f.key])
  if (f.secret) return ''
  const v = initial?.[f.key]
  return v == null ? '' : String(v)
}

// collect builds the config object to submit/test from the current field values.
// number fields are coerced; bool stays boolean; everything else is a string.
function collect(schema: ConfigSchema, values: Record<string, FieldValue>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of schema.fields) {
    const v = values[f.key]
    if (f.type === 'number') {
      out[f.key] = v === '' ? '' : Number(v)
    } else {
      out[f.key] = v
    }
  }
  return out
}

export function AdapterForm({ name, schema, initial, submitLabel = 'Save', onSubmit }: AdapterFormProps) {
  const [values, setValues] = useState<Record<string, FieldValue>>(() => {
    const v: Record<string, FieldValue> = {}
    for (const f of schema.fields) v[f.key] = initialValue(f, initial)
    return v
  })
  const [testState, setTestState] = useState<{ status: 'idle' | 'testing' | 'ok' | 'error'; msg?: string }>({ status: 'idle' })
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  const secretIsSet = useMemo(() => {
    const m: Record<string, boolean> = {}
    for (const f of schema.fields) if (f.secret) m[f.key] = Boolean(initial?.[`${f.key}__isSet`])
    return m
  }, [schema, initial])

  function set(key: string, v: FieldValue) {
    setValues((prev) => ({ ...prev, [key]: v }))
  }

  async function runTest() {
    setTestState({ status: 'testing' })
    try {
      const res = await testAdapter(name, collect(schema, values))
      setTestState(res.ok ? { status: 'ok' } : { status: 'error', msg: res.error || 'Connection failed' })
    } catch (e) {
      setTestState({ status: 'error', msg: e instanceof Error ? e.message : 'Connection failed' })
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitError(null)
    setSubmitting(true)
    try {
      await onSubmit(collect(schema, values))
    } catch (e) {
      setSubmitError(e instanceof Error ? e.message : 'Save failed. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={submit} className="space-y-3">
      {schema.fields.map((f) => (
        <div key={f.key} className="space-y-1">
          <div className="flex items-center gap-1">
            <label htmlFor={`field-${f.key}`} className="block text-sm text-neutral-300">
              {f.label}
            </label>
            {f.required && <span className="text-accent text-sm" aria-hidden="true">*</span>}
          </div>
          {f.type === 'bool' ? (
            <input
              id={`field-${f.key}`}
              type="checkbox"
              checked={Boolean(values[f.key])}
              onChange={(e) => set(f.key, e.target.checked)}
            />
          ) : (
            <>
              <input
                id={`field-${f.key}`}
                type={f.secret ? 'password' : f.type === 'number' ? 'number' : 'text'}
                value={String(values[f.key] ?? '')}
                onChange={(e) => set(f.key, e.target.value)}
                placeholder={f.secret && secretIsSet[f.key] ? 'Leave blank to keep current value' : ''}
                className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
              />
              {f.secret && secretIsSet[f.key] && (
                <p className="text-xs text-neutral-500">Leave blank to keep the current value.</p>
              )}
            </>
          )}
        </div>
      ))}

      {submitError && <p className="text-sm text-accent">{submitError}</p>}
      <div className="flex items-center gap-3 pt-1">
        <button type="submit" disabled={submitting} className="rounded bg-accent px-4 py-2 font-medium text-white disabled:opacity-50">
          {submitLabel}
        </button>
        <button type="button" onClick={runTest} disabled={testState.status === 'testing'} className="rounded border border-neutral-700 px-4 py-2 text-neutral-200 hover:bg-neutral-800">
          {testState.status === 'testing' ? 'Testing…' : 'Test Connection'}
        </button>
        {testState.status === 'ok' && <span className="text-sm text-green-400">✓ Connection OK</span>}
        {testState.status === 'error' && <span className="text-sm text-accent">✗ {testState.msg}</span>}
      </div>
    </form>
  )
}
