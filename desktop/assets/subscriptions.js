import { APIError, describeError } from './api.js';
import { el, button } from './dom.js';
export class SubscriptionController {
    api;
    session;
    entries = new Map();
    serial = 0;
    onChange = () => { };
    onUnauthorized = () => { };
    onConfirmed = (_result) => { };
    onNotice = (_message) => { };
    setSession(session) {
        if (this.session && (session.epoch !== this.session.epoch || session.identity?.id !== this.session.identity?.id))
            this.entries.clear();
        this.session = session;
    }
    constructor(api) {
        this.api = api;
    }
    get(id) {
        let e = this.entries.get(id);
        if (!e) {
            e = { state: 'unknown', pending: false, checking: false, uncertain: false, checked: false, acknowledged: false, error: '', revision: 0 };
            this.entries.set(id, e);
        }
        return e;
    }
    seed(states) {
        for (const [id, state] of Object.entries(states ?? {})) {
            const e = this.get(id);
            if (e.state !== 'subscribed' && !e.pending && !e.uncertain && e.revision === 0)
                e.state = state;
        }
    }
    acknowledge(id) {
        const e = this.get(id);
        if (e.checked)
            e.acknowledged = true;
        this.onChange();
    }
    accept(e, result, confirmsWrite) {
        if (e.state === 'subscribed' && result.state !== 'subscribed')
            return;
        e.state = result.state;
        if (result.state === 'subscribed') {
            e.uncertain = false;
            e.error = '';
            e.revision++;
            if (confirmsWrite)
                this.onConfirmed(result);
        }
        if (result.warning)
            this.onNotice(result.warning.message);
    }
    async check(id) {
        const e = this.get(id);
        if (this.session?.state !== 'connected' || e.checking || e.pending)
            return;
        const session = this.session, revision = e.revision;
        e.checking = true;
        e.error = '';
        this.onChange();
        try {
            const r = await this.api('subscription.status', { epoch: session.epoch, podcastId: id });
            if (this.session?.epoch !== session.epoch || this.entries.get(id) !== e || e.revision !== revision)
                return;
            this.accept(e, r, e.uncertain);
            e.checked = true;
        }
        catch (error) {
            if (this.entries.get(id) === e && e.revision === revision) {
                e.error = describeError(error);
                if (error instanceof APIError && error.code === 'UNAUTHORIZED')
                    this.onUnauthorized();
            }
        }
        finally {
            if (this.entries.get(id) === e) {
                e.checking = false;
                this.onChange();
            }
        }
    }
    async add(id) {
        const e = this.get(id);
        if (this.session?.state !== 'connected' || e.pending || e.state === 'subscribed' || (e.uncertain && !e.acknowledged))
            return;
        const session = this.session;
        e.pending = true;
        e.error = '';
        e.revision++;
        e.acknowledged = false;
        e.checked = false;
        this.onChange();
        try {
            const r = await this.api('subscription.add', { epoch: session.epoch, podcastId: id, requestId: `sub_${Date.now()}_${++this.serial}` });
            if (this.session?.epoch !== session.epoch || this.entries.get(id) !== e)
                return;
            this.accept(e, r, true);
        }
        catch (error) {
            if (this.entries.get(id) !== e)
                return;
            e.error = describeError(error);
            if (error instanceof APIError && error.code === 'UNAUTHORIZED')
                this.onUnauthorized();
            if (error instanceof APIError && error.code === 'SUBSCRIPTION_UNCERTAIN')
                e.uncertain = true;
        }
        finally {
            if (this.entries.get(id) === e) {
                e.pending = false;
                this.onChange();
            }
        }
    }
    render(id) {
        const e = this.get(id), root = el('div', 'subscription-controls');
        root.dataset.subscription = id;
        const connected = this.session?.state === 'connected';
        const add = button(e.pending ? '订阅中…' : e.state === 'subscribed' ? '已订阅' : e.uncertain ? '结果待确认' : e.state === 'unknown' ? '状态未知 · 订阅' : '＋ 订阅', () => this.add(id), 'button small primary-soft');
        add.disabled = !connected || e.pending || e.state === 'subscribed' || (e.uncertain && !e.acknowledged);
        root.append(add);
        if (e.state === 'unknown' || e.uncertain || e.error) {
            const check = button(e.checking ? '核对中…' : '核对状态', () => this.check(id), 'button small');
            check.disabled = !connected || e.checking || e.pending;
            root.append(check);
        }
        if (e.uncertain && e.checked && e.state !== 'subscribed' && !e.acknowledged)
            root.append(button('已核对，允许重新订阅', () => this.acknowledge(id), 'text-button'));
        if (e.uncertain && e.acknowledged)
            add.textContent = '重新尝试订阅';
        if (e.error)
            root.append(el('small', 'inline-warning', e.error));
        return root;
    }
}
