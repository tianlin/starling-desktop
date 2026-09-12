export class APIError extends Error {
    code;
    retryAfter;
    constructor(e) { super(e.message); this.name = 'APIError'; this.code = e.code; this.retryAfter = e.retryAfter ?? 0; }
}
// The demo transport exists only when the explicit test server injects its random capability.
// Production never starts an account HTTP server, nor accepts a user-specified API origin.
export const call = async (action, payload = {}) => {
    let raw;
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
    let value;
    try {
        value = JSON.parse(raw);
    }
    catch {
        throw new APIError({ code: 'BAD_BRIDGE_RESPONSE', message: '桌面后端响应无效。' });
    }
    if (!value.ok)
        throw new APIError(value.error ?? { code: 'INTERNAL', message: '操作未完成。' });
    return value.data;
};
export function describeError(e) { return e instanceof Error ? e.message : '操作未完成。'; }
export async function openExternal(url) { await call('desktop.openExternal', { url }); }
