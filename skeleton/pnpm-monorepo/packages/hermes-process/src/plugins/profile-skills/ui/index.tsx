/* eslint-disable max-lines, complexity -- цельный компонент-страница плагина: типы SDK, под-компоненты и экран с условным рендером в одном бандл-модуле */
/**
 * Profile Skills — UI дашборд-плагина.
 *
 * Бандл-контракт Hermes: IIFE берёт React и UI-компоненты из
 * window.__HERMES_PLUGIN_SDK__ (свой React не бандлим) и регистрирует страницу
 * через window.__HERMES_PLUGINS__.register('profile-skills', ...).
 *
 * Разметку и классы намеренно повторяем за встроенной страницей Skills
 * (web/src/pages/SkillsPage.tsx): дашборд грузит общий скомпилированный Tailwind,
 * поэтому те же классы дают тот же вид, и свой CSS везти не нужно.
 *
 * Данные: GET /api/plugins/profile-skills/matrix, POST .../toggle (см.
 * dashboard/plugin_api.py). fetchJSON из SDK сам добавляет session-токен.
 */

import type * as ReactNS from 'react'

// ── Контракт SDK дашборда ───────────────────────────────────────────────────

type Cmp = ReactNS.ComponentType<Record<string, unknown>>

// Явный список используемых компонентов (а не Record-индекс), чтобы под
// noUncheckedIndexedAccess доступ не давал тип `Cmp | undefined`.
interface HermesComponents {
  Card: Cmp
  CardHeader: Cmp
  CardTitle: Cmp
  CardContent: Cmp
  Badge: Cmp
  Input: Cmp
}

interface HermesSDK {
  React: typeof ReactNS
  hooks: {
    useState: typeof ReactNS.useState
    useEffect: typeof ReactNS.useEffect
    useMemo: typeof ReactNS.useMemo
  }
  components: HermesComponents
  utils: { cn: (...args: unknown[]) => string }
  fetchJSON: <T = unknown>(url: string, init?: RequestInit) => Promise<T>
}

declare global {
  interface Window {
    __HERMES_PLUGIN_SDK__?: HermesSDK
    __HERMES_PLUGINS__?: { register: (name: string, component: ReactNS.ComponentType) => void }
  }
}

// ── Данные ──────────────────────────────────────────────────────────────────

interface Skill {
  name: string
  category: string
  description: string
}

interface ProfileRow {
  name: string
  path: string
  is_default: boolean
  disabled: string[]
}

interface Matrix {
  skills: Skill[]
  profiles: ProfileRow[]
}

const PLUGIN = 'profile-skills'
const API = `/api/plugins/${PLUGIN}`

/** Иммутабельно правит profiles[name].disabled: enable убирает скилл, disable добавляет. */
function patchDisabled(m: Matrix | null, profile: string, skill: string, enable: boolean): Matrix | null {
  if (!m) return m
  return {
    ...m,
    profiles: m.profiles.map((p) => {
      if (p.name !== profile) return p
      const set = new Set(p.disabled)
      if (enable) set.delete(skill)
      else set.add(skill)
      return { ...p, disabled: [...set].sort() }
    }),
  }
}

/** Иммутабельно заменяет весь disabled-набор профиля (для bulk-операций). */
function setDisabledFor(m: Matrix | null, profile: string, disabled: string[]): Matrix | null {
  if (!m) return m
  return {
    ...m,
    profiles: m.profiles.map((p) => (p.name === profile ? { ...p, disabled: [...disabled].sort() } : p)),
  }
}

// SDK гарантированно выставлен загрузчиком до выполнения бандла; если нет —
// просто ничего не регистрируем (без падения).
const SDK = window.__HERMES_PLUGIN_SDK__
const registry = window.__HERMES_PLUGINS__

