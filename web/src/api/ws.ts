import { appUrl } from '../base'
import type { Envelope } from './types'

type Handler = (e: Envelope<any>) => void

/** Reconnecting WebSocket client for /ws (token passed as query parameter). */
export class Socket {
  private ws: WebSocket | null = null
  private queue: string[] = []
  private closed = false
  private retry = 0
  private timer: number | undefined
  onMessage: Handler = () => {}
  onStatus: (connected: boolean) => void = () => {}
  onOpen: () => void = () => {}

  constructor(private token: string) {}

  connect() {
    this.closed = false
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}${appUrl('ws')}?token=${encodeURIComponent(this.token)}`)
    this.ws = ws
    ws.onopen = () => {
      this.retry = 0
      this.onStatus(true)
      this.onOpen()
      for (const m of this.queue) ws.send(m)
      this.queue = []
    }
    ws.onmessage = (ev) => {
      try {
        this.onMessage(JSON.parse(ev.data))
      } catch {
        /* ignore malformed frames */
      }
    }
    ws.onclose = () => {
      this.onStatus(false)
      if (this.closed) return
      const delay = Math.min(10000, 500 * 2 ** this.retry++)
      this.timer = window.setTimeout(() => this.connect(), delay)
    }
  }

  send(type: string, roomId?: string, payload?: unknown) {
    const msg = JSON.stringify({ type, roomId, payload })
    if (this.ws && this.ws.readyState === WebSocket.OPEN) this.ws.send(msg)
    else this.queue.push(msg)
  }

  close() {
    this.closed = true
    window.clearTimeout(this.timer)
    this.ws?.close()
  }
}
