import type { Call, Item, Playback, Progress } from './types.js';
export type PlayerState = 'idle' | 'resolving' | 'buffering' | 'playing' | 'paused' | 'ended' | 'error';
// Exactly one audio instance belongs to this controller, not to a route or a card.
export class Player {
    item: Item | null = null;
    state: PlayerState = 'idle';
    error = '';
    onPlayable: (it: Item) => void = () => { };
    onEnded: () => void = () => { };
    private serial = 0;
    private requestID = '';
    private itemEpoch = 0;
    private resolved = false;
    private savedPosition = 0;
    private savedDuration = 0;
    private seekOnLoad = 0;
    private suppress = false;
    private lastSave = 0;
    private saveChain: Promise<void> = Promise.resolve();
    private retried = false;
    private disposed = false;
    private listeners: Array<[
        string,
        EventListener
    ]> = [];
    constructor(readonly audio: HTMLAudioElement, private api: Call, private epoch: () => number, private changed: () => void) {
        this.listen('loadedmetadata', () => {
            if (!this.resolved)
                return;
            if (Number.isFinite(audio.duration))
                this.savedDuration = audio.duration;
            this.applyInitialSeek();
            this.notify();
        });
        this.listen('canplay', () => this.applyInitialSeek());
        this.listen('progress', () => this.applyInitialSeek());
        this.listen('playing', () => { if (this.resolved) {
            this.state = 'playing';
            this.error = '';
            this.notify();
        } });
        this.listen('waiting', () => { if (this.resolved && !audio.paused) {
            this.state = 'buffering';
            this.notify();
        } });
        this.listen('pause', () => { if (!this.suppress && this.resolved && this.state !== 'ended') {
            this.state = 'paused';
            this.save(false);
            this.notify();
        } });
        this.listen('timeupdate', () => { if (!this.resolved)
            return; this.savedPosition = audio.currentTime; if (Date.now() - this.lastSave >= 5000)
            this.save(false); this.notify(); });
        this.listen('ended', () => {
            if (!this.resolved)
                return;
            this.state = 'ended';
            this.savedPosition = this.duration;
            this.save(true);
            this.notify();
            void this.flush().then(() => { if (!this.disposed && this.state === 'ended')
                this.onEnded(); });
        });
        this.listen('error', () => {
            if (!this.resolved || this.suppress)
                return;
            const code = audio.error?.code;
            if (['playing', 'buffering'].includes(this.state) && !this.retried && (code === 2 || code === 4) && this.item) {
                this.retried = true;
                void this.start(this.item, false, this.position);
                return;
            }
            this.state = 'error';
            this.error = '音频无法播放。可能是网络、格式或访问权限问题；请重试或打开官方页面。';
            this.notify();
        });
    }
    private applyInitialSeek() { if (this.seekOnLoad > 0 && this.seekable) {
        this.seek(this.seekOnLoad);
        this.seekOnLoad = 0;
    } }
    private listen(name: string, fn: () => void) { const handler = fn as EventListener; this.audio.addEventListener(name, handler); this.listeners.push([name, handler]); }
    private notify() { if (!this.disposed)
        this.changed(); }
    get position() { return this.resolved ? this.audio.currentTime : this.savedPosition; }
    get duration() { return this.resolved && Number.isFinite(this.audio.duration) ? this.audio.duration : this.savedDuration; }
    get seekable() { return this.resolved && this.audio.seekable.length > 0 && this.duration > 0; }
    restore(p: Progress) { this.item = p.item; this.itemEpoch = this.epoch(); this.savedPosition = p.ended ? 0 : p.position; this.savedDuration = p.duration; this.state = 'paused'; this.notify(); }
    async play(it: Item): Promise<boolean> { this.retried = false; return this.start(it, true); }
    private async start(it: Item, manual: boolean, position?: number): Promise<boolean> {
        if (this.disposed)
            return false;
        this.save(this.state === 'ended');
        const previous = this.requestID, previousEpoch = this.itemEpoch;
        const seq = ++this.serial;
        this.requestID = `play-${Date.now()}-${seq}`;
        const requestID = this.requestID;
        if (previous)
            void this.api('playback.cancel', { epoch: previousEpoch, requestId: previous }).catch(() => { });
        this.suppress = true;
        this.audio.pause();
        this.audio.removeAttribute('src');
        this.audio.load();
        this.suppress = false;
        this.resolved = false;
        this.item = it;
        this.itemEpoch = this.epoch();
        this.savedPosition = position ?? 0;
        this.savedDuration = it.duration ?? 0;
        this.state = 'resolving';
        this.error = '';
        this.notify();
        const epoch = this.itemEpoch;
        try {
            // Wait for older saves before resolving the persisted position for a reselected item.
            await this.flush();
            if (seq !== this.serial || epoch !== this.epoch())
                return false;
            const result = await this.api<Playback>('playback.resolve', { epoch, id: it.id, requestId: requestID });
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed)
                return false;
            this.item = result.item;
            this.savedPosition = position ?? result.position;
            this.seekOnLoad = this.savedPosition;
            this.savedDuration = result.item.duration ?? 0;
            this.resolved = true;
            this.audio.src = result.url;
            this.state = 'buffering';
            this.audio.load();
            this.notify();
            await this.audio.play();
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed)
                return false;
            this.onPlayable(result.item);
            this.notify();
            return true;
        }
        catch (e) {
            if (seq !== this.serial || epoch !== this.epoch())
                return false;
            this.suppress = true;
            this.audio.pause();
            this.suppress = false;
            this.state = 'error';
            this.error = e instanceof Error ? e.message : '播放失败。';
            this.notify();
            return false;
        }
    }
    pause() {
        ++this.serial;
        if (this.requestID)
            void this.api('playback.cancel', { epoch: this.itemEpoch, requestId: this.requestID }).catch(() => { });
        this.audio.pause();
        if (this.item && this.state !== 'ended')
            this.state = 'paused';
        this.save(this.state === 'ended');
        this.notify();
    }
    async toggle() {
        if (!this.item)
            return;
        if (this.state === 'resolving' || this.state === 'playing' || this.state === 'buffering') {
            this.pause();
            return;
        }
        if (this.resolved && this.state !== 'error' && this.state !== 'ended' && this.itemEpoch === this.epoch()) {
            try {
                await this.audio.play();
            }
            catch {
                this.state = 'paused';
                this.error = '播放未获允许，请再点击播放。';
                this.notify();
            }
        }
        else
            await this.play(this.item);
    }
    seek(seconds: number) {
        if (!this.seekable || !Number.isFinite(seconds))
            return;
        const target = Math.max(0, Math.min(seconds, this.duration));
        for (let i = 0; i < this.audio.seekable.length; i++) {
            if (target >= this.audio.seekable.start(i) && target <= this.audio.seekable.end(i)) {
                try {
                    this.audio.currentTime = target;
                    this.savedPosition = target;
                    this.notify();
                }
                catch { /* resource not seekable yet */ }
                return;
            }
        }
    }
    jump(delta: number) { this.seek(this.position + delta); }
    private save(ended: boolean) {
        if (!this.resolved || !this.item || this.disposed)
            return;
        const position = ended ? this.duration : this.position, duration = this.duration;
        if (!Number.isFinite(position) || position < 0 || !Number.isFinite(duration) || duration <= 0)
            return;
        this.lastSave = Date.now();
        const progress: Progress = { item: this.item, position: Math.min(position, duration), duration, ended, updatedAt: '' };
        const epoch = this.itemEpoch;
        this.saveChain = this.saveChain.then(() => this.api('progress.save', { epoch, progress })).then(() => { }).catch(e => {
            if (epoch === this.epoch() && !this.disposed) {
                this.error = '收听进度未保存：' + (e instanceof Error ? e.message : '写盘失败。');
                this.notify();
            }
        });
    }
    async persist() { this.save(this.state === 'ended'); await this.flush(); }
    async flush() { await this.saveChain; }
    clear() {
        ++this.serial;
        if (this.requestID)
            void this.api('playback.cancel', { epoch: this.itemEpoch, requestId: this.requestID }).catch(() => { });
        this.suppress = true;
        this.resolved = false;
        this.audio.pause();
        this.audio.removeAttribute('src');
        this.audio.load();
        this.suppress = false;
        this.item = null;
        this.savedPosition = 0;
        this.savedDuration = 0;
        this.state = 'idle';
        this.error = '';
        this.notify();
    }
    async dispose() { await this.persist(); this.pause(); this.disposed = true; for (const [n, f] of this.listeners)
        this.audio.removeEventListener(n, f); }
}
