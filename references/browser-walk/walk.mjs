import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';

const DIST = process.argv[2] || '/root/homehub/web/dist';
const OUT = process.argv[3] || '.';
fs.mkdirSync(OUT, { recursive: true });
const ORIGIN = process.env.ORIGIN || 'https://homehub.walk';

const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const stubs = JSON.parse(fs.readFileSync(new URL('./stubs.json', import.meta.url)));
const routes = Object.entries(stubs)
  .map(([k, v]) => {
    const [method, tpl] = k.split(' ');
    const re = new RegExp('^' + tpl.split(/\{[^}]+\}/).map(esc).join('[^/]+') + '$');
    return { method, tpl, re, ...v, params: (tpl.match(/\{/g) || []).length };
  })
  .sort((a, b) => a.params - b.params || b.tpl.length - a.tpl.length);

const models = [
  { id: 'claude-haiku-4-5', label: 'Haiku 4.5', blurb: 'Fast, for routine questions', autoLane: 'basic' },
  { id: 'claude-sonnet-5', label: 'Sonnet 5', blurb: 'Balanced for most work', autoLane: 'advanced' },
  { id: 'claude-opus-5', label: 'Opus 5', blurb: 'Deep reasoning', tier: 'premium', costNote: 'About 5x the cost of Sonnet' },
  { id: 'claude-fable-5', label: 'Fable 5', blurb: 'Frontier model', tier: 'ultra', costNote: 'About 25x the cost of Sonnet' },
];
const overrides = {
  'GET /auth/me': (b) => ({
    ...b,
    user: { ...b.user, username: 'walk', displayName: 'Walk' },
    grants: [{ selector: '*', effect: 'allow', levels: ['view', 'control', 'configure', 'delete'] }],
    viewAsRoleId: null,
  }),
  'GET /assistant': (b) => ({
    ...b, enabled: true, provider: 'anthropic', model: 'claude-sonnet-5', keySet: true, canConfigure: true,
    autoAvailable: true, autoBasic: 'claude-haiku-4-5', autoAdvanced: 'claude-sonnet-5',
    providers: [{ id: 'anthropic', label: 'Anthropic', kind: 'cloud', requiresKey: true, defaultModel: 'claude-sonnet-5', models }],
  }),
};
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2', '.woff': 'font/woff', '.json': 'application/json', '.ico': 'image/x-icon', '.webp': 'image/webp' };

const unmatched = new Set();
const errors = [];
const browser = await chromium.launch();
const [vw, vh] = (process.env.VIEWPORT || '1440x900').split('x').map(Number);
const context = await browser.newContext({ viewport: { width: vw, height: vh } });
const THEME = process.env.THEME || 'light';
await context.addInitScript(([k, theme]) => { localStorage.setItem(k, theme); localStorage.setItem('hh-lang-chosen', '1'); }, ['homehub-theme', THEME]);
await context.route('**/*', async (route) => {
  const req = route.request();
  const u = new URL(req.url());
  if (u.origin !== ORIGIN) return route.abort();
  const p = u.pathname;
  if (p.startsWith('/api/')) {
    const apiPath = p.slice(4);
    const hit = routes.find((r) => r.method === req.method() && r.re.test(apiPath));
    if (!hit) {
      unmatched.add(req.method() + ' ' + apiPath);
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    }
    let body = hit.body;
    const ov = overrides[hit.method + ' ' + hit.tpl];
    if (ov) body = ov(body);
    return route.fulfill({ status: hit.status, contentType: 'application/json', body: body === null ? '' : JSON.stringify(body) });
  }
  let file = path.join(DIST, p);
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(DIST, 'index.html');
  return route.fulfill({ status: 200, contentType: types[path.extname(file)] || 'application/octet-stream', body: fs.readFileSync(file) });
});

