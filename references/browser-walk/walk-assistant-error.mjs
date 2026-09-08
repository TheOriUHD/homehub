// Does a failed turn leave something the reader can actually read?
//
// The error used to live in the streaming bubble, which is torn down the
// moment the stream ends — so it appeared and vanished, and the only report
// available was "a red message that instantly disappears".
import { openApp, report } from './harness.mjs';

const MESSAGE = 'the AI provider refused the request: Jinja Exception: Message has tool role';

const app = await openApp({
  dist: process.argv[2] || '/root/homehub/web/dist',
  out: process.argv[3] || '.',
  overrides: {
    'GET /assistant': (b) => ({
      ...b, enabled: true, provider: 'ollama', model: 'homehub-advanced:latest',
      keySet: false, canConfigure: true, autoAvailable: false,
      providers: [{
        id: 'ollama', label: 'Ollama (local)', kind: 'local', requiresKey: false,
        defaultModel: 'qwen3:8b', defaultBaseUrl: 'http://127.0.0.1:11434/v1',
      }],
    }),
  },
});
const { page, goto } = app;

// A turn that stores the question and then fails, which is what the server
// does when the model refuses: a user event, an error, and the stream closes.
await app.context.route('**/api/assistant/chat/stream', (route) =>
  route.fulfill({
    status: 200,
    contentType: 'application/x-ndjson',
    body:
      JSON.stringify({ type: 'user', messageId: 1 }) + '\n' +
      JSON.stringify({ type: 'error', message: MESSAGE }) + '\n',
  }));

await goto('/assistant');
await page.getByPlaceholder('Ask about your systems').fill('What is the RAM usage of the Minecraft VM?');
await page.keyboard.press('Enter');

// Long after the stream has closed and the live bubble is gone.
await page.waitForTimeout(1500);
const findings = await page.evaluate((msg) => {
  const body = document.body.textContent;
  const dismiss = [...document.querySelectorAll('button[aria-label]')]
    .filter((b) => /close/i.test(b.getAttribute('aria-label')));
  return {
    errorStillOnScreen: body.includes(msg),
    saysItFailed: /could not answer/i.test(body),
    dismissible: dismiss.length > 0,
  };
}, MESSAGE);
await page.screenshot({ path: `${app.out}/assistant-error.png` });

// Dismissing it clears it, and asking again starts clean rather than stacking.
await page.evaluate(() => {
  const b = [...document.querySelectorAll('button[aria-label]')]
    .filter((x) => /close/i.test(x.getAttribute('aria-label'))).pop();
  if (b) b.click();
});
await page.waitForTimeout(300);
findings.clearsWhenDismissed = !(await page.evaluate((m) => document.body.textContent.includes(m), MESSAGE));

await report(app, { findings });
