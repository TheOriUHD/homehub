package dev.homehub.companion.forge;

import com.mojang.brigadier.CommandDispatcher;
import com.mojang.brigadier.arguments.StringArgumentType;
import com.mojang.brigadier.builder.LiteralArgumentBuilder;
import com.mojang.brigadier.builder.RequiredArgumentBuilder;
import dev.homehub.companion.core.BackfillReader;
import dev.homehub.companion.core.CompanionClient;
import dev.homehub.companion.core.CompanionConfig;
import dev.homehub.companion.core.ConsoleLine;
import dev.homehub.companion.core.PlayerInfo;
import dev.homehub.companion.core.StatusSnapshot;
import dev.homehub.companion.forge.compat.Mc;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import net.minecraftforge.api.distmarker.Dist;
import net.minecraftforge.event.RegisterCommandsEvent;
import net.minecraftforge.event.entity.player.PlayerEvent;
import net.minecraftforge.event.server.ServerStartedEvent;
import net.minecraftforge.event.server.ServerStoppingEvent;
import net.minecraftforge.fml.common.Mod;
import net.minecraftforge.fml.loading.FMLEnvironment;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.File;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * Forge dedicated-server adapter for the HomeHub companion. Server-side only:
 * on any other dist the constructor is a no-op. The server instance arrives
 * with ServerStartedEvent; a companion-owned timer hops onto the server
 * thread (server.execute) for every MC-facing read, mirroring the Paper
 * adapter's scheduler discipline. Compiled once per game version; whatever
 * Forge or Mojang renamed between them goes through {@link Mc}.
 */
@Mod("homehubcompanion")
public final class HomeHubCompanionForge {

    public static final String VERSION = "0.1.0";
    private static final String LOADER = "forge";
    private static final Logger LOG = LoggerFactory.getLogger("homehubcompanion");

    private File serverRoot;
    private CompanionConfig config;
    private ConsoleCapture capture;
    private volatile CompanionClient client;
    private volatile MinecraftServer server;
    private ScheduledExecutorService statusTimer;

    public HomeHubCompanionForge() {
        if (FMLEnvironment.dist != Dist.DEDICATED_SERVER) {
            return; // server-side companion; do nothing on a client
        }
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

        Mc.subscribe(this::onServerStarted, this::onServerStopping, this::onRegisterCommands,
                this::onPlayerLoggedIn, this::onPlayerLoggedOut);

        maybeStartClient();
    }

    // ---- lifecycle -------------------------------------------------------

    private void onServerStarted(ServerStartedEvent event) {
        server = event.getServer();
        statusTimer = Executors.newSingleThreadScheduledExecutor(r -> {
            Thread t = new Thread(r, "HomeHub-Status");
            t.setDaemon(true);
            return t;
        });
        statusTimer.scheduleAtFixedRate(() -> {
            MinecraftServer s = server;
            if (s != null) s.execute(() -> pushStatus(s));
        }, 5, 5, TimeUnit.SECONDS);
    }

    private void onServerStopping(ServerStoppingEvent event) {
        if (statusTimer != null) statusTimer.shutdownNow();
        if (capture != null) capture.detach();
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
    }

    private void onPlayerLoggedIn(PlayerEvent.PlayerLoggedInEvent event) {
        MinecraftServer s = server;
        if (s != null) s.execute(() -> pushPlayers(s));
    }

    private void onPlayerLoggedOut(PlayerEvent.PlayerLoggedOutEvent event) {
        MinecraftServer s = server;
        // One tick later the leaving player is gone from the list.
        if (s != null) s.execute(() -> s.execute(() -> pushPlayers(s)));
    }

    // ---- connection ------------------------------------------------------

    private void maybeStartClient() {
        if (config.hasToken() || config.hasCode()) {
            CompanionClient c = new CompanionClient(config, new BridgeAdapter(),
                    serverRoot.getName().isEmpty() ? "minecraft" : serverRoot.getName(),
                    LOADER, Mc.gameVersion(), VERSION);
            client = c;
            c.start();
        } else {
            LOG.info("Not paired with HomeHub yet. Mint a pairing code in HomeHub, then either");
            LOG.info("run \"homehub pair <url> <code>\" from this console, or create");
            LOG.info("{} next to the server jar with url= and code= lines.", config.file().getName());
        }
    }

    private void restartClient() {
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
        maybeStartClient();
    }

    /** Callbacks from companion threads — MC work hops onto the server thread. */
    private final class BridgeAdapter implements CompanionClient.Adapter {
        @Override public void onPlayerDetail(final long id, final String uuid) {
            // Full per-player detail (inventory, stats) is gathered on
            // Paper/Spigot subservers; this loader reports none for now.
            final CompanionClient c = client;
            if (c != null) c.sendPlayerDetail(id, null);
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

    // ---- data (server thread only) ---------------------------------------

    private void pushStatus(MinecraftServer s) {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        Runtime rt = Runtime.getRuntime();
        double usedMb = round1((rt.totalMemory() - rt.freeMemory()) / 1048576.0);
        double maxMb = round1(rt.maxMemory() / 1048576.0);
        double msptRaw = Mc.averageTickMillis(s);
        Double mspt = msptRaw > 0 ? round2(msptRaw) : null;
        Double tps = mspt == null ? null : Math.min(round2(1000.0 / mspt), 20.0);
        c.sendStatus(new StatusSnapshot(tps, mspt,
                s.getPlayerList().getPlayerCount(), s.getPlayerList().getMaxPlayers(),
                usedMb, maxMb));
    }

    private void pushPlayers(MinecraftServer s) {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        List<PlayerInfo> players = new ArrayList<>();
        for (ServerPlayer p : s.getPlayerList().getPlayers()) {
            players.add(new PlayerInfo(Mc.profileName(p), p.getUUID().toString()));
        }
        c.sendPlayers(players);
    }

    // ---- /homehub --------------------------------------------------------

    private void onRegisterCommands(RegisterCommandsEvent event) {
        CommandDispatcher<CommandSourceStack> dispatcher = event.getDispatcher();
        dispatcher.register(
                LiteralArgumentBuilder.<CommandSourceStack>literal("homehub")
                        .requires(Mc::isAdmin)
                        .then(LiteralArgumentBuilder.<CommandSourceStack>literal("pair")
                                .then(RequiredArgumentBuilder.<CommandSourceStack, String>argument("url", StringArgumentType.string())
                                        .then(RequiredArgumentBuilder.<CommandSourceStack, String>argument("code", StringArgumentType.string())
                                                .executes(ctx -> {
                                                    String url = StringArgumentType.getString(ctx, "url");
                                                    String code = StringArgumentType.getString(ctx, "code");
                                                    try {
                                                        config.storePairing(url, code);
                                                    } catch (IOException e) {
                                                        reply(ctx.getSource(), "Could not write "
                                                                + config.file().getName() + ": " + e.getMessage());
                                                        return 0;
                                                    }
                                                    reply(ctx.getSource(), "Pairing with " + config.url()
                                                            + " — watch the console for the result.");
                                                    restartClient();
                                                    return 1;
                                                }))))
                        .then(LiteralArgumentBuilder.<CommandSourceStack>literal("status")
                                .executes(ctx -> {
                                    CompanionClient c = client;
                                    reply(ctx.getSource(), "HomeHub companion " + VERSION + ": "
                                            + (c == null ? "not configured — run /homehub pair <url> <code>"
                                                         : c.describeState()));
                                    return 1;
                                })));
    }

    private static void reply(CommandSourceStack source, String message) {
        source.sendSystemMessage(Component.literal(message));
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
