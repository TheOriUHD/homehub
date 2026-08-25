package dev.homehub.companion.fabric;

import dev.homehub.companion.core.BackfillReader;
import dev.homehub.companion.core.CompanionClient;
import dev.homehub.companion.core.CompanionConfig;
import dev.homehub.companion.core.ConsoleLine;
import dev.homehub.companion.core.PlayerInfo;
import dev.homehub.companion.core.StatusSnapshot;
import dev.homehub.companion.fabric.compat.Mc;
import net.fabricmc.api.DedicatedServerModInitializer;
import net.fabricmc.loader.api.FabricLoader;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.File;
import java.io.IOException;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Fabric dedicated-server adapter for the HomeHub companion. Deliberately
 * fabric-api-free: the server instance and tick timings arrive from
 * {@code MinecraftServerMixin}, and /homehub registers via
 * {@code CommandsMixin}. Everything MC-facing runs on the server thread
 * (either inside the tick hook or via {@code server.execute}).
 */
public final class HomeHubCompanionFabric implements DedicatedServerModInitializer {

    public static final String VERSION = "0.1.0";
    private static final String LOADER = "fabric";
    private static final Logger LOG = LoggerFactory.getLogger("homehubcompanion");

    /** Rolling window for mspt (100 ticks = 5s at 20 tps). */
    private static final int TICK_WINDOW = 100;
    private static final int STATUS_EVERY_TICKS = 100;

    private static volatile HomeHubCompanionFabric instance;

    private File serverRoot;
    private CompanionConfig config;
    private ConsoleCapture capture;
    private volatile CompanionClient client;
    private volatile MinecraftServer server;

    // Tick accounting — touched only on the server thread.
    private long tickStartNanos;
    private final long[] tickNanos = new long[TICK_WINDOW];
    private int tickIndex;
    private int tickCount;
    private Set<String> lastPlayerNames = new HashSet<>();
    private final java.util.Map<java.util.UUID, Integer> lastHeldSlots = new java.util.HashMap<>();

    public static HomeHubCompanionFabric instance() {
        return instance;
    }

