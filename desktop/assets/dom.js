export function el(tag, className = '', text = '') { const node = document.createElement(tag); node.className = className; node.textContent = text; return node; }
export function button(text, fn, className = 'button') { const b = el('button', className, text); b.type = 'button'; b.addEventListener('click', () => { void fn(); }); return b; }
export function icon(text, label, fn) { const b = button(text, fn, 'icon-button'); b.title = label; b.setAttribute('aria-label', label); return b; }
export function cover(it, small = false) {
    const n = Array.from(it.id).reduce((v, c) => v + c.charCodeAt(0), 0) % 6;
    const art = el('div', `cover color-${n}${small ? ' small-cover' : ''}`);
    art.setAttribute('aria-hidden', 'true');
    const u = it.image;
    if (u && /^https:\/\/(image|bts-image|media)\.xyzcdn\.net\//.test(u)) {
        const img = el('img');
        img.src = u;
        img.alt = '';
        img.loading = 'lazy';
        img.referrerPolicy = 'no-referrer';
        img.addEventListener('error', () => { img.remove(); art.textContent = (it.podcastTitle || it.title).slice(0, 2) || '♪'; });
        art.append(img);
    }
    else {
        art.append(el('span', 'cover-word', (it.podcastTitle || it.title).slice(0, 4) || '声'));
        art.append(el('i', 'cover-orbit'));
    }
    return art;
}
export function input(label, type = 'text', placeholder = '') { const box = el('label', 'field'); box.append(el('span', 'field-label', label)); const field = el('input', 'text-input'); field.type = type; field.placeholder = placeholder; box.append(field); return { label: box, input: field }; }
export function empty(title, description, action) {
    const box = el('div', 'empty');
    box.append(el('div', 'empty-mark', '◌'), el('h2', '', title), el('p', '', description));
    if (action)
        box.append(action);
    return box;
}
