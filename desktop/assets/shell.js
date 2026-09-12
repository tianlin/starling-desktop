import { call, describeError, openExternal } from './api.js';
import { Player } from './player.js';
import { el, button, cover, empty } from './dom.js';
import { formatTime, formatDate, libraryStatus, canUseSpace } from './util.js';
import { renderNotes } from './notes.js';
import { showAccount, showLink, showSettings } from './settings.js';
const titles = { home: '继续收听', subscriptions: '我的订阅', favorites: '收藏单集', bookmarks: '本地书签', queue: '稍后听', settings: '设置' };
const navIcons = { home: '◷', subscriptions: '▤', favorites: '♡', bookmarks: '▱', queue: '☷', settings: '⚙' };
export class Application {
    boot;
    desktop = { tray: false, mediaKey: false, demo: false };
    player;
    page = document.querySelector('#page');
    modal = document.querySelector('#modal');
    route = 'home';
    routeGeneration = 0;
    list = null;
    listKind = '';
    podcastID = '';
    filter = '';
    displayPage = 0;
    allLoading = false;
    loading = false;
    alive = true;
    saveSettingsChain = Promise.resolve();
    lastMediaID = '';
    libraryGeneration = 0;
    listCancellation = Promise.resolve();
    constructor() {
        this.player = new Player(document.querySelector('#audio'), call, () => this.boot?.session.epoch ?? 0, () => this.drawPlayer());
        this.player.onPlayable = it => { void this.queue('remove', it).catch(e => this.notice(e)); };
        this.player.onEnded = () => {
            const next = this.boot.queue[0];
            if (next)
                void this.player.play(next);
        };
    }
    async start() {
        this.wire();
        try {
            this.boot = await call('bootstrap');
            this.desktop = await call('desktop.info').catch(() => this.desktop);
            if (window.__STARLING_DEMO__) {
                this.desktop.demo = true;
                document.querySelector('#demo-banner').hidden = false;
                document.body.classList.add('demo');
            }
            this.applySettings();
            if (this.boot.settings.experimentalAccount && this.boot.session.state === 'guest') {
                try {
                    await call('account.restore');
                    this.boot = await call('bootstrap');
                }
                catch (e) {
                    this.notice('已保存的账号暂未恢复：' + describeError(e));
                }
            }
            const previous = this.boot.history[0];
            if (previous)
                this.player.restore(previous);
            this.drawAccount();
            this.nativeEvents();
            await this.navigate('home');
            if (this.boot.warning)
                this.notice(this.boot.warning.message);
        }
        catch (e) {
            this.page.replaceChildren(empty('无法连接桌面后端', describeError(e)));
        }
    }
    wire() {
        const nav = document.querySelector('#navigation');
        for (const name of Object.keys(titles)) {
            const b = button('', () => this.navigate(name), 'nav-item');
            b.dataset.route = name;
            b.append(el('span', 'nav-icon', navIcons[name]), el('span', '', titles[name]));
            nav.append(b);
        }
        document.querySelector('.brand').addEventListener('click', e => { e.preventDefault(); void this.navigate('home'); });
        document.querySelector('#open-link').addEventListener('click', () => showLink(this));
        document.querySelector('#account-button').addEventListener('click', () => showAccount(this));
        document.querySelector('#top-account').addEventListener('click', () => showAccount(this));
        document.querySelector('#toggle-play').addEventListener('click', () => { void this.player.toggle(); });
        document.querySelector('#backward').addEventListener('click', () => this.player.jump(-15));
        document.querySelector('#forward').addEventListener('click', () => this.player.jump(30));
        document.querySelector('#player-title').addEventListener('click', () => {
            if (this.player.item)
                void this.details(this.player.item);
        });
        document.querySelector('#seek').addEventListener('input', e => this.player.seek(Number(e.target.value)));
        document.querySelector('#volume').addEventListener('input', e => {
            if (!this.boot)
                return;
            this.boot.settings.volume = Number(e.target.value);
            this.player.audio.volume = this.boot.settings.volume;
        });
        document.querySelector('#volume').addEventListener('change', () => { void this.saveSettings().catch(e => this.notice(e)); });
        document.querySelector('#rate').addEventListener('change', e => {
            if (!this.boot)
                return;
            this.boot.settings.rate = Number(e.target.value);
            this.player.audio.playbackRate = this.boot.settings.rate;
            void this.saveSettings().catch(e => this.notice(e));
        });
        document.querySelector('#queue-button').addEventListener('click', () => { void this.navigate('queue'); });
        document.addEventListener('keydown', e => {
            if (e.code === 'Space' && !e.repeat && canUseSpace(e.target)) {
                e.preventDefault();
                void this.player.toggle();
            }
        });
        // A normal desktop close is a handshake through nativeEvents; page unload is best-effort only.
        window.addEventListener('pagehide', () => { void this.player.persist(); });
    }
    nativeEvents() {
        window.runtime?.EventsOn('desktop:toggle', () => { void this.player.toggle(); });
        window.runtime?.EventsOn('desktop:pause', () => this.player.pause());
        window.runtime?.EventsOn('desktop:before-quit', () => { this.player.pause(); void this.player.persist().finally(() => call('desktop.quitReady').catch(() => { })); });
        if ('mediaSession' in navigator) {
            const set = (name, fn) => {
                try {
                    navigator.mediaSession.setActionHandler(name, fn);
                }
                catch { /* unsupported host capability */ }
            };
            if (!this.desktop.mediaKey) {
                set('play', () => {
                    if (this.player.state !== 'playing')
                        void this.player.toggle();
                });
                set('pause', () => this.player.pause());
            }
            set('seekbackward', d => this.player.jump(-(d.seekOffset ?? 15)));
            set('seekforward', d => this.player.jump(d.seekOffset ?? 30));
            set('seekto', d => {
                if (d.seekTime !== undefined)
                    this.player.seek(d.seekTime);
            });
        }
    }
    applySettings() { this.player.audio.volume = this.boot.settings.volume; this.player.audio.playbackRate = this.boot.settings.rate; document.querySelector('#volume').value = String(this.boot.settings.volume); document.querySelector('#rate').value = String(this.boot.settings.rate); }
    async saveSettings() {
        const settings = { ...this.boot.settings };
        const task = this.saveSettingsChain.then(() => call('settings.save', settings)).then(() => { });
        this.saveSettingsChain = task.catch(() => { });
        await task;
    }
    async reload() { this.boot = await call('bootstrap'); this.applySettings(); this.drawAccount(); }
    drawAccount() {
        if (!this.boot)
            return;
        const s = this.boot.session;
        const nick = s.identity?.nickname ?? '访客模式';
        document.querySelector('#account-name').textContent = nick;
        document.querySelector('#account-caption').textContent = s.state === 'connected' ? '实验接入 · 云端只读' : s.state === 'needs_login' ? '会话失效 · 请重新连接' : '连接你的个人播客库';
        document.querySelector('.avatar').textContent = nick.slice(0, 1);
        document.querySelector('#top-account').textContent = s.identity ? '账号与连接' : '连接账号';
        this.drawPlayer();
    }
    notice(e) { const n = document.querySelector('#notification'); n.replaceChildren(el('span', '', typeof e === 'string' ? e : describeError(e)), button('关闭', () => { n.hidden = true; }, 'text-button')); n.hidden = false; }
    async logout() {
        this.player.pause();
        await this.player.persist();
        const epoch = this.boot.session.epoch;
        let failure;
        try {
            await call('account.logout', { epoch });
        }
        catch (e) {
            failure = e;
        }
        finally {
            this.player.clear();
            await this.reload();
            this.modal.close();
            await this.navigate('home');
        }
        if (failure)
            this.notice(failure);
    }
    async queue(op, it) {
        const epoch = this.boot.session.epoch;
        const data = await call('queue', { epoch, op, item: it ?? {} });
        if (epoch !== this.boot.session.epoch)
            return;
        this.boot.queue = data;
        this.drawPlayer();
        if (this.route === 'queue')
            this.drawLocal('queue');
    }
    async bookmark(op, it) {
        const epoch = this.boot.session.epoch;
        const data = await call('bookmarks', { epoch, op, item: it });
        if (epoch !== this.boot.session.epoch)
            return;
        this.boot.bookmarks = data;
        if (this.route === 'bookmarks')
            this.drawLocal('bookmarks');
        else
            this.notice(op === 'remove' ? '已移除本地书签。' : '已保存本地书签，仅此设备可见。');
    }
    async external(it) {
        try {
            await openExternal(it.sourceUrl);
        }
        catch (e) {
            this.notice(e);
        }
    }
    stopList() {
        this.allLoading = false;
        this.libraryGeneration++;
        if (this.loading && this.listKind && this.boot) {
            const args = { epoch: this.boot.session.epoch, kind: this.listKind, podcastId: this.podcastID, mode: 'cancel' };
            this.listCancellation = this.listCancellation.then(() => call('library', args)).then(() => { }).catch(() => { });
        }
        this.loading = false;
    }
    async navigate(name) {
        if (!this.boot)
            return;
        this.stopList();
        this.route = name;
        this.routeGeneration++;
        this.list = null;
        this.listKind = '';
        this.podcastID = '';
        this.filter = '';
        this.displayPage = 0;
        document.querySelectorAll('[data-route]').forEach(b => { b.classList.toggle('active', b.dataset.route === name); b.setAttribute('aria-current', b.dataset.route === name ? 'page' : 'false'); });
        if (name === 'settings') {
            showSettings(this);
            return;
        }
        if (name === 'home') {
            await this.drawHome();
            return;
        }
        if (name === 'queue' || name === 'bookmarks') {
            this.drawLocal(name);
            return;
        }
        if (name === 'subscriptions' || name === 'favorites') {
            if (this.boot.session.state !== 'connected') {
                this.page.replaceChildren(this.heading(titles[name], '与本地书签分开显示的云端个人库'), empty('连接账号后查看', '这里显示你在小宇宙里的真实订阅或收藏；访客模式不会生成个人库。', button('连接小宇宙账号', () => showAccount(this), 'button primary')));
                return;
            }
            this.listKind = name;
            this.drawLibrary();
            await this.loadLibrary('cached');
        }
    }
    heading(title, sub, actions) {
        const h = el('div', 'page-heading');
        const text = el('div');
        text.append(el('div', 'eyebrow', 'YOUR LISTENING SPACE'), el('h1', '', title), el('p', 'muted', sub));
        h.append(text);
        if (actions)
            h.append(actions);
        return h;
    }
    async drawHome() {
        const generation = this.routeGeneration;
        await this.player.persist();
        await this.reload();
        if (generation !== this.routeGeneration)
            return;
        const actions = button('打开分享链接', () => showLink(this), 'button');
        this.page.replaceChildren(this.heading('让好声音，接着发生。', '不必从头找起，在桌面继续你的收听。', actions));
        const hero = el('div', 'welcome-panel');
        hero.append(el('div', 'eyebrow', 'LISTEN AT YOUR OWN PACE'), el('h2', '', '留一点时间，给认真听。'), el('p', '', '收藏与订阅来自小宇宙；队列和收听进度留在这台电脑。'));
        const actions2 = el('div', 'actions');
        actions2.append(button(this.boot.session.identity ? '查看收藏单集' : '连接小宇宙账号', () => this.boot.session.identity ? this.navigate('favorites') : showAccount(this), 'button primary'), button('粘贴公开链接', () => showLink(this), 'button transparent'));
        hero.append(actions2, el('span', 'hero-orbit'));
        this.page.append(hero);
        const row = el('div', 'section-heading');
        row.append(el('h2', '', '最近在听'), el('span', 'muted', `${this.boot.history.length} 条本机记录`));
        this.page.append(row);
        if (!this.boot.history.length) {
            this.page.append(empty('还没有本机收听记录', '打开公开单集链接，或连接账号后从收藏单集开始播放。'));
            return;
        }
        const list = el('div', 'episode-list');
        for (const p of this.boot.history.slice(0, 12)) {
            const card = this.itemCard(p.item);
            card.append(el('div', 'history-progress', p.ended ? '本机已听完' : `上次听到 ${formatTime(p.position)} / ${formatTime(p.duration)}`));
            list.append(card);
        }
        this.page.append(list);
    }
    drawLocal(kind) {
        const items = kind === 'queue' ? this.boot.queue : this.boot.bookmarks;
        const actions = el('div', 'actions');
        if (kind === 'queue' && items.length)
            actions.append(button('清空待播', () => {
                if (confirm('清空稍后听队列？当前播放不会停止。'))
                    void this.queue('clear').catch(e => this.notice(e));
            }));
        this.page.replaceChildren(this.heading(titles[kind], kind === 'queue' ? '按顺序播放 · 清空队列不打断当前单集' : '仅此设备 · 不会新增或删除小宇宙云端收藏', actions));
        if (!items.length) {
            this.page.append(empty(kind === 'queue' ? '队列还是空的' : '还没有本地书签', '在单集右侧选择“稍后听”或“本地书签”，把内容留在这里。'));
            return;
        }
        const list = el('div', 'episode-list');
        for (const [i, it] of items.slice(0, 200).entries()) {
            const card = this.itemCard(it, kind);
            if (kind === 'queue')
                card.prepend(el('span', 'sequence', String(i + 1).padStart(2, '0')));
            list.append(card);
        }
        this.page.append(list);
        if (items.length > 200)
            this.page.append(el('p', 'muted', `此视图先显示前 200 条，共 ${items.length} 条。移除前面的内容后可继续查看。`));
    }
    itemCard(it, local) {
        const card = el('article', `episode-card${it.kind === 'podcast' ? ' podcast-card' : ''}`);
        card.dataset.id = it.id;
        const art = cover(it, true);
        card.append(art);
        const content = el('div', 'episode-content');
        const over = el('div', 'episode-meta', it.kind === 'podcast' ? '节目 · 我的订阅' : (it.podcastTitle ?? '播客单集'));
        content.append(over, button(it.title, () => this.details(it), 'episode-title'), el('p', 'episode-description', it.description ?? ''));
        const meta = [formatDate(it.published), it.duration ? `${Math.round(it.duration / 60)} 分钟` : '', it.restricted ? '受限内容' : ''].filter(Boolean).join('  ·  ');
        content.append(el('small', 'muted', meta));
        card.append(content);
        const actions = el('div', 'card-actions');
        if (it.kind === 'episode')
            actions.append(button('▶ 播放', () => this.player.play(it), 'button small primary-soft'));
        const more = document.createElement('details');
        more.className = 'more-menu';
        const summary = el('summary', '', '···');
        summary.setAttribute('aria-label', `${it.title} 的更多操作`);
        more.append(summary);
        const menu = el('div', 'menu-panel');
        const act = (text, fn) => menu.append(button(text, () => { more.open = false; void fn().catch(e => this.notice(e)); }, 'menu-item'));
        if (it.kind === 'episode') {
            act('加入稍后听', () => this.queue('append', it));
            act('下一集播放', () => this.queue('next', it));
        }
        if (local === 'queue')
            act('从队列移除', () => this.queue('remove', it));
        act(local === 'bookmarks' ? '移除本地书签' : '保存本地书签', () => this.bookmark(local === 'bookmarks' ? 'remove' : 'append', it));
        act('打开官方原页', () => this.external(it));
        more.append(menu);
        actions.append(more);
        card.append(actions);
        return card;
    }
    drawLibrary() {
        const name = this.route === 'detail' ? '节目单集' : titles[this.route] ?? '播客库';
        const tools = el('div', 'actions');
        tools.append(button(this.loading ? '加载中…' : '刷新', () => this.loadLibrary('refresh')));
        if (this.allLoading)
            tools.append(button('取消加载全部', () => { this.stopList(); this.drawLibrary(); }));
        else if (this.list?.cursor)
            tools.append(button('加载全部', () => this.loadAll(), 'button primary-soft'));
        this.page.replaceChildren(this.heading(name, '云端只读 · 筛选仅作用于已经加载的内容', tools));
        const bar = el('div', 'list-toolbar');
        const search = el('input', 'search-input');
        search.placeholder = '在已加载内容中筛选';
        search.setAttribute('aria-label', '筛选已加载内容');
        search.value = this.filter;
        search.addEventListener('input', () => { this.filter = search.value; this.displayPage = 0; this.drawLibraryItems(); });
        bar.append(search, el('span', 'muted', this.loading ? '正在加载…' : this.list ? libraryStatus(this.list) : '准备加载'));
        this.page.append(bar);
        if (this.list?.updatedAt)
            this.page.append(el('p', 'sync-time', `最近获取：${new Date(this.list.updatedAt).toLocaleString('zh-CN')}；加载期间手机侧变更可能影响结果。`));
        if (this.list?.error)
            this.page.append(el('div', 'inline-error', this.list.error.message));
        this.page.append(el('div', 'episode-list'));
        this.drawLibraryItems();
    }
    drawLibraryItems() {
        const target = this.page.querySelector('.episode-list');
        if (!target)
            return;
        target.replaceChildren();
        const items = (this.list?.items ?? []).filter(it => (it.title + ' ' + (it.podcastTitle ?? '')).toLocaleLowerCase().includes(this.filter.toLocaleLowerCase()));
        const offset = this.displayPage * 60;
        for (const it of items.slice(offset, offset + 60))
            target.append(this.itemCard(it));
        if (!items.length && !this.loading)
            target.append(empty(this.filter ? '没有匹配的已加载内容' : this.list?.complete ? '列表为空' : '尚未取得内容', this.list?.complete ? '平台本轮返回了空列表。' : '未登录、加载失败或分页不明，都不会被当成空收藏。'));
        if (items.length > 60) {
            const paging = el('div', 'pagination');
            const back = button('上一屏', () => { this.displayPage--; this.drawLibraryItems(); });
            back.disabled = this.displayPage === 0;
            const next = button('下一屏', () => { this.displayPage++; this.drawLibraryItems(); });
            next.disabled = offset + 60 >= items.length;
            paging.append(back, el('span', 'muted', `${offset + 1}–${Math.min(offset + 60, items.length)} / ${items.length} 条已加载内容`), next);
            target.append(paging);
        }
        if (this.list?.cursor && !this.loading)
            target.append(button('加载下一页', () => this.loadLibrary('more'), 'button load-more'));
        if (this.list?.status === 'unknown_end')
            target.append(el('p', 'inline-warning', '平台未提供可验证的结束标志或下一页游标，此处不宣称已取得完整列表。'));
    }
    async loadLibrary(mode) {
        if (this.loading && mode !== 'cached')
            return false;
        const generation = this.routeGeneration, epoch = this.boot.session.epoch, op = ++this.libraryGeneration;
        this.loading = mode !== 'cached';
        if (mode !== 'cached')
            this.drawLibrary();
        try {
            await this.listCancellation;
            if (generation !== this.routeGeneration || op !== this.libraryGeneration)
                return false;
            const view = await call('library', { epoch, kind: this.listKind, podcastId: this.podcastID, mode });
            if (generation !== this.routeGeneration || epoch !== this.boot.session.epoch || op !== this.libraryGeneration)
                return false;
            this.list = view;
            this.loading = false;
            this.drawLibrary();
            if (mode === 'cached' && (view.status === 'idle' || view.status === 'stale'))
                return await this.loadLibrary('refresh');
            if (view.error?.code === 'UNAUTHORIZED')
                await this.reload();
            return true;
        }
        catch (e) {
            if (generation === this.routeGeneration && op === this.libraryGeneration) {
                this.loading = false;
                this.drawLibrary();
                this.notice(e);
            }
            return false;
        }
    }
    async loadAll() {
        this.allLoading = true;
        const gen = this.routeGeneration;
        while (this.allLoading && gen === this.routeGeneration && this.list?.cursor && this.list.status !== 'error') {
            if (!(await this.loadLibrary('more')))
                break;
            if (!this.allLoading || gen !== this.routeGeneration)
                break;
            await new Promise(r => setTimeout(r, 200));
        }
        this.allLoading = false;
        if (gen === this.routeGeneration)
            this.drawLibrary();
    }
    async details(it) {
        this.stopList();
        this.route = 'detail';
        const generation = ++this.routeGeneration;
        const epoch = this.boot.session.epoch;
        this.page.replaceChildren(this.heading('内容详情', '正在读取…'));
        try {
            const detail = await call('detail', { epoch, kind: it.kind, id: it.id });
            if (generation !== this.routeGeneration || epoch !== this.boot.session.epoch)
                return;
            this.showDetail(detail);
        }
        catch (e) {
            if (generation === this.routeGeneration)
                this.page.replaceChildren(empty('内容暂时无法打开', describeError(e), button('打开官方原页', () => this.external(it))));
        }
    }
    showDetail(it) {
        this.stopList();
        this.route = 'detail';
        this.routeGeneration++;
        this.listKind = '';
        this.podcastID = '';
        const hero = el('div', 'detail-hero');
        hero.append(cover(it));
        const text = el('div', 'detail-copy');
        text.append(el('div', 'eyebrow', it.kind === 'podcast' ? 'PODCAST' : 'EPISODE'), el('h1', '', it.title), el('p', 'muted', it.podcastTitle ?? ''));
        const actions = el('div', 'actions');
        if (it.kind === 'episode')
            actions.append(button('▶  播放单集', () => this.player.play(it), 'button primary'), button('＋ 稍后听', () => this.queue('append', it).catch(e => this.notice(e))));
        else
            actions.append(button('查看单集列表', () => { this.listKind = 'episodes'; this.podcastID = it.id; this.list = null; this.drawLibrary(); void this.loadLibrary('cached'); }, 'button primary'));
        actions.append(button('保存本地书签', () => this.bookmark('append', it).catch(e => this.notice(e))), button('官方原页 ↗', () => this.external(it), 'text-button'));
        text.append(actions);
        hero.append(text);
        this.page.replaceChildren(hero);
        if (it.restricted)
            this.page.append(el('p', 'inline-warning', it.restriction || '此内容受限，首版不支持付费或私有内容。'));
        this.page.append(el('h2', 'section-heading', '节目说明'), renderNotes(it.showNotes || it.description || '暂无说明。', seconds => {
            if (this.player.item?.id !== it.id) {
                this.notice('先播放当前单集，再点击时间点跳转。');
                return;
            }
            if (seconds > this.player.duration || !this.player.seekable) {
                this.notice('当前时间点不可定位。');
                return;
            }
            this.player.seek(seconds);
        }, url => { void openExternal(url).catch(e => this.notice(e)); }));
    }
    drawPlayer() {
        const p = this.player;
        if (!p)
            return;
        const names = { idle: '进度仅保存在本机', resolving: '正在解析音频…', buffering: '缓冲中…', playing: '正在播放', paused: '已暂停', ended: '本机已听完', error: '播放失败' };
        const play = document.querySelector('#toggle-play');
        const active = ['resolving', 'buffering', 'playing'].includes(p.state);
        play.disabled = !p.item;
        play.textContent = active ? 'Ⅱ' : '▶';
        play.setAttribute('aria-label', active ? '暂停' : '播放');
        document.querySelector('#player-title').textContent = p.item?.title ?? '选择一集，开始收听';
        document.querySelector('#player-subtitle').textContent = (p.item?.podcastTitle ? p.item.podcastTitle + ' · ' : '') + names[p.state];
        const old = document.querySelector('#player-art');
        if (old.dataset.id !== p.item?.id) {
            const art = p.item ? cover(p.item, true) : el('div', 'cover small-cover color-0', '♪');
            art.id = 'player-art';
            art.dataset.id = p.item?.id ?? '';
            old.replaceWith(art);
        }
        document.querySelector('#position').textContent = formatTime(p.position);
        document.querySelector('#duration').textContent = formatTime(p.duration);
        const seek = document.querySelector('#seek');
        seek.max = String(p.duration || 1);
        if (!seek.matches(':active'))
            seek.value = String(p.position || 0);
        seek.disabled = !p.seekable;
        document.querySelector('#queue-count').textContent = String(this.boot?.queue.length ?? 0);
        const message = document.querySelector('#player-message');
        message.textContent = p.error;
        message.hidden = !p.error;
        if ('mediaSession' in navigator) {
            try {
                navigator.mediaSession.playbackState = p.state === 'playing' ? 'playing' : 'paused';
                if (p.item?.id !== this.lastMediaID) {
                    this.lastMediaID = p.item?.id ?? '';
                    navigator.mediaSession.metadata = p.item ? new MediaMetadata({ title: p.item.title, artist: p.item.podcastTitle ?? '', album: 'Starling · 星听' }) : null;
                }
            }
            catch { /* optional host support */ }
        }
    }
}
