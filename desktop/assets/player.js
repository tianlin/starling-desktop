// Exactly one audio instance belongs to this controller, not to a route or a card.
export class Player {
    audio;
    api;
    epoch;
    changed;
    item = null;
    state = 'idle';
    mediaError = '';
    progressError = '';
    get error() { return this.mediaError || this.progressError; }
    get notice() {
        // Empty seek ranges are normal while loading metadata or restoring a
        // checkpoint. Wait for HAVE_FUTURE_DATA before reporting a limitation.
        if (!this.resolved || this.audio.readyState < 3 || this.audio.seeking)
            return '';
        if (this.seekOnLoad > 0)
            return '尚未恢复到上次位置：当前音频暂时无法定位，原进度会保留。';
        return this.seekable ? '' : '当前音频暂不支持定位，已禁用进度拖动和快进、后退。';
    }
    onPlayable = () => { };
    onEnded = () => { };
    serial = 0;
    requestID = '';
    requestGeneration = 0;
    itemEpoch = 0;
    resolved = false;
    savedPosition = 0;
    savedDuration = 0;
    seekOnLoad = 0;
    suppress = false;
    lastSave = 0;
    saveChain = Promise.resolve();
    retried = false;
    disposed = false;
    listeners = [];
    constructor(audio, api, epoch, changed) {
        this.audio = audio;
        this.api = api;
        this.epoch = epoch;
        this.changed = changed;
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
        this.listen('playing', () => {
            if (this.resolved) {
                this.state = 'playing';
                this.mediaError = '';
                this.notify();
            }
        });
        this.listen('waiting', () => {
            if (this.resolved && !audio.paused) {
                this.state = 'buffering';
                this.notify();
            }
        });
        this.listen('pause', () => {
            if (!this.suppress && this.resolved && this.state !== 'ended' && this.state !== 'error') {
                this.state = 'paused';
                this.save(false);
                this.notify();
            }
        });
        this.listen('timeupdate', () => {
            if (!this.resolved)
                return;
            this.applyInitialSeek();
            if (this.seekOnLoad > 0) {
                this.notify();
                return;
            }
            this.savedPosition = audio.currentTime;
            if (Date.now() - this.lastSave >= 5000)
                this.save(false);
            this.notify();
        });
        this.listen('ended', () => {
            if (!this.resolved)
                return;
            this.state = 'ended';
            this.seekOnLoad = 0;
            this.savedPosition = this.duration;
            this.save(true);
            this.notify();
            void this.flush().then(() => {
                if (!this.disposed && this.state === 'ended')
                    this.onEnded();
            });
        });
        this.listen('error', () => {
            if (!this.resolved || this.suppress)
                return;
            const code = audio.error?.code;
            if (['playing', 'buffering'].includes(this.state) && !this.retried && (code === 2 || code === 4) && this.item) {
                this.retried = true;
                void this.start(this.item, this.seekOnLoad || this.position);
                return;
            }
            this.state = 'error';
            this.mediaError = '音频无法播放。可能是网络、格式或访问权限问题；请重试或打开官方页面。';
            this.notify();
        });
    }
    applyInitialSeek() {
        if (!this.resolved || this.seekOnLoad <= 0)
            return;
        // A non-seekable stream can still reach the old checkpoint by playing.
        if (this.audio.currentTime >= this.seekOnLoad) {
            this.seekOnLoad = 0;
            this.savedPosition = this.audio.currentTime;
        }
        else if (this.seekable)
            this.seek(this.seekOnLoad);
    }
    listen(name, fn) { const handler = fn; this.audio.addEventListener(name, handler); this.listeners.push([name, handler]); }
    notify() {
        if (!this.disposed)
            this.changed();
    }
    observeGeneration(generation) {
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
    restore(p) { this.item = p.item; this.itemEpoch = this.epoch(); this.savedPosition = p.ended ? 0 : p.position; this.savedDuration = p.duration; this.state = 'paused'; this.notify(); }
    async play(it) { this.retried = false; return this.start(it); }
    async start(it, position) {
        if (this.disposed)
            return false;
        this.save(this.state === 'ended');
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
        this.seekOnLoad = 0;
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
            const result = await this.api('playback.resolve', { epoch, id: it.id, requestId: requestID, generation: seq });
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
            this.mediaError = e instanceof Error ? e.message : '播放失败。';
            this.notify();
            return false;
        }
    }
    pause() {
        ++this.serial;
        if (this.requestID)
            void this.api('playback.cancel', { epoch: this.itemEpoch, requestId: this.requestID, generation: this.requestGeneration }).catch(() => { });
        this.audio.pause();
        if (this.item && this.state !== 'ended' && this.state !== 'error')
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
            const seq = this.serial, requestID = this.requestID, epoch = this.itemEpoch;
            try {
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
    seek(seconds) {
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
                    this.seekOnLoad = 0;
                    this.notify();
                    return true;
                }
                catch { /* resource not seekable yet */ }
                return false;
            }
        }
        return false;
    }
    jump(delta) { this.seek(this.position + delta); }
    save(ended) {
        if (!this.resolved || !this.item || this.disposed || this.seekOnLoad > 0)
            return;
        const position = ended ? this.duration : this.position, duration = this.duration;
        if (!Number.isFinite(position) || position < 0 || !Number.isFinite(duration) || duration <= 0)
            return;
        this.lastSave = Date.now();
        const progress = { item: this.item, position: Math.min(position, duration), duration, ended, updatedAt: '' };
        const epoch = this.itemEpoch;
        const requestID = this.requestID;
        const current = () => epoch === this.epoch() && requestID === this.requestID && !this.disposed;
        this.saveChain = this.saveChain.then(() => this.api('progress.save', { epoch, progress })).then(() => {
            if (current() && this.progressError) {
                this.progressError = '';
                this.notify();
            }
        }).catch(e => {
            if (current()) {
                this.progressError = '收听进度未保存：' + (e instanceof Error ? e.message : '写盘失败。');
                this.notify();
            }
        });
    }
    async persist() { this.save(this.state === 'ended'); await this.flush(); }
    async flush() { await this.saveChain; }
    clear() {
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
        this.seekOnLoad = 0;
        this.savedDuration = 0;
        this.state = 'idle';
        this.mediaError = '';
        this.progressError = '';
        this.notify();
    }
    async dispose() {
        await this.persist();
        this.pause();
        this.disposed = true;
        for (const [n, f] of this.listeners)
            this.audio.removeEventListener(n, f);
    }
}
