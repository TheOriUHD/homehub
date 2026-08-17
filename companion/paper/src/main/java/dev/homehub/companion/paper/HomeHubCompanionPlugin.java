package dev.homehub.companion.paper;

import dev.homehub.companion.core.BackfillReader;
import dev.homehub.companion.core.CompanionClient;
import dev.homehub.companion.core.CompanionConfig;
import dev.homehub.companion.core.ConsoleLine;
import dev.homehub.companion.core.PlayerInfo;
import dev.homehub.companion.core.StatusSnapshot;
import org.bukkit.Bukkit;
import org.bukkit.command.Command;
import org.bukkit.command.CommandSender;
import org.bukkit.entity.Player;
import org.bukkit.event.EventHandler;
import org.bukkit.event.EventPriority;
import org.bukkit.event.Listener;
import org.bukkit.event.player.PlayerJoinEvent;
import org.bukkit.event.player.PlayerQuitEvent;
import org.bukkit.plugin.java.JavaPlugin;

import java.io.File;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;

/**
 * Paper/Spigot adapter for the HomeHub companion. One jar serves both
 * platforms from 1.16.5 up; Paper-only APIs are reached via reflection.
 */
public final class HomeHubCompanionPlugin extends JavaPlugin implements Listener {

    private File serverRoot;
    private CompanionConfig config;
    private ConsoleCapture capture;
    private TickStats tickStats;
    private volatile CompanionClient client;
    private String loader;
    private String mcVersion;
    private String serverName;

    @Override
    public void onEnable() {
        serverRoot = canonical(Bukkit.getWorldContainer());
        loader = detectLoader();
        mcVersion = Bukkit.getBukkitVersion().split("-")[0];
        serverName = serverRoot.getName().isEmpty() ? "minecraft" : serverRoot.getName();
        tickStats = new TickStats(Bukkit.getServer());

        config = new CompanionConfig(new File(serverRoot, "homehub-companion.properties"));
        try {
            config.load();
        } catch (IOException e) {
            getLogger().severe("Could not read " + config.file() + ": " + e.getMessage());
        }

        capture = new ConsoleCapture(new ConsoleCapture.Sink() {
            @Override public void line(long ts, String level, String text) {
                CompanionClient c = client;
                if (c != null) c.enqueueConsole(ts, level, text);
            }
        });
        capture.attach();

        Bukkit.getPluginManager().registerEvents(this, this);
        Bukkit.getScheduler().runTaskTimer(this, new Runnable() {
            @Override public void run() { pushStatus(); }
        }, 100L, 100L);

        maybeStartClient();
    }

    @Override
    public void onDisable() {
        if (capture != null) capture.detach();
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
    }

    // ---- connection ------------------------------------------------------

    private void maybeStartClient() {
        if (config.hasToken() || config.hasCode()) {
            CompanionClient c = new CompanionClient(config, new BridgeAdapter(),
                    serverName, loader, mcVersion, getDescription().getVersion());
            client = c;
            c.start();
        } else {
            getLogger().info("Not paired with HomeHub yet. Mint a pairing code in HomeHub, then either");
            getLogger().info("run \"homehub pair <url> <code>\" from this console, or create");
            getLogger().info(config.file().getName() + " next to the server jar with url= and code= lines.");
        }
    }

    private void restartClient() {
        CompanionClient c = client;
        client = null;
        if (c != null) c.stop();
        maybeStartClient();
    }

    /** Callbacks from companion threads — everything Bukkit goes through the scheduler. */
    private final class BridgeAdapter implements CompanionClient.Adapter {
        @Override public void onCommand(final long id, final String command) {
            final CompanionClient c = client;
            if (c == null) return;
            if (command == null || command.trim().isEmpty()) {
                c.sendCmdResult(id, false, "empty command");
                return;
            }
            try {
                Bukkit.getScheduler().runTask(HomeHubCompanionPlugin.this, new Runnable() {
                    @Override public void run() {
                        boolean ok;
                        String message = null;
                        try {
                            ok = Bukkit.dispatchCommand(Bukkit.getConsoleSender(), command);
                            if (!ok) {
                                message = "unknown or failed command";
                                // Print into the console stream like a real
                                // console would, so every viewer sees it.
                                getLogger().warning("Unknown command '" + command + "'");
                            }
                        } catch (Throwable t) {
                            ok = false;
                            message = t.getMessage() != null ? t.getMessage() : t.getClass().getSimpleName();
                            getLogger().warning("Command '" + command + "' failed: " + message);
                        }
                        c.sendCmdResult(id, ok, message);
                    }
                });
            } catch (IllegalStateException e) {
                // Plugin disabled mid-flight.
                c.sendCmdResult(id, false, "plugin is disabled");
            }
        }

