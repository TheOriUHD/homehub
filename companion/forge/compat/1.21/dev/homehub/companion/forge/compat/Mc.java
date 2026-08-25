package dev.homehub.companion.forge.compat;

import net.minecraft.SharedConstants;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import net.minecraftforge.common.MinecraftForge;
import net.minecraftforge.event.RegisterCommandsEvent;
import net.minecraftforge.event.entity.player.PlayerEvent;
import net.minecraftforge.event.server.ServerStartedEvent;
import net.minecraftforge.event.server.ServerStoppingEvent;

import java.util.function.Consumer;

/**
 * Everything the shared Forge sources call that Forge or Mojang renamed
 * between game versions. One copy per era under forge/compat/; the target's
 * gradle.properties picks which one is compiled in. Keep the signatures
 * identical across eras — the shared code is written against them.
 *
 * Era: Forge 1.21 – 1.21.5 (EventBus 6, op-level permissions, GameProfile#getName).
 */
public final class Mc {
    private Mc() {}

    /** EventBus 6: one global bus for the whole game. */
    public static void subscribe(Consumer<ServerStartedEvent> started, Consumer<ServerStoppingEvent> stopping,
                                 Consumer<RegisterCommandsEvent> commands,
                                 Consumer<PlayerEvent.PlayerLoggedInEvent> loggedIn,
                                 Consumer<PlayerEvent.PlayerLoggedOutEvent> loggedOut) {
        MinecraftForge.EVENT_BUS.addListener(started);
        MinecraftForge.EVENT_BUS.addListener(stopping);
        MinecraftForge.EVENT_BUS.addListener(commands);
        MinecraftForge.EVENT_BUS.addListener(loggedIn);
        MinecraftForge.EVENT_BUS.addListener(loggedOut);
    }

    public static String gameVersion() { return SharedConstants.getCurrentVersion().getName(); }

    public static double averageTickMillis(MinecraftServer s) { return s.getAverageTickTimeNanos() / 1_000_000.0; }

    public static String profileName(ServerPlayer p) { return p.getGameProfile().getName(); }

    // Op level 4 is what /homehub asked for before the permission registry existed.
    public static boolean isAdmin(CommandSourceStack src) { return src.hasPermission(4); }
}
