import { el, button, cover } from './dom.js';
import { formatDate } from './util.js';
export function renderItemCard(it, actions, local, updates = false, podcastContext = '节目') {
    const card = el('article', `episode-card${it.kind === 'podcast' ? ' podcast-card' : ''}`);
    card.dataset.id = it.id;
    const art = cover(it, true);
    if (updates && it.podcastId) {
        const link = button('', () => actions.podcast(it), 'cover-link');
        link.setAttribute('aria-label', `查看节目 ${it.podcastTitle ?? ''}`);
        link.append(art);
        card.append(link);
    }
    else
        card.append(art);
    const content = el('div', 'episode-content');
    const over = updates && it.podcastId ? button(it.podcastTitle ?? '查看节目', () => actions.podcast(it), 'episode-meta text-button') : el('div', 'episode-meta', it.kind === 'podcast' ? podcastContext : (it.podcastTitle ?? '播客单集'));
    content.append(over, button(it.title, () => actions.details(it), 'episode-title'), el('p', 'episode-description', it.description ?? ''));
    const meta = [formatDate(it.published), it.duration ? `${Math.round(it.duration / 60)} 分钟` : '', it.restricted ? '受限内容' : ''].filter(Boolean).join('  ·  ');
    content.append(el('small', 'muted', meta));
    card.append(content);
    const controls = el('div', 'card-actions');
    if (it.kind === 'episode')
        controls.append(button('▶ 播放', () => actions.play(it), 'button small primary-soft'));
    if (updates && it.kind === 'episode')
        controls.append(button('＋ 稍后听', () => actions.queue('append', it).catch(e => actions.notice(e)), 'button small'));
    const more = document.createElement('details');
    more.className = 'more-menu';
    const summary = el('summary', '', '···');
    summary.setAttribute('aria-label', `${it.title} 的更多操作`);
    more.append(summary);
    const menu = el('div', 'menu-panel');
    const act = (text, fn) => menu.append(button(text, () => { more.open = false; void fn().catch(e => actions.notice(e)); }, 'menu-item'));
    if (it.kind === 'episode') {
        act('加入稍后听', () => actions.queue('append', it));
        act('下一集播放', () => actions.queue('next', it));
    }
    if (local === 'queue')
        act('从队列移除', () => actions.queue('remove', it));
    act(local === 'bookmarks' ? '移除本地书签' : '保存本地书签', () => actions.bookmark(local === 'bookmarks' ? 'remove' : 'append', it));
    act('打开官方原页', () => actions.external(it));
    more.append(menu);
    controls.append(more);
    card.append(controls);
    return card;
}
