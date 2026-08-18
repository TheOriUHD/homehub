package dev.homehub.companion.core;

import com.google.gson.JsonObject;
import com.neovisionaries.ws.client.OpeningHandshakeException;
import com.neovisionaries.ws.client.WebSocket;
import com.neovisionaries.ws.client.WebSocketAdapter;
import com.neovisionaries.ws.client.WebSocketFactory;
import com.neovisionaries.ws.client.WebSocketFrame;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.concurrent.CountDownLatch;

/**
 * The HomeHub agent protocol client: dials ws(s)://…/api/mc/agent, pairs or
 * authenticates, backfills the log, batches live console lines, and carries
 * status/player/command traffic. Loader adapters supply platform data via
 * {@link Adapter}; nothing in here touches Bukkit or any loader API.
 *
 * Reconnects with exponential backoff (5s → 2min) whenever the socket drops.
 * An auth rejection (error frame before welcome/paired) is fatal: the loop
 * stops and the operator must re-pair.
 */
public final class CompanionClient {

    /** Platform callbacks. All are invoked from companion threads, never the server main thread. */
    public interface Adapter {
        /** Execute a console command; reply through {@link #sendCmdResult}. */
        void onCommand(long id, String command);

        /** Build one player's full detail (inventory, stats, position, …) as
         *  a JSON object string; reply through {@link #sendPlayerDetail}.
         *  Runs on a companion thread — adapters that touch server state must
         *  hop to the main thread themselves. */
        void onPlayerDetail(long id, String uuid);

        /** Connection is authenticated (backfill already sent). Send players + a status snapshot. */
        void onAuthenticated(boolean freshlyPaired);

        /** Read the pre-connection scrollback (logs/latest.log). */
        List<ConsoleLine> backfillLines();

        void info(String message);

        void warn(String message);
    }

    private static final long BACKOFF_MIN_MS = 5_000;
    private static final long BACKOFF_MAX_MS = 120_000;
    private static final int CONNECT_TIMEOUT_MS = 10_000;

    private final CompanionConfig config;
    private final Adapter adapter;
    private final String serverName;
    private final String loader;
    private final String mcVersion;
    private final String companionVersion;
    private final ConsoleBatcher batcher;

    private Thread thread;
    private volatile boolean running;
    private volatile WebSocket socket;
    private volatile boolean authed;
    private volatile String fatalAuthMessage;

    public CompanionClient(CompanionConfig config, Adapter adapter, String serverName,
                           String loader, String mcVersion, String companionVersion) {
        this.config = config;
        this.adapter = adapter;
        this.serverName = serverName;
        this.loader = loader;
        this.mcVersion = mcVersion;
        this.companionVersion = companionVersion;
        this.batcher = new ConsoleBatcher(new ConsoleBatcher.Sink() {
            @Override public boolean sendConsole(List<ConsoleLine> lines) {
                return sendIfAuthed(Frames.console(lines));
            }
        });
    }

    public synchronized void start() {
        if (running) return;
        running = true;
        fatalAuthMessage = null;
        batcher.start();
        thread = new Thread(new Runnable() {
            @Override public void run() { runLoop(); }
        }, "HomeHub-Companion");
        thread.setDaemon(true);
        thread.start();
    }

    public synchronized void stop() {
        running = false;
        batcher.stop();
        WebSocket s = socket;
        if (s != null) s.disconnect();
        if (thread != null) thread.interrupt();
        thread = null;
    }

    public boolean isConnected() {
        return socket != null && authed;
    }

    public String describeState() {
        String url = config.url();
        if (fatalAuthMessage != null) {
            return "authentication failed (" + fatalAuthMessage + ") — run /homehub pair <url> <code>";
        }
        if (isConnected()) return "connected to " + url;
        if (socket != null) return "handshaking with " + url;
        if (running) return "reconnecting to " + url;
        return "stopped";
    }

    // ---- outbound --------------------------------------------------------

    public void enqueueConsole(long ts, String level, String text) {
        batcher.add(new ConsoleLine(ts, level, text));
    }

    public boolean sendStatus(StatusSnapshot s) {
        return sendIfAuthed(Frames.status(s));
    }

    public boolean sendPlayers(List<PlayerInfo> players) {
        return sendIfAuthed(Frames.players(players));
    }

    public void sendCmdResult(long id, boolean ok, String message) {
        sendIfAuthed(Frames.cmdResult(id, ok, message));
    }

    /** Reply to a player_detail request. detailJson is a JSON object string
     *  (the player's full snapshot), or null when the player is offline. */
    public void sendPlayerDetail(long id, String detailJson) {
        sendIfAuthed(Frames.playerDetailResult(id, detailJson));
    }

    private boolean sendIfAuthed(String json) {
        WebSocket s = socket;
        if (s == null || !authed) return false;
        s.sendText(json);
        return true;
    }

    // ---- connection loop -------------------------------------------------

