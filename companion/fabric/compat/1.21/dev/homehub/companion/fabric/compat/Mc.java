package dev.homehub.companion.fabric.compat;

import it.unimi.dsi.fastutil.objects.Object2IntMap;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.core.Holder;
import net.minecraft.core.component.DataComponents;
import net.minecraft.network.chat.Component;
import net.minecraft.resources.ResourceLocation;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.stats.ServerStatsCounter;
import net.minecraft.stats.Stats;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.enchantment.Enchantment;
import net.minecraft.world.item.enchantment.ItemEnchantments;
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
 * Era: 1.21 – 1.21.4 (gameMode field, Inventory#selected / #items, GameProfile#getName).
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

    public static Integer latency(ServerPlayer p) { return p.connection == null ? null : p.connection.latency(); }

    public static String effectId(MobEffectInstance e) { return e.getEffect().getRegisteredName(); }

    public static String customName(ItemStack stack) {
        Component cn = stack.getComponents().get(DataComponents.CUSTOM_NAME);
        return cn == null ? null : cn.getString();
    }

    public static Map<String, Integer> enchantments(ItemStack stack) {
        Map<String, Integer> out = new LinkedHashMap<>();
        ItemEnchantments ench = stack.getEnchantments();
        if (ench == null) return out;
        for (Object2IntMap.Entry<Holder<Enchantment>> e : ench.entrySet()) {
            out.put(e.getKey().getRegisteredName(), e.getIntValue());
        }
        return out;
    }

    public static int customStat(ServerStatsCounter sc, Object id) { return sc.getValue(Stats.CUSTOM.get((ResourceLocation) id)); }

    // Op level 4 is what /homehub asked for before the permission registry existed.
    public static boolean isAdmin(CommandSourceStack src) { return src.hasPermission(4); }
}
