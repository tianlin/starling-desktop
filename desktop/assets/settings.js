import { APIError, call, describeError } from './api.js';
import { qrLogin } from './qr-login.js';
import { button, el, input, icon } from './dom.js';
// Reopening the dialog must wait for the previous SMS cancellation and reload.
let accountCleanup = Promise.resolve();
function openModal(app, title) {
    const body = document.querySelector('#modal-content');
    body.replaceChildren();
    const head = el('div', 'modal-head');
    const heading = el('h2', '', title);
    heading.id = 'modal-title';
    app.modal.setAttribute('aria-labelledby', heading.id);
    head.append(heading, icon('×', '关闭弹窗', () => app.modal.close()));
    body.append(head);
    const keepFocus = (event) => {
        if (event.key !== 'Tab' || event.ctrlKey || event.altKey || event.metaKey)
            return;
        const controls = [...body.querySelectorAll('button,input,select,textarea,a[href],[tabindex]')]
            .filter(node => node.tabIndex >= 0 && !node.matches(':disabled') && node.getClientRects().length > 0);
        const first = controls[0], last = controls.at(-1);
        if (!first || !last)
            return;
        if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
        }
        else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
        }
    };
    app.modal.addEventListener('keydown', keepFocus);
    app.modal.addEventListener('close', () => app.modal.removeEventListener('keydown', keepFocus), { once: true });
    app.modal.showModal();
    return body;
}
export function showLink(app) {
    if (!app.boot)
        return;
    const body = openModal(app, '打开小宇宙分享链接');
    const form = el('form');
    const field = input('节目或单集的完整链接', 'text', 'https://www.xiaoyuzhoufm.com/episode/…');
    field.input.required = true;
    field.input.maxLength = 16384;
    const hint = el('p', 'muted', '可粘贴整段分享文本。不读取剪贴板，不接受短链或其他网站。');
    const error = el('p', 'form-error');
    error.setAttribute('role', 'alert');
    const go = el('button', 'button primary', '打开内容');
    go.type = 'submit';
    form.append(field.label, hint, error, go);
    body.append(form);
    form.addEventListener('submit', e => {
        e.preventDefault();
        go.disabled = true;
        const epoch = app.boot.session.epoch;
        void call('openLink', { epoch, text: field.input.value }).then(it => {
            if (epoch === app.boot.session.epoch && app.modal.open) {
                app.modal.close();
                app.showDetail(it);
            }
        }).catch(e => { error.textContent = describeError(e); }).finally(() => { go.disabled = false; });
    });
    field.input.focus();
}
export function showAccount(app) {
    if (!app.boot)
        return;
    const body = openModal(app, '连接小宇宙账号');
    if (app.boot.session.identity) {
        body.append(el('p', '', `当前账号：${app.boot.session.identity.nickname}`), el('p', 'muted', `连接状态：${app.boot.session.state}。退出仅清除此客户端的会话与账号本地数据，不会删除小宇宙账号。`), button('退出并清除本机账号数据', () => app.logout().catch(e => app.notice(e)), 'button danger'));
        return;
    }
    body.append(el('div', 'inline-warning', '实验性账号接入：推荐使用小宇宙 App 扫码。扫码确认后还需核对听众身份；个人库兼容性仍需实测。旧短信接口未包含当前官方网页验证流程，可能无法发送。'));
    const risk = el('label', 'check-row');
    const check = el('input');
    check.type = 'checkbox';
    check.checked = app.boot.settings.experimentalAccount;
    risk.append(check, el('span', '', '我已了解上述风险，启用实验性账号接入'));
    body.append(risk);
    const form = el('form');
    const phone = input('手机号', 'tel', '输入你的手机号');
    phone.input.autocomplete = 'tel-national';
    phone.input.maxLength = 15;
    phone.input.pattern = '[0-9]{5,15}';
    phone.input.required = true;
    const area = input('国家 / 地区码', 'tel', '+86');
    area.input.value = '+86';
    area.input.maxLength = 5;
    const phoneRow = el('div', 'phone-row');
    phoneRow.append(area.label, phone.label);
    const code = input('短信验证码', 'text', '4–8 位验证码');
    code.input.autocomplete = 'one-time-code';
    code.input.inputMode = 'numeric';
    code.input.pattern = '[0-9]{4,8}';
    code.input.maxLength = 8;
    code.input.required = true;
    const status = el('p', 'form-error');
    status.setAttribute('role', 'alert');
    const remember = el('label', 'check-row');
    const rememberBox = el('input');
    rememberBox.type = 'checkbox';
    rememberBox.checked = app.boot.session.storageAvailable;
    rememberBox.disabled = !app.boot.session.storageAvailable;
    remember.append(rememberBox, el('span', '', app.boot.session.storageAvailable ? '使用 Windows 系统保护保存会话' : '系统保护不可用，仅本次会话'));
    let timer;
    let busy = false;
    let disposed = false;
    let loginEpoch;
    const enable = async () => {
        if (!check.checked)
            throw Error('请先确认实验接入风险。');
        app.boot.settings.experimentalAccount = true;
        await app.saveSettings();
    };
    const send = button('发送验证码', async () => {
        if (busy)
            return;
        busy = true;
        send.disabled = true;
        status.textContent = '';
        try {
            await enable();
            if (disposed)
                return;
            await call('account.sendCode', { phone: phone.input.value, area: area.input.value });
            if (disposed)
                return;
            status.textContent = '已请求发送。请查看手机；客户端不会自动重发。';
            let left = 60;
            send.textContent = `${left}s 后可重发`;
            timer = setInterval(() => {
                send.textContent = `${--left}s 后可重发`;
                if (left <= 0) {
                    clearInterval(timer);
                    send.disabled = false;
                    send.textContent = '发送验证码';
                }
            }, 1000);
        }
        catch (e) {
            status.textContent = describeError(e);
            send.disabled = false;
        }
        finally {
            busy = false;
        }
    });
    const submit = el('button', 'button primary', '验证并连接');
    submit.type = 'submit';
    const cancelLogin = button('取消连接', () => app.modal.close());
    cancelLogin.hidden = true;
    form.append(phoneRow, send, code.label, remember, status, submit, cancelLogin);
    body.append(qrLogin(app.modal, rememberBox, async () => {
        await accountCleanup;
        if (disposed)
            return;
        await app.reload();
        if (disposed)
            return;
        await enable();
        app.player.pause();
        await app.player.persist();
        app.player.clear();
    }, async () => {
        if (disposed)
            return;
        app.player.clear();
        await app.reload();
        if (disposed)
            return;
        app.modal.close();
        await app.navigate('favorites');
    }, () => app.reload()));
    body.append(form, el('p', 'fine-print', '验证码只用于这次认证，不保存到 SQLite 或日志。凭据不会返回前端。'));
    form.addEventListener('submit', e => {
        e.preventDefault();
        if (busy)
            return;
        busy = true;
        submit.disabled = true;
        cancelLogin.hidden = false;
        status.textContent = '正在验证身份，可点击取消连接或关闭此窗口。';
        const credentials = { phone: phone.input.value, area: area.input.value, code: code.input.value, remember: rememberBox.checked };
        code.input.value = '';
        void (async () => {
            await accountCleanup;
            if (disposed)
                return;
            await app.reload();
            if (disposed)
                return;
            loginEpoch = app.boot.session.epoch;
            await enable();
            if (disposed)
                return;
            app.player.pause();
            await app.player.persist();
            if (disposed)
                return;
            await call('account.login', { epoch: loginEpoch, ...credentials });
            loginEpoch = undefined;
            if (disposed)
                return;
            app.player.clear();
            await app.reload();
            if (disposed)
                return;
            app.modal.close();
            await app.navigate('favorites');
        })().catch(async (e) => { if (!disposed) {
            status.textContent = describeError(e);
            await app.reload().catch(() => { });
        } }).finally(() => { credentials.code = ''; busy = false; submit.disabled = false; cancelLogin.hidden = true; });
    });
    app.modal.addEventListener('close', () => {
        disposed = true;
        if (timer)
            clearInterval(timer);
        phone.input.value = '';
        code.input.value = '';
        const epoch = loginEpoch;
        loginEpoch = undefined;
        if (epoch !== undefined) {
            accountCleanup = accountCleanup.then(async () => {
                try {
                    await call('account.cancelLogin', { epoch });
                }
                catch (e) {
                    if (!(e instanceof APIError && e.code === 'STALE_SESSION'))
                        app.notice(e);
                }
                await app.reload().catch(e => app.notice(e));
            });
        }
    }, { once: true });
}
export function showSettings(app) {
    app.page.replaceChildren(app.heading('设置', '本地优先。连接、播放和数据由你控制。'));
    const panel = el('div', 'settings-panel');
    const section = (title, detail, ...controls) => { const row = el('section', 'setting-row'); const copy = el('div'); copy.append(el('h3', '', title), el('p', 'muted', detail)); row.append(copy, ...controls); panel.append(row); };
    section('账号连接', app.boot.session.identity ? app.boot.session.identity.nickname : '访客模式 · 不会显示虚构的云端收藏', button('管理账号', () => showAccount(app)));
    const enabled = el('input');
    enabled.type = 'checkbox';
    enabled.checked = app.boot.settings.experimentalAccount;
    enabled.disabled = !!app.boot.session.identity;
    enabled.setAttribute('aria-label', '实验性账号接入');
    enabled.addEventListener('change', () => {
        if (enabled.checked) {
            enabled.checked = false;
            showAccount(app);
            return;
        }
        app.boot.settings.experimentalAccount = false;
        void app.saveSettings().catch(e => app.notice(e));
    });
    section('实验性账号接入', '默认关闭。启用前阅读账号风控与非官方接口风险。连接后可查看评论；点击“发表评论”会以当前账号发送公开评论。', enabled);
    section('恢复已保存会话', '只读取本应用自身的受保护凭据，不读取其他应用或浏览器的登录状态。', button('尝试恢复', () => { void call('account.restore').then(() => app.reload()).then(() => app.navigate('settings')).catch(e => app.notice(e)); }));
    const close = el('select');
    close.setAttribute('aria-label', '关闭窗口行为');
    for (const [value, label] of [['ask', '每次询问'], ['tray', '最小化到托盘'], ['exit', '退出程序']]) {
        const option = el('option', '', label);
        option.value = value;
        close.append(option);
    }
    close.value = app.boot.settings.closeBehavior;
    close.addEventListener('change', () => { app.boot.settings.closeBehavior = close.value; void app.saveSettings().catch(e => app.notice(e)); });
    section('关闭主窗口', app.desktop.tray ? '托盘可显示窗口、播放 / 暂停或退出。' : '当前宿主尚未报告托盘能力；无法隐藏时会明确提示。', close);
    section('媒体键', app.desktop.mediaKey ? '原生媒体键已注册；实际键盘与系统冲突仍需实机验证。' : '使用宿主 Media Session 能力；本环境未验证 Windows 媒体键。');
    section('清除列表缓存', '不会退出账号，也不会删除书签、队列或收听进度。', button('清除缓存', () => { void call('cache.clear', { epoch: app.boot.session.epoch }).then(() => app.notice('列表缓存已清除。')).catch(e => app.notice(e)); }));
    section('本地诊断', '仅保存本次进程最近 100 条业务错误码，不包含账号、令牌或收听内容。', button('查看诊断', () => { void call('diagnostics').then(data => { const body = openModal(app, '诊断预览'); body.append(el('pre', 'diagnostics', JSON.stringify(data, null, 2)), button('保存到文件', () => { void call('desktop.exportDiagnostics').then(() => app.notice('诊断保存操作已结束。')).catch(e => app.notice(e)); })); }).catch(e => app.notice(e)); }));
    section('重置全部本地数据', '清除本应用凭据、账号与访客书签、队列、进度和设置。无法撤销；不承诺取证级安全擦除。', button('重置数据', () => {
        if (!confirm('确定清除 Starling 的全部本地数据？此操作无法撤销。'))
            return;
        app.player.pause();
        void app.player.persist().then(() => call('data.reset', { epoch: app.boot.session.epoch, confirm: 'RESET' })).then(() => { app.player.clear(); return app.reload(); }).then(() => app.navigate('home')).catch(e => app.notice(e));
    }, 'button danger'));
    section('关于 Starling · 星听', `${app.boot.version} · ${app.boot.adapter}。非官方开源客户端，与小宇宙无隶属或授权关系。仅在本机处理数据；正常播放仍会请求平台或 CDN。`);
    app.page.append(panel);
}
