import type { Analysis, MoveReview } from '../api/types'
import { pct } from '../lib/gems'

const STRATEGY_NAME: Record<string, string> = {
  block: '预留封锁',
  race: '冲分',
  noble: '贵族',
  engine: '引擎',
  target: '目标卡',
}

const VERDICT_STYLE: Record<string, string> = {
  best: 'bg-emerald-700',
  good: 'bg-sky-700',
  mistake: 'bg-amber-600',
  blunder: 'bg-rose-700',
}

export function ReviewBadge({ r, who }: { r: MoveReview; who?: string }) {
  return (
    <div className={`${VERDICT_STYLE[r.verdict]} rounded px-2 py-1 text-sm`}>
      {who && <div className="text-xs opacity-90">{who} 的上一手</div>}
      <b>{r.verdictText}</b>：{r.playedText}
      {r.verdict !== 'best' && (
        <div className="text-xs opacity-90">
          {r.comment}（你的着法 {pct(r.playedWinRate)} / 推荐 {pct(r.bestWinRate)}）
        </div>
      )}
    </div>
  )
}

export function HintPanel({
  enabled,
  myTurn,
  loading,
  hint,
  onRequest,
  onFocus,
  focus,
  lastReview,
  seatName = (s) => `玩家${s + 1}`,
  replay = false,
}: {
  enabled: boolean
  myTurn: boolean
  loading: boolean
  hint: Analysis | null
  onRequest: () => void
  onFocus: (i: number) => void
  focus: number
  lastReview: MoveReview | null
  seatName?: (seat: number) => string
  replay?: boolean
}) {
  if (!enabled) {
    return <div className="text-sm text-slate-400">本房间未开启提示（房主可在创建房间时开启）。</div>
  }
  return (
    <div className="min-w-0 space-y-3 break-words">
      <div className="flex flex-wrap items-center gap-2">
        {!replay && <button
          className="min-h-[44px] min-w-[44px] px-3 py-1.5 rounded bg-fuchsia-600 hover:bg-fuchsia-500 disabled:opacity-40 font-semibold"
          disabled={!myTurn || loading}
          onClick={onRequest}
        >
          {loading ? '大师 AI 演算中…' : '提示'}
        </button>}
        <span className="text-xs text-slate-400">{replay ? '大师 AI 按行动前的玩家视角分析，不读取未来牌序或对手暗牌。' : '由大师 AI（ISMCTS）在你的视角下实时演算'}</span>
        {replay && loading && <span className="text-xs text-fuchsia-300">本手分析生成中…</span>}
      </div>
      {lastReview && (
        <div>
          <div className="text-xs text-slate-400 mb-1">上一手复盘</div>
          <ReviewBadge r={lastReview} />
        </div>
      )}
      {hint && (
        <div className="space-y-2">
          <div className="text-sm">
            <span className="px-1.5 py-0.5 rounded bg-indigo-700 text-xs mr-1">{STRATEGY_NAME[hint.strategy] ?? hint.strategy}</span>
            {hint.strategyText}
          </div>
          <div className="text-xs text-slate-400">
            估计胜率 {pct(hint.winRate)} · 模拟 {hint.iterations} 次 · 用时 {hint.elapsedMs}ms{hint.endgame ? ' · 残局精确搜索' : ''}
          </div>
          <div className="text-xs text-slate-400 xl:hidden">点击候选着法，回到棋盘查看高亮；不会自动落子。</div>
          {hint.candidates.map((c, i) => (
            <div
              key={i}
              role="button"
              tabIndex={0}
              aria-pressed={focus === i}
              onClick={() => onFocus(i)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  onFocus(i)
                }
              }}
              className={`min-h-[44px] min-w-0 rounded border p-2 cursor-pointer ${focus === i ? 'border-fuchsia-400 bg-slate-800' : 'border-slate-700 hover:bg-slate-800/60'}`}
            >
              <div className="flex justify-between gap-2 text-sm font-semibold">
                <span className="min-w-0 flex-1">
                  {i + 1}. {c.text}
                </span>
                <span className="shrink-0">{pct(c.winRate)}</span>
              </div>
              <div className="h-1.5 bg-slate-700 rounded mt-1">
                <div className="h-1.5 bg-fuchsia-500 rounded" style={{ width: `${Math.max(2, c.winRate * 100)}%` }} />
              </div>
              <ul className="mt-1 text-sm sm:text-xs list-disc pl-4 space-y-0.5 text-slate-200">
                {c.reasons.map((r, j) => (
                  <li key={j}>{r.text}</li>
                ))}
              </ul>
              {c.pv.length > 0 && (
                <div className="mt-1 text-xs sm:text-[11px] text-slate-400">
                  预期变化：{c.pv.map((p) => `${seatName(p.seat)} ${p.text}`).join(' → ')}
                </div>
              )}
              {c.visits > 0 && <div className="text-[10px] text-slate-500">访问 {c.visits}</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
