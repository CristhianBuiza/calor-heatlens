// Demo sites used by `make demo`, the e2e check and the seed script.
// Each site is served on its own *.localhost host so the API's per-site
// Origin checks are exercised exactly as in production.

const css = (t) => `
*{box-sizing:border-box}body{margin:0;font-family:${t.font};color:${t.ink};background:${t.bg};line-height:1.55}
a{color:inherit}.wrap{max-width:1120px;margin:0 auto;padding:0 32px}
header.top{position:sticky;top:0;z-index:5;background:${t.headerBg};border-bottom:1px solid ${t.line};backdrop-filter:blur(6px)}
header.top .wrap{display:flex;align-items:center;gap:28px;height:68px}
.logo{font-weight:800;font-size:20px;text-decoration:none;letter-spacing:-.02em;display:flex;align-items:center;gap:10px}
.logo i{width:28px;height:28px;border-radius:8px;background:${t.accent};display:inline-block}
nav.main{display:flex;gap:22px;flex:1}nav.main a{text-decoration:none;color:${t.muted};font-weight:500}nav.main a:hover{color:${t.ink}}
.btn{display:inline-flex;align-items:center;gap:8px;padding:12px 20px;border-radius:${t.radius};font-weight:650;text-decoration:none;border:0;cursor:pointer;font-size:15px;font-family:inherit}
.btn.primary{background:${t.accent};color:${t.onAccent}}.btn.ghost{background:transparent;border:1.5px solid ${t.line};color:${t.ink}}
.btn.small{padding:8px 14px;font-size:14px}
.hero{padding:88px 0 72px}.hero h1{font-size:54px;line-height:1.08;letter-spacing:-.03em;margin:0 0 18px;max-width:760px;font-family:${t.display}}
.hero p.lead{font-size:20px;color:${t.muted};max-width:620px;margin:0 0 30px}.hero .ctas{display:flex;gap:14px}
.eyebrow{text-transform:uppercase;letter-spacing:.14em;font-size:12px;font-weight:700;color:${t.accent};margin-bottom:14px}
section{padding:56px 0}section h2{font-size:32px;letter-spacing:-.02em;margin:0 0 26px;font-family:${t.display}}
.grid{display:grid;gap:22px}.g3{grid-template-columns:repeat(3,1fr)}.g2{grid-template-columns:repeat(2,1fr)}.g4{grid-template-columns:repeat(4,1fr)}
.card{background:${t.card};border:1px solid ${t.line};border-radius:${t.radius};padding:24px}
.card h3{margin:0 0 8px;font-size:19px}.card p{margin:0 0 14px;color:${t.muted}}
.thumb{height:170px;border-radius:calc(${t.radius} - 4px);margin:-8px -8px 18px;background:linear-gradient(135deg,${t.thumbA},${t.thumbB})}
.price{font-size:20px;font-weight:750;margin-bottom:14px}
.band{background:${t.band};color:${t.bandInk}}.band p{color:inherit;opacity:.8}
.muted{color:${t.muted}}.row{display:flex;gap:12px;align-items:center}
input.field{padding:12px 14px;border-radius:${t.radius};border:1.5px solid ${t.line};font:inherit;min-width:280px;background:${t.card};color:${t.ink}}
footer{padding:44px 0 60px;border-top:1px solid ${t.line};color:${t.muted};font-size:14px}footer .wrap{display:flex;justify-content:space-between;gap:20px}
footer a{margin-right:18px}
article.post{max-width:720px;margin:0 auto;padding:56px 32px}article.post h1{font-size:46px;line-height:1.1;font-family:${t.display};letter-spacing:-.02em}
article.post p{font-size:19px;margin:0 0 22px}article.post h2{font-family:${t.display};font-size:28px;margin:40px 0 14px}
.cover{height:360px;border-radius:${t.radius};background:linear-gradient(160deg,${t.thumbA},${t.thumbB});margin:28px 0 36px}
.chips{display:flex;gap:10px;margin-bottom:24px}.chip{padding:8px 14px;border-radius:999px;border:1.5px solid ${t.line};background:${t.card};font:inherit;cursor:pointer}
.chip.on{background:${t.ink};color:${t.bg};border-color:${t.ink}}
details{border-bottom:1px solid ${t.line};padding:16px 0}summary{cursor:pointer;font-weight:600;font-size:17px}
.tabs{display:flex;gap:6px;border-bottom:1px solid ${t.line};margin-bottom:24px}.tabs a{padding:12px 16px;text-decoration:none;color:${t.muted};border-bottom:2px solid transparent}
.tabs a.on{color:${t.ink};border-color:${t.accent};font-weight:600}
.kpi{font-size:30px;font-weight:750}.bar{height:10px;border-radius:5px;background:${t.accent};opacity:.85}
.quote{font-size:22px;font-family:${t.display};max-width:760px}
.logos{display:flex;gap:44px;opacity:.55;font-weight:800;font-size:20px;letter-spacing:.02em}
`;

