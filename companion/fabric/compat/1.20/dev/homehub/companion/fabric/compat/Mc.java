package dev.homehub.companion.fabric.compat;

import net.minecraft.commands.CommandSourceStack;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.ResourceLocation;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.stats.ServerStatsCounter;
import net.minecraft.stats.Stats;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.enchantment.Enchantment;
import net.minecraft.world.item.enchantment.EnchantmentHelper;
import net.minecraft.world.level.GameType;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Everything the shared Fabric sources call that Mojang renamed between game
 * versions. One copy of this class per era under fabric/compat/; the target's
 * gradle.properties picks which one is compiled in. Keep the signatures
 * identical across eras — the shared code is written against them.
 *
 * Era: 1.20 – 1.20.1 (NBT item data, ServerPlayer#latency field, registry lookups).
 */
public final class Mc {
    private Mc() {}
    public static String profileName(ServerPlayer p) { return p.getGameProfile().getName(); }

    public static String gameMode(ServerPlayer p) {
        GameType gm = p.gameMode.getGameModeForPlayer();
        return gm == null ? null : gm.getName();
    }

    public static String dimension(ServerPlayer p) { return p.level().dimension().location().toString(); }

    public static int selectedSlot(ServerPlayer p) { return p.getInventory().selected; }

    public static List<ItemStack> mainItems(ServerPlayer p) { return p.getInventory().items; }

    public static Integer latency(ServerPlayer p) { return p.latency; }

    public static String effectId(MobEffectInstance e) {
        ResourceLocation key = BuiltInRegistries.MOB_EFFECT.getKey(e.getEffect());
        return key == null ? "unknown" : key.toString();
    }

    public static String customName(ItemStack stack) {
        return stack.hasCustomHoverName() ? stack.getHoverName().getString() : null;
    }

    public static Map<String, Integer> enchantments(ItemStack stack) {
        Map<String, Integer> out = new LinkedHashMap<>();
        for (Map.Entry<Enchantment, Integer> e : EnchantmentHelper.getEnchantments(stack).entrySet()) {
            ResourceLocation key = BuiltInRegistries.ENCHANTMENT.getKey(e.getKey());
            if (key != null) out.put(key.toString(), e.getValue());
        }
        return out;
    }

    public static int customStat(ServerStatsCounter sc, Object id) { return sc.getValue(Stats.CUSTOM.get((ResourceLocation) id)); }

    // Op level 4 is what /homehub asked for before the permission registry existed.
    public static boolean isAdmin(CommandSourceStack src) { return src.hasPermission(4); }
}