    private void runLoop() {
        long backoff = BACKOFF_MIN_MS;
        while (running) {
            long startedAt = System.currentTimeMillis();
            try {
                runConnection();
            } catch (Exception e) {
                if (running) adapter.warn("HomeHub connection failed: " + describe(e));
            }
            if (!running) return;
            if (fatalAuthMessage != null) {
                adapter.warn("HomeHub rejected this companion: " + fatalAuthMessage);
                adapter.warn("Fix it with /homehub pair <url> <code> (mint a code in HomeHub).");
                return;
            }
            // A session that held for a while earns a fresh backoff ladder.
            if (System.currentTimeMillis() - startedAt > 60_000) backoff = BACKOFF_MIN_MS;
            adapter.info("Reconnecting to HomeHub in " + (backoff / 1000) + "s…");
            try {
                Thread.sleep(backoff);
            } catch (InterruptedException e) {
                if (!running) return;
            }
            backoff = Math.min(backoff * 2, BACKOFF_MAX_MS);
        }
    }

    private void runConnection() throws Exception {
        String base = config.url();
        if (base == null) {
            fatalAuthMessage = "no HomeHub url configured";
            return;
        }
        boolean pairing = config.token() == null;
        if (pairing && config.code() == null) {
            fatalAuthMessage = "no token and no pairing code configured";
            return;
        }
        String wsUrl = toWsUrl(base) + "/api/mc/agent";

        WebSocketFactory factory = new WebSocketFactory();
        factory.setConnectionTimeout(CONNECT_TIMEOUT_MS);
        final WebSocket ws = factory.createSocket(wsUrl);
        final CountDownLatch closed = new CountDownLatch(1);
        ws.addListener(new WebSocketAdapter() {
            @Override public void onTextMessage(WebSocket s, String text) {
                try {
                    handleFrame(text);
                } catch (RuntimeException e) {
                    adapter.warn("Error handling a HomeHub frame: " + describe(e));
                }
            }

            @Override public void onDisconnected(WebSocket s, WebSocketFrame serverFrame,
                                                 WebSocketFrame clientFrame, boolean byServer) {
                closed.countDown();
            }
        });

        authed = false;
        try {
            ws.connect();
        } catch (OpeningHandshakeException e) {
            // The HomeHub side sends specific bodies with HTTP failures — surface them.
            byte[] body = e.getBody();
            String detail = body != null && body.length > 0
                    ? " — " + new String(body, StandardCharsets.UTF_8).trim() : "";
            throw new IOException("WebSocket handshake rejected: " + e.getStatusLine() + detail);
        }

        socket = ws;
        try {
            adapter.info(pairing
                    ? "Pairing with HomeHub at " + base + "…"
                    : "Connecting to HomeHub at " + base + "…");
            ws.sendText(pairing
                    ? Frames.pair(config.code(), serverName, loader, mcVersion, companionVersion)
                    : Frames.hello(config.token(), serverName, loader, mcVersion, companionVersion));
            closed.await();
        } finally {
            socket = null;
            authed = false;
            ws.disconnect();
        }
        adapter.info("Disconnected from HomeHub.");
    }

    private void handleFrame(String text) {
        JsonObject o = Frames.parse(text);
        if (o == null) return;
        String type = Frames.str(o, "type");
        if (type == null) return;
        if ("paired".equals(type)) {
            String token = Frames.str(o, "token");
            try {
                config.storeToken(token);
                adapter.info("Paired with HomeHub — token stored in " + config.file().getName() + ".");
            } catch (IOException e) {
                adapter.warn("Paired, but could not persist the token: " + describe(e));
            }
            authed = true;
            afterAuth(true);
        } else if ("welcome".equals(type)) {
            adapter.info("Connected to HomeHub.");
            authed = true;
            afterAuth(false);
        } else if ("error".equals(type)) {
            String msg = Frames.str(o, "message");
            if (msg == null) msg = "unknown error";
            if (!authed) fatalAuthMessage = msg;
            adapter.warn("HomeHub error: " + msg);
        } else if ("cmd".equals(type)) {
            adapter.onCommand(Frames.num(o, "id"), Frames.str(o, "command"));
        } else if ("player_detail".equals(type)) {
            adapter.onPlayerDetail(Frames.num(o, "id"), Frames.str(o, "uuid"));
        } else if ("ping".equals(type)) {
            WebSocket s = socket;
            if (s != null) s.sendText(Frames.pong());
        }
    }

    /** Backfill precedes everything live; the live queue is superseded by it. */
    private void afterAuth(boolean freshlyPaired) {
        batcher.clear();
        List<ConsoleLine> lines;
        try {
            lines = adapter.backfillLines();
        } catch (RuntimeException e) {
            adapter.warn("Could not read the log for backfill: " + describe(e));
            lines = java.util.Collections.emptyList();
        }
        WebSocket s = socket;
        if (s != null) {
            s.sendText(Frames.backfill(lines));
            adapter.info("Backfilled " + lines.size() + " console line(s).");
        }
        adapter.onAuthenticated(freshlyPaired);
    }

    private static String toWsUrl(String base) {
        if (base.startsWith("https://")) return "wss://" + base.substring("https://".length());
        if (base.startsWith("http://")) return "ws://" + base.substring("http://".length());
        if (base.startsWith("ws://") || base.startsWith("wss://")) return base;
        return "ws://" + base;
    }

    private static String describe(Exception e) {
        String m = e.getMessage();
        return e.getClass().getSimpleName() + (m != null && !m.isEmpty() ? ": " + m : "");
    }
}
