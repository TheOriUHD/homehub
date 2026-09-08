// Can someone locked out by a changed SSH host key see both fingerprints and
// accept the new one, without rebuilding the host entry?
//
// Three states worth looking at: a pin that still matches, a pin that changed
// (the lockout), and an address that does not answer. The last two must not
// look alike — "the machine is off" and "the machine is presenting a different
// key" are different problems.
import { openApp, report } from './harness.mjs';

const hosts = [
  {
    id: '00000000-0000-4000-8000-000000000001', name: 'Minecraft VM',
    hostname: '192.168.20.73', port: 22, username: 'minecraft', authType: 'key',
    notes: '', inUse: 1, boundResources: ['minecraft/…'],
    createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z',
  },
  {
    id: '00000000-0000-4000-8000-000000000002', name: 'WebHost LXC',
    hostname: '192.168.20.235', port: 22, username: 'root', authType: 'key',
    notes: '', inUse: 0, boundResources: [],
    createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z',
  },
];

const PINNED = 'SHA256:v8YpxSXJdM5t0RPca5l/4pvKLqd1mBB11s7UqmvoaTA';
const LIVE = 'SHA256:kuCrZ/k5fhUh3Jv1t/rJEl5xr7D+iKuiAFaSBEPjOLQ';

// The check answers differently per address, and flips to a match once the new
// key has been trusted — the whole point being that the screen shows recovery.
let trusted = false;
const checkFor = (host) => {
  // Messages copied from api_sshknownhosts.go: a stub that paraphrases the
  // server is a stub that cannot catch the two of them saying the same thing.
  if (host === '192.168.20.235') {
    return { host, port: 22, status: 'unreachable', message: 'cannot reach 192.168.20.235:22: dial tcp 192.168.20.235:22: connect: no route to host' };
  }
  if (trusted) {
    return {
      host, port: 22, status: 'match', pinned: LIVE, observed: LIVE,
      message: 'Connections to this address work again — the terminal, any system that dials it over SSH, and the assistant.',
    };
  }
  return {
    host, port: 22, status: 'changed', pinned: PINNED, observed: LIVE,
    message: 'Every connection to this address is being refused until it is resolved.',
    alsoStale: ['Minecraft VM'],
  };
};

const app = await openApp({
  dist: process.argv[2] || '/root/homehub/web/dist',
  out: process.argv[3] || '.',
  overrides: {
    'GET /ssh-hosts': () => hosts,
    'GET /ssh-known-hosts': () => ({
      items: [
        { host: '192.168.20.73', port: 22, fingerprint: trusted ? LIVE : PINNED, updatedAt: '2026-08-21T22:41:13Z', usedBy: ['Minecraft VM', 'AcardiaMC'] },
        { host: '192.168.20.235', port: 22, fingerprint: 'SHA256:xr+8wAmlc8YR5GnMIlIJA', updatedAt: '2026-08-20T03:42:08Z', usedBy: ['WebHost LXC'] },
        { host: '192.168.20.164', port: 22, fingerprint: 'SHA256:a3GNb2FMcKDDiEeuiPYHD', updatedAt: '2026-08-20T03:41:17Z', usedBy: [] },
      ],
    }),
  },
});
const { page, goto } = app;

// The stub routes above are static; check and trust need to react, so they get
// their own handler ahead of them.
await app.context.route('**/api/ssh-known-hosts/check', (route) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(checkFor(route.request().postDataJSON().host)) }));
await app.context.route('**/api/ssh-known-hosts/trust', (route) => {
  trusted = true;
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(checkFor(route.request().postDataJSON().host)) });
});
await app.context.route('**/api/ssh-hosts/*/test', (route) =>
  route.fulfill({
    status: 200, contentType: 'application/json',
    body: JSON.stringify({ ok: false, message: `SSH connection to 192.168.20.73:22 failed: ssh: handshake failed: host key changed for 192.168.20.73:22 — refusing to connect (pinned ${PINNED}, now ${LIVE}); if this host was rebuilt, clear its pin` }),
  }));

await goto('/ssh-hosts');
await page.getByRole('heading', { name: 'SSH Hosts' }).waitFor().catch(() => {});
await page.waitForTimeout(400);

const findings = {};

// The pin list is the entry point for addresses with no registry row.
findings.knownHostsCard = await page.evaluate(() => {
  const rows = [...document.querySelectorAll('table')].pop()?.querySelectorAll('tbody tr') ?? [];
  return {
    rows: rows.length,
    firstRow: rows[0] ? rows[0].textContent.replace(/\s+/g, ' ').trim().slice(0, 90) : null,
    rawKeys: /ssh\.hostKey\./.test(document.body.textContent),
  };
});

/** Read whatever the open dialog is saying. */
const readDialog = async () => {
  await page.waitForTimeout(350);
  return page.evaluate(() => {
    const d = [...document.querySelectorAll('[role="dialog"]')].pop();
    if (!d) return null;
    const text = d.textContent;
    return {
      title: d.querySelector('h2, [id]')?.textContent?.slice(0, 60) ?? '',
      fingerprints: [...d.querySelectorAll('code')].map((c) => c.textContent.trim()),
      buttons: [...d.querySelectorAll('button')].map((b) => b.textContent.trim()).filter(Boolean),
      mentionsInterception: /intercept/i.test(text),
      mentionsPatchpilot: /PatchPilot/.test(text),
      rawKeys: /ssh\.hostKey\./.test(text),
    };
  });
};

// A failing test offers the cure next to the error, which is where it is read.
await page.locator('tbody tr').first().getByRole('button', { name: 'Test' }).click();
await page.waitForTimeout(400);
findings.failedTestOffersTheFix = await page.evaluate(() => {
  const row = document.querySelector('tbody tr');
  const link = [...row.querySelectorAll('button')].find((b) => /host key/i.test(b.textContent));
  return { present: !!link, label: link?.textContent.trim() ?? null };
});

await page.evaluate(() => {
  const row = document.querySelector('tbody tr');
  [...row.querySelectorAll('button')].find((b) => /host key/i.test(b.textContent)).click();
});
findings.changed = await readDialog();
await page.screenshot({ path: `${app.out}/hostkey-changed.png` });

// Trusting it: the same dialog should now report a match, both fingerprints equal.
await page.getByRole('button', { name: 'Trust the new key' }).click();
findings.afterTrust = await readDialog();
await page.screenshot({ path: `${app.out}/hostkey-trusted.png` });
await page.keyboard.press('Escape');
await page.waitForTimeout(300);

// An address that does not answer must read as unreachable, not as a lockout.
await page.locator('tbody tr').nth(1).getByRole('button', { name: 'Host key' }).click();
findings.unreachable = await readDialog();

await report(app, { findings });
