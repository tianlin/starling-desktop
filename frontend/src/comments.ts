import type { Call, Comment, CommentCreated, CommentOrder, CommentPage, Session } from './types.js';
import { APIError, describeError } from './api.js';
import { button, el, empty } from './dom.js';

interface PageState extends CommentPage { loaded: boolean; loading: boolean; error: string; cursors: Set<string>; revision: number; stale: boolean; scroll: number }
interface ThreadState extends PageState { confirmed: Map<string, Comment> }
interface ReplyTarget { id: string; primaryCommentId: string; nickname: string; summary: string; valid: boolean; rejected?: boolean }
interface ComposerState { draft: string; uncertain: boolean; sendError: string; sent: boolean; replyTarget?: ReplyTarget }
interface EpisodeState extends PageState, ComposerState {
    order: CommentOrder; pages: Record<CommentOrder, PageState>;
    threads: Map<string, ThreadState>; expanded: Set<string>;
    composers: Map<string, ComposerState>; targetKey: string; sending: boolean;
    confirmed: Map<string, Comment>;
}
const freshPage = (): PageState => ({ items: [], cursor: '', complete: false, loaded: false, loading: false, error: '', cursors: new Set(), revision: 0, stale: false, scroll: 0 });
const freshComposer = (): ComposerState => ({ draft: '', uncertain: false, sendError: '', sent: false, replyTarget: undefined });
const freshThread = (): ThreadState => ({ ...freshPage(), confirmed: new Map() });
function freshEpisode(): EpisodeState {
    const episode = { order: 'hot', pages: { hot: freshPage(), latest: freshPage() }, threads: new Map(), expanded: new Set(), composers: new Map([['', freshComposer()]]), targetKey: '', sending: false, confirmed: new Map() } as EpisodeState;
    // Consumers see the selected page; requests retain the concrete page they started with.
    for (const key of Object.keys(freshPage()) as (keyof PageState)[]) {
        Object.defineProperty(episode, key, {
            get: () => episode.pages[episode.order][key],
            set: value => { Object.assign(episode.pages[episode.order], { [key]: value }); },
        });
    }
    for (const key of Object.keys(freshComposer()) as (keyof ComposerState)[]) {
        Object.defineProperty(episode, key, {
            get: () => episode.composers.get(episode.targetKey)![key],
            set: value => { Object.assign(episode.composers.get(episode.targetKey)!, { [key]: value }); },
        });
    }
    return episode;
}
function isComment(value: unknown): value is Comment {
    if (!value || typeof value !== 'object') return false;
    const item = value as Comment;
    return typeof item.id === 'string' && !!item.id && typeof item.text === 'string' && !!item.text.trim()
        && typeof item.createdAt === 'string' && Number.isFinite(Date.parse(item.createdAt))
        && !!item.author && typeof item.author.id === 'string' && !!item.author.id && typeof item.author.nickname === 'string'
        && (item.replyCount === undefined || (Number.isInteger(item.replyCount) && item.replyCount >= 0));
}

