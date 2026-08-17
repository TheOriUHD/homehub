package dev.homehub.companion.paper;

import org.bukkit.Server;

import java.lang.reflect.Field;
import java.lang.reflect.Method;

/**
 * TPS/MSPT with graceful omission. The plugin compiles against spigot-api
 * 1.16.5, which has neither accessor, so everything here is reflection:
 *   Paper:  Server#getTPS() → double[], Server#getAverageTickTime() → double
 *   Spigot: CraftServer#getServer() → MinecraftServer.recentTps (public field)
 * Whatever cannot be resolved returns null and the status frame omits the key.
 */
final class TickStats {
    private final Server server;
    private Method getTps;        // Paper
    private Method getAvgTick;    // Paper
    private Object mcServer;      // Spigot fallback
    private Field recentTps;      // Spigot fallback
    private boolean resolved;

    TickStats(Server server) {
        this.server = server;
    }

    private void resolve() {
        if (resolved) return;
        resolved = true;
        try {
            getTps = server.getClass().getMethod("getTPS");
        } catch (ReflectiveOperationException | RuntimeException ignored) {}
        try {
            getAvgTick = server.getClass().getMethod("getAverageTickTime");
        } catch (ReflectiveOperationException | RuntimeException ignored) {}
        if (getTps == null) {
            try {
                mcServer = server.getClass().getMethod("getServer").invoke(server);
                recentTps = mcServer.getClass().getField("recentTps");
            } catch (ReflectiveOperationException | RuntimeException ignored) {
                mcServer = null;
                recentTps = null;
            }
        }
    }

    /** 1-minute TPS, capped at 20; null when the platform cannot report it. */
    Double tps() {
        resolve();
        try {
            double[] arr = null;
            if (getTps != null) {
                arr = (double[]) getTps.invoke(server);
            } else if (recentTps != null) {
                arr = (double[]) recentTps.get(mcServer);
            }
            if (arr == null || arr.length == 0) return null;
            return Math.min(round2(arr[0]), 20.0);
        } catch (ReflectiveOperationException | RuntimeException e) {
            return null;
        }
    }

    /** Mean ms per tick (Paper only); null elsewhere. */
    Double mspt() {
        resolve();
        try {
            if (getAvgTick == null) return null;
            return round2(((Number) getAvgTick.invoke(server)).doubleValue());
        } catch (ReflectiveOperationException | RuntimeException e) {
            return null;
        }
    }

    private static double round2(double v) {
        return Math.round(v * 100.0) / 100.0;
    }
}
