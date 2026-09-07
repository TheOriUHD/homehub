// Does the local-model setup guide render, switch modes, and fill the form in?
//
// Every local runtime is walked in both modes, because the two are separate
// bodies of copy and a missing translation key renders as the key itself.
import { openApp, report } from './harness.mjs';

const LOCAL = ['ollama', 'lmstudio', 'llamacpp', 'vllm', 'localai', 'custom'];

// Enough of a provider catalog for the settings dialog to offer local runtimes.
const providers = [
  { id: 'anthropic', label: 'Anthropic', kind: 'cloud', requiresKey: true, defaultModel: 'claude-sonnet-5' },
  { id: 'ollama', label: 'Ollama (local)', kind: 'local', requiresKey: false, defaultModel: 'qwen3:8b', defaultBaseUrl: 'http://127.0.0.1:11434/v1' },
  { id: 'lmstudio', label: 'LM Studio (local)', kind: 'local', requiresKey: false, defaultModel: 'local-model', defaultBaseUrl: 'http://127.0.0.1:1234/v1' },
  { id: 'llamacpp', label: 'llama.cpp server (local)', kind: 'local', requiresKey: false, defaultModel: 'default', defaultBaseUrl: 'http://127.0.0.1:8080/v1' },
  { id: 'vllm', label: 'vLLM (local)', kind: 'local', requiresKey: false, defaultModel: 'Qwen/Qwen3-8B', defaultBaseUrl: 'http://127.0.0.1:8000/v1' },
  { id: 'localai', label: 'LocalAI (local)', kind: 'local', requiresKey: false, defaultModel: 'gpt-4', defaultBaseUrl: 'http://127.0.0.1:8080/v1' },
  { id: 'custom', label: 'Custom (OpenAI-compatible)', kind: 'local', requiresKey: false, defaultModel: '', defaultBaseUrl: 'http://127.0.0.1:8000/v1' },
];

const app = await openApp({
  out: process.argv[3] || '.',
  dist: process.argv[2] || '/root/homehub/web/dist',
  overrides: {
    'GET /assistant': (b) => ({
      ...b, enabled: true, provider: 'anthropic', model: 'claude-sonnet-5',
      keySet: true, canConfigure: true, providers,
    }),
  },
});
const { page, goto } = app;

await goto('/assistant');
await page.getByRole('button', { name: 'Settings' }).click();
await page.getByRole('dialog').waitFor();

/** Read one provider's guide in one mode. */
async function readGuide(label) {
  await page.waitForTimeout(150);
  return page.evaluate(() => {
    const heading = [...document.querySelectorAll('button')]
      .find((b) => b.textContent.includes('Set this up step by step'));
    if (!heading) return { present: false };
    const panel = heading.parentElement;
    const steps = [...panel.querySelectorAll('li')].map((li) => ({
      title: li.querySelector('p')?.textContent ?? '',
      body: li.querySelectorAll('p')[1]?.textContent ?? '',
      cmd: li.querySelector('pre')?.textContent ?? null,
    }));
    const text = panel.textContent;
    return {
      present: true,
      steps: steps.length,
      // A missing key renders as the key itself — the failure this catches.
      rawKeys: /assistant\.guide\.[a-z]/i.test(text),
      empty: steps.some((s) => !s.title.trim() || !s.body.trim()),
      commands: steps.filter((s) => s.cmd).length,
      hasFill: !!panel.querySelector('button')
        && [...panel.querySelectorAll('button')].some((b) => b.textContent.includes('Fill these in above')),
      notes: [...panel.querySelectorAll('p')].filter((p) => p.textContent.includes('127.0.0.1') || p.textContent.includes('tool calling')).length,
      lastStep: steps[steps.length - 1]?.body ?? '',
    };
  }, label);
}

const findings = {};
for (const id of LOCAL) {
  // The provider picker is a Radix Select; open it and choose by label.
  await page.locator('button[role="combobox"]').first().click();
  await page.getByRole('option', { name: providers.find((p) => p.id === id).label }).click();
  await page.waitForTimeout(200);

  const single = await readGuide(id);
  await page.getByRole('button', { name: 'Auto mode', exact: true }).click();
  const auto = await readGuide(id);
  await page.getByRole('button', { name: 'One model', exact: true }).click();
  findings[id] = { single, auto };
}

// Filling the form in: the last step's button writes the example values into
// the fields, which is the whole reason it is a button and not a paragraph.
await page.locator('button[role="combobox"]').first().click();
await page.getByRole('option', { name: 'Ollama (local)' }).click();
await page.getByRole('button', { name: 'Auto mode', exact: true }).click();
await page.getByRole('button', { name: 'Fill these in above' }).click();
await page.waitForTimeout(200);
findings.fillsTheForm = await page.evaluate(() =>
  [...document.querySelectorAll('label')].map((l) => {
    const input = document.getElementById(l.getAttribute('for'));
    return { label: l.textContent.trim(), value: input && 'value' in input ? input.value : null };
  }),
);
await page.screenshot({ path: app.out + '/guide-ollama-auto.png', fullPage: true });

await report(app, { findings });
