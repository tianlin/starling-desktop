import type { Call, CommentPage, Session } from './types.js';
import { describeError } from './api.js';
import { button, el, empty } from './dom.js';

interface PageState extends CommentPage { loaded: boolean; loading: boolean; error: string }
interface EpisodeState extends PageState { threads: Map<string, PageState>; expanded: Set<string> }
const freshPage = (): PageState => ({ items: [], cursor: '', complete: false, loaded: false, loading: false, error: '' });
const freshEpisode = (): EpisodeState => ({ ...freshPage(), threads: new Map(), expanded: new Set() });

// Memory belongs to the current account and process; no comments are persisted.
export class CommentsController {
    private cache = new Map<string, EpisodeState>();
    private sessionKey = '';
    private epoch = 0;
    private generation = 0;
    private episodeId = '';
    connected = false;
    state: EpisodeState = freshEpisode();
    onChange: (() => void) | undefined;
    constructor(private call: Call) {}
    setSession(session: Session) {
        const key = `${session.epoch}:${session.state}:${session.identity?.id ?? ''}`;
        if (key === this.sessionKey) return;
        this.sessionKey = key; this.epoch = session.epoch;
        this.connected = session.state === 'connected' && !!session.identity;
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
    async loadThread(commentId: string, more = false) {
        let thread = this.state.threads.get(commentId);
        if (!thread) { thread = freshPage(); this.state.threads.set(commentId, thread); }
        this.state.expanded.add(commentId);
        if (thread.loaded && !more) { this.onChange?.(); return; }
        await this.fetchPage(thread, commentId);
    }
    private async fetchPage(target: PageState, commentId?: string) {
        if (!this.connected || !this.episodeId || target.loading || (target.loaded && (target.complete || !target.cursor))) return;
        const generation = this.generation, epoch = this.epoch, episodeId = this.episodeId;
        target.loading = true; target.error = ''; this.onChange?.();
        try {
            const page = await this.call<CommentPage>(commentId ? 'comments.thread' : 'comments.list', { epoch, episodeId, ...(commentId ? { commentId } : {}), cursor: target.cursor });
            if (generation !== this.generation) return;
            const ids = new Set(target.items.map(item => item.id));
            for (const item of page.items) if (!ids.has(item.id)) { ids.add(item.id); target.items.push(item); }
            target.cursor = page.cursor; target.complete = page.complete; target.loaded = true;
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
            error.append(button('重试', load, 'button small')); container.append(error);
        }
        if (target.loading) { const status = el('p', 'muted', '正在加载评论…'); status.setAttribute('role', 'status'); container.append(status); }
        else if (target.cursor && !target.complete && !target.error) container.append(button(reply ? '加载更多回复' : '加载更多评论', load, 'button load-more'));
        if (target.loaded && !target.items.length && target.complete) container.append(el('p', 'muted', reply ? '暂无回复。' : '暂无评论。'));
        else if (target.loaded && !target.complete && !target.cursor) container.append(el('p', 'muted', '平台未提供下一页信息，暂时只能显示已获取的内容。'));
    };
    drawPage(controller.state, root, () => controller.load());
    return root;
}
