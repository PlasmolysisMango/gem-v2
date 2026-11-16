import type { Card, Color, Noble } from '../api/types'
import { ALL_COLORS, GEM_COLORS } from '../api/types'
import { COLOR_NAME } from '../lib/gems'

export const GEM_BG: Record<Color, string> = {
  white: 'bg-gem-white text-slate-900',
  blue: 'bg-gem-blue text-white',
  green: 'bg-gem-green text-white',
  red: 'bg-gem-red text-white',
  black: 'bg-gem-black text-white',
  gold: 'bg-gem-gold text-slate-900',
}

const CARD_TINT: Record<Color, string> = {
  white: 'from-slate-100 to-slate-300 text-slate-900',
  blue: 'from-blue-500 to-blue-800 text-white',
  green: 'from-emerald-500 to-emerald-800 text-white',
  red: 'from-rose-500 to-rose-800 text-white',
  black: 'from-stone-600 to-stone-900 text-white',
  gold: 'from-amber-300 to-amber-500 text-slate-900',
}

export function GemToken({
  color,
  count,
  size = 'md',
  onClick,
  disabled,
  active,
  highlight,
  title,
}: {
  color: Color
  count?: number
  size?: 'sm' | 'md' | 'lg'
  onClick?: () => void
  disabled?: boolean
  active?: boolean
  highlight?: boolean
  title?: string
}) {
  const dim =
    size === 'lg'
      ? 'w-11 h-11 sm:w-14 sm:h-14 text-xl'
      : onClick
        ? 'w-11 h-11 text-sm'
        : size === 'sm'
          ? 'w-6 h-6 text-xs'
          : 'w-9 h-9 text-sm'
  return (
    <button
      type="button"
      title={title ?? COLOR_NAME[color]}
      aria-label={`${title ?? COLOR_NAME[color]}${count !== undefined ? ` ${count} 枚` : ''}`}
      onClick={onClick}
      disabled={disabled || !onClick}
      className={`${dim} ${GEM_BG[color]} shrink-0 rounded-full border-2 font-bold flex items-center justify-center shadow transition
        ${active ? 'border-yellow-300 scale-110' : 'border-black/30'}
        ${highlight ? 'ring-4 ring-fuchsia-400 animate-pulse' : ''}
        ${onClick && !disabled ? 'hover:brightness-110 cursor-pointer' : 'cursor-default'}
        ${disabled && onClick ? 'opacity-40' : ''}`}
    >
      {count ?? ''}
    </button>
  )
}

export function CostDots({ cost, small }: { cost: number[]; small?: boolean }) {
  return (
    <div className="flex flex-col-reverse gap-0.5 items-start">
      {GEM_COLORS.map((c, i) =>
        cost[i] > 0 ? (
          <span
            key={c}
            className={`${GEM_BG[c]} ${small ? 'w-4 h-4 text-[10px]' : 'w-5 h-5 text-xs'} rounded-full font-bold flex items-center justify-center border border-black/30`}
          >
            {cost[i]}
          </span>
        ) : null,
      )}
    </div>
  )
}

