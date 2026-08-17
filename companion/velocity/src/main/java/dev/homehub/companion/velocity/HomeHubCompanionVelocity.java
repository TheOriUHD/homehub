package dev.homehub.companion.velocity;

import com.google.inject.Inject;
import com.velocitypowered.api.command.CommandManager;
import com.velocitypowered.api.command.CommandMeta;
import com.velocitypowered.api.command.SimpleCommand;
import com.velocitypowered.api.event.Subscribe;
import com.velocitypowered.api.event.connection.DisconnectEvent;
import com.velocitypowered.api.event.connection.PostLoginEvent;
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent;
import com.velocitypowered.api.event.proxy.ProxyShutdownEvent;
import com.velocitypowered.api.plugin.Plugin;
import com.velocitypowered.api.proxy.Player;
import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.server.RegisteredServer;
import dev.homehub.companion.core.BackfillReader;
import dev.homehub.companion.core.CompanionClient;
import dev.homehub.companion.core.CompanionConfig;
import dev.homehub.companion.core.ConsoleLine;
import dev.homehub.companion.core.DownstreamInfo;
import dev.homehub.companion.core.PlayerInfo;
import dev.homehub.companion.core.StatusSnapshot;
import net.kyori.adventure.text.Component;
import org.slf4j.Logger;

import java.io.File;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * Velocity proxy adapter for the HomeHub companion. Same shape as the Paper
 * adapter: core does the protocol, this class wires proxy-specific capture,
 * command dispatch, and status. A proxy has no ticks, so status frames omit
 * tps/mspt and instead carry the downstream server list.
 */
@Plugin(id = "homehubcompanion", name = "HomeHub Companion", version = HomeHubCompanionVelocity.VERSION,
        description = "Connects this proxy to HomeHub for console, players, metrics, and remote commands.",
        authors = {"HomeHub"})
public final class HomeHubCompanionVelocity {

    static final String VERSION = "0.1.0";
    private static final String LOADER = "velocity";

    private final ProxyServer proxy;
    private final Logger logger;

    private File serverRoot;
    private CompanionConfig config;
    private ConsoleCapture capture;
    private volatile CompanionClient client;
    private String serverName;

    @Inject
    public HomeHubCompanionVelocity(ProxyServer proxy, Logger logger) {
        this.proxy = proxy;
        this.logger = logger;
    }

    @Subscribe
    public void onProxyInitialize(ProxyInitializeEvent event) {
        serverRoot = canonical(new File("."));
        serverName = serverRoot.getName().isEmpty() ? "velocity" : serverRoot.getName();

        config = new CompanionConfig(new File(serverRoot, "homehub-companion.properties"));
        try {
            config.load();
        } catch (IOException e) {
            logger.error("Could not read {}: {}", config.file(), e.getMessage());
        }

        capture = new ConsoleCapture(new ConsoleCapture.Sink() {
            @Override public void line(long ts, String level, String text) {
                CompanionClient c = client;
                if (c != null) c.enqueueConsole(ts, level, text);
            }
        });
        capture.attach();

        registerCommand();
        proxy.getScheduler().buildTask(this, this::pushStatus)
                .repeat(5L, TimeUnit.SECONDS)
                .schedule();

        maybeStartClient();
    }

    @Subscribe
    public void onProxyShutdown(ProxyShutdownEvent event) {
        if (capture != null) capture.detach();
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
    }

    // ---- connection ------------------------------------------------------

    private void maybeStartClient() {
        if (config.hasToken() || config.hasCode()) {
            CompanionClient c = new CompanionClient(config, new BridgeAdapter(),
                    serverName, LOADER, proxy.getVersion().getVersion(), VERSION);
            client = c;
            c.start();
        } else {
            logger.info("Not paired with HomeHub yet. Mint a pairing code in HomeHub, then either");
            logger.info("run \"homehub pair <url> <code>\" from this console, or create");
            logger.info("{} next to the proxy jar with url= and code= lines.", config.file().getName());
        }
    }

    private void restartClient() {
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
        maybeStartClient();
    }