const page = (site, title, body, endpoint) => `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>${title} · ${site.name}</title><style>${css(site.theme)}</style>
<script src="/calor.min.js" data-site-id="${site.id}" data-endpoint="${endpoint}" defer></script>
</head><body>
<header class="top"><div class="wrap">
<a class="logo" href="/"><i></i>${site.name}</a>
<nav class="main" aria-label="Main">${site.nav.map(([href, label]) => `<a href="${href}">${label}</a>`).join('')}</nav>
${site.headerCta}
</div></header>
${body}
<footer><div class="wrap"><div>© 2026 ${site.name}. ${site.tagline}</div>
<div>${site.footer.map(([href, label]) => `<a href="${href}">${label}</a>`).join('')}</div></div></footer>
${site.extraScript || ''}
</body></html>`;

const lorem = [
  'The morning light arrives slowly here, first on the terracotta roofs and then on the river, and by eight the bakeries have already sold their first trays.',
  'We walked without a plan, which turned out to be the plan. Every lane seemed to bend toward another viewpoint, another tiled facade, another reason to stop.',
  'There is a particular pleasure in ordering the same thing every day at the same counter until the owner starts it before you reach the front of the line.',
  'Getting there is easy: regional trains run every hour, the tickets cost less than a coffee and a pastry, and the views are worth the slow pace.',
  'Pack lighter than you think you need to. Laundromats are everywhere, the hills are steep, and you will want room for tins of sardines and ceramics.',
  'By the third day the city had stopped feeling like a destination and started feeling like a neighbourhood we happened to be borrowing for a while.',
];
const paras = (n, off = 0) => Array.from({ length: n }, (_, i) => `<p>${lorem[(i + off) % lorem.length]}</p>`).join('');

