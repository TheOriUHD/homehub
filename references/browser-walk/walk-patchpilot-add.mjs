// Adding a registry host to PatchPilot when its login needs a sudo password.
//
// The reported bug: detection asks for the password, the fields appear — and
// vanish on the first keystroke, because typing clears the detection result
// and the fields were keyed off that same result. Clicking Test again then
// sent a half-typed password nobody could see.
import { openApp, report } from './harness.mjs';

const FLEET = '00000000-0000-4000-8000-0000000000f1';
const HOST = '00000000-0000-4000-8000-0000000000a1';

const detections = [];
const app = await openApp({
  dist: process.argv[2] || '/root/homehub/web/dist',
  out: process.argv[3] || '.',
  overrides: {
    // The page is the fleet's, so the fleet has to exist first.
    'GET /integrations': () => [{
      id: FLEET, type: 'patchpilot', name: 'Patchpilot', enabled: true,
      status: 'connected', statusMessage: '', config: {},
      createdAt: '2026-08-19T00:00:00Z', statusChangedAt: '2026-09-01T00:00:00Z',
    }],
    'GET /ssh-hosts': () => [{
      id: HOST, name: 'MC-Database VM', hostname: '192.168.20.34', port: 22,
      username: 'database', authType: 'key', notes: '', inUse: 0, boundResources: [],
      createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z',
    }],
  },
});
const { page, goto } = app;

// Detection answers like the server does for a key login that cannot become
// root on its own — and records exactly what each Test sent.
await app.context.route(`**/api/patchpilot/${FLEET}/hosts/detect`, (route) => {
  const body = route.request().postDataJSON();
  detections.push(body);
  // Three answers, as the server gives them: no password yet, a wrong one
  // (the message copied from ResolveEscalation), and the right one.
  let answer;
  if (!body.password) {
    answer = { reachable: true, osPretty: 'Debian 13', packageManager: 'apt', needsPassword: true,
      blocker: 'this account can\'t elevate to root — enter root\'s password above, or use a login that has sudo access',
      message: 'this account can\'t elevate to root — enter root\'s password above, or use a login that has sudo access' };
  } else if (body.password !== 'secret') {
    answer = { reachable: true, osPretty: 'Debian 13', packageManager: 'apt', needsPassword: true,
      blocker: 'sudo did not accept that password — check it, or enter root\'s password instead',
      message: 'sudo did not accept that password — check it, or enter root\'s password instead' };
  } else {
    answer = { reachable: true, osPretty: 'Debian 13', packageManager: 'apt',
      escalation: 'sudo-password', message: 'Ready to onboard via sudo-password' };
  }
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(answer) });
});

await goto(`/patchpilot/${FLEET}`);
await page.waitForTimeout(700);
const add = page.getByRole('button', { name: /add host/i }).first();
await add.click();
await page.getByRole('dialog').waitFor();

// Pick the registry host.
await page.locator('[role="dialog"] button[role="combobox"]').first().click();
await page.getByRole('option', { name: /MC-Database VM/ }).click();
await page.waitForTimeout(200);

const passwordFields = () => page.locator('[role="dialog"] input[type="password"]').count();

const findings = {};
findings.fieldsBeforeTest = await passwordFields();

await page.getByRole('button', { name: 'Test connection' }).click();
await page.waitForTimeout(500);
findings.fieldsAfterTest = await passwordFields();

// The moment of the bug: one character.
await page.locator('[role="dialog"] input[type="password"]').first().type('s');
await page.waitForTimeout(250);
findings.fieldsAfterOneKeystroke = await passwordFields();

await page.locator('[role="dialog"] input[type="password"]').first().type('ekret');
await page.waitForTimeout(150);
findings.fieldsAfterTypingMore = await passwordFields();
findings.valueKept = await page.locator('[role="dialog"] input[type="password"]').first().inputValue();

// A typo. The answer must come back, name the password, and leave the fields
// there to fix it — not a spinner, and not an empty form.
await page.getByRole('button', { name: 'Test connection' }).click();
await page.waitForTimeout(600);
findings.typoSent = detections.at(-1)?.password ?? null;
findings.typoFieldsStay = await passwordFields();
findings.typoSaysPassword = await page.evaluate(() => /did not accept that password/.test(document.body.textContent));
await page.screenshot({ path: `${app.out}/patchpilot-add-typo.png` });

// Fix it and test again.
const field = page.locator('[role="dialog"] input[type="password"]').first();
await field.fill('secret');
await page.getByRole('button', { name: 'Test connection' }).click();
await page.waitForTimeout(600);
findings.fixedSent = detections.at(-1)?.password ?? null;
findings.readyNoteShown = await page.evaluate(() => /elevates via sudo-password/.test(document.body.textContent));
findings.detectCalls = detections.length;
await page.screenshot({ path: `${app.out}/patchpilot-add.png` });

await report(app, { findings });