        @Override public void onAuthenticated(boolean freshlyPaired) {
            try {
                Bukkit.getScheduler().runTask(HomeHubCompanionPlugin.this, new Runnable() {
                    @Override public void run() {
                        pushPlayers();
                        pushStatus();
                    }
                });
            } catch (IllegalStateException ignored) {}
        }

        @Override public List<ConsoleLine> backfillLines() {
            return BackfillReader.read(new File(serverRoot, "logs" + File.separator + "latest.log"));
        }

        @Override public void info(String message) { getLogger().info(message); }
        @Override public void warn(String message) { getLogger().warning(message); }
    }

    // ---- periodic + event data ------------------------------------------

    private void pushStatus() {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        Runtime rt = Runtime.getRuntime();
        double usedMb = round1((rt.totalMemory() - rt.freeMemory()) / 1048576.0);
        double maxMb = round1(rt.maxMemory() / 1048576.0);
        c.sendStatus(new StatusSnapshot(tickStats.tps(), tickStats.mspt(),
                Bukkit.getOnlinePlayers().size(), Bukkit.getMaxPlayers(), usedMb, maxMb));
    }

    private void pushPlayers() {
        CompanionClient c = client;
        if (c == null || !c.isConnected()) return;
        List<PlayerInfo> players = new ArrayList<PlayerInfo>();
        for (Player p : Bukkit.getOnlinePlayers()) {
            players.add(new PlayerInfo(p.getName(), p.getUniqueId().toString()));
        }
        c.sendPlayers(players);
    }

    @EventHandler(priority = EventPriority.MONITOR)
    public void onJoin(PlayerJoinEvent event) {
        // The joining player is already online inside the event.
        pushPlayers();
    }

    @EventHandler(priority = EventPriority.MONITOR)
    public void onQuit(PlayerQuitEvent event) {
        // One tick later the quitting player is gone from the online list.
        Bukkit.getScheduler().runTask(this, new Runnable() {
            @Override public void run() { pushPlayers(); }
        });
    }

    // ---- /homehub --------------------------------------------------------

    @Override
    public boolean onCommand(CommandSender sender, Command command, String label, String[] args) {
        if (args.length >= 1 && "pair".equalsIgnoreCase(args[0])) {
            if (args.length != 3) {
                sender.sendMessage("Usage: /" + label + " pair <url> <code>");
                return true;
            }
            try {
                config.storePairing(args[1], args[2]);
            } catch (IOException e) {
                sender.sendMessage("Could not write " + config.file().getName() + ": " + e.getMessage());
                return true;
            }
            sender.sendMessage("Pairing with " + config.url() + " — watch the console for the result.");
            restartClient();
            return true;
        }
        if (args.length >= 1 && "status".equalsIgnoreCase(args[0])) {
            CompanionClient c = client;
            sender.sendMessage("HomeHub companion " + getDescription().getVersion() + ": "
                    + (c == null ? "not configured — run /" + label + " pair <url> <code>" : c.describeState()));
            return true;
        }
        return false;
    }

    // ---- helpers ---------------------------------------------------------

    private static String detectLoader() {
        String[] paperMarkers = {
                "com.destroystokyo.paper.PaperConfig",          // Paper 1.8 – 1.18
                "io.papermc.paper.configuration.Configuration", // Paper 1.19+
        };
        for (String cls : paperMarkers) {
            try {
                Class.forName(cls);
                return "paper";
            } catch (ClassNotFoundException | LinkageError ignored) {}
        }
        return "spigot";
    }

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