    /** Callbacks from companion threads — the Velocity API is thread-safe. */
    private final class BridgeAdapter implements CompanionClient.Adapter {
        @Override public void onCommand(final long id, final String command) {
            final CompanionClient c = client;
            if (c == null) return;
            if (command == null || command.trim().isEmpty()) {
                c.sendCmdResult(id, false, "empty command");
                return;
            }
            proxy.getCommandManager()
                    .executeImmediatelyAsync(proxy.getConsoleCommandSource(), command)
                    .whenComplete((ok, err) -> {
                        if (err != null) {
                            String m = err.getMessage();
                            c.sendCmdResult(id, false, m != null ? m : err.getClass().getSimpleName());
                        } else {
                            c.sendCmdResult(id, Boolean.TRUE.equals(ok),
                                    Boolean.TRUE.equals(ok) ? null : "unknown or failed command");
                        }
                    });
        }

        @Override public void onAuthenticated(boolean freshlyPaired) {
            pushPlayers(null);
            pushStatus();
        }

        @Override public List<ConsoleLine> backfillLines() {
            return BackfillReader.read(new File(serverRoot, "logs" + File.separator + "latest.log"));
        }

        @Override public void info(String message) { logger.info(message); }
        @Override public void warn(String message) { logger.warn(message); }
    }

    // ---- periodic + event data ------------------------------------------

    private void pushStatus() {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        Runtime rt = Runtime.getRuntime();
        double usedMb = round1((rt.totalMemory() - rt.freeMemory()) / 1048576.0);
        double maxMb = round1(rt.maxMemory() / 1048576.0);
        List<DownstreamInfo> downstream = new ArrayList<>();
        for (RegisteredServer rs : proxy.getAllServers()) {
            downstream.add(new DownstreamInfo(rs.getServerInfo().getName(),
                    rs.getPlayersConnected().size()));
        }
        // No ticks on a proxy: tps/mspt stay null and are omitted on the wire.
        c.sendStatus(new StatusSnapshot(null, null, proxy.getPlayerCount(),
                proxy.getConfiguration().getShowMaxPlayers(), usedMb, maxMb, downstream));
    }

    /** Sends the proxy-wide player list, excluding {@code leaving} when set. */
    private void pushPlayers(Player leaving) {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        List<PlayerInfo> players = new ArrayList<>();
        for (Player p : proxy.getAllPlayers()) {
            if (leaving != null && p.getUniqueId().equals(leaving.getUniqueId())) continue;
            players.add(new PlayerInfo(p.getUsername(), p.getUniqueId().toString()));
        }
        c.sendPlayers(players);
    }

    @Subscribe
    public void onPostLogin(PostLoginEvent event) {
        pushPlayers(null);
    }

    @Subscribe
    public void onDisconnect(DisconnectEvent event) {
        // The leaving player may still be in getAllPlayers() inside the event.
        pushPlayers(event.getPlayer());
    }

    // ---- /homehub --------------------------------------------------------

    private void registerCommand() {
        CommandManager cm = proxy.getCommandManager();
        CommandMeta meta = cm.metaBuilder("homehub").plugin(this).build();
        cm.register(meta, new SimpleCommand() {
            @Override public void execute(Invocation invocation) {
                String[] args = invocation.arguments();
                if (args.length >= 1 && "pair".equalsIgnoreCase(args[0])) {
                    if (args.length != 3) {
                        reply(invocation, "Usage: /homehub pair <url> <code>");
                        return;
                    }
                    try {
                        config.storePairing(args[1], args[2]);
                    } catch (IOException e) {
                        reply(invocation, "Could not write " + config.file().getName() + ": " + e.getMessage());
                        return;
                    }
                    reply(invocation, "Pairing with " + config.url() + " — watch the console for the result.");
                    restartClient();
                    return;
                }
                if (args.length >= 1 && "status".equalsIgnoreCase(args[0])) {
                    CompanionClient c = client;
                    reply(invocation, "HomeHub companion " + VERSION + ": "
                            + (c == null ? "not configured — run /homehub pair <url> <code>" : c.describeState()));
                    return;
                }
                reply(invocation, "Usage: /homehub pair <url> <code> | /homehub status");
            }

            @Override public boolean hasPermission(Invocation invocation) {
                return !(invocation.source() instanceof Player)
                        || invocation.source().hasPermission("homehub.admin");
            }
        });
    }

    private static void reply(SimpleCommand.Invocation invocation, String message) {
        invocation.source().sendMessage(Component.text(message));
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
}
