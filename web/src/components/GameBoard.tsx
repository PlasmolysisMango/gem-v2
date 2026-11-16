import { useEffect, useMemo, useState } from 'react'
import type { Action, Card, RoomState, View } from '../api/types'
import { ALL_COLORS } from '../api/types'
import {
  autoPay,
  canAddDiscard,
  canAddToken,
  COLOR_NAME,
  discardTarget,
  emptyGems,
  findAction,
  gemsText,
  highlightOf,
  matchDiscard,
  matchTake,
  takeInProgress,
  total,
} from '../lib/gems'
import { CardTile, DeckTile, GemToken, NobleTile, TokenRow } from './Pieces'

export function seatLabel(room: RoomState | null, seat: number): string {
  const s = room?.seats[seat]
  return s?.name || `玩家${seat + 1}`
}

type Selection = { kind: 'card'; card: Card; reserved: boolean } | { kind: 'deck'; level: number } | null

/** Interactive board. Without onAct it is read-only (spectating / replay). */
export function GameBoard({
  view,
  room,
  onAct,
  hintAction,
}: {
  view: View
  room: RoomState | null
  onAct?: (a: Action) => void
  hintAction?: Action
}) {
  const me = view.viewer >= 0 ? view.players[view.viewer] : null
  const legal = onAct && view.current === view.viewer ? (view.legal ?? []) : []
  const myTurn = legal.length > 0
  const [sel, setSel] = useState<number[]>(emptyGems())
  const [pick, setPick] = useState<Selection>(null)
  const hl = useMemo(() => highlightOf(hintAction), [hintAction])

  useEffect(() => {
    setSel(emptyGems())
    setPick(null)
  }, [view.turn, view.phase, view.current])

  const takeAction = myTurn && view.phase === 'main' ? matchTake(legal, sel) : undefined
  const canMore = myTurn && view.phase === 'main' && takeInProgress(legal, sel)

  const clickBank = (i: number) => {
    if (!myTurn || view.phase !== 'main') return
    if (!canAddToken(sel, view.bank, i)) return
    const next = [...sel]
    next[i]++
    setSel(next)
    setPick(null)
  }
  const unselect = (i: number) => {
    const next = [...sel]
    next[i] = Math.max(0, next[i] - 1)
    setSel(next)
  }

  const select = (selection: NonNullable<Selection>) => {
    setSel(emptyGems())
    setPick(selection)
  }

  const send = (a?: Action) => {
    if (a && onAct) onAct(a)
    setSel(emptyGems())
    setPick(null)
  }

  const cardActions = (c: Card, reserved: boolean) => ({
    buy: findAction(legal, reserved ? 'buy_reserved' : 'buy', c.id),
    reserve: reserved ? undefined : findAction(legal, 'reserve', c.id),
  })

  const pickInfo = (() => {
    if (!pick) return null
    if (pick.kind === 'deck') {
      const a = findAction(legal, 'reserve_deck', undefined, pick.level)
      return (
        <div className="rounded-lg bg-slate-800 p-3 space-y-2">
          <div>
            盲扣 L{pick.level} 牌堆顶牌{view.bank[5] > 0 ? '（获得 1 金）' : ''}
          </div>
          <div className="flex flex-wrap gap-2">
            <button className="btn-primary flex-1 sm:flex-none" disabled={!a} onClick={() => send(a)}>
              确认盲扣
            </button>
            <button className="btn" onClick={() => setPick(null)}>
              取消选择
            </button>
          </div>
        </div>
      )
    }
    const { buy, reserve } = cardActions(pick.card, pick.reserved)
    const pay = me ? autoPay(me, pick.card) : null
    return (
      <div className="rounded-lg bg-slate-800 p-3 space-y-2 break-words">
        <div>
          L{pick.card.level} {COLOR_NAME[pick.card.bonus]}卡 {pick.card.points} 分
        </div>
        {buy && pay && <div className="text-sm text-slate-300">支付：{gemsText(pay)}</div>}
        <div className="flex flex-wrap gap-2">
          <button className="btn-primary flex-1 sm:flex-none" disabled={!buy} onClick={() => send(buy)}>
            购买
          </button>
          {!pick.reserved && (
            <button className="btn flex-1 sm:flex-none" disabled={!reserve} onClick={() => send(reserve)}>
              预留{reserve && view.bank[5] > 0 ? '（+1 金）' : ''}
            </button>
          )}
          <button className="btn" onClick={() => setPick(null)}>
            取消选择
          </button>
        </div>
      </div>
    )
  })()

  return (
    <div className="min-w-0 space-y-3">
      {/* nobles */}
      <div className="flex flex-wrap gap-2 items-center">
        <span className="text-sm text-slate-400 w-full sm:w-10 shrink-0">贵族</span>
        {view.nobles.map((n) => (
          <NobleTile key={n.id} noble={n} />
        ))}
      </div>
      {/* card rows, level 3 on top */}
      {[2, 1, 0].map((lv) => (
        <div key={lv} className="grid grid-cols-2 min-[420px]:grid-cols-4 sm:grid-cols-[5rem_repeat(4,6rem)] gap-2 items-center">
          <div className="col-span-full sm:col-span-1">
            <DeckTile
              level={lv + 1}
              count={view.deckLen[lv]}
              selected={pick?.kind === 'deck' && pick.level === lv + 1}
              highlight={hl.deck === lv + 1}
              onClick={
                myTurn && view.phase === 'main' && findAction(legal, 'reserve_deck', undefined, lv + 1)
                  ? () => select({ kind: 'deck', level: lv + 1 })
                  : undefined
              }
            />
          </div>
          {view.board[lv].map((c, i) => (
            <CardTile
              key={c ? c.id : `e${i}`}
              card={c}
              fluid
              highlight={!!c && hl.cards.has(c.id)}
              selected={!!c && pick?.kind === 'card' && pick.card.id === c.id}
              affordable={!!c && myTurn && !!findAction(legal, 'buy', c.id)}
              onClick={c && myTurn && view.phase === 'main' ? () => select({ kind: 'card', card: c, reserved: false }) : undefined}
            />
          ))}
          {pick && (pick.kind === 'deck' ? pick.level === lv + 1 : !pick.reserved && pick.card.level === lv + 1) && (
            <div className="col-span-full" aria-live="polite">
              {pickInfo}
            </div>
          )}
        </div>
      ))}
      {/* bank */}
      <div className="space-y-2">
        <div className="text-sm text-slate-400">银行（点击选择）</div>
        <div className="grid grid-cols-6 gap-1 sm:flex sm:gap-3">
          {ALL_COLORS.map((c, i) => (
            <GemToken
              key={c}
              color={c}
              count={view.bank[i] - sel[i]}
              size="lg"
              highlight={hl.colors.has(i)}
              onClick={myTurn && view.phase === 'main' && i < 5 ? () => clickBank(i) : undefined}
              disabled={!canAddToken(sel, view.bank, i)}
            />
          ))}
        </div>
      </div>
      {/* action bar */}
      {myTurn && view.phase === 'main' && !pick && (
        <div className="rounded-lg bg-slate-800 p-3 flex items-center gap-2 flex-wrap min-h-[3.5rem]" aria-live="polite">
          {total(sel) > 0 ? (
            <>
              <span>拿取：</span>
              {ALL_COLORS.map((c, i) =>
                Array.from({ length: sel[i] }).map((_, k) => (
                  <GemToken key={`${c}${k}`} color={c} size="sm" onClick={() => unselect(i)} title="点击放回" />
                )),
              )}
              <button className="btn-primary" disabled={!takeAction} onClick={() => send(takeAction)}>
                确认拿取
              </button>
              <button className="btn" onClick={() => setSel(emptyGems())}>
                清空
              </button>
              {!takeAction && canMore && <span className="text-xs text-slate-400">继续选择筹码…</span>}
              {me && total(me.tokens) + total(sel) > 10 && <span className="text-xs text-amber-400">拿取后超过 10 枚，需要归还</span>}
            </>
          ) : (
            <span className="text-sm text-slate-400">轮到你了：点击银行筹码拿取，或点击卡牌购买/预留，点击牌堆盲扣。</span>
          )}
          {findAction(legal, 'pass') && (
            <button className="btn" onClick={() => send(findAction(legal, 'pass'))}>
              无法行动，跳过
            </button>
          )}
        </div>
      )}
      {me && myTurn && view.phase === 'discard' && <DiscardBar view={view} legal={legal} onAct={send} />}
      {myTurn && view.phase === 'noble' && (
        <div className="rounded-lg bg-amber-900/60 p-3 flex flex-wrap items-center gap-3">
          <span className="w-full">同时满足多位贵族，请选择一位迎接：</span>
          {legal
            .filter((a) => a.type === 'noble')
            .map((a) => {
              const n = view.nobles.find((x) => x.id === a.noble)
              return n ? <NobleTile key={a.noble} noble={n} onClick={() => send(a)} /> : null
            })}
        </div>
      )}
      {/* my reserved cards */}
      {me && (
        <div className="space-y-2">
          <div className="text-sm text-slate-400">我的预留{me.reserved.length === 0 ? '：无' : ''}</div>
          <div className="grid grid-cols-2 min-[420px]:grid-cols-3 sm:grid-cols-[repeat(3,6rem)] gap-2">
            {me.reserved.map((c) => (
              <CardTile
                key={c.id}
                card={c}
                fluid
                highlight={hl.cards.has(c.id)}
                selected={pick?.kind === 'card' && pick.card.id === c.id}
                affordable={myTurn && !!findAction(legal, 'buy_reserved', c.id)}
                onClick={myTurn && view.phase === 'main' ? () => select({ kind: 'card', card: c, reserved: true }) : undefined}
              />
            ))}
            {pick?.kind === 'card' && pick.reserved && (
              <div className="col-span-full" aria-live="polite">
                {pickInfo}
              </div>
            )}
          </div>
          <div>
            <TokenRow tokens={me.tokens} bonuses={me.bonuses} />
            <div className="text-xs text-slate-400 mt-1">筹码 {total(me.tokens)}/10</div>
          </div>
        </div>
      )}
      {view.viewer < 0 && <div className="text-xs text-slate-500">观战视角：{seatLabel(room, view.current)} 行动中</div>}
    </div>
  )
}

