import { useEffect, useState } from 'react'
import { api } from '../api/http'
import type { Card, Color, Gems, MoveEvent, Noble } from '../api/types'
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

// Dark tokens need a light edge to stay visible on the dark table and on black cards.
const GEM_EDGE: Record<Color, string> = {
  white: 'border-slate-400',
  blue: 'border-black/30',
  green: 'border-black/30',
  red: 'border-black/30',
  black: 'border-slate-300/70',
  gold: 'border-black/30',
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
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'panel'
  onClick?: () => void
  disabled?: boolean
  active?: boolean
  highlight?: boolean
  title?: string
}) {
  const dim =
    size === 'lg'
      ? 'w-11 h-11 sm:w-14 sm:h-14 xl:w-[clamp(2rem,5.5dvh,3.5rem)] xl:h-[clamp(2rem,5.5dvh,3.5rem)] text-xl'
      : onClick
        ? 'w-11 h-11 text-sm'
        : size === 'panel'
          ? 'w-7 h-7 text-sm'
          : size === 'xs'
          ? 'w-5 h-5 xs:w-6 xs:h-6 text-xs'
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
        ${active ? 'border-yellow-300 scale-110' : GEM_EDGE[color]}
        ${highlight ? 'ring-4 ring-fuchsia-400 animate-pulse' : ''}
        ${onClick && !disabled ? 'hover:brightness-110 cursor-pointer' : 'cursor-default'}
        ${disabled && onClick ? 'opacity-40' : ''}`}
    >
      {count ?? ''}
    </button>
  )
}

/**
 * Card price. Each dot has a light ring and a dark shadow so a cost in the card's own color
 * (blue on a blue card, black on a black card) stays readable. Four colors (and small tiles) use a
 * 2x2 grid so the stack never reaches the points. gap adds the per-color shortfall.
 */
export function CostDots({ cost, small, fluid, gap }: { cost: number[]; small?: boolean; fluid?: boolean; gap?: number[] }) {
  const dot = small
    ? 'w-4 h-4 text-[10px]'
    : fluid
      ? 'w-4 h-4 text-[10px] sm:w-5 sm:h-5 sm:text-xs 2xl:w-6 2xl:h-6 2xl:text-sm'
      : 'w-5 h-5 text-xs'
  const grid = small || cost.filter((n) => n > 0).length >= 4
  return (
    <div className={grid ? 'cost-dots grid grid-cols-2 gap-0.5' : 'cost-dots flex flex-col-reverse gap-0.5 items-start'}>
      {GEM_COLORS.map((c, i) =>
        cost[i] > 0 ? (
          <span key={c} className={`flex items-center ${grid ? 'gap-px' : 'gap-0.5'}`}>
            <span
              className={`${GEM_BG[c]} ${dot} rounded-full font-bold flex items-center justify-center ring-1 ring-white/90 shadow shadow-black/60`}
              aria-label={`${COLOR_NAME[c]} ${cost[i]}`}
            >
              {cost[i]}
            </span>
            {gap && gap[i] > 0 && (
              <span
                className={`rounded bg-rose-600 font-bold leading-4 text-white ${grid ? 'px-px text-[9px] sm:px-0.5 sm:text-[10px]' : 'px-0.5 text-[10px]'}`}
                title={`还缺${COLOR_NAME[c]} ${gap[i]}`}
              >
                -{gap[i]}
              </span>
            )}
          </span>
        ) : null,
      )}
    </div>
  )
}

export type CardGap = { gap: number[]; gold: number; missing: number }

function GapBadge({ g }: { g: CardGap }) {
  const need = g.gap.reduce((a, b) => a + b, 0)
  const [text, tone, title] =
    need === 0
      ? ['可买', 'bg-emerald-600 text-white', '宝石已足够']
      : g.missing === 0
        ? [`金${g.gold}`, 'bg-gem-gold text-slate-900', `用 ${g.gold} 枚黄金补足即可购买`]
        : [`差${g.missing}`, 'bg-rose-600 text-white', g.gold > 0 ? `黄金抵扣 ${g.gold} 枚后仍差 ${g.missing} 枚` : `还差 ${g.missing} 枚`]
  return (
    <span className={`rounded px-1 text-[10px] font-bold leading-4 ring-1 ring-black/40 ${tone}`} title={title}>
      {text}
    </span>
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
  gap,
}: {
  card: Card | null
  onClick?: () => void
  selected?: boolean
  highlight?: boolean
  affordable?: boolean
  small?: boolean
  /** fill the grid cell: compact on phones, taller on tablets, full row height on desktop */
  fluid?: boolean
  /** shortfall overlay for the viewer (toggled by the board's "缺口" button) */
  gap?: CardGap
}) {
  const dim = fluid ? 'w-full min-w-0 h-[6.5rem] sm:h-32 xl:h-full xl:min-h-[5.75rem]' : small ? 'w-16 h-20 shrink-0' : 'w-24 h-32 shrink-0'
  if (!card) return <div className={`${dim} rounded-lg border-2 border-dashed border-slate-700`} />
  if (card.hidden) {
    return (
      <div
        className={`${dim} rounded-lg bg-gradient-to-br from-indigo-900 to-slate-800 border-2 border-slate-600 flex items-center justify-center text-center text-xs sm:text-sm text-slate-300`}
      >
        L{card.level} 暗牌
      </div>
    )
  }
  const points = small ? 'text-base' : fluid ? 'text-lg sm:text-2xl' : 'text-2xl'
  const diamond = small ? 'w-3.5 h-3.5' : fluid ? 'w-4 h-4 sm:w-6 sm:h-6' : 'w-6 h-6'
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
      <div className="absolute top-0.5 left-1 right-1 flex justify-between items-start">
        <span className={`${points} leading-tight font-black drop-shadow`}>{card.points || ''}</span>
        <span className={`${GEM_BG[card.bonus]} ${diamond} mt-1 rounded-sm rotate-45 ring-1 ring-white/70`} />
      </div>
      {gap && (
        <span className={`absolute right-1 ${small ? 'top-5' : 'top-6 sm:top-9'}`}>
          <GapBadge g={gap} />
        </span>
      )}
      <div className="absolute bottom-1 left-1">
        <CostDots cost={card.cost} small={small} fluid={fluid} gap={gap?.gap} />
      </div>
      {!gap && <span className="absolute bottom-0.5 right-1 text-[10px] opacity-70">L{card.level}</span>}
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
      aria-label={`L${level} 牌堆，剩 ${count} 张${onClick ? '，点击盲扣' : ''}`}
      className={`w-full h-[6.5rem] sm:h-32 xl:h-full xl:min-h-[5.75rem] min-w-0 rounded-lg bg-gradient-to-br ${tone} border-2 flex flex-col items-center justify-center gap-0.5 shadow-md
        ${selected ? 'border-yellow-300' : 'border-black/40'} ${highlight ? 'ring-4 ring-fuchsia-400 animate-pulse' : ''}
        ${onClick && count > 0 ? 'hover:-translate-y-1' : ''}`}
      title="点击可盲扣牌堆顶牌"
    >
      <span className="text-sm sm:text-2xl font-black">L{level}</span>
      <span className="deck-marks hidden sm:block text-lg font-black opacity-80">{'I'.repeat(level)}</span>
      <span className="text-[10px] sm:text-xs opacity-80">剩{count}</span>
      {onClick && <span className="text-[10px] sm:text-xs opacity-80">盲扣</span>}
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
  // Without small the tile adapts: compact on phones, larger on tablets, viewport-height based on desktop.
  const dim = small ? 'w-10 h-10' : 'w-14 h-14 sm:w-20 sm:h-20 xl:w-[clamp(3.25rem,8dvh,5rem)] xl:h-[clamp(3.25rem,8dvh,5rem)]'
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick}
      className={`noble-tile ${dim} shrink-0 rounded-lg bg-gradient-to-br from-amber-100 to-amber-300 text-slate-900 border-2 border-amber-600 p-1 flex gap-1 shadow
        ${highlight ? 'ring-4 ring-fuchsia-400' : ''} ${onClick ? 'hover:scale-105' : ''}`}
      title={`贵族 ${noble.points} 分`}
    >
      <span className={`${small ? 'text-sm' : 'text-base sm:text-xl'} leading-none font-black`}>{noble.points}</span>
      <div className="flex flex-col gap-0.5 ml-auto">
        {GEM_COLORS.map((c, i) =>
          noble.req[i] > 0 ? (
            <span
              key={c}
              className={`noble-cost ${GEM_BG[c]} ${small ? 'w-3 h-3 text-[8px]' : 'w-3.5 h-3.5 text-[9px] sm:w-4 sm:h-4 sm:text-[10px]'} rounded-sm font-bold flex items-center justify-center ring-1 ring-black/40`}
            >
              {noble.req[i]}
            </span>
          ) : null,
        )}
      </div>
    </button>
  )
}

let knownCards: Card[] = []

/** 卡牌目录共用缓存，已买走的卡也能显示颜色和分数。 */
export function useCardCatalog() {
  const [cards, setCards] = useState(knownCards)
  useEffect(() => {
    let alive = true
    api.cards().then((data) => {
      knownCards = data.cards
      if (alive) setCards(data.cards)
    }).catch(() => {})
    return () => { alive = false }
  }, [])
  return cards
}

export function CardMark({ card, level = 0 }: { card?: Card; level?: number }) {
  const hidden = !card || card.hidden
  const cardLevel = card?.level ?? level
  const label = hidden ? (cardLevel > 0 ? `L${cardLevel} 暗牌` : '卡牌') : `${COLOR_NAME[card.bonus]}卡 ${card.points}分`
  return (
    <span role="img" aria-label={label} title={label}
      className={`relative inline-flex h-6 w-5 shrink-0 items-center justify-center rounded-sm border border-white/70 text-[10px] font-bold ${hidden ? 'bg-indigo-900 text-slate-200' : GEM_BG[card.bonus]}`}>
      {hidden ? (cardLevel > 0 ? `L${cardLevel}` : '?') : card.points}
      {!hidden && <span className="absolute right-0.5 top-0.5 h-1 w-1 rotate-45 border border-current" />}
    </span>
  )
}

/** 同色合并为一个数字筹码；极长的回合可自然换行，保留完整信息。 */
export function MoveIcons({ events, cards, compact, onReserveClick }: {
  events: MoveEvent[]
  cards: Card[]
  compact?: boolean
  onReserveClick?: (card: Card) => void
}) {
  const chips = (gems: Gems) => ALL_COLORS.map((c, i) => gems[i] > 0 && (
    <span key={c} aria-label={`${COLOR_NAME[c]} ${gems[i]} 枚`}
      className={`${GEM_BG[c]} ${compact ? 'w-4 h-4 text-[10px]' : 'w-5 h-5 text-xs'} inline-flex shrink-0 items-center justify-center rounded-full font-bold ring-1 ring-white/70`}>
      {gems[i]}
    </span>
  ))
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5">
      {events.map((e, i) => {
        if (e.kind === 'noble') return (
          <span key={i} className="inline-flex items-center gap-0.5 text-amber-300" title="贵族到访，获得3分" aria-label="贵族到访，获得3分">
            <svg aria-hidden="true" width="18" height="18" viewBox="0 0 20 20" fill="currentColor"><path d="M2 5l4 4 4-6 4 6 4-4-2 11H4z" /></svg>+3
          </span>
        )
        const type = e.action.type
        const take = type === 'take_diff' || type === 'take_same'
        const buy = type === 'buy' || type === 'buy_reserved'
        const reserve = type === 'reserve' || type === 'reserve_deck'
        const card = e.card >= 0 ? cards.find((c) => c.id === e.card) : undefined
        const level = e.card <= -11 && e.card >= -13 ? -10 - e.card : e.action.level
        // 暗牌仅使用事件中公开的等级，不能用牌库目录还原身份。
        const reservedCard: Card | undefined = card ?? (level > 0 ? {
          id: -1, level, hidden: true, bonus: 'white', points: 0, cost: [0, 0, 0, 0, 0],
        } : undefined)
        return (
          <span key={i} className="inline-flex max-w-full min-w-0 flex-wrap items-center gap-0.5">
            {i > 0 && <span className="text-slate-600">·</span>}
            <span className="text-slate-400">{take ? '拿' : buy ? '买' : reserve ? '留' : type === 'discard' ? '还' : '跳过'}</span>
            {(take || type === 'discard') && chips(e.action.gems)}
            {reserve && onReserveClick ? (
              <button type="button" className="inline-flex min-h-[32px] min-w-[32px] items-center justify-center rounded hover:bg-slate-700"
                aria-label="查看上一步预留卡详情" aria-haspopup="dialog" disabled={!reservedCard}
                onClick={() => reservedCard && onReserveClick(reservedCard)}>
                <CardMark card={card} level={level} />
              </button>
            ) : (buy || reserve) && <CardMark card={card} level={level} />}
            {buy && e.paid.some((n) => n > 0) && <><span className="text-slate-500">−</span>{chips(e.paid)}</>}
            {reserve && e.gold && <><span className="text-slate-500">+</span>{chips([0, 0, 0, 0, 0, 1])}</>}
          </span>
        )
      })}
    </span>
  )
}

/** 筹码圆与卡牌数字按颜色对齐，六列居中填满信息框。 */
export function TokenRow({ tokens, bonuses, dense }: { tokens: number[]; bonuses?: number[]; dense?: boolean }) {
  return (
    <div className={`grid w-full grid-cols-6 justify-items-center items-start ${dense ? 'gap-0 px-1 py-1.5' : 'gap-0 px-1 py-2'}`}>
      {ALL_COLORS.map((c, i) => (
        <div
          key={c}
          className="flex flex-col items-center gap-0.5"
          title={bonuses && i < 5 ? `${COLOR_NAME[c]}：筹码 ${tokens[i]}，卡牌 ${bonuses[i]}` : `${COLOR_NAME[c]}：筹码 ${tokens[i]}`}
        >
          <GemToken color={c} count={tokens[i]} size={dense ? 'xs' : 'panel'} />
          {bonuses && i < 5 && (
            <span
              className={`${GEM_BG[c]} ${dense ? 'w-5 xs:w-6 text-[11px] leading-4' : 'w-7 text-sm leading-5'} rounded-sm text-center font-bold ring-1 ring-white/40`}
              aria-label={`${COLOR_NAME[c]}卡 ${bonuses[i]} 张`}
            >
              {bonuses[i]}
            </span>
          )}
        </div>
      ))}
    </div>
  )
}