// ---------------------------------------------------------------- Acme Coffee
const acme = {
  id: 'acme-coffee',
  name: 'Acme Coffee Roasters',
  host: 'acme.localhost',
  tagline: 'Roasted in Portland, OR.',
  theme: { font: 'Inter,system-ui,sans-serif', display: 'Georgia,serif', ink: '#2b1d14', muted: '#7a6455', bg: '#fbf6f0', card: '#fff', line: '#eadfd3', accent: '#b4532a', onAccent: '#fff', headerBg: 'rgba(251,246,240,.92)', band: '#2b1d14', bandInk: '#fbf6f0', radius: '14px', thumbA: '#c98b5e', thumbB: '#5a3420' },
  nav: [['/shop', 'Shop'], ['/subscribe', 'Subscriptions'], ['/brew-guides', 'Brew guides'], ['/about', 'About']],
  headerCta: '<a class="btn small ghost" href="/cart" aria-label="Cart">Cart (2)</a>',
  footer: [['/about', 'Our story'], ['https://instagram.com/acmecoffee', 'Instagram'], ['/contact', 'Wholesale']],
};
const slug = (name) => name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
const product = (name, notes, price) => `<div class="card"><div class="thumb"></div><h3><a href="/products/${slug(name)}" style="text-decoration:none">${name}</a></h3><p>${notes}</p>
<div class="row" style="justify-content:space-between"><span class="price">$${price}</span><button class="btn primary small add-to-cart">Add to cart</button></div></div>`;
acme.pages = {
  '/': ['Fresh roasted coffee', `
<div class="wrap hero"><div class="eyebrow">Roasted Tuesday · at your door Thursday</div>
<h1>Small-batch coffee for people who care about the cup.</h1>
<p class="lead">Single-origin beans sourced directly from 14 farms, roasted in small batches and shipped within 48 hours.</p>
<div class="ctas"><a class="btn primary" href="/shop">Shop coffee</a><a class="btn ghost" href="/subscribe">Start a subscription</a></div></div>
<section><div class="wrap"><h2>Bestsellers</h2><div class="grid g3">
${product('Ethiopia Guji', 'Peach, jasmine, bergamot', 19)}${product('Colombia Huila', 'Red apple, caramel, cocoa', 17)}${product('House Espresso', 'Dark chocolate, hazelnut', 16)}
</div></div></section>
<section class="band"><div class="wrap"><div class="grid g3">
<div><h3>Direct trade</h3><p>We pay farmers 2-3x Fair Trade minimums and visit every partner yearly.</p></div>
<div><h3>Roasted to order</h3><p>Nothing sits on a shelf. Your beans are roasted the week they ship.</p></div>
<div><h3>Free shipping over $35</h3><p>Carbon-neutral delivery anywhere in the US, always free for subscribers.</p></div>
</div></div></section>
<section><div class="wrap"><p class="quote">“The Guji changed how I think about coffee. It tastes like a peach orchard.”</p><p class="muted">— Dana R., subscriber since 2023</p></div></section>
<section><div class="wrap card"><h2>Get 15% off your first bag</h2><p class="muted">Brew tips, new arrivals and roaster's notes, twice a month.</p>
<form class="row" onsubmit="event.preventDefault()"><input class="field" type="email" placeholder="you@example.com" name="email"><button class="btn primary" type="submit">Subscribe</button></form></div></section>`],
  '/shop': ['Shop', `
<div class="wrap" style="padding-top:48px"><h1 style="font-family:Georgia,serif;font-size:42px;margin:0 0 20px">All coffee</h1>
<div class="chips"><button class="chip on">All</button><button class="chip">Light roast</button><button class="chip">Medium roast</button><button class="chip">Dark roast</button><button class="chip">Decaf</button></div>
<div class="grid g3">${product('Ethiopia Guji', 'Peach, jasmine, bergamot', 19)}${product('Colombia Huila', 'Red apple, caramel, cocoa', 17)}${product('House Espresso', 'Dark chocolate, hazelnut', 16)}
${product('Kenya Nyeri', 'Blackcurrant, grapefruit', 21)}${product('Guatemala Antigua', 'Brown sugar, orange', 17)}${product('Swiss Water Decaf', 'Milk chocolate, almond', 18)}</div>
<section><a class="btn ghost" href="/subscribe">Save 10% with a subscription →</a></section></div>`],
  '/subscribe': ['Subscriptions', `
<div class="wrap hero" style="padding-bottom:24px"><div class="eyebrow">Never run out</div><h1>Coffee on your schedule.</h1><p class="lead">Pause, skip or cancel any time. Subscribers save 10% and ship free.</p></div>
<section style="padding-top:0"><div class="wrap grid g3">
<div class="card"><h3>Explorer</h3><p>A different single origin every delivery.</p><div class="price">$17 / bag</div><button class="btn primary choose-plan">Choose Explorer</button></div>
<div class="card" style="border-color:#b4532a;border-width:2px"><h3>Roaster's choice</h3><p>Our favourite coffee of the month, two bags.</p><div class="price">$32 / delivery</div><button class="btn primary choose-plan">Choose Roaster's choice</button></div>
<div class="card"><h3>Espresso lover</h3><p>House Espresso, every two weeks.</p><div class="price">$15 / bag</div><button class="btn primary choose-plan">Choose Espresso lover</button></div>
</div></section>
<section><div class="wrap"><h2>Questions</h2><details><summary>Can I pause my subscription?</summary><p>Yes, from your account page, any time.</p></details>
<details><summary>Whole bean or ground?</summary><p>Both. Choose your grind per delivery.</p></details><details><summary>When will I be charged?</summary><p>The day your coffee is roasted.</p></details></div></section>`],
  '/brew-guides': ['Brew guides', `<article class="post"><div class="eyebrow">Brew guide</div><h1>The 4-minute pour-over that never fails</h1><div class="cover"></div>
${paras(4)}<h2>What you need</h2>${paras(3, 2)}<h2>Step by step</h2>${paras(5, 1)}<p><a class="btn primary" href="/shop">Shop pour-over coffees</a></p></article>`],
  '/about': ['About', `<article class="post"><div class="eyebrow">Our story</div><h1>Started in a garage in 2014.</h1><div class="cover"></div>${paras(6)}</article>`],
  '/cart': ['Cart', `<div class="wrap" style="padding:56px 32px"><h1 style="font-family:Georgia,serif">Your cart</h1>
<div class="card" style="max-width:640px"><div class="row" style="justify-content:space-between"><span>Ethiopia Guji × 1</span><b>$19.00</b></div><hr style="border:0;border-top:1px solid #eadfd3;margin:16px 0">
<div class="row" style="justify-content:space-between"><span>House Espresso × 1</span><b>$16.00</b></div><hr style="border:0;border-top:1px solid #eadfd3;margin:16px 0">
<div class="row" style="justify-content:space-between;margin-bottom:20px"><span class="muted">Free shipping applied</span><b>$35.00</b></div>
<div class="row"><button class="btn primary checkout">Checkout</button><a class="btn ghost" href="/shop">Keep shopping</a></div></div></div>`],
};