function DiscardBar({ view, legal, onAct }: { view: View; legal: Action[]; onAct: (a?: Action) => void }) {
  const me = view.players[view.viewer]
  const [sel, setSel] = useState<number[]>(emptyGems())
  const need = discardTarget(me)
  const a = matchDiscard(legal, sel)
  return (
    <div className="rounded-lg bg-rose-900/60 p-3 space-y-2">
      <div>
        筹码超过 10 枚，请选择归还 {need} 枚（已选 {total(sel)}）：
      </div>
      <div className="grid grid-cols-6 gap-0.5 sm:flex sm:gap-2">
        {ALL_COLORS.map((c, i) => (
          <GemToken
            key={c}
            color={c}
            count={me.tokens[i] - sel[i]}
            onClick={() => canAddDiscard(sel, me, i) && setSel(sel.map((v, j) => (j === i ? v + 1 : v)))}
            disabled={!canAddDiscard(sel, me, i)}
          />
        ))}
      </div>
      <div className="flex flex-wrap gap-2 items-center">
        <span className="text-sm w-full sm:w-auto">归还：{gemsText(sel)}</span>
        <button className="btn-primary" disabled={!a} onClick={() => onAct(a)}>
          确认归还
        </button>
        <button className="btn" onClick={() => setSel(emptyGems())}>
          重选
        </button>
      </div>
    </div>
  )
}
