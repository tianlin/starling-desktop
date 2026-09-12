import type { Call, Item, Library, Session } from './types.js';
import { describeError, APIError } from './api.js';
import { el, button, empty } from './dom.js';
export function sortUpdates(items: Item[]): Item[] {
    const time = (it: Item) => { const n = Date.parse(it.published ?? ''); return Number.isFinite(n) ? n : -Infinity; };
    return [...items].sort((a, b) => (time(b) - time(a) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)));
}
export function dateGroup(published?: string, now = new Date()): string {
    const d = new Date(published ?? '');
    if (!Number.isFinite(d.getTime())) return '日期未知';
    const key = (v: Date) => `${v.getFullYear()}-${String(v.getMonth()+1).padStart(2,'0')}-${String(v.getDate()).padStart(2,'0')}`;
    const yesterday = new Date(now); yesterday.setDate(now.getDate()-1);
    return key(d) === key(now) ? '今天' : key(d) === key(yesterday) ? '昨天' : key(d);
}
export class UpdatesController {
    state = { view: null as Library | null, loading: false, error: '', lastSuccess: 0, displayPage: 0, scrollTop: 0 };
    onChange = () => {};
    onUnauthorized = () => {};
    private session: Session | undefined;
    private active = false;
    private generation = 0;
    private cacheRead = false;
    private refreshed = false;
    private cancellation: Promise<void> = Promise.resolve();
    constructor(private api: Call, private now = () => Date.now()) {}
    invalidate() {
        this.leave(); this.cacheRead = false; this.refreshed = false;
        this.state = { view: null, loading: false, error: '', lastSuccess: 0, displayPage: 0, scrollTop: 0 };
    }
    setSession(session: Session) {
        if (this.session && (session.epoch !== this.session.epoch || session.identity?.id !== this.session.identity?.id || session.state !== this.session.state)) {
            this.leave(); this.cacheRead = false; this.refreshed = false;
            this.state = { view: null, loading: false, error: '', lastSuccess: 0, displayPage: 0, scrollTop: 0 };
        }
        this.session = session;
    }
    async enter() {
        this.active = true;
        if (this.session?.state !== 'connected') return;
        const generation = this.generation;
        if (!this.cacheRead) { await this.load('cached'); if (!this.active || generation !== this.generation) return; }
        if (!this.refreshed || this.now() - this.state.lastSuccess >= 600000) await this.load('refresh');
    }
    leave() {
        this.active = false; ++this.generation;
        if (this.state.loading && this.session) {
            const epoch = this.session.epoch;
            this.cancellation = this.cancellation.then(() => this.api('library', { epoch, kind: 'updates', podcastId: '', mode: 'cancel' })).then(() => {}).catch(() => {});
            // An interrupted refresh must be retried even if an earlier refresh was recent.
            this.refreshed = false;
        }
        this.state.loading = false;
    }
    cancel() { this.leave(); this.active = true; this.onChange(); }
    async load(mode: 'cached' | 'refresh' | 'more') {
        if (!this.active || this.session?.state !== 'connected' || this.state.loading || (mode === 'more' && !this.state.view?.cursor)) return;
        const generation = this.generation, epoch = this.session.epoch;
        this.state.loading = true; this.state.error = ''; this.onChange();
        const current = () => this.active && generation === this.generation && epoch === this.session?.epoch;
        try {
            await this.cancellation;
            if (!current()) return;
            const view = await this.api<Library>('library', {epoch,kind:'updates',podcastId:'',mode});
            if (!current() || view.epoch !== epoch) return;
            if (mode === 'cached') {
                this.cacheRead = true;
                this.state.view = {...view, cursor: '', items: sortUpdates(view.items)};
            } else if (view.error || view.status === 'error') {
                this.state.error = view.error?.message ?? '更新获取失败，请重试。';
                if (!this.state.view?.items.length && view.items.length) this.state.view = {...view, items: sortUpdates(view.items)};
                if (view.error?.code === 'UNAUTHORIZED') this.onUnauthorized();
            } else {
                this.state.view = {...view, items: sortUpdates(view.items)};
                if (mode === 'refresh') { this.state.lastSuccess = this.now(); this.refreshed = true; this.state.displayPage = 0; }
            }
        } catch (e) { if (current()) { this.state.error = describeError(e); if (e instanceof APIError && e.code === 'UNAUTHORIZED') this.onUnauthorized(); if (mode === 'cached') this.cacheRead = true; } }
        finally { if (current()) { this.state.loading = false; this.onChange(); } }
    }
}
export function renderUpdates(c: UpdatesController, card: (item: Item) => HTMLElement): HTMLElement {
    const root = el('div', 'updates-view'), state = c.state, view = state.view;
    const tools = el('div', 'actions');
    const refresh = button(state.loading ? '加载中…' : '刷新', () => c.load('refresh')); refresh.disabled = state.loading; tools.append(refresh);
    if (state.loading) tools.append(button('取消加载', () => c.cancel()));
    root.append(tools);
    const status = el('p', 'sync-time', state.lastSuccess ? `最近成功刷新：${new Date(state.lastSuccess).toLocaleString('zh-CN')}` : view?.updatedAt ? `缓存快照：${new Date(view.updatedAt).toLocaleString('zh-CN')} · 等待刷新` : '订阅节目的最新单集');
    status.setAttribute('role','status'); root.append(status);
    if (state.error) { const error = el('div','inline-error',state.error); error.setAttribute('role','alert');root.append(error); }
    const items = view?.items ?? [], offset = state.displayPage * 60;
    const list = el('div','episode-list');let group = '';
    for (const it of items.slice(offset, offset+60)) { const next = dateGroup(it.published); if (next !== group) { list.append(el('h2','updates-date',next));group=next; } list.append(card(it)); }
    if (!items.length && !state.loading) list.append(empty(view?.complete && !state.error ? '暂无订阅更新' : '尚未取得更新',view?.complete && !state.error ? '已返回完整的空列表。' : '请刷新重试；加载失败或分页不明不表示没有更新。'));
    root.append(list);
    if (items.length > 60) {
        const paging = el('div','pagination');
        const back = button('上一屏',()=>{state.displayPage--;c.onChange();});back.disabled=state.displayPage===0;
        const next = button('下一屏',()=>{state.displayPage++;c.onChange();});next.disabled=offset+60>=items.length;
        paging.append(back,el('span','muted',`${offset+1}–${Math.min(offset+60,items.length)} / ${items.length} 条已加载内容`),next);root.append(paging);
    }
    if (view?.cursor) { const more = button('加载下一页',()=>c.load('more'),'button load-more');more.disabled=state.loading;root.append(more); }
    if (view?.complete && items.length && !state.error) root.append(el('p','muted','本轮更新已加载完成'));
    return root;
}
