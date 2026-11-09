'use client';

import { useActionState, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { SaveState } from '@/app/s/[site]/components/actions';
import type { ElementList, MatchPreview } from '@/lib/types';

export type EditorDraft = { name: string; selector: string; attrName: string; attrValue: string; pagePattern: string };

type Props = {
  site: string;
  initial: EditorDraft;
  pages: { url: string; path: string; clicks: number }[];
  query: Record<string, string>;
  action: (prev: SaveState, fd: FormData) => Promise<SaveState>;
  isNew: boolean;
};

const ATTR = 'data-calor-component';
const HOVER = '__calor-hover';
const MATCH = '__calor-match';
const pathOf = (u: string) => u.replace(/^https?:\/\/[^/]+/, '') || '/';

function attrSelector(name: string, value: string) {
  if (!name) return '';
  return value ? `[${name}="${value.replace(/["\\]/g, '\\$&')}"]` : `[${name}]`;
}

// ---- selector generation (limited to the subset the API accepts) ----

function classesOf(el: Element) {
  return [...el.classList].filter((c) => !c.startsWith('__calor-'));
}

function compound(el: Element, withNth = false) {
  const tag = el.tagName.toLowerCase();
  if (el.id) return `#${CSS.escape(el.id)}`;
  let s = tag + classesOf(el).slice(0, 2).map((c) => `.${CSS.escape(c)}`).join('');
  if (withNth && el.parentElement) {
    const same = [...el.parentElement.children].filter((k) => k.tagName === el.tagName);
    if (same.length > 1) s += `:nth-of-type(${same.indexOf(el) + 1})`;
  }
  return s;
}

function count(doc: Document, sel: string) {
  try {
    return doc.querySelectorAll(sel).length;
  } catch {
    return -1;
  }
}

/** "similar" matches every element shaped like el; "exact" narrows to el alone. */
function candidates(el: Element, doc: Document) {
  const own = compound(el);
  let similar = own;
  const parent = el.parentElement;
  if (!el.id && !classesOf(el).length && parent && parent !== doc.body) similar = `${compound(parent)} > ${own}`;
  else if (!el.id && parent && parent !== doc.body && (parent.id || classesOf(parent).length) && count(doc, own) > 12) similar = `${compound(parent)} ${own}`;

  const parts: string[] = [];
  let exact = '';
  for (let n: Element | null = el; n && n !== doc.documentElement && parts.length < 6; n = n.parentElement) {
    const plain = [compound(n), ...parts].join(' > ');
    if (n.id || count(doc, plain) === 1) {
      exact = plain;
      break;
    }
    parts.unshift(compound(n, true));
    exact = parts.join(' > ');
    if (count(doc, exact) === 1) break;
  }
  return { similar, exact };
}

function ancestry(el: Element, doc: Document) {
  const out: Element[] = [];
  for (let n: Element | null = el; n && n !== doc.body && n !== doc.documentElement && out.length < 7; n = n.parentElement) out.push(n);
  return out;
}

/** Parses a recorded tracker path ("body>main>div.ctas>a.btn") into parts. */
function recordedParts(sel: string) {
  return sel.split(/(?<!\\)>/).map((p) => p.trim()).filter(Boolean);
}

// ---- component ----

export default function ComponentEditor({ site, initial, pages, query, action, isNew }: Props) {
  const [state, formAction, saving] = useActionState(action, {});
  const [mode, setMode] = useState<'selector' | 'attribute'>(initial.attrName && !initial.selector ? 'attribute' : 'selector');
  const [name, setName] = useState(initial.name);
  const [selector, setSelector] = useState(initial.selector);
  const [attrName, setAttrName] = useState(initial.attrName || ATTR);
  const [attrValue, setAttrValue] = useState(initial.attrValue);
  const [pagePattern, setPagePattern] = useState(initial.pagePattern);

  const [preview, setPreview] = useState<MatchPreview | null>(null);
  const [previewError, setPreviewError] = useState('');
  const [previewing, setPreviewing] = useState(false);

  const [pickerUrl, setPickerUrl] = useState(pages[0]?.url || '');
  const [picking, setPicking] = useState(true);
  const [frameDoc, setFrameDoc] = useState<Document | null>(null);
  const [chain, setChain] = useState<Element[]>([]);
  const [root, setRoot] = useState(0);
  const [suggestAttr, setSuggestAttr] = useState('');
  const [pageCount, setPageCount] = useState<number | null>(null);
  const [recorded, setRecorded] = useState<ElementList['rows']>([]);
  const frame = useRef<HTMLIFrameElement>(null);

  const browserSelector = mode === 'selector' ? selector.trim() : attrSelector(attrName.trim(), attrValue.trim());
  const draft = useMemo(
    () => (mode === 'selector' ? { selector: selector.trim(), pagePattern: pagePattern.trim() } : { attrName: attrName.trim(), attrValue: attrValue.trim(), pagePattern: pagePattern.trim() }),
    [mode, selector, attrName, attrValue, pagePattern],
  );

  // Live match preview against recorded clicks, debounced.
  useEffect(() => {
    if (!browserSelector) {
      setPreview(null);
      setPreviewError('');
      return;
    }
    const ctl = new AbortController();
    const t = setTimeout(async () => {
      setPreviewing(true);
      try {
        const r = await fetch(`/api/calor/sites/${encodeURIComponent(site)}/components/preview?${new URLSearchParams(query)}`, { method: 'POST', body: JSON.stringify(draft), signal: ctl.signal });
        const j = await r.json();
        if (!r.ok) {
          setPreview(null);
          setPreviewError(j.error || r.statusText);
        } else {
          setPreview(j.preview);
          setPreviewError('');
        }
      } catch (e) {
        if ((e as Error).name !== 'AbortError') setPreviewError(String((e as Error).message || e));
      } finally {
        setPreviewing(false);
      }
    }, 300);
    return () => {
      clearTimeout(t);
      ctl.abort();
    };
  }, [site, query, draft, browserSelector]);

  // Recorded elements on the picker page: a fallback when the live page cannot load.
  useEffect(() => {
    if (!pickerUrl) return;
    const ctl = new AbortController();
    fetch(`/api/calor/sites/${encodeURIComponent(site)}/elements?${new URLSearchParams({ ...query, url: pickerUrl, limit: '25' })}`, { signal: ctl.signal })
      .then((r) => (r.ok ? r.json() : null))
      .then((j: ElementList | null) => setRecorded(j?.rows || []))
      .catch(() => {});
    return () => ctl.abort();
  }, [site, query, pickerUrl]);

  const choose = useCallback(
    (els: Element[], idx: number) => {
      const doc = frameDoc;
      if (!doc || !els[idx]) return;
      setChain(els);
      setRoot(idx);
      const c = candidates(els[idx], doc);
      setMode('selector');
      setSelector(c.similar);
      const tagged = els[idx].closest(`[${ATTR}]`);
      setSuggestAttr(tagged ? tagged.getAttribute(ATTR) || '' : '');
    },
    [frameDoc],
  );

  // Hover outline + click-to-pick inside the sanitized same-origin frame.
  useEffect(() => {
    const doc = frameDoc;
    if (!doc) return;
    let hovered: Element | null = null;
    const over = (e: Event) => {
      if (!picking) return;
      hovered?.classList.remove(HOVER);
      hovered = e.target as Element;
      hovered.classList?.add(HOVER);
    };
    const click = (e: Event) => {
      e.preventDefault();
      e.stopPropagation();
      if (!picking) return;
      const t = e.target as Element;
      if (!t || t.nodeType !== 1) return;
      choose(ancestry(t, doc), 0);
    };
    const block = (e: Event) => e.preventDefault();
    doc.addEventListener('mouseover', over, true);
    doc.addEventListener('click', click, true);
    doc.addEventListener('submit', block, true);
    return () => {
      hovered?.classList.remove(HOVER);
      doc.removeEventListener('mouseover', over, true);
      doc.removeEventListener('click', click, true);
      doc.removeEventListener('submit', block, true);
    };
  }, [frameDoc, picking, choose]);

  // Outline every element on the picker page that the current rule matches.
  useEffect(() => {
    const doc = frameDoc;
    if (!doc) return;
    doc.querySelectorAll(`.${MATCH}`).forEach((n) => n.classList.remove(MATCH));
    const applies = !pagePattern.trim() || new RegExp(`^${pagePattern.trim().replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*')}$`).test(pathOf(pickerUrl));
    if (!browserSelector) {
      setPageCount(null);
      return;
    }
    try {
      const found = doc.querySelectorAll(browserSelector);
      if (applies) found.forEach((n) => n.classList.add(MATCH));
      setPageCount(applies ? found.length : 0);
    } catch {
      setPageCount(-1);
    }
  }, [frameDoc, browserSelector, pagePattern, pickerUrl]);

  function onFrameLoad() {
    const doc = frame.current?.contentDocument || null;
    if (doc && doc.head) {
      const st = doc.createElement('style');
      st.textContent = `.${HOVER}{outline:2px solid #6366f1!important;outline-offset:2px;cursor:crosshair!important}.${MATCH}{outline:2px dashed #f97316!important;outline-offset:1px;background-color:rgba(249,115,22,.10)!important}*{cursor:crosshair!important}`;
      doc.head.appendChild(st);
    }
    setFrameDoc(doc);
    setChain([]);
  }

  const cands = frameDoc && chain[root] ? candidates(chain[root], frameDoc) : null;

  function pickRecorded(sel: string) {
    const parts = recordedParts(sel);
    const last = parts.length - 1;
    const plain = (p: string) => !/[#.]/.test(p.replace(/:nth-of-type\(\d+\)/, ''));
    setMode('selector');
    setSelector(plain(parts[last]) && last > 0 ? `${parts[last - 1]} > ${parts[last]}` : parts[last]);
  }

  return (
    <form action={formAction} className="editor">
      <input type="hidden" name="mode" value={mode} />
      <div className="editor-main">
        <section className="card">
          <header className="card-head">
            <div>
              <h2>{isNew ? 'New component' : 'Edit component'}</h2>
              <p className="muted">Clicks inside matching elements are tagged with this component. Existing clicks are tagged retroactively when you save.</p>
            </div>
          </header>
          <div className="form">
            <label>
              Name
              <input name="name" required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} placeholder="Hero call-to-action buttons" />
            </label>
            <div className="seg" role="tablist" aria-label="Rule type">
              <button type="button" role="tab" className={mode === 'selector' ? 'on' : ''} onClick={() => setMode('selector')}>
                CSS selector
              </button>
              <button type="button" role="tab" className={mode === 'attribute' ? 'on' : ''} onClick={() => setMode('attribute')}>
                Data attribute
              </button>
            </div>
            {mode === 'selector' ? (
              <label>
                CSS selector
                <input name="selector" value={selector} onChange={(e) => setSelector(e.target.value)} placeholder=".hero .ctas, #signup" spellCheck={false} className="mono" />
                <small className="muted">Supported: tag, #id, .class, [attr], [attr=value], :nth-of-type(n), descendant (space) and child (&gt;) combinators, comma lists.</small>
              </label>
            ) : (
              <div className="row-2">
                <label>
                  Attribute
                  <input name="attrName" value={attrName} onChange={(e) => setAttrName(e.target.value)} spellCheck={false} className="mono" />
                </label>
                <label>
                  Value
                  <input name="attrValue" value={attrValue} onChange={(e) => setAttrValue(e.target.value)} placeholder="newsletter" spellCheck={false} className="mono" />
                </label>
                <small className="muted span-all">
                  Add <code>{`${attrName || ATTR}="${attrValue || 'name'}"`}</code> to the element in your markup. The tracker records the closest <code>{ATTR}</code> on every click; other attribute names are matched in the browser only.
                </small>
              </div>
            )}
            <label>
              Pages (optional)
              <input name="pagePattern" value={pagePattern} onChange={(e) => setPagePattern(e.target.value)} placeholder="/products/*" spellCheck={false} className="mono" />
              <small className="muted">Path pattern; * matches anything. Leave empty to match on every page.</small>
            </label>
            <div className="row">
              <button className="btn primary" type="submit" disabled={saving}>
                {saving ? 'Saving…' : isNew ? 'Create component' : 'Save changes'}
              </button>
              {state.error && <p className="error">{state.error}</p>}
            </div>
          </div>
        </section>

        <section className="card" id="match-preview">
          <header className="card-head">
            <div>
              <h2>Match preview {previewing && <span className="spinner inline" />}</h2>
              <p className="muted">Recorded clicks in the selected date range that this rule would tag.</p>
            </div>
          </header>
          {previewError ? (
            <p className="error">{previewError}</p>
          ) : !preview ? (
            <p className="empty">Enter a selector or attribute, or pick an element on the page.</p>
          ) : (
            <>
              <div className="mini-kpis three">
                <div>
                  <b>{preview.matchedClicks.toLocaleString()}</b>
                  <span>matching clicks</span>
                </div>
                <div>
                  <b>{preview.share}%</b>
                  <span>of {preview.totalClicks.toLocaleString()} clicks</span>
                </div>
                <div>
                  <b>{preview.pages.length}</b>
                  <span>pages</span>
                </div>
              </div>
              {!preview.evaluable && <p className="note">This rule uses an attribute the tracker does not record, so past clicks cannot be matched; new clicks are tagged in the browser.</p>}
              {preview.elements.length > 0 && (
                <ul className="match-list">
                  {preview.elements.slice(0, 6).map((e, i) => (
                    <li key={i}>
                      <span>
                        <b>{e.label}</b> <code className="muted">{pathOf(e.pageUrl)}</code>
                        <code className="selector">{e.selector}</code>
                      </span>
                      <span className="num">{e.clicks.toLocaleString()}</span>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </section>
      </div>

      <section className="card picker">
        <header className="card-head">
          <div>
            <h2>Pick an element</h2>
            <p className="muted">Hover and click an element on the live page to generate a selector. The page is loaded without scripts.</p>
          </div>
          <div className="card-actions">
            <select value={pickerUrl} onChange={(e) => setPickerUrl(e.target.value)} aria-label="Page">
              {pages.map((p) => (
                <option key={p.url} value={p.url}>
                  {p.path} ({p.clicks.toLocaleString()} clicks)
                </option>
              ))}
            </select>
            <label className="check">
              <input type="checkbox" checked={picking} onChange={(e) => setPicking(e.target.checked)} /> Pick mode
            </label>
          </div>
        </header>
        {chain.length > 0 && cands && (
          <div className="pick-result">
            <div className="chips-row">
              <span className="muted small">Component root:</span>
              {chain.map((el, i) => (
                <button type="button" key={i} className={`chip ${i === root ? 'on' : ''}`} onClick={() => choose(chain, i)} title="Use this ancestor as the component">
                  <code>{compound(el)}</code>
                </button>
              ))}
            </div>
            <div className="chips-row">
              <span className="muted small">Selector:</span>
              <button type="button" className={`chip ${selector === cands.similar ? 'on' : ''}`} onClick={() => setSelector(cands.similar)}>
                All similar <code>{cands.similar}</code> · {count(frameDoc!, cands.similar)}
              </button>
              {cands.exact !== cands.similar && (
                <button type="button" className={`chip ${selector === cands.exact ? 'on' : ''}`} onClick={() => setSelector(cands.exact)}>
                  Only this one <code>{cands.exact}</code> · {count(frameDoc!, cands.exact)}
                </button>
              )}
              {suggestAttr && (
                <button
                  type="button"
                  className="chip attr"
                  onClick={() => {
                    setMode('attribute');
                    setAttrName(ATTR);
                    setAttrValue(suggestAttr);
                  }}
                >
                  Use attribute <code>{`${ATTR}="${suggestAttr}"`}</code>
                </button>
              )}
            </div>
          </div>
        )}
        <p className="muted small" id="picker-count">
          {pageCount === null ? 'No rule yet.' : pageCount < 0 ? 'The selector is not valid CSS.' : `${pageCount} element${pageCount === 1 ? ' on this page matches' : 's on this page match'} the current rule (dashed outline).`}
        </p>
        <div className="picker-layout">
          <div className="picker-stage">
            {pickerUrl ? (
              <iframe ref={frame} key={pickerUrl} title="Page picker" src={`/api/calor-frame?${new URLSearchParams({ site, url: pickerUrl })}`} sandbox="allow-same-origin" onLoad={onFrameLoad} />
            ) : (
              <p className="empty">No tracked pages yet.</p>
            )}
          </div>
          <aside className="recorded">
            <h3 className="mini">Recorded elements on this page</h3>
            <p className="muted small">Or build the rule from an element visitors clicked.</p>
            <ul>
              {recorded.map((r, i) => (
                <li key={i}>
                  <button type="button" onClick={() => pickRecorded(r.selector)} title={r.selector}>
                    <b>{r.label}</b>
                    <code className="selector">{r.selector}</code>
                    <span className="muted small">{r.clicks.toLocaleString()} clicks</span>
                  </button>
                </li>
              ))}
              {!recorded.length && <li className="empty">No clicks recorded in this range.</li>}
            </ul>
          </aside>
        </div>
      </section>
    </form>
  );
}
