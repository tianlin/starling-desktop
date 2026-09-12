import type { Call, Session, SearchKind, SearchPage, CreatorPage, Creator, Item, SubscriptionState } from './types.js';
import { describeError, APIError } from './api.js';
import { el, button, cover, empty } from './dom.js';
const initial = () => ({ query: '', resultQuery: '', kind: 'podcast' as SearchKind, page: null as SearchPage | null, loading: false, error: '', history: [] as string[], historyError: '', historyLoading: false, suggestions: [] as string[], suggestionsError: '', suggestionsLoading: false, scrollTop: 0, creator: null as CreatorPage | null, creatorLoading: false, creatorError: '', creatorScroll: 0, showCreator: false });
const unique = <T extends {
    id: string;
}>(items: T[]) => [...new Map(items.map(it => [it.id, it])).values()];
export class DiscoveryController {
    state = initial();
    onChange = () => { };
    onLogin = () => { };
    onLink = async (_query: string, _current: () => boolean) => { };
    onUnauthorized = () => { };
    onSubscriptions = (_states: Record<string, SubscriptionState>) => { };
    private session?: Session;
    private active = false;
    private generation = 0;
    private token = 0;
    private intentGeneration = 0;
    private creatorToken = 0;
    private historyToken = 0;
    private suggestionToken = 0;
    private pending = '';
    private pendingIdentity = '';
    private pendingReady = false;
    private cursors = new Set<string>();
    private creatorCursors = new Set<string>();
    constructor(private api: Call) { }
    setSession(session: Session, seed = 0) {
        const old = this.session;
        const changed = !!old && (old.epoch !== session.epoch || old.identity?.id !== session.identity?.id);
        if (changed) {
            const keep = this.pending && (!this.pendingIdentity || this.pendingIdentity === session.identity?.id);
            this.leave();
            this.state = initial();
            if (old.epoch !== session.epoch)
                this.generation = 0;
            if (!keep)
                this.pending = '';
            else
                this.pendingReady = session.state === 'connected';
        }
        if (old?.state !== session.state && session.state !== 'connected')
            this.leave();
        this.session = session;
        this.generation = Math.max(this.generation, seed);
        if (this.pending && session.state === 'connected' && (!this.pendingIdentity || this.pendingIdentity === session.identity?.id))
            this.pendingReady = true;
    }
    takePending() {
        if (!this.pendingReady)
            return '';
        const q = this.pending;
        this.pending = '';
        this.pendingReady = false;
        return q;
    }
    hasPending() { return this.pendingReady; }
    async enter() {
        this.active = true;
        if (this.session?.state !== 'connected')
            return;
        await Promise.all([this.history('read'), this.state.suggestions.length ? Promise.resolve() : this.suggestions()]);
    }
    private invalidateIntent() {
        ++this.intentGeneration;
        ++this.token;
        ++this.creatorToken;
        this.state.loading = false;
        this.state.creatorLoading = false;
    }
    leave() { this.active = false; this.invalidateIntent(); ++this.historyToken; ++this.suggestionToken; this.state.loading = false; this.state.creatorLoading = false; this.state.historyLoading = false; this.state.suggestionsLoading = false; }
    async history(mode: 'read' | 'record' | 'remove' | 'clear', query = '') {
        if (this.session?.state !== 'connected')
            return;
        const epoch = this.session.epoch, token = ++this.historyToken;
        this.state.historyLoading = true;
        this.state.historyError = '';
        this.onChange();
        try {
            const r = await this.api<{
                terms: string[];
            }>('discovery.history', { epoch, mode, query });
            if (epoch === this.session?.epoch && token === this.historyToken)
                this.state.history = r.terms.slice(0, 10);
        }
        catch (e) {
            if (epoch === this.session?.epoch && token === this.historyToken)
                this.state.historyError = describeError(e);
        }
        finally {
            if (epoch === this.session?.epoch && token === this.historyToken) {
                this.state.historyLoading = false;
                this.onChange();
            }
        }
    }
    async suggestions() {
        if (this.session?.state !== 'connected')
            return;
        const epoch = this.session.epoch, token = ++this.suggestionToken;
        this.state.suggestionsLoading = true;
        this.state.suggestionsError = '';
        this.onChange();
        try {
            const r = await this.api<{
                terms: string[];
            }>('discovery.suggestions', { epoch });
            if (epoch === this.session?.epoch && token === this.suggestionToken)
                this.state.suggestions = r.terms;
        }
        catch (e) {
            if (epoch === this.session?.epoch && token === this.suggestionToken)
                this.state.suggestionsError = describeError(e);
        }
        finally {
            if (epoch === this.session?.epoch && token === this.suggestionToken) {
                this.state.suggestionsLoading = false;
                this.onChange();
            }
        }
    }
    async submit(query = this.state.query) {
        this.invalidateIntent();
        const q = query.trim();
        this.state.query = q;
        if (!q)
            return;
        if (Array.from(q).length > 200 || /[\x00-\x1f\x7f]/.test(q)) {
            this.state.error = '搜索词最多 200 字，请勿包含控制字符。';
            this.onChange();
            return;
        }
        if (this.session?.state !== 'connected') {
            this.pending = q;
            this.pendingIdentity = this.session?.identity?.id ?? '';
            this.onLogin();
            return;
        }
        void this.history('record', q);
        if (/https?:\/\//i.test(q)) {
            const intent = this.intentGeneration, epoch = this.session.epoch;
            await this.onLink(q, () => this.active && intent === this.intentGeneration && epoch === this.session?.epoch);
            return;
        }
        this.state.showCreator = false;
        await this.search();
    }
    async selectKind(kind: SearchKind) {
        if (kind === this.state.kind)
            return;
        this.invalidateIntent();
        this.state.kind = kind;
        this.state.showCreator = false;
        this.state.page = null;
        this.cursors.clear();
        ++this.token;
        this.state.loading = false;
        this.onChange();
        if (this.state.query && this.session?.state === 'connected')
            await this.search();
    }
    async search(more = false) {
        const query = more ? this.state.resultQuery : this.state.query;
        if (!this.active || this.session?.state !== 'connected' || !query || (more && (!this.state.page?.cursor || this.state.loading)))
            return;
        this.invalidateIntent();
        const token = ++this.token, epoch = this.session.epoch, kind = this.state.kind, cursor = more ? this.state.page!.cursor : '';
        const generation = ++this.generation;
        this.state.loading = true;
        this.state.error = '';
        if (!more)
            this.cursors.clear();
        this.onChange();
        const current = () => this.active && token === this.token && epoch === this.session?.epoch;
        try {
            const result = await this.api<SearchPage>('discovery.search', { epoch, generation, query, kind, cursor });
            if (!current())
                return;
            const loop = !!result.cursor && (result.cursor === cursor || this.cursors.has(result.cursor));
            if (cursor)
                this.cursors.add(cursor);
            this.state.resultQuery = query;
            this.state.page = { ...result, items: unique([...(more ? this.state.page?.items ?? [] : []), ...(result.items ?? [])]), users: unique([...(more ? this.state.page?.users ?? [] : []), ...(result.users ?? [])]), cursor: loop ? '' : result.cursor, complete: loop ? false : result.complete };
            this.onSubscriptions(result.subscriptions);
            if (loop)
                this.state.error = '分页游标重复，已停止继续加载；无法确认结果是否完整。';
        }
        catch (e) {
            if (current()) {
                this.state.error = describeError(e);
                if (e instanceof APIError && e.code === 'UNAUTHORIZED')
                    this.onUnauthorized();
            }
        }
        finally {
            if (current()) {
                this.state.loading = false;
                this.onChange();
            }
        }
    }
    more() { return this.search(true); }
    async openCreator(creator: Creator) { this.invalidateIntent(); this.state.creator = { creator, items: [], subscriptions: {}, cursor: '', complete: false }; this.state.showCreator = true; this.state.creatorScroll = 0; this.creatorCursors.clear(); await this.loadCreator(); }
    async loadCreator(more = false) {
        if (!this.active || this.session?.state !== 'connected' || !this.state.creator || (more && (!this.state.creator.cursor || this.state.creatorLoading)))
            return;
        this.invalidateIntent();
        const token = ++this.creatorToken, epoch = this.session.epoch, id = this.state.creator.creator.id, cursor = more ? this.state.creator.cursor : '';
        this.state.creatorLoading = true;
        this.state.creatorError = '';
        this.onChange();
        const current = () => this.active && token === this.creatorToken && epoch === this.session?.epoch;
        try {
            const result = await this.api<CreatorPage>('discovery.creator', { epoch, id, cursor });
            if (!current())
                return;
            const loop = !!result.cursor && (result.cursor === cursor || this.creatorCursors.has(result.cursor));
            if (cursor)
                this.creatorCursors.add(cursor);
            this.state.creator = { ...result, items: unique([...(more ? this.state.creator.items : []), ...result.items]), cursor: loop ? '' : result.cursor, complete: loop ? false : result.complete };
            this.onSubscriptions(result.subscriptions);
            if (loop)
                this.state.creatorError = '分页游标重复，无法确认节目是否完整。';
        }
        catch (e) {
            if (current())
                this.state.creatorError = describeError(e);
        }
        finally {
            if (current()) {
                this.state.creatorLoading = false;
                this.onChange();
            }
        }
    }
    backToSearch() { this.invalidateIntent(); this.state.creatorLoading = false; this.state.showCreator = false; this.onChange(); }
}
export function renderDiscovery(c: DiscoveryController, card: (item: Item) => HTMLElement): HTMLElement {
    const s = c.state, root = el('div', 'discovery-view');
    if (s.showCreator && s.creator) {
        root.append(button('← 返回探索结果', () => c.backToSearch(), 'button discovery-back'));
        const creator = s.creator.creator, hero = el('div', 'creator-heading');
        hero.append(creatorArt(creator), el('h2', '', creator.nickname), el('p', 'muted', creator.bio ?? ''));
        root.append(hero, el('h2', 'section-heading', '创作节目'));
        if (s.creatorError)
            root.append(el('p', 'inline-error', s.creatorError), button('重试节目列表', () => c.loadCreator()));
        const list = el('div', 'episode-list');
        for (const it of s.creator.items)
            list.append(card(it));
        root.append(list);
        if (s.creatorLoading)
            root.append(el('p', 'muted', '正在加载创作节目…'));
        else if (!s.creator.items.length)
            root.append(empty(s.creator.complete ? '暂无创作节目' : '尚未取得创作节目', s.creator.complete ? '这位用户目前没有创作节目。' : '请重试加载。'));
        if (s.creator.cursor) {
            const more = button('加载更多节目', () => c.loadCreator(true));
            more.disabled = s.creatorLoading;
            root.append(more);
        }
        else if (!s.creator.complete && !s.creatorLoading)
            root.append(el('p', 'inline-warning', '平台未确认列表结束，无法确认是否已加载全部节目。'));
        return root;
    }
    const form = el('form', 'discovery-search'), field = el('input', 'search-input');
    field.id = 'discovery-query';
    field.type = 'search';
    field.placeholder = '搜索节目、单集或主播';
    field.setAttribute('aria-label', field.placeholder);
    field.value = s.query;
    let composing = false;
    field.addEventListener('compositionstart', () => { composing = true; });
    field.addEventListener('compositionend', () => { composing = false; s.query = field.value; });
    field.addEventListener('input', () => { s.query = field.value; });
    field.addEventListener('keydown', e => {
        if (e.key === 'Enter' && (e.isComposing || composing || e.keyCode === 229))
            e.preventDefault();
    });
    const submit = el('button', 'button primary', '搜索');
    submit.type = 'submit';
    form.append(field, submit);
    form.addEventListener('submit', e => {
        e.preventDefault();
        if (!composing)
            void c.submit(field.value);
    });
    root.append(form);
    const suggestions = el('section', 'discovery-suggestions');
    suggestions.append(el('h2', '', '你可能想搜'));
    if (s.suggestionsLoading)
        suggestions.append(el('span', 'muted', '加载建议中…'));
    for (const q of s.suggestions)
        suggestions.append(button(q, () => c.submit(q), 'button small'));
    if (s.suggestionsError)
        suggestions.append(el('span', 'inline-error', s.suggestionsError), button('重试建议', () => c.suggestions(), 'text-button'));
    const history = el('section', 'discovery-history');
    history.append(el('h2', '', '最近搜索'));
    if (s.history.length)
        history.append(button('清空历史', () => c.history('clear'), 'text-button'));
    for (const q of s.history) {
        const chip = el('span', 'history-chip');
        chip.append(button(q, () => c.submit(q), 'text-button'));
        const remove = button('×', () => c.history('remove', q), 'icon-button');
        remove.setAttribute('aria-label', `删除搜索历史 ${q}`);
        chip.append(remove);
        history.append(chip);
    }
    if (s.historyError)
        history.append(el('span', 'inline-error', s.historyError), button('重试历史', () => c.history('read'), 'text-button'));
    root.append(history, suggestions);
    const tabs = el('div', 'detail-tabs');
    tabs.setAttribute('role', 'tablist');
    tabs.setAttribute('aria-label', '搜索类型');
    const kinds: SearchKind[] = ['podcast', 'episode', 'user'];
    for (const [i, kind] of kinds.entries()) {
        const tab = button(['节目', '单集', '主播 / 用户'][i]!, () => c.selectKind(kind), 'detail-tab');
        tab.id = `discovery-tab-${kind}`;
        tab.setAttribute('role', 'tab');
        tab.setAttribute('aria-selected', String(s.kind === kind));
        tab.tabIndex = s.kind === kind ? 0 : -1;
        tab.addEventListener('keydown', e => {
            if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key))
                return;
            e.preventDefault();
            const index = e.key === 'Home' ? 0 : e.key === 'End' ? 2 : (i + (e.key === 'ArrowRight' ? 1 : 2)) % 3;
            void c.selectKind(kinds[index]!);
            document.getElementById(`discovery-tab-${kinds[index]}`)?.focus({ preventScroll: true });
        });
        tabs.append(tab);
    }
    root.append(tabs);
    if (s.page && s.resultQuery && s.resultQuery !== s.query) root.append(el('p', 'muted', `当前显示“${s.resultQuery}”的搜索结果`));
    if (s.loading)
        root.append(el('p', 'muted', '正在搜索…'));
    if (s.error)
        root.append(el('p', 'inline-error', s.error), button('重试搜索', () => c.search()));
    const list = el('div', 'episode-list');
    if (s.kind === 'user') {
        for (const creator of s.page?.users ?? []) {
            const row = el('article', 'episode-card creator-card');
            row.append(creatorArt(creator));
            const content = el('div', 'episode-content');
            content.append(button(creator.nickname, () => c.openCreator(creator), 'episode-title'), el('p', 'episode-description', creator.bio ?? ''));
            row.append(content);
            list.append(row);
        }
    }
    else
        for (const it of s.page?.items ?? [])
            list.append(card(it));
    root.append(list);
    if (!s.page && !s.loading && !s.error)
        root.append(empty('下一档喜欢的播客，从这里开始', '输入关键词，发现节目、单集和创作者。'));
    else if (s.page && !s.loading && !s.page.items.length && !s.page.users.length && !s.error)
        root.append(empty(s.page.complete ? '没有找到相关内容' : '暂未取得结果', s.page.complete ? '换个关键词试试。' : '平台尚未确认结果是否完整。'));
    if (s.page?.cursor) {
        const more = button('加载更多结果', () => c.more(), 'button load-more');
        more.disabled = s.loading;
        root.append(more);
    }
    else if (s.page?.complete && !s.loading && !s.error && (s.page.items.length || s.page.users.length))
        root.append(el('p', 'muted', '已显示本次搜索的全部结果'));
    return root;
}
function creatorArt(c: Creator) { return cover({ id: c.id, title: c.nickname, image: c.avatar, kind: 'podcast', sourceUrl: '', restricted: false }, true); }