export function CardTile({
  card,
  onClick,
  selected,
  highlight,
  affordable,
  small,
  fluid,
}: {
  card: Card | null
  onClick?: () => void
  selected?: boolean
  highlight?: boolean
  affordable?: boolean
  small?: boolean
  fluid?: boolean
}) {
  const dim = fluid ? 'w-full min-w-0 h-32' : small ? 'w-16 h-24 shrink-0' : 'w-24 h-32 shrink-0'
  if (!card) return <div className={`${dim} rounded-lg border-2 border-dashed border-slate-700`} />
  if (card.hidden) {
    return (
      <div
        className={`${dim} rounded-lg bg-gradient-to-br from-indigo-900 to-slate-800 border-2 border-slate-600 flex items-center justify-center text-slate-300`}
      >
        L{card.level} 暗牌
      </div>
    )
  }
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={onClick ? !!selected : undefined}
      className={`${dim} relative rounded-lg bg-gradient-to-br ${CARD_TINT[card.bonus]} border-2 p-1.5 text-left shadow-md transition
        ${selected ? 'border-yellow-300 -translate-y-1' : affordable ? 'border-lime-300' : 'border-black/40'}
        ${highlight ? 'ring-4 ring-fuchsia-400 animate-pulse' : ''}
        ${onClick ? 'hover:-translate-y-1 cursor-pointer' : 'cursor-default'}`}
      title={`L${card.level} ${COLOR_NAME[card.bonus]}卡 ${card.points}分`}
    >
      <div className="absolute top-1 left-1.5 right-1.5 flex justify-between items-start">
        <span className={`${small ? 'text-lg' : 'text-2xl'} font-black drop-shadow`}>{card.points || ''}</span>
        <span className={`${GEM_BG[card.bonus]} ${small ? 'w-4 h-4' : 'w-6 h-6'} rounded-sm rotate-45 border border-black/40`} />
      </div>
      <div className="absolute bottom-1 left-1">
        <CostDots cost={card.cost} small={small} />
      </div>
      <span className="absolute bottom-1 right-1 text-[10px] opacity-70">L{card.level}</span>
    </button>
  )
}

export function DeckTile({
  level,
  count,
  onClick,
  selected,
  highlight,
}: {
  level: number
  count: number
  onClick?: () => void
  selected?: boolean
  highlight?: boolean
}) {
  const tone = ['from-emerald-900 to-emerald-700', 'from-amber-900 to-amber-700', 'from-sky-900 to-sky-700'][level - 1]
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick || count === 0}
      aria-pressed={onClick ? !!selected : undefined}
      className={`w-full h-11 sm:w-20 sm:h-32 min-w-0 rounded-lg bg-gradient-to-br ${tone} border-2 flex flex-row sm:flex-col gap-2 sm:gap-0 items-center justify-center shadow-md
        ${selected ? 'border-yellow-300' : 'border-black/40'} ${highlight ? 'ring-4 ring-fuchsia-400 animate-pulse' : ''}
        ${onClick && count > 0 ? 'hover:-translate-y-1' : ''}`}
      title="点击可盲扣牌堆顶牌"
    >
      <span className="text-lg sm:text-2xl font-black">
        L{level} · {'I'.repeat(level)}
      </span>
      <span className="text-xs sm:mt-1 opacity-80">剩 {count} 张 · 盲扣</span>
    </button>
  )
}

export function NobleTile({
  noble,
  small,
  onClick,
  highlight,
}: {
  noble: Noble
  small?: boolean
  onClick?: () => void
  highlight?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick}
      className={`${small ? 'w-14 h-14' : 'w-20 h-20'} shrink-0 rounded-lg bg-gradient-to-br from-amber-100 to-amber-300 text-slate-900 border-2 border-amber-600 p-1 flex gap-1 shadow
        ${highlight ? 'ring-4 ring-fuchsia-400' : ''} ${onClick ? 'hover:scale-105' : ''}`}
      title={`贵族 ${noble.points} 分`}
    >
      <span className={`${small ? 'text-base' : 'text-xl'} font-black`}>{noble.points}</span>
      <div className="flex flex-col gap-0.5 ml-auto">
        {GEM_COLORS.map((c, i) =>
          noble.req[i] > 0 ? (
            <span
              key={c}
              className={`${GEM_BG[c]} ${small ? 'w-3.5 h-3.5 text-[9px]' : 'w-4 h-4 text-[10px]'} rounded-sm font-bold flex items-center justify-center border border-black/30`}
            >
              {noble.req[i]}
            </span>
          ) : null,
        )}
      </div>
    </button>
  )
}

export function TokenRow({ tokens, bonuses }: { tokens: number[]; bonuses?: number[] }) {
  return (
    <div className="flex gap-1.5 flex-wrap">
      {ALL_COLORS.map((c, i) => (
        <div key={c} className="flex flex-col items-center">
          <GemToken color={c} count={tokens[i]} size="sm" />
          {bonuses && i < 5 && <span className="text-[10px] text-slate-400">+{bonuses[i]}</span>}
        </div>
      ))}
    </div>
  )
}