const PRODUCTS = [
  ['Ethiopia Guji', 'Peach, jasmine, bergamot', 19, 'Light'],
  ['Colombia Huila', 'Red apple, caramel, cocoa', 17, 'Medium'],
  ['House Espresso', 'Dark chocolate, hazelnut', 16, 'Dark'],
  ['Kenya Nyeri', 'Blackcurrant, grapefruit', 21, 'Light'],
  ['Guatemala Antigua', 'Brown sugar, orange', 17, 'Medium'],
  ['Swiss Water Decaf', 'Milk chocolate, almond', 18, 'Medium'],
];
for (const [name, notes, price, roast] of PRODUCTS) {
  acme.pages[`/products/${slug(name)}`] = [name, `
<div class="wrap" style="padding-top:48px;display:grid;grid-template-columns:1.1fr 1fr;gap:48px">
<div class="cover" style="margin:0;height:520px"></div>
<div><div class="eyebrow">${roast} roast · 12 oz</div><h1 style="font-family:Georgia,serif;font-size:44px;margin:0 0 10px">${name}</h1>
<p class="lead muted" style="font-size:19px">${notes}. Roasted to order every Tuesday.</p><div class="price" style="font-size:28px">$${price}.00</div>
<div class="chips"><button class="chip on">Whole bean</button><button class="chip">Drip</button><button class="chip">Espresso</button><button class="chip">French press</button></div>
<div class="row" style="margin-bottom:18px"><button class="btn primary add-to-cart">Add to cart</button><a class="btn ghost" href="/subscribe">Subscribe &amp; save 10%</a></div>
<details><summary>Tasting notes</summary><p>${notes}, with a long sweet finish.</p></details><details><summary>Brewing recommendations</summary><p>Pour-over: 1:16, 94°C. Espresso: 18g in, 38g out.</p></details>
<details><summary>Shipping &amp; returns</summary><p>Free shipping over $35. Not in love? We'll replace it.</p></details></div></div>
<section><div class="wrap"><h2>You might also like</h2><div class="grid g3">${PRODUCTS.filter((p) => p[0] !== name).slice(0, 3).map((p) => product(p[0], p[1], p[2])).join('')}</div></div></section>`];
}
acme.pages['/contact'] = ['Wholesale', `<div class="wrap" style="max-width:640px;padding:72px 32px"><h1 style="font-family:Georgia,serif;font-size:40px">Wholesale &amp; cafés</h1><p class="lead muted">We roast for 40+ cafés and offices in the Pacific Northwest.</p>
<form class="card" style="display:flex;flex-direction:column;gap:14px" onsubmit="event.preventDefault()"><input class="field" placeholder="Business name" name="business"><input class="field" placeholder="Email" name="email"><input class="field" placeholder="Monthly volume (lbs)" name="volume"><button class="btn primary" type="submit">Request a price list</button></form></div>`];