if (SDK && registry) {
  const React = SDK.React
  const { useState, useEffect, useMemo } = SDK.hooks
  const cn = SDK.utils.cn
  const fetchJSON = SDK.fetchJSON
  const { Card, CardHeader, CardTitle, CardContent, Badge, Input } = SDK.components

  // ── Свитч (в SDK Switch нет — рисуем сами: цвета токенами, геометрия inline,
  //    чтобы не зависеть от того, какие size-классы попали в скомпилированный CSS).
  function ToggleSwitch(props: { checked: boolean; disabled?: boolean; onClick: () => void }) {
    return (
      <button
        type='button'
        role='switch'
        aria-checked={props.checked}
        disabled={props.disabled}
        onClick={props.onClick}
        className={cn(
          'border-border relative shrink-0 rounded-full border transition-colors',
          props.checked ? 'bg-primary' : 'bg-muted/60',
          props.disabled && 'cursor-wait opacity-50'
        )}
        style={{ height: 16, width: 28 }}>
        <span
          className='bg-background absolute rounded-full transition-all'
          style={{ height: 12, left: props.checked ? 14 : 2, top: 1, width: 12 }}
        />
      </button>
    )
  }

  function SearchIcon() {
    return (
      <svg
        className='text-muted-foreground absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2'
        viewBox='0 0 24 24'
        fill='none'
        stroke='currentColor'
        strokeWidth='2'
        aria-hidden='true'>
        <circle cx='11' cy='11' r='8' />
        <path d='m21 21-4.3-4.3' />
      </svg>
    )
  }

  // ── Строка скилла (повтор SkillRow из оригинала) ──
  function SkillRow(props: { skill: Skill; enabled: boolean; toggling: boolean; onToggle: () => void }) {
    const { skill, enabled, toggling, onToggle } = props
    return (
      <div className='group hover:bg-muted/40 flex items-start gap-3 px-3 py-2.5 transition-colors'>
        <div className='shrink-0 pt-0.5'>
          <ToggleSwitch checked={enabled} disabled={toggling} onClick={onToggle} />
        </div>
        <div className='min-w-0 flex-1'>
          <div className='mb-0.5 flex items-center gap-2'>
            <span className={cn('font-mono-ui text-sm', enabled ? 'text-foreground' : 'text-muted-foreground')}>
              {skill.name}
            </span>
            {skill.category ? (
              <Badge tone='outline' className='text-xs'>
                {skill.category}
              </Badge>
            ) : null}
          </div>
          <p className='text-muted-foreground line-clamp-2 text-xs leading-relaxed'>{skill.description || '—'}</p>
        </div>
      </div>
    )
  }

  function ProfilesSidebar(props: {
    profiles: ProfileRow[]
    selected: string | null
    total: number
    onSelect: (name: string) => void
  }) {
    return (
      <aside aria-label='Профили' className='sm:sticky sm:top-0 sm:w-56 sm:shrink-0 sm:self-start'>
        <div className='border-border bg-muted/20 flex flex-col rounded-none border'>
          <div className='border-border hidden items-center gap-2 border-b px-3 py-2 sm:flex'>
            <span className='font-mondwest text-display text-text-secondary text-xs tracking-[0.12em]'>ПРОФИЛИ</span>
          </div>
          <div className='flex gap-1 overflow-x-auto p-2 sm:flex-col sm:overflow-x-visible'>
            {props.profiles.map((p) => {
              const active = p.name === props.selected
              return (
                <button
                  key={p.name}
                  type='button'
                  onClick={() => props.onSelect(p.name)}
                  className={cn(
                    'font-mono-ui relative flex items-center gap-2 whitespace-nowrap rounded-none px-2.5 py-1.5 text-left text-xs transition-colors',
                    active ? 'bg-muted/20 text-foreground font-medium' : 'text-text-secondary hover:bg-muted/40'
                  )}>
                  {active ? <span className='bg-primary absolute bottom-0 left-0 top-0' style={{ width: 2 }} /> : null}
                  <span className='flex-1 truncate'>{p.name}</span>
                  <span className={cn('text-xs tabular-nums', active ? 'text-foreground' : 'text-text-tertiary')}>
                    {props.total - p.disabled.length}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      </aside>
    )
  }

  function ProfileSkillsPage() {
    const [matrix, setMatrix] = useState<Matrix | null>(null)
    const [selected, setSelected] = useState<string | null>(null)
    const [search, setSearch] = useState('')
    const [toggling, setToggling] = useState<Set<string>>(new Set())
    const [error, setError] = useState<string | null>(null)
    const [loading, setLoading] = useState(true)
    const [bulkBusy, setBulkBusy] = useState(false)

    useEffect(() => {
      let cancelled = false
      fetchJSON<Matrix>(`${API}/matrix`)
        .then((m) => {
          if (cancelled) return
          setMatrix(m)
          setSelected((cur) => cur ?? m.profiles[0]?.name ?? null)
        })
        .catch((e: unknown) => !cancelled && setError(String(e)))
        .finally(() => !cancelled && setLoading(false))
      return () => {
        cancelled = true
      }
    }, [])

    const profiles = matrix?.profiles ?? []
    const skills = matrix?.skills ?? []
    const total = skills.length
    const current = useMemo(() => profiles.find((p) => p.name === selected) ?? null, [profiles, selected])
    const disabledSet = useMemo(() => new Set(current?.disabled ?? []), [current])

    const rows = useMemo(() => {
      const q = search.trim().toLowerCase()
      const matched = q
        ? skills.filter(
            (s) =>
              s.name.toLowerCase().includes(q) ||
              s.description.toLowerCase().includes(q) ||
              s.category.toLowerCase().includes(q)
          )
        : skills
      return [...matched].sort((a, b) => {
        const ea = disabledSet.has(a.name) ? 1 : 0
        const eb = disabledSet.has(b.name) ? 1 : 0
        if (ea !== eb) return ea - eb // enabled-first
        return a.name.localeCompare(b.name)
      })
    }, [skills, search, disabledSet])

    async function toggle(skill: Skill) {
      if (!current) return
      const willEnable = disabledSet.has(skill.name)
      const profileName = current.name

      setToggling((t) => new Set(t).add(skill.name))
      setMatrix((m) => patchDisabled(m, profileName, skill.name, willEnable)) // оптимистично
      try {
        await fetchJSON(`${API}/toggle`, {
          body: JSON.stringify({ enabled: willEnable, profile: profileName, skill: skill.name }),
          headers: { 'content-type': 'application/json' },
          method: 'POST',
        })
      } catch (e: unknown) {
        setMatrix((m) => patchDisabled(m, profileName, skill.name, !willEnable)) // откат
        setError(`Не удалось переключить ${skill.name}: ${String(e)}`)
      } finally {
        setToggling((t) => {
          const next = new Set(t)
          next.delete(skill.name)
          return next
        })
      }
    }

    // Включить/выключить весь видимый (с учётом поиска) список разом через /set.
    async function bulkSet(enable: boolean) {
      if (!current) return
      const profileName = current.name
      const prev = current.disabled
      const next = new Set(prev)
      for (const s of rows) {
        if (enable) next.delete(s.name)
        else next.add(s.name)
      }
      const nextArr = [...next].sort()

      setBulkBusy(true)
      setMatrix((m) => setDisabledFor(m, profileName, nextArr)) // оптимистично
      try {
        await fetchJSON(`${API}/set`, {
          body: JSON.stringify({ disabled: nextArr, profile: profileName }),
          headers: { 'content-type': 'application/json' },
          method: 'POST',
        })
      } catch (e: unknown) {
        setMatrix((m) => setDisabledFor(m, profileName, prev)) // откат
        setError(`Не удалось применить ко всем: ${String(e)}`)
      } finally {
        setBulkBusy(false)
      }
    }

    if (loading) {
      return <div className='text-muted-foreground py-24 text-center text-sm'>Загрузка…</div>
    }

    return (
      <div className='flex flex-col gap-4'>
        {error ? (
          <div className='border-destructive/40 text-destructive rounded-none border px-3 py-2 text-xs'>{error}</div>
        ) : null}

        <div className='flex flex-col gap-4 sm:flex-row sm:items-start'>
          <ProfilesSidebar profiles={profiles} selected={selected} total={total} onSelect={setSelected} />

          <div className='min-w-0 flex-1'>
            <Card className='rounded-none'>
              <CardHeader className='px-4 py-3'>
                <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                  <CardTitle className='flex items-center gap-2 text-sm'>
                    {selected ?? '—'}
                    {current ? (
                      <Badge tone='secondary' className='text-xs'>
                        {total - current.disabled.length}/{total} включено
                      </Badge>
                    ) : null}
                  </CardTitle>
                  <div className='flex flex-wrap items-center gap-2'>
                    <button
                      type='button'
                      disabled={bulkBusy || rows.length === 0}
                      onClick={() => void bulkSet(true)}
                      className={cn(
                        'border-border hover:bg-muted/40 h-8 whitespace-nowrap rounded-none border px-3 text-xs transition-colors',
                        (bulkBusy || rows.length === 0) && 'cursor-not-allowed opacity-50'
                      )}>
                      Включить все
                    </button>
                    <button
                      type='button'
                      disabled={bulkBusy || rows.length === 0}
                      onClick={() => void bulkSet(false)}
                      className={cn(
                        'border-border hover:bg-muted/40 h-8 whitespace-nowrap rounded-none border px-3 text-xs transition-colors',
                        (bulkBusy || rows.length === 0) && 'cursor-not-allowed opacity-50'
                      )}>
                      Выключить все
                    </button>
                    <div className='relative w-full sm:w-56'>
                      <SearchIcon />
                      <Input
                        className='h-8 rounded-none pl-8 text-xs'
                        placeholder='Поиск скилла…'
                        value={search}
                        onChange={(e: ReactNS.ChangeEvent<HTMLInputElement>) => setSearch(e.target.value)}
                      />
                    </div>
                  </div>
                </div>
              </CardHeader>
              <CardContent className='px-4 pb-4'>
                {rows.length === 0 ? (
                  <p className='text-muted-foreground py-8 text-center text-sm'>Ничего не найдено</p>
                ) : (
                  <div className='grid gap-1'>
                    {rows.map((skill) => (
                      <SkillRow
                        key={skill.name}
                        skill={skill}
                        enabled={!disabledSet.has(skill.name)}
                        toggling={toggling.has(skill.name)}
                        onToggle={() => void toggle(skill)}
                      />
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    )
  }

  registry.register(PLUGIN, ProfileSkillsPage)
}