// Memory belongs to the current account and process; no comments are persisted.
export class CommentsController {
    private cache = new Map<string, EpisodeState>();
    private sessionKey = '';
    private epoch = 0;
    private episodeId = '';
    private authorId = '';
    connected = false;
    nickname = '';
    state: EpisodeState = freshEpisode();
    onChange: (() => void) | undefined;
    constructor(private call: Call) {}
    setSession(session: Session) {
        const key = `${session.epoch}:${session.state === 'guest' ? '' : session.identity?.id ?? ''}`;
        const connected = session.state === 'connected' && !!session.identity;
        const changed = connected !== this.connected;
        this.nickname = session.identity?.nickname ?? '';
        this.authorId = session.identity?.id ?? '';
        this.connected = connected;
        if (key === this.sessionKey) {
            if (changed) {
                for (const episode of this.cache.values()) for (const page of [...Object.values(episode.pages), ...episode.threads.values()]) { page.revision++; page.loading = false; }
                this.onChange?.();
            }
            return;
        }
        this.sessionKey = key; this.epoch = session.epoch;
        this.cache.clear(); this.state = freshEpisode();
        if (this.episodeId) this.cache.set(this.episodeId, this.state);
        this.onChange?.();
    }
    open(episodeId: string) {
        this.leave(); this.episodeId = episodeId;
        this.state = this.cache.get(episodeId) ?? freshEpisode();
        this.cache.set(episodeId, this.state);
    }
    leave() {
        this.episodeId = ''; this.onChange = undefined;
    }
    async load() { await this.fetchPage(this.state, this.state.pages[this.state.order], this.episodeId, this.state.order, undefined, this.state.stale); }
    async selectOrder(order: CommentOrder) {
        if (order === this.state.order) return;
        this.state.order = order; this.onChange?.();
        if (!this.state.loaded || this.state.stale) await this.load();
    }
    rememberScroll(scroll: number, order = this.state.order) { this.state.pages[order].scroll = Math.max(0, scroll); }
    selectReply(commentId: string, primaryCommentId?: string): boolean {
        if (this.state.sending) return false;
        let source: Comment | undefined, primary = primaryCommentId ?? '';
        for (const page of Object.values(this.state.pages)) {
            const root = page.items.find(item => item.id === commentId);
            if (root && (!root.primaryCommentId || root.primaryCommentId === root.id)) { source = root; primary = root.id; break; }
        }
        if (!source && primary) source = this.state.threads.get(primary)?.items.find(item => item.id === commentId && item.primaryCommentId === primary);
        let composer = this.state.composers.get(commentId);
        if (!composer) { composer = freshComposer(); this.state.composers.set(commentId, composer); }
        if (!composer.replyTarget?.rejected) composer.replyTarget = { id: commentId, primaryCommentId: primary, nickname: source?.author.nickname || '这位听友', summary: source?.text.slice(0, 160) || '原评论暂不可确认', valid: !!source && !!primary };
        this.state.targetKey = commentId;
        if (!composer.replyTarget?.valid) composer.sendError = '无法确认回复关系，请刷新评论或所属回复后重新选择。';
        this.onChange?.(); return true;
    }
    cancelReply(): boolean {
        if (this.state.sending) return false;
        this.state.targetKey = ''; this.onChange?.(); return true;
    }
    setDraft(text: string) { if (!this.state.sending) { this.state.draft = text; this.state.sent = false; } }
    acknowledgePublished() {
        if (this.state.sending || !this.state.uncertain) return;
        this.state.draft = ''; this.state.uncertain = false; this.state.sendError = '';
        this.onChange?.();
    }
    async submit(explicitRetry = false) {
        const target = this.state, episodeId = this.episodeId, epoch = this.epoch, key = this.sessionKey;
        if (!this.connected || !episodeId || target.sending || (target.uncertain && !explicitRetry)) return;
        const composer = target.composers.get(target.targetKey)!;
        const replyTarget = composer.replyTarget;
        if (replyTarget && !replyTarget.valid) { composer.sendError = '无法确认回复关系，请刷新评论或所属回复后重新选择。'; this.onChange?.(); return; }
        if (!target.draft.trim()) { target.sendError = '请填写评论内容，不能只含空白。'; this.onChange?.(); return; }
        const text = target.draft;
        target.sending = true; target.sendError = ''; target.sent = false; this.onChange?.();
        try {
            const result = await this.call<CommentCreated>('comments.create', { epoch, episodeId, text, requestId: crypto.randomUUID(), ...(replyTarget ? { replyToCommentId: replyTarget.id, primaryCommentId: replyTarget.primaryCommentId } : {}) });
            if (key !== this.sessionKey) return;
            if (!isComment(result?.comment) || result.comment.author.id !== this.authorId) throw new Error('发表响应无法确认。');
            if (replyTarget && (result.comment.primaryCommentId !== replyTarget.primaryCommentId || result.comment.replyTo?.id !== replyTarget.id)) throw new Error('回复关系无法确认。');
            if (!replyTarget && (result.comment.replyTo || (result.comment.primaryCommentId && result.comment.primaryCommentId !== result.comment.id))) throw new Error('发表响应无法确认。');
            composer.draft = ''; composer.uncertain = false; composer.sent = true;
            if (replyTarget) {
                const thread = target.threads.get(replyTarget.primaryCommentId) ?? freshThread();
                target.threads.set(replyTarget.primaryCommentId, thread);
                thread.confirmed.set(result.comment.id, result.comment); thread.stale = true;
                target.expanded.add(replyTarget.primaryCommentId);
                void this.fetchPage(target, thread, episodeId, target.order, replyTarget.primaryCommentId, true);
            } else {
                target.confirmed.set(result.comment.id, result.comment);
                for (const page of Object.values(target.pages)) { page.stale = true; page.revision++; page.loading = false; }
                if (this.state === target && this.episodeId === episodeId) void this.fetchPage(target, target.pages[target.order], episodeId, target.order, undefined, true);
            }
        } catch (e) {
            if (key !== this.sessionKey) return;
            const definite = new Set(['UNAUTHORIZED', 'STALE_SESSION', 'INVALID_ID', 'INVALID_COMMENT', 'INVALID_REQUEST', 'UNSUPPORTED', 'RATE_LIMIT', 'FORBIDDEN', 'REQUEST_REJECTED', 'EXPIRED', 'COMMENT_BUSY', 'COMMENT_SESSION_LIMIT', 'COMMENT_TARGET_INVALID']);
            composer.uncertain = !(e instanceof APIError && definite.has(e.code));
            composer.sendError = describeError(e);
            if (replyTarget && e instanceof APIError && e.code === 'COMMENT_TARGET_INVALID') { replyTarget.valid = false; replyTarget.rejected = true; }
            if (e instanceof APIError && e.code === 'UNAUTHORIZED') composer.sendError += ' 请在账号与连接中重新连接；草稿已保留。';
        } finally {
            if (key === this.sessionKey) { target.sending = false; if (this.state === target) this.onChange?.(); }
        }
    }
    async refresh() {
        if (!this.connected || !this.episodeId) return;
        await this.fetchPage(this.state, this.state.pages[this.state.order], this.episodeId, this.state.order, undefined, true);
    }
    async refreshTarget() {
        const replyTarget = this.state.replyTarget;
        const primary = replyTarget?.primaryCommentId;
        if (!primary || (replyTarget && !replyTarget.valid && replyTarget.id === primary)) { await this.refresh(); return; }
        const thread = this.state.threads.get(primary) ?? freshThread(); this.state.threads.set(primary, thread);
        this.state.expanded.add(primary);
        await this.fetchPage(this.state, thread, this.episodeId, this.state.order, primary, true);
    }
    async loadThread(commentId: string, more = false) {
        let thread = this.state.threads.get(commentId);
        if (!thread) { thread = freshThread(); this.state.threads.set(commentId, thread); }
        this.state.expanded.add(commentId);
        if (thread.loaded && !more && !thread.stale) { this.onChange?.(); return; }
        await this.fetchPage(this.state, thread, this.episodeId, this.state.order, commentId, thread.stale);
    }
    private async fetchPage(episode: EpisodeState, target: PageState, episodeId: string, order: CommentOrder, commentId?: string, refresh = false) {
        if (!this.connected || !episodeId || (!refresh && (target.loading || (target.loaded && (target.complete || !target.cursor))))) return;
        const revision = ++target.revision, epoch = this.epoch, key = this.sessionKey;
        const current = () => key === this.sessionKey && revision === target.revision;
        const notify = () => { if (this.state === episode && this.episodeId === episodeId && (commentId || episode.order === order)) this.onChange?.(); };
        const cursor = refresh ? '' : target.cursor;
        target.loading = true; target.error = ''; notify();
        try {
            const page = await this.call<CommentPage>(commentId ? 'comments.thread' : 'comments.list', { epoch, episodeId, ...(commentId ? { commentId } : { order }), cursor });
            if (!current()) return;
            if (!refresh && page.cursor && (page.cursor === cursor || target.cursors.has(page.cursor))) {
                target.cursor = ''; target.complete = false;
                throw new Error('评论分页游标重复，已停止加载。请刷新评论后重试。');
            }
            if (refresh) {
                target.items = []; target.cursors.clear();
            }
            const ids = new Set(target.items.map(item => item.id));
            for (const item of page.items) if (!ids.has(item.id)) { ids.add(item.id); target.items.push(item); }
            if (!commentId) for (const item of page.items) episode.confirmed.delete(item.id);
            else for (const item of page.items) episode.threads.get(commentId)?.confirmed.delete(item.id);
            for (const item of page.items) {
                const replyTarget = episode.composers.get(item.id)?.replyTarget;
                if (replyTarget && (commentId ? item.primaryCommentId === replyTarget.primaryCommentId : item.id === replyTarget.primaryCommentId)) { replyTarget.valid = true; replyTarget.rejected = false; }
            }
            if (cursor) target.cursors.add(cursor);
            target.cursor = page.complete || target.cursors.has(page.cursor) ? '' : page.cursor;
            target.complete = page.complete; target.loaded = true; target.stale = false;
        } catch (e) {
            if (!current()) return;
            target.error = describeError(e);
        } finally {
            if (current()) { target.loading = false; notify(); }
        }
    }
}