// ---------------------------------------------------------------- Nimbus SaaS
const nimbus = {
  id: 'nimbus-analytics',
  name: 'Nimbus',
  host: 'nimbus.localhost',
  tagline: 'Dashboards for product teams.',
  theme: { font: 'Inter,system-ui,sans-serif', display: 'Inter,system-ui,sans-serif', ink: '#0f172a', muted: '#55627a', bg: '#ffffff', card: '#f8fafc', line: '#e2e8f0', accent: '#4f46e5', onAccent: '#fff', headerBg: 'rgba(255,255,255,.92)', band: '#0f172a', bandInk: '#e2e8f0', radius: '10px', thumbA: '#818cf8', thumbB: '#312e81' },
  nav: [['/product', 'Product'], ['/pricing', 'Pricing'], ['/docs', 'Docs'], ['/app', 'Live demo']],
  headerCta: '<a class="btn small ghost" href="/login">Log in</a><a class="btn small primary" href="/signup">Start free trial</a>',
  footer: [['/docs', 'Documentation'], ['https://status.nimbus.dev', 'Status'], ['https://github.com/nimbus', 'GitHub']],
};
nimbus.pages = {
  '/': ['Product analytics dashboards', `
<div class="wrap hero"><div class="eyebrow">New · AI summaries for every chart</div>
<h1>Ship dashboards your team actually opens.</h1>
<p class="lead">Nimbus connects to your warehouse in minutes and turns SQL into living dashboards, alerts and weekly digests.</p>
<div class="ctas"><a class="btn primary" href="/signup">Start free trial</a><a class="btn ghost" href="/demo">Book a demo</a></div></div>
<section style="padding-top:0"><div class="wrap logos"><span>NORTHWIND</span><span>Globex</span><span>INITECH</span><span>Umbrella</span><span>Hooli</span></div></section>
<section><div class="wrap grid g3">
<div class="card"><h3>Warehouse-native</h3><p>Snowflake, BigQuery, Postgres and DuckDB. No copies, no ETL.</p><a href="/docs">Read the docs →</a></div>
<div class="card"><h3>Alerts that matter</h3><p>Anomaly detection on any metric, routed to Slack or PagerDuty.</p><a href="/product">See alerts →</a></div>
<div class="card"><h3>Weekly digests</h3><p>Plain-language summaries of what changed and why.</p><a href="/product">See digests →</a></div>
</div></section>
<section class="band"><div class="wrap"><div class="grid g4">
<div><div class="kpi">4,200+</div><p>teams</p></div><div><div class="kpi">11 min</div><p>median setup</p></div><div><div class="kpi">99.98%</div><p>uptime</p></div><div><div class="kpi">SOC 2</div><p>Type II</p></div></div></div></section>
<section><div class="wrap row" style="justify-content:space-between"><div><h2 style="margin:0">Free for 14 days. No card required.</h2><p class="muted">Plans from $29/month after your trial.</p></div>
<div class="row"><a class="btn ghost" href="/pricing">See pricing</a><a class="btn primary" href="/signup">Start free trial</a></div></div></section>`],
  '/pricing': ['Pricing', `
<div class="wrap hero" style="padding-bottom:20px;text-align:center"><h1 style="margin:0 auto 14px">Simple pricing that scales with you</h1><p class="lead" style="margin:0 auto 26px">Every plan includes unlimited viewers.</p>
<div class="chips" style="justify-content:center"><button class="chip on" role="tab">Monthly</button><button class="chip" role="tab">Yearly · save 20%</button></div></div>
<section style="padding-top:0"><div class="wrap grid g3">
<div class="card"><h3>Starter</h3><p>For small teams getting started.</p><div class="price">$29 / mo</div><a class="btn ghost" href="/signup">Start trial</a></div>
<div class="card" style="border:2px solid #4f46e5"><h3>Team</h3><p>Alerts, digests and 20 editors.</p><div class="price">$99 / mo</div><a class="btn primary" href="/signup">Start trial</a></div>
<div class="card"><h3>Enterprise</h3><p>SSO, audit logs, dedicated support.</p><div class="price">Custom</div><a class="btn ghost" href="/demo">Contact sales</a></div>
</div></section>
<section><div class="wrap" style="max-width:780px"><h2>Frequently asked questions</h2>
<details><summary>What counts as an editor?</summary><p>Anyone who can create or change dashboards.</p></details>
<details><summary>Can I self-host Nimbus?</summary><p>Enterprise plans include a self-hosted option.</p></details>
<details><summary>Do you offer discounts for startups?</summary><p>Yes, 50% off for the first year.</p></details>
<details><summary>How does billing work?</summary><p>Monthly or yearly, cancel any time.</p></details></div></section>`],
  '/docs': ['Docs', `<div class="wrap" style="display:grid;grid-template-columns:220px 1fr;gap:40px;padding-top:40px">
<aside style="position:sticky;top:90px;align-self:start;display:flex;flex-direction:column;gap:10px"><b>Getting started</b><a href="/docs">Quickstart</a><a href="/docs">Connect a warehouse</a><a href="/docs">Your first dashboard</a><b style="margin-top:12px">Guides</b><a href="/docs">Alerts</a><a href="/docs">Digests</a><a href="/docs">Embedding</a></aside>
<article class="post" style="margin:0;padding:0 0 60px"><h1>Quickstart</h1>${paras(3)}<h2>1. Connect your warehouse</h2>${paras(3, 1)}<h2>2. Write a query</h2>${paras(3, 2)}<h2>3. Share it</h2>${paras(3, 3)}<p><a class="btn primary" href="/signup">Create your workspace</a></p></article></div>`],
  '/signup': ['Start your free trial', `<div class="wrap" style="max-width:520px;padding:72px 32px"><h1 style="font-size:36px;margin:0 0 8px">Start your free trial</h1><p class="muted">14 days, no credit card.</p>
<form class="card" style="display:flex;flex-direction:column;gap:14px" onsubmit="event.preventDefault()"><input class="field" placeholder="Work email" name="email"><input class="field" placeholder="Company" name="company"><input class="field" type="password" placeholder="Password" name="password">
<button class="btn primary" type="submit">Create account</button><button class="btn ghost" type="button">Continue with Google</button></form></div>`],
  '/app': ['Live demo', ''],
};
// SPA: /app, /app/reports, /app/alerts are client-side routes using pushState.
const appViews = {
  '/app': ['Overview', '<div class="grid g4"><div class="card"><div class="muted">Weekly active</div><div class="kpi">18,204</div></div><div class="card"><div class="muted">Activation</div><div class="kpi">42%</div></div><div class="card"><div class="muted">Retention W4</div><div class="kpi">31%</div></div><div class="card"><div class="muted">NPS</div><div class="kpi">47</div></div></div><div class="card" style="margin-top:22px"><h3>Signups by week</h3><div style="display:flex;align-items:flex-end;gap:10px;height:200px">' + [40, 52, 48, 66, 72, 69, 85, 90, 78, 96].map((h) => `<div class="bar" style="flex:1;height:${h}%"></div>`).join('') + '</div></div>'],
  '/app/reports': ['Reports', '<div class="grid g2">' + ['Funnel: signup → activation', 'Revenue by plan', 'Feature adoption', 'Churn drivers'].map((r) => `<div class="card"><h3>${r}</h3><p>Updated 2 hours ago</p><button class="btn small ghost open-report">Open report</button></div>`).join('') + '</div>'],
  '/app/alerts': ['Alerts', '<div class="card">' + ['Signups dropped 18% vs last week', 'p95 query latency above 2s', 'Trial conversions up 9%'].map((a) => `<details><summary>${a}</summary><p>Triggered 3 times this week.</p></details>`).join('') + '<p><button class="btn primary new-alert">New alert</button></p></div>'],
};
nimbus.spa = appViews;
nimbus.extraScript = `<script>
(function(){var views=${JSON.stringify(Object.fromEntries(Object.entries(appViews).map(([k, v]) => [k, v])))};
function render(p){var root=document.getElementById('app-root');if(!root)return;var v=views[p]||views['/app'];
root.innerHTML='<div class="tabs">'+Object.keys(views).map(function(k){return '<a href="'+k+'" data-route class="'+(k===p?'on':'')+'">'+views[k][0]+'</a>'}).join('')+'</div>'+v[1];}
document.addEventListener('click',function(e){var a=e.target.closest&&e.target.closest('a[data-route]');if(!a)return;e.preventDefault();history.pushState({},'',a.getAttribute('href'));render(location.pathname);});
window.addEventListener('popstate',function(){render(location.pathname)});render(location.pathname);})();
</script>`;