    @Override
    public void onInitializeServer() {
        instance = this;
        serverRoot = canonical(new File("."));

        config = new CompanionConfig(new File(serverRoot, "homehub-companion.properties"));
        try {
            config.load();
        } catch (IOException e) {
            LOG.error("Could not read {}: {}", config.file(), e.getMessage());
        }

        capture = new ConsoleCapture((ts, level, text) -> {
            CompanionClient c = client;
            if (c != null) c.enqueueConsole(ts, level, text);
        });
        capture.attach();

        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            CompanionClient c = client;
            client = null;
            if (c != null) c.stop();
        }, "HomeHub-Companion-Shutdown"));

        maybeStartClient();
    }

    // ---- connection ------------------------------------------------------

    private void maybeStartClient() {
        if (config.hasToken() || config.hasCode()) {
            String mcVersion = FabricLoader.getInstance().getModContainer("minecraft")
                    .map(m -> m.getMetadata().getVersion().getFriendlyString()).orElse(null);
            CompanionClient c = new CompanionClient(config, new BridgeAdapter(),
                    serverRoot.getName().isEmpty() ? "minecraft" : serverRoot.getName(),
                    LOADER, mcVersion, VERSION);
            client = c;
            c.start();
        } else {
            LOG.info("Not paired with HomeHub yet. Mint a pairing code in HomeHub, then either");
            LOG.info("run \"homehub pair <url> <code>\" from this console, or create");
            LOG.info("{} next to the server jar with url= and code= lines.", config.file().getName());
        }
    }

    void restartClient() {
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
        maybeStartClient();
    }

    CompanionClient client() {
        return client;
    }

    CompanionConfig config() {
        return config;
    }

    /** Callbacks from companion threads — MC work hops onto the server thread. */
    private final class BridgeAdapter implements CompanionClient.Adapter {
        @Override public void onPlayerDetail(final long id, final String uuid) {
            final CompanionClient c = client;
            if (c == null) return;
            final MinecraftServer s = server;
            if (s == null) { c.sendPlayerDetail(id, null); return; }
            // Gather on the server thread — inventory/stats reads are not
            // thread-safe off-tick.
            s.execute(() -> {
                String detail = null;
                try {
                    ServerPlayer p = findPlayer(s, uuid);
                    if (p != null) detail = FabricPlayerDetailBuilder.build(p).toString();
                } catch (Throwable t) {
                    LOG.warn("player_detail build failed: {}", t.toString());
                }
                c.sendPlayerDetail(id, detail);
            });
        }

        @Override public void onCommand(final long id, final String command) {
            final CompanionClient c = client;
            if (c == null) return;
            if (command == null || command.trim().isEmpty()) {
                c.sendCmdResult(id, false, "empty command");
                return;
            }
            final MinecraftServer s = server;
            if (s == null) {
                c.sendCmdResult(id, false, "server still starting");
                return;
            }
            s.execute(() -> {
                boolean ok;
                String message = null;
                try {
                    s.getCommands().performPrefixedCommand(s.createCommandSourceStack(), command);
                    ok = true;
                } catch (Throwable t) {
                    ok = false;
                    message = t.getMessage() != null ? t.getMessage() : t.getClass().getSimpleName();
                }
                c.sendCmdResult(id, ok, message);
            });
        }

        @Override public void onAuthenticated(boolean freshlyPaired) {
            MinecraftServer s = server;
            if (s == null) return;
            s.execute(() -> {
                pushPlayers(s);
                pushStatus(s);
            });
        }

        @Override public List<ConsoleLine> backfillLines() {
            return BackfillReader.read(new File(serverRoot, "logs" + File.separator + "latest.log"));
        }

        @Override public void info(String message) { LOG.info(message); }
        @Override public void warn(String message) { LOG.warn(message); }
    }

    // ---- tick hooks (server thread, via MinecraftServerMixin) -------------

    public static void onTickStart(MinecraftServer srv) {
        HomeHubCompanionFabric self = instance;
        if (self != null) self.tickStartNanos = System.nanoTime();
    }

    public static void onTickEnd(MinecraftServer srv) {
        HomeHubCompanionFabric self = instance;
        if (self != null) self.handleTickEnd(srv);
    }

    private void handleTickEnd(MinecraftServer srv) {
        server = srv;
        if (tickStartNanos != 0) {
            tickNanos[tickIndex] = System.nanoTime() - tickStartNanos;
            tickIndex = (tickIndex + 1) % TICK_WINDOW;
            if (tickCount < Integer.MAX_VALUE) tickCount++;
        }
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        if (tickCount % STATUS_EVERY_TICKS == 0) pushStatus(srv);

        Set<String> names = new HashSet<>();
        boolean heldChanged = false;
        for (ServerPlayer p : srv.getPlayerList().getPlayers()) {
            names.add(Mc.profileName(p));
            try {
                Integer held = Mc.selectedSlot(p);
                if (!held.equals(lastHeldSlots.put(p.getUUID(), held))) heldChanged = true;
            } catch (Throwable ignored) {}
        }
        lastHeldSlots.keySet().removeIf(u -> {
            for (ServerPlayer p : srv.getPlayerList().getPlayers()) if (p.getUUID().equals(u)) return false;
            return true;
        });
        // Push on membership or hotbar change immediately; otherwise refresh
        // the live fields (position, health) once a second while occupied.
        if (!names.equals(lastPlayerNames) || heldChanged) {
            lastPlayerNames = names;
            pushPlayers(srv);
        } else if (!names.isEmpty() && tickCount % 20 == 0) {
            pushPlayers(srv);
        }
    }

    // ---- data ------------------------------------------------------------

    private void pushStatus(MinecraftServer srv) {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        Runtime rt = Runtime.getRuntime();
        double usedMb = round1((rt.totalMemory() - rt.freeMemory()) / 1048576.0);
        double maxMb = round1(rt.maxMemory() / 1048576.0);
        Double mspt = averageMspt();
        Double tps = mspt == null || mspt <= 0 ? null : Math.min(round2(1000.0 / mspt), 20.0);
        c.sendStatus(new StatusSnapshot(tps, mspt,
                srv.getPlayerList().getPlayerCount(), srv.getPlayerList().getMaxPlayers(),
                usedMb, maxMb));
    }

    private void pushPlayers(MinecraftServer srv) {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        List<PlayerInfo> players = new ArrayList<>();
        for (ServerPlayer p : srv.getPlayerList().getPlayers()) {
            PlayerInfo pi = new PlayerInfo(Mc.profileName(p), p.getUUID().toString());
            enrich(pi, p);
            players.add(pi);
        }
        c.sendPlayers(players);
    }

    /** Fill the cheap live fields on the streaming player entry (server thread). */
    private static void enrich(PlayerInfo pi, ServerPlayer p) {
        try {
            String gm = Mc.gameMode(p);
            if (gm != null) pi.gameMode = gm;
        } catch (Throwable ignored) {}
        try {
            pi.world = Mc.dimension(p);
            pi.hasPos = true;
            pi.x = FabricPlayerDetailBuilder.round2(p.getX());
            pi.y = FabricPlayerDetailBuilder.round2(p.getY());
            pi.z = FabricPlayerDetailBuilder.round2(p.getZ());
        } catch (Throwable ignored) {}
        try {
            pi.health = FabricPlayerDetailBuilder.round1(p.getHealth());
            pi.maxHealth = FabricPlayerDetailBuilder.round1(p.getMaxHealth());
        } catch (Throwable ignored) {}
        try { pi.food = p.getFoodData().getFoodLevel(); } catch (Throwable ignored) {}
        try { pi.heldSlot = Mc.selectedSlot(p); } catch (Throwable ignored) {}
        try { pi.level = p.experienceLevel; } catch (Throwable ignored) {}
        try { pi.pingMs = Mc.latency(p); } catch (Throwable ignored) {}
    }

    private static ServerPlayer findPlayer(MinecraftServer s, String uuid) {
        for (ServerPlayer p : s.getPlayerList().getPlayers()) {
            if (p.getUUID().toString().equals(uuid) || Mc.profileName(p).equals(uuid)) return p;
        }
        return null;
    }

    /** Mean ms per tick over the filled part of the window; null before the first tick. */
    private Double averageMspt() {
        int n = Math.min(tickCount, TICK_WINDOW);
        if (n == 0) return null;
        long total = 0;
        for (int i = 0; i < n; i++) total += tickNanos[i];
        return round2(total / (double) n / 1_000_000.0);
    }

    // ---- helpers ---------------------------------------------------------

    private static File canonical(File f) {
        try {
            return f.getCanonicalFile();
        } catch (IOException e) {
            return f.getAbsoluteFile();
        }
    }

    private static double round1(double v) {
        return Math.round(v * 10.0) / 10.0;
    }

    private static double round2(double v) {
        return Math.round(v * 100.0) / 100.0;
    }
}
