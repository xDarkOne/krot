// Общие помощники окна. Данные с узлов — только как текст (textContent).

export const $ = (id) => document.getElementById(id);

export function el(tag, props = {}, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(props)) {
        if (k === 'class') e.className = v;
        else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
        else if (v !== undefined && v !== null) e.setAttribute(k, v);
    }
    for (const k of kids.flat()) {
        if (k === null || k === undefined || k === false) continue;
        e.append(k instanceof Node ? k : document.createTextNode(String(k)));
    }
    return e;
}

export function toast(text, bad = false) {
    const t = el('div', {class: bad ? 'bad' : ''}, text);
    $('log').append(t);
    setTimeout(() => t.remove(), bad ? 9000 : 4500);
}

export const errText = (e) => (typeof e === 'string' ? e : (e && e.message) || String(e));