// ---------------------------------------------------------------- Fieldnote blog
const fieldnote = {
  id: 'fieldnote-journal',
  name: 'Fieldnote',
  host: 'fieldnote.localhost',
  tagline: 'Slow travel, written down.',
  theme: { font: 'Charter,Georgia,serif', display: '"Iowan Old Style",Georgia,serif', ink: '#1f2a24', muted: '#5f6f66', bg: '#f4f1ea', card: '#fffdf8', line: '#ddd6c8', accent: '#2f6b4f', onAccent: '#fff', headerBg: 'rgba(244,241,234,.94)', band: '#2f6b4f', bandInk: '#f4f1ea', radius: '6px', thumbA: '#9cb7a3', thumbB: '#36584a' },
  nav: [['/', 'Stories'], ['/destinations', 'Destinations'], ['/about', 'About'], ['/newsletter', 'Newsletter']],
  headerCta: '<a class="btn small primary" href="/newsletter">Subscribe</a>',
  footer: [['/about', 'About'], ['https://instagram.com/fieldnote', 'Instagram'], ['/rss', 'RSS']],
};
const postCard = (slug, title, blurb, mins) => `<div class="card"><div class="thumb"></div><div class="eyebrow">${mins} min read</div><h3><a href="/posts/${slug}" style="text-decoration:none">${title}</a></h3><p>${blurb}</p><a class="read-more" href="/posts/${slug}">Read the story →</a></div>`;
const post = (title, eyebrow, n) => `<article class="post"><div class="eyebrow">${eyebrow}</div><h1>${title}</h1><p class="muted">By Maya Lindqvist · March 2026</p><div class="cover"></div>
${paras(4)}<h2>Arriving</h2>${paras(n, 1)}<h2>Where we stayed</h2>${paras(n, 2)}<h2>What we'd do again</h2>${paras(n, 3)}
<div class="card row" style="justify-content:space-between;margin-top:36px"><span>Enjoyed this? Get the next story by email.</span><a class="btn primary" href="/newsletter">Subscribe</a></div>
<div class="row" style="margin-top:20px"><button class="btn small ghost share" aria-label="Share on X">Share</button><button class="btn small ghost copy-link">Copy link</button><a class="btn small ghost" href="/">More stories</a></div></article>`;
fieldnote.pages = {
  '/': ['Stories', `<div class="wrap hero" style="padding-bottom:30px"><div class="eyebrow">Fieldnote Journal</div><h1>Slow travel, written down.</h1><p class="lead">Long-form stories from trains, trails and small kitchens. A new story every other Sunday.</p>
<div class="ctas"><a class="btn primary" href="/posts/slow-travel-in-portugal">Read the latest</a><a class="btn ghost" href="/newsletter">Get it by email</a></div></div>
<section style="padding-top:10px"><div class="wrap grid g3">
${postCard('slow-travel-in-portugal', 'Three weeks of slow travel in Portugal', 'Trains, tiles and too many pastéis de nata.', 12)}
${postCard('kumano-kodo', 'A week on the Kumano Kodo', 'Walking Japan’s ancient pilgrimage trail, inn to inn.', 15)}
${postCard('night-trains-europe', 'The return of the European night train', 'Vienna to Venice, asleep.', 9)}
</div></section>`],
  '/posts/slow-travel-in-portugal': ['Three weeks of slow travel in Portugal', post('Three weeks of slow travel in Portugal', 'Portugal · 12 min read', 5)],
  '/posts/kumano-kodo': ['A week on the Kumano Kodo', post('A week on the Kumano Kodo', 'Japan · 15 min read', 6)],
  '/posts/night-trains-europe': ['The return of the European night train', post('The return of the European night train', 'Europe · 9 min read', 3)],
  '/destinations': ['Destinations', `<div class="wrap" style="padding-top:48px"><h1 style="font-size:42px">Destinations</h1><div class="grid g4">${['Portugal', 'Japan', 'Italy', 'Norway', 'Mexico', 'Georgia', 'Vietnam', 'Scotland'].map((c) => `<a class="card destination" href="/" style="text-decoration:none"><div class="thumb" style="height:110px"></div><h3>${c}</h3></a>`).join('')}</div></div>`],
  '/about': ['About', `<article class="post"><h1>About Fieldnote</h1>${paras(5)}</article>`],
  '/newsletter': ['Newsletter', `<div class="wrap" style="max-width:640px;padding:80px 32px;text-align:center"><h1 style="font-size:40px">One story, every other Sunday.</h1><p class="lead muted">Join 12,000 readers. No ads, unsubscribe any time.</p>
<form class="row" style="justify-content:center" onsubmit="event.preventDefault()"><input class="field" type="email" placeholder="Your email" name="email"><button class="btn primary" type="submit">Subscribe</button></form></div>`],
};

export const sites = [acme, nimbus, fieldnote];

export function render(host, path, endpoint) {
  const site = sites.find((s) => s.host === host);
  if (!site) return null;
  let entry = site.pages[path];
  if (!entry && site.spa && site.spa[path]) entry = site.pages['/app'];
  if (!entry) return null;
  let [title, body] = entry;
  if (path.startsWith('/app')) body = '<div class="wrap" style="padding:36px 32px"><div id="app-root"></div></div>';
  return page(site, title, body, endpoint);
}
