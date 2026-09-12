import type { Call, Item, Playback, Progress, ProgressPreparation, ProgressSyncStatus } from './types.js';
export type PlayerState = 'idle' | 'resolving' | 'buffering' | 'playing' | 'paused' | 'ended' | 'error';
// Exactly one audio instance belongs to this controller, not to a route or a card.
export class Player {
    item: Item | null = null;
    state: PlayerState = 'idle';
    private mediaError = '';
    private progressError = '';
    get error() { return this.mediaError || this.progressError; }
    get notice() {
        // Empty seek ranges are normal while loading metadata or restoring a
        // checkpoint. Wait for HAVE_FUTURE_DATA before reporting a limitation.
        if (!this.resolved || this.audio.readyState < 3 || this.audio.seeking)
            return '';
        if (this.pendingInitialSeek)
            return '尚未恢复到上次位置：当前音频暂时无法定位，原进度会保留。';
        return this.seekable ? '' : '当前音频暂不支持定位，已禁用进度拖动和快进、后退。';
    }
    onPlayable: (it: Item) => void = () => { };
    onEnded: () => void = () => { };
    sync: ProgressSyncStatus = { state: 'idle', pending: 0 };
    conflict: ProgressPreparation['conflict'];
    private conflictDecision?: (value: ProgressPreparation | null) => void;
    private conflictBusy = false;
    get syncNotice() { return this.sync.state === 'error' ? (this.sync.message || '云端进度同步暂不可用，收听进度仍保存在本机。') : ''; }
    private lastUrgent = true;
    private explicitResume = false;
    private checkpoint = 0;
    private checkpointEnded = false;
    private serial = 0;
    private requestID = '';
    private requestGeneration = 0;
    private itemEpoch = 0;
    private resolved = false;
    private savedPosition = 0;
    private savedDuration = 0;
    private seekOnLoad = 0;
    private pendingInitialSeek = false;
    private allowCatchup = true;
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
        this.listen('canplay', () => { this.applyInitialSeek(); this.notify(); });
        this.listen('progress', () => { this.applyInitialSeek(); this.notify(); });
        this.listen('playing', () => { if (this.resolved) {
            this.state = 'playing';
            this.mediaError = '';
            this.notify();
        } });
        this.listen('waiting', () => { if (this.resolved && !audio.paused) {
            this.state = 'buffering';
            this.notify();
        } });
        this.listen('pause', () => { if (!this.suppress && this.resolved && this.state !== 'ended' && this.state !== 'error') {
            this.state = 'paused';
            this.save(false, true);
            this.notify();
        } });
        this.listen('timeupdate', () => {
            if (!this.resolved)
                return;
            this.applyInitialSeek();
            if (this.pendingInitialSeek) {
                this.notify();
                return;
            }
            if (audio.paused || audio.seeking) { this.notify(); return; }
            this.savedPosition = audio.currentTime;
            if (Date.now() - this.lastSave >= 5000)
                this.save(false);
            this.notify();
        });
        this.listen('ended', () => {
            if (!this.resolved)
                return;
            this.state = 'ended';
            this.seekOnLoad = 0; this.pendingInitialSeek = false;
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
                void this.start(this.item, this.pendingInitialSeek ? this.seekOnLoad : this.position);
                return;
            }
            this.state = 'error';
            this.mediaError = '音频无法播放。可能是网络、格式或访问权限问题；请重试或打开官方页面。';
            this.notify();
        });
    }
    private applyInitialSeek() {
        if (!this.resolved || !this.pendingInitialSeek)
            return;
        // A non-seekable stream can still reach the old checkpoint by playing.
        if (this.allowCatchup && this.audio.currentTime >= this.seekOnLoad) {
            this.seekOnLoad = 0; this.pendingInitialSeek = false;
            this.savedPosition = this.audio.currentTime;
        }
        else if (this.seekable)
            this.seek(this.seekOnLoad, false);
    }
    private cancelConflict() {
        this.conflict = undefined;
        this.conflictDecision?.(null);
        this.conflictDecision = undefined;
        this.conflictBusy = false;
    }
    private async prepareProgress(epoch: number, eid: string, fallback: number, seq: number): Promise<ProgressPreparation | null> {
        try {
            const result = await this.api<ProgressPreparation>('progress.prepare', { epoch, eid });
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed) return null;
            if (this.resolved && this.explicitResume) return null;
            this.sync = result.sync;
            if (!result.conflict) { this.notify(); return result; }
            this.conflict = result.conflict;
            const decision = new Promise<ProgressPreparation | null>(resolve => { this.conflictDecision = resolve; });
            this.notify();
            return await decision;
        } catch (e) {
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed) return null;
            this.sync = { state: 'error', pending: 0, message: '云端进度读取失败，继续使用本机进度：' + (e instanceof Error ? e.message : '请稍后重试。') };
            this.notify();
            return { position: fallback, sync: this.sync };
        }
    }
    async chooseProgress(choice: 'local' | 'cloud') {
        if (!this.conflict || !this.item || this.conflictBusy) return;
        const token = this.conflict.token, seq = this.serial, epoch = this.itemEpoch;
        this.conflictBusy = true;
        try {
            const result = await this.api<ProgressPreparation>('progress.choose', { epoch, eid: this.item.id, token, choice });
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed) return;
            if (token !== this.conflict?.token) return;
            this.sync = result.sync;
            this.conflict = undefined;
            this.conflictDecision?.(result);
            this.conflictDecision = undefined;
        } catch (e) {
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed) return;
            if (token !== this.conflict?.token) return;
            this.sync = { state: 'error', pending: 0, message: '进度选择失败，请重新播放以获取最新进度：' + (e instanceof Error ? e.message : '') };
        } finally {
            if (seq === this.serial) { this.conflictBusy = false; this.notify(); }
        }
    }
    private listen(name: string, fn: () => void) { const handler = fn as EventListener; this.audio.addEventListener(name, handler); this.listeners.push([name, handler]); }
    private notify() { if (!this.disposed)
        this.changed(); }
    observeGeneration(generation: number) {
        if (Number.isSafeInteger(generation) && generation > this.serial)
            this.serial = generation;
    }
    get position() { return this.resolved ? this.audio.currentTime : this.savedPosition; }
    get duration() { return this.resolved && Number.isFinite(this.audio.duration) ? this.audio.duration : this.savedDuration; }
    get seekable() {
        if (!this.resolved || this.duration <= 0)
            return false;
        for (let i = 0; i < this.audio.seekable.length; i++) {
            if (this.audio.seekable.end(i) > this.audio.seekable.start(i))
                return true;
        }
        return false;
    }
    restore(p: Progress) { this.item = p.item; this.itemEpoch = this.epoch(); this.savedPosition = p.ended ? 0 : p.position; this.savedDuration = p.duration; this.state = 'paused'; this.notify(); }
    async play(it: Item, position?: number): Promise<boolean> { this.retried = false; return this.start(it, position); }
    private async start(it: Item, position?: number): Promise<boolean> {
        if (this.disposed)
            return false;
        this.save(this.state === 'ended', true);
        this.cancelConflict();
        const previous = this.requestID, previousEpoch = this.itemEpoch, previousGeneration = this.requestGeneration;
        const seq = ++this.serial;
        this.requestGeneration = seq;
        this.requestID = `play-${Date.now()}-${seq}`;
        const requestID = this.requestID;
        if (previous)
            void this.api('playback.cancel', { epoch: previousEpoch, requestId: previous, generation: previousGeneration }).catch(() => { });
        this.suppress = true;
        this.audio.pause();
        this.audio.removeAttribute('src');
        this.audio.load();
        this.suppress = false;
        this.resolved = false;
        this.item = it;
        this.itemEpoch = this.epoch();
        this.savedPosition = position ?? 0;
        this.seekOnLoad = 0; this.pendingInitialSeek = false;
        this.savedDuration = it.duration ?? 0;
        this.state = 'resolving';
        this.mediaError = '';
        this.progressError = '';
        this.notify();
        const epoch = this.itemEpoch;
        try {
            // Wait for older saves before resolving the persisted position for a reselected item.
            await this.flush();
            if (seq !== this.serial || epoch !== this.epoch())
                return false;
            const result = await this.api<Playback>('playback.resolve', { epoch, id: it.id, requestId: requestID, generation: seq });
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed)
                return false;
            const prepared = position === undefined ? await this.prepareProgress(epoch, result.item.id, result.position, seq) : null;
            if (seq !== this.serial || epoch !== this.epoch() || this.disposed) return false;
            this.item = result.item;
            this.savedPosition = position ?? prepared?.position ?? result.position;
            this.lastUrgent = true;
            this.explicitResume = false;
            this.checkpoint = this.savedPosition;
            this.checkpointEnded = false;
            this.seekOnLoad = this.savedPosition;
            this.pendingInitialSeek = this.savedPosition > 0;
            this.allowCatchup = true;
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
            this.mediaError = e instanceof Error ? e.message : '播放失败。';
            this.notify();
            return false;
        }
    }
    pause() {
        this.cancelConflict();
        ++this.serial;
        if (this.requestID)
            void this.api('playback.cancel', { epoch: this.itemEpoch, requestId: this.requestID, generation: this.requestGeneration }).catch(() => { });
        this.suppress = true;
        this.audio.pause();
        this.suppress = false;
        if (this.item && this.state !== 'ended' && this.state !== 'error')
            this.state = 'paused';
        this.save(this.state === 'ended', true);
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
            const seq = ++this.serial, requestID = this.requestID, epoch = this.itemEpoch;
            this.state = 'resolving';
            this.notify();
            try {
                await this.flush();
                if (seq !== this.serial || epoch !== this.epoch() || this.disposed) return;
                const prepared = this.explicitResume ? null : await this.prepareProgress(epoch, this.item.id, this.position, seq);
                if (seq !== this.serial || requestID !== this.requestID || epoch !== this.epoch() || this.disposed) return;
                if (prepared && !this.explicitResume && prepared.position !== this.position) {
                    this.savedPosition = prepared.position;
                    this.checkpoint = prepared.position;
                    this.checkpointEnded = false;
                    this.seekOnLoad = prepared.position;
                    this.pendingInitialSeek = true;
                    this.allowCatchup = false;
                    if (prepared.position === 0) this.seek(0, false);
                    else this.seek(prepared.position, false);
                }
                this.explicitResume = false;
                await this.audio.play();
            }
            catch {
                if (seq !== this.serial || requestID !== this.requestID || epoch !== this.epoch() || this.disposed)
                    return;
                this.state = 'paused';
                this.mediaError = '播放未获允许，请再点击播放。';
                this.notify();
            }
        }
        else
            await this.play(this.item);
    }
    seek(seconds: number, user = true): boolean {
        if (!this.seekable || !Number.isFinite(seconds))
            return false;
        const target = Math.max(0, Math.min(seconds, this.duration));
        for (let i = 0; i < this.audio.seekable.length; i++) {
            if (target >= this.audio.seekable.start(i) && target <= this.audio.seekable.end(i)) {
                try {
                    this.audio.currentTime = target;
                    this.savedPosition = target;
                    // Keep an unreachable resume target until this assignment succeeds.
                    // An explicit successful user seek also replaces that target.
                    this.seekOnLoad = 0; this.pendingInitialSeek = false;
                    if (user) { this.explicitResume = this.audio.paused; this.cancelConflict(); this.save(false, true); }
                    this.notify();
                    return true;
                }
                catch { /* resource not seekable yet */ }
                return false;
            }
        }
        return false;
    }
    jump(delta: number) { this.seek(this.position + delta); }
    private save(ended: boolean, urgent = false) {
        if (!this.resolved || !this.item || this.disposed || this.pendingInitialSeek)
            return;
        const position = ended ? this.duration : this.position, duration = this.duration;
        if (!Number.isFinite(position) || position < 0 || !Number.isFinite(duration) || duration <= 0)
            return;
        if (position === this.checkpoint && ended === this.checkpointEnded && (!urgent || this.lastUrgent)) return;
        this.lastUrgent = urgent || ended;
        this.checkpoint = position;
        this.checkpointEnded = ended;
        this.lastSave = Date.now();
        const progress: Progress = { item: this.item, position: Math.min(position, duration), duration, ended, updatedAt: '' };
        const epoch = this.itemEpoch;
        const requestID = this.requestID;
        const current = () => epoch === this.epoch() && requestID === this.requestID && !this.disposed;
        this.saveChain = this.saveChain.then(() => this.api('progress.save', { epoch, progress, urgent: urgent || ended })).then(() => {
            if (current()) void this.api<ProgressSyncStatus>('progress.status', { epoch }).then(status => {
                if (current() && status) { this.sync = status; this.notify(); }
            }).catch(() => {});
            if (current() && this.progressError) {
                this.progressError = '';
                this.notify();
            }
        }).catch(e => {
            if (current()) {
                this.checkpoint = Number.NaN;
                this.progressError = '收听进度未保存：' + (e instanceof Error ? e.message : '写盘失败。');
                this.notify();
            }
        });
    }
    async persist() { this.save(this.state === 'ended', true); await this.flush(); }
    async flush() { await this.saveChain; }
    clear() {
        this.cancelConflict();
        this.sync = { state: 'idle', pending: 0 };
        ++this.serial;
        if (this.requestID)
            void this.api('playback.cancel', { epoch: this.itemEpoch, requestId: this.requestID, generation: this.requestGeneration }).catch(() => { });
        this.requestID = '';
        this.requestGeneration = 0;
        this.suppress = true;
        this.resolved = false;
        this.audio.pause();
        this.audio.removeAttribute('src');
        this.audio.load();
        this.suppress = false;
        this.item = null;
        this.savedPosition = 0;
        this.seekOnLoad = 0; this.pendingInitialSeek = false;
        this.savedDuration = 0;
        this.state = 'idle';
        this.mediaError = '';
        this.progressError = '';
        this.notify();
    }
    async dispose() { await this.persist(); this.pause(); this.disposed = true; for (const [n, f] of this.listeners)
        this.audio.removeEventListener(n, f); }
}
