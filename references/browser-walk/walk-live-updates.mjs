// Does a change reach an open page without a reload?
//
// Two paths, and both matter. The calendar has a broadcast topic, so an edit
// from anywhere — another tab, a CalDAV sync, the assistant — refreshes every
// open calendar. And the assistant announces everything it changes on a
// per-user topic, so whatever page you happen to be on catches up even when
// the thing it touched has no topic of its own.
//
// The harness serves the built bundle against stubs, so the WebSocket is
// driven here rather than by a server: what is under test is the client half —
// that the page subscribes, and that a push actually refetches.
import { openApp, report } from './harness.mjs';

// Two versions of the same week. The second is what a refetch should pick up.
let edited = false;
const events = () => [{
  eventId: '00000000-0000-4000-8000-000000000010',
  calendarId: '00000000-0000-4000-8000-000000000020',
  uid: 'evt-1',
  title: edited ? 'Dentist check-up' : 'Dentist',
  start: edited ? '2026-09-24T15:00:00Z' : '2026-09-24T14:00:00Z',
  end: edited ? '2026-09-24T16:00:00Z' : '2026-09-24T15:00:00Z',
  allDay: false,
  occurrenceStart: edited ? '2026-09-24T15:00:00Z' : '2026-09-24T14:00:00Z',
  recurring: false,
  location: 'Hauptstrasse 3',
  description: '',
  url: '',
}];

const app = await openApp({
  dist: process.argv[2] || '/root/homehub/web/dist',
  out: process.argv[3] || '.',
  overrides: {
    'GET /calendars': () => [{
      id: '00000000-0000-4000-8000-000000000020',
      name: 'Home', color: '#6366f1', kind: 'local', readOnly: false,
      url: '', username: '', syncError: '', settings: {},
      createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z',
    }],
    'GET /calendar/events': () => events(),
  },
});
const { page, goto } = app;

// A fake socket the test can push through, installed before the app boots.
await app.context.addInitScript(() => {
  class FakeSocket {
    // The client compares readyState against WebSocket.OPEN; without the
    // constants both sides are undefined, the comparison passes, and it
    // sends on a socket it thinks is closed.
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    constructor() {
      this.readyState = 1;
      this.subscribed = [];
      window.__socket = this;
      setTimeout(() => this.onopen && this.onopen(), 0);
    }
    send(raw) {
      const msg = JSON.parse(raw);
      if (msg.type === 'subscribe') this.subscribed.push(msg.topic);
    }
    close() {}
    deliver(topic, data) {
      // The envelope the server sends: a bare {topic,data} is ignored.
      this.onmessage && this.onmessage({ data: JSON.stringify({ type: 'event', topic, data }) });
    }
  }
  window.WebSocket = FakeSocket;
});

await goto('/calendar');
await page.waitForTimeout(900);

const findings = {};
findings.subscribedTopics = await page.evaluate(() => window.__socket?.subscribed ?? []);
findings.showsOriginal = await page.evaluate(() => document.body.textContent.includes('Dentist'));

// The edit lands somewhere else entirely; only a push tells this page.
edited = true;
await page.waitForTimeout(300);
findings.staleBeforePush = await page.evaluate(() => !document.body.textContent.includes('Dentist check-up'));

await page.evaluate(() => window.__socket.deliver('calendar', { event: 'changed' }));
await page.waitForTimeout(1200);
findings.refreshedAfterCalendarPush = await page.evaluate(() =>
  document.body.textContent.includes('Dentist check-up'));
await page.screenshot({ path: `${app.out}/calendar-live.png` });

// The other half: the agent's own announcement, from a page that is not the
// calendar, must still refresh what is on screen.
edited = false;
const second = await app.context.newPage();
app.watch(second);
await goto('/calendar', second);
await second.waitForTimeout(900);
edited = true;
await second.evaluate(() => window.__socket.deliver('agent', { event: 'changed', method: 'PUT', path: '/calendar/events/x' }));
await second.waitForTimeout(1200);
findings.refreshedAfterAgentPush = await second.evaluate(() =>
  document.body.textContent.includes('Dentist check-up'));

await report(app, { findings });