export function renderComments(controller: CommentsController, login: () => unknown): HTMLElement {
    const root = el('section', 'comments-panel');
    root.setAttribute('aria-label', '单集评论');
    if (!controller.connected) {
        root.append(empty('连接账号后查看评论', '评论需要小宇宙账号连接。', button('连接账号', login, 'button primary')));
        return root;
    }
    const state = controller.state;
    const composer = el('form', 'comment-composer');
    composer.append(el('p', 'comment-identity', `以 ${controller.nickname || '当前账号'} 的身份发表评论`));
    if (state.replyTarget) {
        const context = el('div', 'comment-reply-target');
        context.append(el('strong', '', `回复 @${state.replyTarget.nickname}`), el('p', 'comment-target-summary', state.replyTarget.summary));
        const cancel = button('取消回复', () => { if (controller.cancelReply()) document.querySelector<HTMLTextAreaElement>('.comment-draft')?.focus(); }, 'text-button');
        cancel.disabled = state.sending; context.append(cancel); composer.append(context);
    }
    const draft = el('textarea', 'comment-draft');
    draft.setAttribute('aria-label', '评论内容'); draft.rows = 4; draft.value = state.draft;
    draft.placeholder = '写下你的想法…'; draft.readOnly = state.sending;
    const send = el('button', 'button primary', state.sending ? '正在发表…' : state.replyTarget ? '发送回复' : '发表评论'); send.type = 'submit';
    const retry = button('确认未发表，重新发送', () => controller.submit(true), 'button');
    const updateButtons = () => { const invalid = !!state.replyTarget && !state.replyTarget.valid; send.disabled = state.sending || state.uncertain || invalid || !state.draft.trim(); retry.disabled = state.sending || invalid || !state.draft.trim(); };
    draft.addEventListener('input', () => { controller.setDraft(draft.value); updateButtons(); });
    composer.addEventListener('submit', event => { event.preventDefault(); void controller.submit(); });
    composer.append(draft);
    if (state.sendError) { const error = el('p', 'comment-send-error', state.sendError); error.setAttribute('role', 'alert'); composer.append(error); }
    if (state.uncertain) {
        composer.append(el('p', 'inline-warning', `发表结果尚未确认。请${state.replyTarget ? '刷新回复' : '刷新评论'}或在官方客户端核对，确认没有发表后再重新发送。草稿已保留，不会自动重发。`));
    }
    if (state.sent) { const sent = el('p', 'comment-sent', state.replyTarget ? '回复已发表' : '评论已发表'); sent.setAttribute('role', 'status'); composer.append(sent); }
    const controls = el('div', 'actions'); controls.append(send);
    if (state.uncertain) controls.append(retry);
    if (state.uncertain) {
        const acknowledge = button('已确认发表，清除草稿', () => controller.acknowledgePublished());
        acknowledge.disabled = state.sending; controls.append(acknowledge);
    }
    const refreshThread = state.replyTarget && (state.replyTarget.valid || state.replyTarget.id !== state.replyTarget.primaryCommentId) ? state.replyTarget.primaryCommentId : '';
    const refresh = button(refreshThread ? '刷新回复' : '刷新评论', () => controller.refreshTarget());
    refresh.disabled = state.sending || (refreshThread ? !!state.threads.get(refreshThread)?.loading : state.loading);
    controls.append(refresh); composer.append(controls); updateButtons();
    composer.append(el('p', 'fine-print', '点击发表会以当前账号发送公开评论。草稿仅保留在本次运行中。'));
    const sorts = el('div', 'comment-order-tabs'); sorts.setAttribute('role', 'tablist'); sorts.setAttribute('aria-label', '评论排序');
    for (const [order, label] of [['hot', '热门'], ['latest', '最新']] as const) {
        const tab = button(label, () => controller.selectOrder(order), 'comment-order-tab'); tab.id = `comment-order-${order}`;
        tab.setAttribute('role', 'tab'); tab.setAttribute('aria-selected', String(state.order === order));
        tab.setAttribute('aria-controls', 'comment-order-list'); tab.tabIndex = state.order === order ? 0 : -1;
        tab.addEventListener('keydown', event => {
            if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
            event.preventDefault();
            const next = event.key === 'Home' ? 'hot' : event.key === 'End' ? 'latest' : state.order === 'hot' ? 'latest' : 'hot';
            void controller.selectOrder(next);
            document.getElementById(`comment-order-${next}`)?.focus({ preventScroll: true });
        });
        sorts.append(tab);
    }
    root.append(sorts, composer);
    if (state.confirmed.size) {
        const published = el('section', 'comment-published'); published.setAttribute('aria-label', '刚刚发表');
        published.append(el('h3', '', '刚刚发表'), el('p', 'muted', '已收到发表确认；列表中的位置以平台排序为准。'));
        for (const item of [...state.confirmed.values()].reverse()) {
            const entry = el('article', 'comment'); entry.dataset.commentId = item.id;
            entry.append(el('strong', '', item.author.nickname), el('p', 'comment-text', item.text)); published.append(entry);
        }
        root.append(published);
    }
    const drawPage = (target: PageState, container: HTMLElement, load: () => Promise<void>, reply = false, primaryCommentId?: string) => {
        for (const item of target.items) {
            const card = el('article', 'comment'); card.dataset.commentId = item.id;
            const meta = el('div', 'comment-meta');
            meta.append(el('strong', '', item.author.nickname || '听友'));
            const date = el('time', 'muted'); date.dateTime = item.createdAt;
            const stamp = new Date(item.createdAt);
            date.textContent = Number.isNaN(stamp.getTime()) ? '' : stamp.toLocaleString('zh-CN');
            meta.append(date); card.append(meta, el('p', 'comment-text', item.text));
            if (reply && item.replyTo) {
                const reference = el('div', 'comment-reference');
                reference.append(el('span', '', `回复 @${item.replyTo.nickname || '听友'}`), el('p', '', item.replyTo.summary)); card.append(reference);
            }
            const replyAction = button('回复', () => {
                if (controller.selectReply(item.id, reply ? primaryCommentId : undefined)) document.querySelector<HTMLTextAreaElement>('.comment-draft')?.focus();
            }, 'text-button comment-reply-action');
            replyAction.disabled = state.sending; card.append(replyAction);
            if (!reply && (item.replyCount !== undefined || state.threads.has(item.id))) {
                const expanded = controller.state.expanded.has(item.id);
                const thread = state.threads.get(item.id);
                const observedCount = thread ? new Set([...thread.items.map(reply => reply.id), ...thread.confirmed.keys()]).size : 0;
                const replyCount = thread?.loaded && thread.complete ? observedCount : Math.max(item.replyCount ?? 0, observedCount);
                const count = item.replyCount === undefined && !thread?.loaded && !observedCount ? '' : `（${replyCount}）`;
                const toggle = button(expanded ? `收起回复${count}` : `查看回复${count}`, () => {
                    if (expanded) { controller.state.expanded.delete(item.id); controller.onChange?.(); }
                    else { void controller.loadThread(item.id); controller.onChange?.(); }
                }, 'text-button comment-replies-toggle');
                toggle.setAttribute('aria-expanded', String(expanded));
                card.append(toggle);
                if (expanded) {
                    const thread = controller.state.threads.get(item.id);
                    const replies = el('div', 'comment-replies'); replies.dataset.primaryCommentId = item.id;
                    if (thread?.confirmed.size) {
                        const feedback = el('section', 'comment-reply-published'); feedback.setAttribute('aria-label', '刚刚回复'); feedback.append(el('h3', '', '刚刚回复'));
                        for (const sent of thread.confirmed.values()) {
                            const entry = el('article', 'comment'); entry.dataset.commentId = sent.id;
                            entry.append(el('strong', '', sent.author.nickname), el('p', 'comment-text', sent.text));
                            if (sent.replyTo) entry.append(el('p', 'comment-reference', `回复 @${sent.replyTo.nickname}：${sent.replyTo.summary}`));
                            feedback.append(entry);
                        }
                        replies.append(feedback);
                    }
                    if (thread) drawPage(thread, replies, () => controller.loadThread(item.id, true), true, item.id);
                    card.append(replies);
                }
            }
            container.append(card);
        }
        if (target.error) {
            const error = el('div', 'inline-error', target.error); error.setAttribute('role', 'alert');
            if (!target.loaded || target.cursor) error.append(button('重试', load, 'button small'));
            container.append(error);
        }
        if (target.loading) { const status = el('p', 'muted', '正在加载评论…'); status.setAttribute('role', 'status'); container.append(status); }
        else if (target.cursor && !target.complete && !target.error) container.append(button(reply ? '加载更多回复' : '加载更多评论', load, 'button load-more'));
        if (target.loaded && !target.items.length && target.complete) container.append(el('p', 'muted', reply ? '暂无回复。' : '暂无评论。'));
    };
    const list = el('div', 'comment-list'); list.id = 'comment-order-list'; list.setAttribute('role', 'tabpanel');
    list.setAttribute('aria-labelledby', `comment-order-${state.order}`); list.dataset.order = state.order;
    drawPage(controller.state, list, () => controller.load()); root.append(list);
    return root;
}
