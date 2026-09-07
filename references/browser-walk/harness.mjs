// The reusable half of the stubbed browser walk: serve web/dist from disk,
// answer every /api/* call from spec-generated examples, and hand back a page
// that is signed in and past the first-run prompts.
//
// No session, no database, no credentials — which is the point: minting a
// session row is blocked in auto mode, and this needs neither.
//
// See README.md. Probes live in their own walk-*.mjs files; this file should
// stay free of anything specific to one question.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';

const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

const TYPES = {
  '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css',
  '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2',
  '.woff': 'font/woff', '.json': 'application/json', '.ico': 'image/x-icon',
  '.webp': 'image/webp',
};

/**
 * openApp boots the built frontend against stubbed APIs.
 *
 * dist      — the web/dist to serve (default the repo's)
 * out       — directory for screenshots
 * origin    — https://homehub.walk for a SECURE context (what Caddy gives the
 *             owner), or http://192.168.20.26:8080 for the insecure LAN origin
 *             where crypto.randomUUID and friends do not exist
 * theme     — 'light' | 'dark'
 * viewport  — [w, h]
 * overrides — { 'GET /assistant': (body) => body, … } per spec route
 */
export async function openApp({
  dist = '/root/homehub/web/dist',
  out = '.',
  origin = process.env.ORIGIN || 'https://homehub.walk',
  theme = process.env.THEME || 'light',
  viewport = (process.env.VIEWPORT || '1440x900').split('x').map(Number),
  overrides = {},
} = {}) {
  fs.mkdirSync(out, { recursive: true });
  const stubs = JSON.parse(fs.readFileSync(new URL('./stubs.json', import.meta.url)));
  const routes = Object.entries(stubs)
    .map(([k, v]) => {
      const [method, tpl] = k.split(' ');
      const re = new RegExp('^' + tpl.split(/\{[^}]+\}/).map(esc).join('[^/]+') + '$');
      return { method, tpl, re, ...v, params: (tpl.match(/\{/g) || []).length };
    })
    .sort((a, b) => a.params - b.params || b.tpl.length - a.tpl.length);

  // Signed in, and allowed to see everything: what is being looked at is the
  // page, not the permission logic (that has its own tests).
  const withIdentity = {
    'GET /auth/me': (b) => ({
      ...b,
      user: { ...b.user, username: 'walk', displayName: 'Walk' },
      grants: [{ selector: '*', effect: 'allow', levels: ['view', 'control', 'configure', 'delete'] }],
      viewAsRoleId: null,
    }),
    ...overrides,
  };

  const unmatched = new Set();
  const errors = [];
  const posts = [];
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: viewport[0], height: viewport[1] } });
  await context.addInitScript(([t]) => {
    try {
      localStorage.setItem('homehub-theme', t);
      localStorage.setItem('hh-lang-chosen', '1');
    } catch { /* private mode */ }
  }, [theme]);

  await context.route('**/*', async (route) => {
    const req = route.request();
    const u = new URL(req.url());
    if (u.origin !== origin) return route.abort();
    const p = u.pathname;
    if (p.startsWith('/api/')) {
      const apiPath = p.slice(4);
      if (req.method() !== 'GET') posts.push({ method: req.method(), path: apiPath, body: req.postDataJSON() });
      const hit = routes.find((r) => r.method === req.method() && r.re.test(apiPath));
      if (!hit) {
        unmatched.add(req.method() + ' ' + apiPath);
        return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
      }
      const ov = withIdentity[hit.method + ' ' + hit.tpl];
      const body = ov ? ov(hit.body) : hit.body;
      return route.fulfill({
        status: hit.status, contentType: 'application/json',
        body: body === null ? '' : JSON.stringify(body),
      });
    }
    let file = path.join(dist, p);
    if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
    return route.fulfill({
      status: 200,
      contentType: TYPES[path.extname(file)] || 'application/octet-stream',
      body: fs.readFileSync(file),
    });
  });

  const page = await context.newPage();
  const watch = (p) => {
    p.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
    p.on('console', (m) => {
      if (m.type() === 'error' && !/WebSocket|ws:\/\//.test(m.text())) errors.push('console: ' + m.text().slice(0, 200));
    });
  };
  watch(page);

  const goto = async (p, target = page) => {
    await target.goto(origin + p, { waitUntil: 'domcontentloaded' });
    const keep = target.getByRole('button', { name: 'Keep English' });
    if (await keep.isVisible({ timeout: 3000 }).catch(() => false)) await keep.click();
  };

  return { browser, context, page, goto, watch, errors, unmatched, posts, origin, out, theme, viewport };
}

/** report prints a walk's findings and closes the browser. */
export async function report(app, findings) {
  console.log(JSON.stringify({
    theme: app.theme, viewport: app.viewport, origin: app.origin,
    ...findings,
    errors: app.errors,
    unmatchedApi: [...app.unmatched],
  }, null, 1));
  await app.browser.close();
}
