import type { Library } from './types.js';
export function formatTime(seconds: number): string {
    const n = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
    const h = Math.floor(n / 3600), m = Math.floor(n % 3600 / 60), s = n % 60;
    return h ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`;
}
export function parseTimestamp(text: string): number | null {
    if (!/^\d{1,3}:\d{2}(?::\d{2})?$/.test(text.trim()))
        return null;
    const p = text.trim().split(':').map(Number);
    if (p.slice(1).some(v => v > 59))
        return null;
    return p.reduce((n, v) => n * 60 + v, 0);
}
export function canUseSpace(target: Pick<HTMLElement, 'tagName' | 'isContentEditable' | 'closest'> | null): boolean {
    return !!target && !['INPUT', 'TEXTAREA', 'SELECT', 'BUTTON', 'A'].includes(target.tagName) && !target.isContentEditable && !target.closest('dialog');
}
export function libraryStatus(v: Pick<Library, 'status' | 'items' | 'complete'>): string {
    const n = v.items.length;
    if (v.status === 'error')
        return `已保留 ${n} 条 · 本轮加载失败`;
    if (v.complete)
        return `本轮加载结束 · ${n} 条（非平台快照）`;
    if (v.status === 'unknown_end')
        return `已加载 ${n} 条 · 平台未确认完整性`;
    if (v.status === 'stale')
        return `上次缓存 ${n} 条 · 已过期`;
    if (v.status === 'cached')
        return `上次完整缓存 · ${n} 条`;
    if (v.status === 'idle')
        return '尚未加载';
    return `已加载 ${n} 条 · 部分内容`;
}
export function formatDate(value?: string): string { if (!value)
    return ''; const d = new Date(value); return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('zh-CN'); }
export function isPublicExternal(url: string): boolean {
    try {
        const u = new URL(url);
        return u.protocol === 'https:' && !u.username && !u.password && !u.port && !/^(localhost|127\.|10\.|192\.168\.|\[)/i.test(u.hostname) && u.hostname.includes('.');
    }
    catch {
        return false;
    }
}