const page = await context.newPage();
page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
page.on('console', (m) => { if (m.type() === 'error' && !/WebSocket|ws:\/\//.test(m.text())) errors.push('console: ' + m.text().slice(0, 200)); });

/** What is painted on top at the menu's centre, judged with pointer-events
 *  forced on — Radix turns them off on <body> while a menu is open, which is
 *  exactly what lets a hidden menu still take the click. */
async function probeMenu(label) {
  await page.getByRole('button', { name: 'Switch model' }).click();
  const menu = page.locator('[role="menu"]');
  await menu.waitFor({ state: 'attached', timeout: 5000 });
  await page.waitForTimeout(250);
  const r = await page.evaluate(() => {
    const menu = document.querySelector('[role="menu"]');
    const box = menu.getBoundingClientRect();
    const cx = box.left + box.width / 2, cy = box.top + box.height / 2;
    const widget = document.querySelector('[role="dialog"][data-assistant-widget]');
    const wrapper = menu.closest('[data-radix-popper-content-wrapper]');
    const hitWithRadixState = document.elementFromPoint(cx, cy);
    const prev = document.body.style.pointerEvents;
    document.body.style.pointerEvents = 'auto';
    if (widget) widget.style.pointerEvents = 'auto';
    const painted = document.elementFromPoint(cx, cy);
    document.body.style.pointerEvents = prev;
    if (widget) widget.style.pointerEvents = '';
    const desc = (el) => (el ? el.tagName.toLowerCase() + (el.getAttribute('role') ? '[role=' + el.getAttribute('role') + ']' : '') + (el.closest('[role="menu"]') ? ' (in menu)' : el.closest('[data-assistant-widget]') ? ' (in widget)' : '') : null);
    return {
      items: menu.querySelectorAll('[role="menuitem"]').length,
      box: [Math.round(box.left), Math.round(box.top), Math.round(box.width), Math.round(box.height)],
      menuZ: wrapper ? getComputedStyle(wrapper).zIndex : null,
      widgetZ: widget ? getComputedStyle(widget).zIndex : null,
      bodyPointerEvents: prev || '(none set)',
      clickTarget: desc(hitWithRadixState),
      paintedOnTop: desc(painted),
      menuVisibleToUser: !!painted?.closest('[role="menu"]'),
      menuOpacity: getComputedStyle(menu).opacity,
      menuMaxHeight: getComputedStyle(menu).maxHeight,
      menuScrollable: menu.scrollHeight > menu.clientHeight,
    };
  });
  await page.screenshot({ path: path.join(OUT, `${label}-menu.png`) });
  return r;
}

async function probeEscape() {
  await page.keyboard.press('Escape');
  await page.waitForTimeout(250);
  return {
    menuStillOpen: (await page.locator('[role="menu"]').count()) > 0,
    widgetStillOpen: (await page.locator('[role="dialog"][data-assistant-widget]').count()) > 0,
  };
}

async function probeSelect() {
  const out = {};
  await page.getByRole('button', { name: 'Switch model' }).click();
  await page.getByRole('menuitem', { name: /^Opus 5/ }).click();
  await page.waitForTimeout(300);
  out.gateDialogShown = (await page.locator('[role="dialog"]:not([data-assistant-widget])').count()) > 0;
  await page.getByRole('button', { name: 'Cancel' }).click();
  await page.waitForTimeout(300);
  await page.getByRole('button', { name: 'Switch model' }).click();
  await page.getByRole('menuitem', { name: /^Haiku 4\.5/ }).click();
  await page.waitForTimeout(300);
  out.pillAfterPick = (await page.getByRole('button', { name: 'Switch model' }).innerText()).trim();
  out.bodyPointerEventsAfter = await page.evaluate(() => document.body.style.pointerEvents || '(none set)');
  out.composerFocusedAfter = await page.evaluate(() => document.activeElement?.tagName.toLowerCase());
  return out;
}

const report = { dist: DIST, theme: THEME, viewport: [vw, vh] };
await page.goto(ORIGIN + '/', { waitUntil: 'domcontentloaded' });
const keep = page.getByRole('button', { name: 'Keep English' });
if (await keep.isVisible({ timeout: 3000 }).catch(() => false)) await keep.click();
await page.getByRole('button', { name: 'Open assistant' }).click();
await page.locator('[role="dialog"][data-assistant-widget]').waitFor();
report.widget = { menu: await probeMenu('widget'), escape: await probeEscape() };
if (!report.widget.escape.widgetStillOpen) {
  await page.getByRole('button', { name: 'Open assistant' }).click();
  await page.locator('[role="dialog"][data-assistant-widget]').waitFor();
}
report.widget.select = await probeSelect();

await page.goto(ORIGIN + '/assistant', { waitUntil: 'domcontentloaded' });
await page.getByRole('button', { name: 'Switch model' }).waitFor();
report.page = { menu: await probeMenu('page'), escape: await probeEscape() };
report.page.select = await probeSelect();

report.errors = errors;
report.unmatchedApi = [...unmatched];
console.log(JSON.stringify(report, null, 1));
await browser.close();
