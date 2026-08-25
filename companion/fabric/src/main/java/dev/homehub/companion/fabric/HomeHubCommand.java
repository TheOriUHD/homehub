package dev.homehub.companion.fabric;

import com.mojang.brigadier.CommandDispatcher;
import com.mojang.brigadier.arguments.StringArgumentType;
import com.mojang.brigadier.builder.LiteralArgumentBuilder;
import com.mojang.brigadier.builder.RequiredArgumentBuilder;
import dev.homehub.companion.core.CompanionClient;
import dev.homehub.companion.fabric.compat.Mc;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.network.chat.Component;

import java.io.IOException;

/**
 * /homehub pair <url> <code> | /homehub status — registered straight into the
 * Brigadier dispatcher by {@code CommandsMixin} (no fabric-api).
 */
public final class HomeHubCommand {
    private HomeHubCommand() {}

    public static void register(CommandDispatcher<CommandSourceStack> dispatcher) {
        dispatcher.register(
                LiteralArgumentBuilder.<CommandSourceStack>literal("homehub")
                        .requires(Mc::isAdmin)
                        .then(LiteralArgumentBuilder.<CommandSourceStack>literal("pair")
                                .then(RequiredArgumentBuilder.<CommandSourceStack, String>argument("url", StringArgumentType.string())
                                        .then(RequiredArgumentBuilder.<CommandSourceStack, String>argument("code", StringArgumentType.string())
                                                .executes(ctx -> {
                                                    String url = StringArgumentType.getString(ctx, "url");
                                                    String code = StringArgumentType.getString(ctx, "code");
                                                    return pair(ctx.getSource(), url, code);
                                                }))))
                        .then(LiteralArgumentBuilder.<CommandSourceStack>literal("status")
                                .executes(ctx -> status(ctx.getSource()))));
    }

    private static int pair(CommandSourceStack source, String url, String code) {
        HomeHubCompanionFabric mod = HomeHubCompanionFabric.instance();
        if (mod == null) {
            reply(source, "HomeHub companion is not initialized.");
            return 0;
        }
        try {
            mod.config().storePairing(url, code);
        } catch (IOException e) {
            reply(source, "Could not write " + mod.config().file().getName() + ": " + e.getMessage());
            return 0;
        }
        reply(source, "Pairing with " + mod.config().url() + " — watch the console for the result.");
        mod.restartClient();
        return 1;
    }

    private static int status(CommandSourceStack source) {
        HomeHubCompanionFabric mod = HomeHubCompanionFabric.instance();
        CompanionClient c = mod == null ? null : mod.client();
        reply(source, "HomeHub companion " + HomeHubCompanionFabric.VERSION + ": "
                + (c == null ? "not configured — run /homehub pair <url> <code>" : c.describeState()));
        return 1;
    }

    private static void reply(CommandSourceStack source, String message) {
        source.sendSystemMessage(Component.literal(message));
    }
}
