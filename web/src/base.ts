// Build-time base path, e.g. "/" or "/gem/". Vite replaces import.meta.env.BASE_URL.
export const BASE_PATH = import.meta.env.BASE_URL

export function appUrl(path: string): string {
  return `${BASE_PATH}${path.replace(/^\//, '')}`
}

export const ROUTER_BASENAME = BASE_PATH.replace(/\/$/, '') || undefined
