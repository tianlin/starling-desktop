import { call, describeError } from './api.js';
import { button, el } from './dom.js';
// Each dialog owns its attempt. No credentials enter the renderer.
export function qrLogin(modal, remember, prepare, connected, refresh) {
    const panel = el('section', 'qr-login');
    const message = el('p', 'muted', '用已登录的小宇宙 App 扫码，无需短信。');
    const canvas = el('canvas');
    canvas.hidden = true;
    canvas.setAttribute('aria-label', '小宇宙 App 登录二维码');
    const start = button('生成扫码登录二维码', () => { void begin(); }, 'button primary');
    let id = '', disposed = false, timer;
    let creating = false;
    const cancel = async () => {
        if (timer)
            clearTimeout(timer);
        const old = id;
        id = '';
        if (old) {
            await call('account.qrCancel', { id: old });
            await refresh();
        }
    };
    async function poll(attempt) {
        if (disposed || id !== attempt)
            return;
        try {
            const result = await call('account.qrPoll', { id: attempt, remember: remember.checked });
            if (disposed || id !== attempt)
                return;
            if (result.status === 'CONFIRMED') {
                id = '';
                canvas.hidden = true;
                message.textContent = '身份已核对，正在读取个人播客库…';
                await connected();
                return;
            }
            message.textContent = result.status === 'SCANNED' ? '扫码成功，请在手机上确认登录。' : '打开小宇宙 App，扫描二维码登录（不是微信扫一扫）。';
            timer = setTimeout(() => { void poll(attempt); }, 1500);
        }
        catch (e) {
            if (disposed || id !== attempt)
                return;
            message.textContent = describeError(e);
            canvas.hidden = true;
            start.disabled = false;
            start.textContent = '重新生成二维码';
            await cancel().catch(() => { });
        }
    }
    async function begin() {
        if (creating || disposed)
            return;
        start.disabled = true;
        creating = true;
        try {
            await cancel();
            await prepare();
            if (disposed)
                return;
            message.textContent = '正在创建二维码…';
            const q = await call('account.qrStart');
            if (disposed) {
                await call('account.qrCancel', { id: q.id });
                await refresh();
                return;
            }
            id = q.id;
            const scale = Math.max(3, Math.floor(240 / q.modules.length));
            canvas.width = canvas.height = q.modules.length * scale;
            const ctx = canvas.getContext('2d');
            ctx.fillStyle = '#fff';
            ctx.fillRect(0, 0, canvas.width, canvas.height);
            ctx.fillStyle = '#000';
            q.modules.forEach((row, y) => row.forEach((on, x) => { if (on)
                ctx.fillRect(x * scale, y * scale, scale, scale); }));
            canvas.hidden = false;
            message.textContent = '打开小宇宙 App，扫描二维码并确认。';
            void poll(id);
        }
        catch (e) {
            await refresh().catch(() => { });
            if (!disposed) {
                message.textContent = describeError(e);
                start.disabled = false;
            }
        }
        finally {
            creating = false;
        }
    }
    modal.addEventListener('close', () => { disposed = true; void cancel().catch(() => { }); }, { once: true });
    panel.append(el('h3', '', '小宇宙 App 扫码登录'), message, canvas, start);
    return panel;
}
