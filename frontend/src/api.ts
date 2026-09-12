import type { AppFailure, Call } from './types.js';
declare global {
    interface Window {
        go?: {
            main?: {
                App?: {
                    Call: (action: string, payload: string) => Promise<string>;
                };
            };
        };
        runtime?: {
            EventsOn: (event: string, cb: (...args: unknown[]) => void) => (() => void);
        };
        __STARLING_DEMO__?: {
            token: string;
        };
    }
}
export class APIError extends Error {
    readonly code: string;
    readonly retryAfter: number;
    constructor(e: AppFailure) { super(e.message); this.name = 'APIError'; this.code = e.code; this.retryAfter = e.retryAfter ?? 0; }
}
// The demo transport exists only when the explicit test server injects its random capability.
// Production never starts an account HTTP server, nor accepts a user-specified API origin.
export const call: Call = async <T>(action: string, payload: unknown = {}) => {
    let raw: string;
    if (window.go?.main?.App) {
        raw = await window.go.main.App.Call(action, JSON.stringify(payload));
    }
    else if (window.__STARLING_DEMO__) {
        const res = await fetch('/api', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Starling-Demo': window.__STARLING_DEMO__.token }, body: JSON.stringify({ action, payload }) });
        if (!res.ok)
            throw new APIError({ code: 'DEMO_TRANSPORT', message: '演示后端未响应。' });
        raw = await res.text();
    }
    else
        throw new APIError({ code: 'NO_BRIDGE', message: '未连接桌面后端。请启动 Starling 桌面程序；仅打开 HTML 文件不会连接账号。' });
    let value: {
        ok: boolean;
        data: T;
        error?: AppFailure;
    };
    try {
        value = JSON.parse(raw) as typeof value;
    }
    catch {
        throw new APIError({ code: 'BAD_BRIDGE_RESPONSE', message: '桌面后端响应无效。' });
    }
    if (!value.ok)
        throw new APIError(value.error ?? { code: 'INTERNAL', message: '操作未完成。' });
    return value.data;
};
export function describeError(e: unknown): string { return e instanceof Error ? e.message : '操作未完成。'; }
export async function openExternal(url: string): Promise<void> { await call('desktop.openExternal', { url }); }
