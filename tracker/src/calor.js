/*! Calor tracker | MIT */
(function (w, d) {
  'use strict';
  if (w.calor && w.calor.v) return;

  var script = d.currentScript || d.querySelector('script[data-site-id]');
  var cfg = w.CalorConfig || {};
  function opt(name, key) {
    return cfg[key] != null ? cfg[key] : script && script.getAttribute('data-' + name);
  }
  var site = opt('site-id', 'siteId');
  if (!site) {
    if (w.console) console.warn('[calor] data-site-id is required');
    return;
  }
  var endpoint = opt('endpoint', 'endpoint') ||
    (script && script.src ? new URL('/api/events', script.src).href : '/api/events');
  var FLUSH_MS = +opt('flush-interval', 'flushInterval') || 5000;
  var MAX_BATCH = +opt('max-batch', 'maxBatch') || 25;
  if (opt('respect-dnt', 'respectDnt') != null && (navigator.doNotTrack == '1' || w.doNotTrack == '1')) return;

  var CA = 'data-calor-component';
  var INTERACTIVE = 'a,button,input,select,textarea,label,summary,[role=button],[role=link],[role=tab],[role=menuitem],[role=checkbox],[onclick],[contenteditable],[tabindex]';
  var queue = [];
  var recent = [];
  var lastLinkNav = 0;
  var pv, maxDepth, sentDepth, sentMs, visibleMs, visibleSince;

  function now() { return Date.now(); }
  function rid() {
    var s = '';
    for (var i = 0; i < 4; i++) s += (Math.random() * 1679616 | 0).toString(36);
    return s + now().toString(36);
  }
  function page(u) { return String(u).split(/[?#]/)[0]; }
  function clip(s, n) {
    s = String(s || '').slice(0, 300).replace(/\s+/g, ' ').trim();
    return s.length > n ? s.slice(0, n - 1) + '\u2026' : s;
  }

  var sid;
  try {
    sid = sessionStorage.getItem('calor_sid');
    if (!sid) sessionStorage.setItem('calor_sid', (sid = rid()));
  } catch (e) {
    sid = rid();
  }
  var cur = page(location.href);

  function docW() { var e = d.documentElement, b = d.body; return Math.max(e.scrollWidth, b ? b.scrollWidth : 0, e.clientWidth); }
  function docH() { var e = d.documentElement, b = d.body; return Math.max(e.scrollHeight, b ? b.scrollHeight : 0, e.clientHeight); }
  function depth() { return Math.min(100, Math.round((w.scrollY + w.innerHeight) * 100 / docH())); }
  function engaged() { return visibleMs + (visibleSince ? now() - visibleSince : 0); }

  function push(ev) {
    ev._t = now();
    ev.u = cur;
    ev.p = pv;
    ev.vw = w.innerWidth;
    ev.vh = w.innerHeight;
    queue.push(ev);
    if (queue.length >= MAX_BATCH) flush();
  }

  function send(body) {
    try {
      if (navigator.sendBeacon && navigator.sendBeacon(endpoint, body)) return;
    } catch (e) {}
    try {
      fetch(endpoint, { method: 'POST', body: body, keepalive: true, mode: 'cors', credentials: 'omit', headers: { 'Content-Type': 'text/plain' } })['catch'](function () {});
    } catch (e) {}
  }

  // Batch-level u/p/vw/vh are defaults; events repeat them only when different.
  function flush() {
    if (!queue.length) return;
    var t = now(), evs = queue.splice(0, MAX_BATCH);
    var b = { v: 1, site: site, sid: sid, ts: t, u: cur, p: pv, vw: w.innerWidth, vh: w.innerHeight, e: evs };
    for (var i = 0; i < evs.length; i++) {
      var ev = evs[i];
      ev.d = ev._t - t;
      delete ev._t;
      for (var k in { u: 1, p: 1, vw: 1, vh: 1 }) if (ev[k] === b[k]) delete ev[k];
    }
    send(JSON.stringify(b));
    if (queue.length) flush();
  }

  function classes(el, n) {
    var c = el.getAttribute('class');
    return c ? c.trim().split(/\s+/).slice(0, n).join(' ') : '';
  }
  function esc(s) { return w.CSS && CSS.escape ? CSS.escape(s) : s; }

  function selector(el) {
    var parts = [];
    for (var n = el, i = 0; n && n.nodeType === 1 && i < 5; n = n.parentElement, i++) {
      var s = n.tagName.toLowerCase();
      if (n.id) { parts.unshift(s + '#' + esc(n.id)); break; }
      var c = classes(n, 2);
      if (c) s += '.' + c.split(' ').slice(0, 2).map(esc).join('.');
      var p = n.parentElement, same = 0, idx = 0;
      if (p) for (var k = p.firstElementChild; k; k = k.nextElementSibling) {
        if (k.tagName === n.tagName) { same++; if (k === n) idx = same; }
      }
      if (same > 1) s += ':nth-of-type(' + idx + ')';
      parts.unshift(s);
      if (s === 'body') break;
    }
    return parts.join('>');
  }

  function describe(el) {
    var tag = el.tagName.toLowerCase(), o = { sel: selector(el), tag: tag }, c = classes(el, 6);
    if (el.id) o.id = el.id;
    if (c) o.cls = c;
    var txt = /^(input|textarea|select)$/.test(tag)
      ? el.getAttribute('placeholder') || el.getAttribute('name') || el.getAttribute('type') || ''
      : el.textContent;
    txt = clip(txt, 80);
    if (txt) o.txt = txt;
    var aria = el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('alt');
    if (aria) o.aria = clip(aria, 80);
    var ca = el.closest('[' + CA + ']');
    if (ca) o.ca = clip(ca.getAttribute(CA), 60);
    return o;
  }

  // Component rules: [id, selector, page glob]. Embedded via
  // CalorConfig.components / data-components (JSON), else fetched once per
  // session; data-components="off" disables selector matching.
  var comps = [], CFG = 'calor_cfg';
  function glob(p) {
    return !p || new RegExp('^' + p.replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*') + '$').test(location.pathname);
  }
  function match(el) {
    var ids = [];
    for (var i = 0; i < comps.length; i++) {
      try {
        if (glob(comps[i][2]) && el.closest(comps[i][1])) ids.push(comps[i][0]);
      } catch (e) {}
    }
    return ids;
  }
  var emb = opt('components', 'components');
  if (emb && emb !== 'off') {
    try { comps = typeof emb == 'string' ? JSON.parse(emb) : emb; } catch (e) {}
  } else if (!emb && w.fetch) {
    try {
      var cached = JSON.parse(sessionStorage.getItem(CFG) || 0);
      if (cached && cached.s === site && now() - cached.t < 3e5) comps = cached.c;
    } catch (e) {}
    if (!cached || cached.s !== site || now() - cached.t >= 3e5) try {
      fetch(new URL('/api/sites/' + encodeURIComponent(site) + '/tracker-config', new URL(endpoint, location.href)).href, { credentials: 'omit' })
        .then(function (r) { return r.ok && r.json(); })
        .then(function (j) {
          if (!j) return;
          comps = j.c || [];
          try { sessionStorage.setItem(CFG, JSON.stringify({ s: site, t: now(), c: comps })); } catch (e) {}
        })['catch'](function () {});
    } catch (e) {}
  }

  function onClick(e) {
    var t = e.target;
    if (t && t.nodeType !== 1) t = t.parentElement;
    if (!t || !t.closest || t.closest('[data-calor-ignore]')) return;
    var hit = t.closest(INTERACTIVE), el = hit || t;
    var x = e.pageX, y = e.pageY;
    if (!e.detail && !e.clientX && !e.clientY) {
      var r = el.getBoundingClientRect();
      x = r.left + r.width / 2 + w.scrollX;
      y = r.top + r.height / 2 + w.scrollY;
    }
    x = Math.round(x);
    y = Math.round(y);
    var ts = now(), n = 1;
    recent = recent.filter(function (c) { return ts - c[2] < 1000; });
    for (var i = 0; i < recent.length; i++) {
      if (Math.abs(recent[i][0] - x) < 30 && Math.abs(recent[i][1] - y) < 30) n++;
    }
    recent.push([x, y, ts]);
    var ev = { t: 'c', x: x, y: y, dw: docW(), dh: docH(), el: describe(el) }, ids = match(t);
    if (ids.length) ev.cmp = ids;
    if (n >= 3) ev.rage = 1;
    if (!hit) ev.dead = 1;
    push(ev);
    e.__calor = [x, y, ev.dw, ev.dh];
  }

  // Bubble phase on window runs after page handlers, so SPA routers that
  // preventDefault() are recorded as pushState navigations instead.
  function onLink(e) {
    var pos = e.__calor;
    var a = pos && e.target.closest && e.target.closest('a[href]');
    if (!a || e.defaultPrevented || !/^https?:/.test(a.href) || page(a.href) === cur) return;
    push({ t: 'n', k: 'link', to: page(a.href), x: pos[0], y: pos[1], dw: pos[2], dh: pos[3] });
    // Same-tab: ship the page's scroll summary in this beacon so pagehide has nothing left to send.
    if (!(e.ctrlKey || e.metaKey || e.shiftKey || a.target === '_blank')) {
      lastLinkNav = now();
      endPV();
    }
    flush();
  }

  function startPV(kind, ref) {
    pv = rid();
    visibleMs = 0;
    visibleSince = d.visibilityState === 'visible' ? now() : 0;
    maxDepth = depth();
    sentDepth = sentMs = -1;
    var ev = { t: 'p', k: kind, sd: maxDepth };
    if (ref) ev.ref = page(ref);
    if (kind === 'load' && w.performance && performance.getEntriesByType) {
      var nav = performance.getEntriesByType('navigation')[0];
      if (nav && nav.redirectCount) ev.rc = nav.redirectCount;
    }
    push(ev);
  }

  function endPV() {
    var dd = Math.max(maxDepth, depth()), ms = engaged();
    maxDepth = dd;
    if (dd === sentDepth && ms - sentMs < 1000) return;
    sentDepth = dd;
    sentMs = ms;
    push({ t: 's', sd: dd, ms: ms });
  }

  function route(kind) {
    var u = page(location.href);
    if (u === cur) return;
    var from = cur;
    endPV();
    push({ t: 'n', k: kind, to: u });
    cur = u;
    startPV(kind, from);
  }

  ['pushState', 'replaceState'].forEach(function (m) {
    var orig = history[m];
    history[m] = function () {
      var r = orig.apply(this, arguments);
      route(m.toLowerCase());
      return r;
    };
  });

  var ticking = 0;
  w.addEventListener('scroll', function () {
    if (ticking) return;
    ticking = setTimeout(function () {
      ticking = 0;
      var dd = depth();
      if (dd > maxDepth) maxDepth = dd;
    }, 250);
  }, { passive: true });
  d.addEventListener('click', onClick, { capture: true, passive: true });
  w.addEventListener('click', onLink, { passive: true });
  w.addEventListener('popstate', function () { route('popstate'); });
  d.addEventListener('visibilitychange', function () {
    if (d.visibilityState === 'hidden') {
      if (visibleSince) visibleMs += now() - visibleSince;
      visibleSince = 0;
      endPV();
      flush();
    } else {
      visibleSince = now();
    }
  });
  w.addEventListener('pagehide', function () {
    if (now() - lastLinkNav > 3000) push({ t: 'n', k: 'unload' });
    endPV();
    flush();
  });
  w.addEventListener('pageshow', function (e) {
    if (e.persisted) startPV('load', '');
  });
  setInterval(flush, FLUSH_MS);

  startPV('load', d.referrer);
  w.calor = { v: 1, flush: flush, sessionId: sid, describe: describe, match: match };
})(window, document);
