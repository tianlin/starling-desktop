import type { Call, Comment, CommentCreated, CommentPage, Session } from './types.js';
import { APIError, describeError } from './api.js';
import { button, el, empty } from './dom.js';

interface PageState extends CommentPage { loaded: boolean; loading: boolean; error: string; cursors: Set<string> }
interface EpisodeState extends PageState {
    threads: Map<string, PageState>; expanded: Set<string>;
    draft: string; sending: boolean; uncertain: boolean; sendError: string; sent: boolean;
    confirmed: Map<string, Comment>;
}
const freshPage = (): PageState => ({ items: [], cursor: '', complete: false, loaded: false, loading: false, error: '', cursors: new Set() });
const freshEpisode = (): EpisodeState => ({ ...freshPage(), threads: new Map(), expanded: new Set(), draft: '', sending: false, uncertain: false, sendError: '', sent: false, confirmed: new Map() });
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
    private generation = 0;
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
                this.generation++; this.state.loading = false;
                for (const thread of this.state.threads.values()) thread.loading = false;
                this.onChange?.();
            }
            return;
        }
        this.sessionKey = key; this.epoch = session.epoch;
        this.generation++; this.cache.clear(); this.state = freshEpisode();
        if (this.episodeId) this.cache.set(this.episodeId, this.state);
        this.onChange?.();
    }
    open(episodeId: string) {
        this.leave(); this.episodeId = episodeId;
        this.state = this.cache.get(episodeId) ?? freshEpisode();
        this.cache.set(episodeId, this.state);
    }
    leave() {
        this.generation++;
        this.state.loading = false;
        for (const thread of this.state.threads.values()) thread.loading = false;
        this.episodeId = ''; this.onChange = undefined;
    }
    async load() { await this.fetchPage(this.state); }
    setDraft(text: string) { if (!this.state.sending) { this.state.draft = text; this.state.sent = false; } }
    acknowledgePublished() {
        if (this.state.sending || !this.state.uncertain) return;
        this.state.draft = ''; this.state.uncertain = false; this.state.sendError = '';
        this.onChange?.();
    }
    async submit(explicitRetry = false) {
        const target = this.state, episodeId = this.episodeId, epoch = this.epoch, key = this.sessionKey;
        if (!this.connected || !episodeId || target.sending || (target.uncertain && !explicitRetry)) return;
        if (!target.draft.trim()) { target.sendError = '请填写评论内容，不能只含空白。'; this.onChange?.(); return; }
        const text = target.draft;
        target.sending = true; target.sendError = ''; target.sent = false; this.onChange?.();
        try {
            const result = await this.call<CommentCreated>('comments.create', { epoch, episodeId, text, requestId: crypto.randomUUID() });
            if (key !== this.sessionKey) return;
            if (!isComment(result?.comment) || result.comment.author.id !== this.authorId) throw new Error('发表响应无法确认。');
            target.confirmed.set(result.comment.id, result.comment);
            target.items = [result.comment, ...target.items.filter(item => item.id !== result.comment.id)];
            target.draft = ''; target.uncertain = false; target.sent = true;
        } catch (e) {
            if (key !== this.sessionKey) return;
            const definite = new Set(['UNAUTHORIZED', 'STALE_SESSION', 'INVALID_ID', 'INVALID_COMMENT', 'INVALID_REQUEST', 'UNSUPPORTED', 'RATE_LIMIT', 'FORBIDDEN', 'REQUEST_REJECTED', 'EXPIRED', 'COMMENT_BUSY', 'COMMENT_SESSION_LIMIT']);
            target.uncertain = !(e instanceof APIError && definite.has(e.code));
            target.sendError = describeError(e);
            if (e instanceof APIError && e.code === 'UNAUTHORIZED') target.sendError += ' 请在账号与连接中重新连接；草稿已保留。';
        } finally {
            if (key === this.sessionKey) { target.sending = false; if (this.state === target) this.onChange?.(); }
        }
    }
    async refresh() {
        if (!this.connected || !this.episodeId) return;
        this.generation++; this.state.loading = false;
        for (const thread of this.state.threads.values()) thread.loading = false;
        await this.fetchPage(this.state, undefined, true);
    }
    async loadThread(commentId: string, more = false) {
        let thread = this.state.threads.get(commentId);
        if (!thread) { thread = freshPage(); this.state.threads.set(commentId, thread); }
        this.state.expanded.add(commentId);
        if (thread.loaded && !more) { this.onChange?.(); return; }
        await this.fetchPage(thread, commentId);
    }
    private async fetchPage(target: PageState, commentId?: string, refresh = false) {
        if (!this.connected || !this.episodeId || target.loading || (!refresh && target.loaded && (target.complete || !target.cursor))) return;
        const generation = this.generation, epoch = this.epoch, episodeId = this.episodeId;
        const cursor = refresh ? '' : target.cursor;
        target.loading = true; target.error = ''; this.onChange?.();
        try {
            const page = await this.call<CommentPage>(commentId ? 'comments.thread' : 'comments.list', { epoch, episodeId, ...(commentId ? { commentId } : {}), cursor });
            if (generation !== this.generation) return;
            if (!refresh && page.cursor && (page.cursor === cursor || target.cursors.has(page.cursor))) {
                target.cursor = ''; target.complete = false;
                throw new Error('评论分页游标重复，已停止加载。请刷新评论后重试。');
            }
            if (refresh) {
                target.items = [...this.state.confirmed.values()].reverse(); target.cursors.clear();
            }
            const ids = new Set(target.items.map(item => item.id));
            for (const item of page.items) if (!ids.has(item.id)) { ids.add(item.id); target.items.push(item); }
            if (cursor) target.cursors.add(cursor);
            target.cursor = page.complete || target.cursors.has(page.cursor) ? '' : page.cursor;
            target.complete = page.complete; target.loaded = true;
        } catch (e) {
            if (generation !== this.generation) return;
            target.error = describeError(e);
        } finally {
            if (generation === this.generation) { target.loading = false; this.onChange?.(); }
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
    const draft = el('textarea', 'comment-draft');
    draft.setAttribute('aria-label', '评论内容'); draft.rows = 4; draft.value = state.draft;
    draft.placeholder = '写下你的想法…'; draft.readOnly = state.sending;
    const send = el('button', 'button primary', state.sending ? '正在发表…' : '发表评论'); send.type = 'submit';
    const retry = button('确认未发表，重新发送', () => controller.submit(true), 'button');
    const updateButtons = () => { send.disabled = state.sending || state.uncertain || !state.draft.trim(); retry.disabled = state.sending || !state.draft.trim(); };
    draft.addEventListener('input', () => { controller.setDraft(draft.value); updateButtons(); });
    composer.addEventListener('submit', event => { event.preventDefault(); void controller.submit(); });
    composer.append(draft);
    if (state.sendError) { const error = el('p', 'comment-send-error', state.sendError); error.setAttribute('role', 'alert'); composer.append(error); }
    if (state.uncertain) {
        composer.append(el('p', 'inline-warning', '发表结果尚未确认。请刷新评论或在官方客户端核对，确认没有发表后再重新发送。草稿已保留，不会自动重发。'));
    }
    if (state.sent) { const sent = el('p', 'comment-sent', '评论已发表'); sent.setAttribute('role', 'status'); composer.append(sent); }
    const controls = el('div', 'actions'); controls.append(send);
    if (state.uncertain) controls.append(retry);
    if (state.uncertain) {
        const acknowledge = button('已确认发表，清除草稿', () => controller.acknowledgePublished());
        acknowledge.disabled = state.sending; controls.append(acknowledge);
    }
    const refresh = button('刷新评论', () => controller.refresh()); refresh.disabled = state.loading || state.sending;
    controls.append(refresh); composer.append(controls); updateButtons();
    composer.append(el('p', 'fine-print', '点击发表会以当前账号发送公开评论。草稿仅保留在本次运行中。'));
    root.append(composer);
    const drawPage = (target: PageState, container: HTMLElement, load: () => Promise<void>, reply = false) => {
        for (const item of target.items) {
            const card = el('article', 'comment'); card.dataset.commentId = item.id;
            const meta = el('div', 'comment-meta');
            meta.append(el('strong', '', item.author.nickname || '听友'));
            const date = el('time', 'muted'); date.dateTime = item.createdAt;
            const stamp = new Date(item.createdAt);
            date.textContent = Number.isNaN(stamp.getTime()) ? '' : stamp.toLocaleString('zh-CN');
            meta.append(date); card.append(meta, el('p', 'comment-text', item.text));
            if (!reply && item.replyCount !== undefined) {
                const expanded = controller.state.expanded.has(item.id);
                const toggle = button(expanded ? `收起回复（${item.replyCount}）` : `查看回复（${item.replyCount}）`, () => {
                    if (expanded) { controller.state.expanded.delete(item.id); controller.onChange?.(); }
                    else { void controller.loadThread(item.id); controller.onChange?.(); }
                }, 'text-button comment-replies-toggle');
                toggle.setAttribute('aria-expanded', String(expanded));
                card.append(toggle);
                if (expanded) {
                    const thread = controller.state.threads.get(item.id);
                    const replies = el('div', 'comment-replies');
                    if (thread) drawPage(thread, replies, () => controller.loadThread(item.id, true), true);
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
        else if (target.loaded && !target.complete && !target.cursor) container.append(el('p', 'muted', '平台未提供下一页信息，暂时只能显示已获取的内容。'));
    };
    drawPage(controller.state, root, () => controller.load());
    return root;
}
