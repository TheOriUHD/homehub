// Is the assistant's model picker visible, and does picking a model work?
//
// The menu is portaled to <body>, so it competes with the floating assistant
// widget on z-index alone. It once opened BEHIND that panel: still clickable,
// because Radix turns pointer events off everywhere else while a menu is open,
// but invisible. Hence the paint probe below rather than a visibility check.
import { openApp, report } from './harness.mjs';

const models = [
  { id: 'claude-haiku-4-5', label: 'Haiku 4.5', blurb: 'Fast, for routine questions', autoLane: 'basic' },
  { id: 'claude-sonnet-5', label: 'Sonnet 5', blurb: 'Balanced for most work', autoLane: 'advanced' },
  { id: 'claude-opus-5', label: 'Opus 5', blurb: 'Deep reasoning', tier: 'premium', costNote: 'About 5x the cost of Sonnet' },
  { id: 'claude-fable-5', label: 'Fable 5', blurb: 'Frontier model', tier: 'ultra', costNote: 'About 25x the cost of Sonnet' },
];

const app = await openApp({
  dist: process.argv[2] || '/root/homehub/web/dist',
  out: process.argv[3] || '.',
  overrides: {
    'GET /assistant': (b) => ({
      ...b, enabled: true, provider: 'anthropic', model: 'claude-sonnet-5', keySet: true, canConfigure: true,
      autoAvailable: process.env.AUTO_AVAILABLE !== '0',
      autoBasic: 'claude-haiku-4-5', autoAdvanced: 'claude-sonnet-5',
      providers: [{ id: 'anthropic', label: 'Anthropic', kind: 'cloud', requiresKey: true, defaultModel: 'claude-sonnet-5', models }],
    }),
  },
});
let { page, goto, posts, out: OUT } = app;
const path = await import('node:path');

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
  await page.screenshot({ path: path.default.join(OUT, `${label}-menu.png`) });
  return r;
}

/** The untouched state of a fresh tab: what the pill says before anyone
 *  picks, and which model the first turn actually posts. */
async function probeDefault() {
  const pill = (await page.getByRole('button', { name: 'Switch model' }).innerText()).trim();
  const before = posts.length;
  await page.getByPlaceholder('Ask about your systems').fill('hello');
  await page.keyboard.press('Enter');
  await page.waitForTimeout(600);
  const turn = posts.slice(before).find((x) => x.body && 'model' in x.body);
  return { pill, postedModel: turn ? turn.body.model : null, postedTo: turn ? turn.path : null };
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

const report_ = {};
await goto('/');
await page.getByRole('button', { name: 'Open assistant' }).click();
await page.locator('[role="dialog"][data-assistant-widget]').waitFor();
report_.widget = { default: await probeDefault(), menu: await probeMenu('widget'), escape: await probeEscape() };
if (!report_.widget.escape.widgetStillOpen) {
  await page.getByRole('button', { name: 'Open assistant' }).click();
  await page.locator('[role="dialog"][data-assistant-widget]').waitFor();
}
report_.widget.select = await probeSelect();

// A new tab: sessionStorage is per tab, so the widget's pick above must not
// colour what the page shows untouched.
page = await app.context.newPage();
app.watch(page);
await goto('/assistant', page);
await page.getByRole('button', { name: 'Switch model' }).waitFor();
report_.page = { default: await probeDefault(), menu: await probeMenu('page'), escape: await probeEscape() };
report_.page.select = await probeSelect();

await report(app, report_);
