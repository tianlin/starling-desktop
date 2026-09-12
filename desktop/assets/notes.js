import { el, button } from './dom.js';
import { parseTimestamp, isPublicExternal } from './util.js';
const allowed = new Set(['P', 'DIV', 'SPAN', 'BR', 'STRONG', 'B', 'EM', 'I', 'UL', 'OL', 'LI', 'H1', 'H2', 'H3', 'H4', 'BLOCKQUOTE', 'PRE', 'CODE', 'HR']);
const blocked = new Set(['SCRIPT', 'STYLE', 'IFRAME', 'OBJECT', 'EMBED', 'SVG', 'MATH', 'IMG', 'VIDEO', 'AUDIO', 'FORM', 'INPUT', 'LINK', 'META', 'BASE']);
// Parse into an inert template, then build a fresh allowlisted tree. No untrusted attribute is copied.
export function renderNotes(html, onTime, onLink) {
    const target = el('div', 'shownotes');
    const template = document.createElement('template');
    template.innerHTML = html.slice(0, 200000);
    let count = 0;
    function text(s, parent) {
        const parts = s.split(/(\b\d{1,3}:\d{2}(?::\d{2})?\b)/g);
        for (const part of parts) {
            const time = parseTimestamp(part);
            if (time !== null)
                parent.appendChild(button(part, () => onTime(time), 'time-link'));
            else
                parent.appendChild(document.createTextNode(part));
        }
    }
    function visit(n, parent, depth) {
        if (++count > 5000 || depth > 30)
            return;
        if (n.nodeType === Node.TEXT_NODE) {
            text(n.textContent ?? '', parent);
            return;
        }
        if (!(n instanceof HTMLElement))
            return;
        const tag = n.tagName;
        if (blocked.has(tag))
            return;
        let out = parent;
        if (tag === 'A') {
            const href = n.getAttribute('href') ?? '';
            if (isPublicExternal(href)) {
                out = button(n.textContent ?? '打开外链', () => onLink(href), 'notes-link');
                parent.appendChild(out);
                return;
            }
        }
        else if (allowed.has(tag)) {
            out = document.createElement(tag.toLowerCase());
            parent.appendChild(out);
        }
        for (const child of n.childNodes)
            visit(child, out, depth + 1);
    }
    for (const n of template.content.childNodes)
        visit(n, target, 0);
    return target;
}
