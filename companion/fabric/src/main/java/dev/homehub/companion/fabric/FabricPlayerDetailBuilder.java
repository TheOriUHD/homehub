package dev.homehub.companion.fabric;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import it.unimi.dsi.fastutil.objects.Object2IntMap;
import net.minecraft.core.Holder;
import net.minecraft.core.component.DataComponents;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.chat.Component;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.stats.ServerStatsCounter;
import net.minecraft.stats.Stats;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.player.Inventory;
import net.minecraft.world.food.FoodData;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.enchantment.Enchantment;
import net.minecraft.world.item.enchantment.ItemEnchantments;
import net.minecraft.world.level.GameType;

/**
 * The Fabric equivalent of Paper's PlayerDetailBuilder: a full live snapshot
 * for one player as JSON — position, vitals, gamemode, effects, the whole
 * inventory (hotbar/main/armor/offhand with counts, custom names, enchants),
 * ender chest, and a curated statistics set. Compiled against Mojang 1.21.11
 * mappings; loom remaps to the runtime namespace, so we use direct member
 * calls (never reflection by name — that would look up unmapped strings).
 * Every section is guarded so one version-sensitive call can't blank the doc.
 * Runs on the server thread.
 */
final class FabricPlayerDetailBuilder {

    static JsonObject build(ServerPlayer p) {
        JsonObject o = new JsonObject();
        o.addProperty("name", p.getGameProfile().name());
        o.addProperty("uuid", p.getUUID().toString());

        try {
            GameType gm = p.gameMode();
            if (gm != null) o.addProperty("gameMode", gm.getName());
        } catch (Throwable ignored) {}

        // position
        try {
            JsonObject pos = new JsonObject();
            try { pos.addProperty("world", p.level().dimension().identifier().toString()); } catch (Throwable ignored) {}
            pos.addProperty("x", round2(p.getX()));
            pos.addProperty("y", round2(p.getY()));
            pos.addProperty("z", round2(p.getZ()));
            pos.addProperty("yaw", round1(p.getYRot()));
            pos.addProperty("pitch", round1(p.getXRot()));
            o.add("position", pos);
        } catch (Throwable ignored) {}

        // vitals
        try {
            JsonObject v = new JsonObject();
            v.addProperty("health", round1(p.getHealth()));
            try { v.addProperty("maxHealth", round1(p.getMaxHealth())); } catch (Throwable ignored) {}
            try {
                FoodData fd = p.getFoodData();
                v.addProperty("food", fd.getFoodLevel());
                v.addProperty("saturation", round1(fd.getSaturationLevel()));
            } catch (Throwable ignored) {}
            v.addProperty("level", p.experienceLevel);
            v.addProperty("exp", round2(p.experienceProgress));
            v.addProperty("totalExperience", p.totalExperience);
            try {
                v.addProperty("remainingAir", p.getAirSupply());
                v.addProperty("maximumAir", p.getMaxAirSupply());
            } catch (Throwable ignored) {}
            try {
                if (p.connection != null) v.addProperty("pingMs", p.connection.latency());
            } catch (Throwable ignored) {}
            o.add("vitals", v);
        } catch (Throwable ignored) {}

        // potion effects
        try {
            JsonArray effects = new JsonArray();
            for (MobEffectInstance e : p.getActiveEffects()) {
                JsonObject je = new JsonObject();
                je.addProperty("type", stripNs(e.getEffect().getRegisteredName()));
                je.addProperty("amplifier", e.getAmplifier());
                je.addProperty("durationTicks", e.getDuration());
                effects.add(je);
            }
            if (effects.size() > 0) o.add("effects", effects);
        } catch (Throwable ignored) {}

        // inventory
        try {
            Inventory inv = p.getInventory();
            JsonObject invObj = new JsonObject();
            try { invObj.addProperty("heldSlot", inv.getSelectedSlot()); } catch (Throwable ignored) {}

            JsonArray items = new JsonArray();
            try {
                java.util.List<ItemStack> main = inv.getNonEquipmentItems();
                for (int i = 0; i < main.size(); i++) {
                    JsonObject item = item(main.get(i));
                    if (item != null) {
                        item.addProperty("slot", i);
                        items.add(item);
                    }
                }
            } catch (Throwable ignored) {}
            invObj.add("items", items);

            JsonObject armor = new JsonObject();
            addItem(armor, "helmet", p.getItemBySlot(EquipmentSlot.HEAD));
            addItem(armor, "chestplate", p.getItemBySlot(EquipmentSlot.CHEST));
            addItem(armor, "leggings", p.getItemBySlot(EquipmentSlot.LEGS));
            addItem(armor, "boots", p.getItemBySlot(EquipmentSlot.FEET));
            invObj.add("armor", armor);

            JsonObject off = item(p.getItemBySlot(EquipmentSlot.OFFHAND));
            if (off != null) invObj.add("offHand", off);

            o.add("inventory", invObj);
        } catch (Throwable ignored) {}

        // ender chest
        try {
            JsonArray ec = new JsonArray();
            var chest = p.getEnderChestInventory();
            int size = chest.getContainerSize();
            for (int i = 0; i < size; i++) {
                JsonObject item = item(chest.getItem(i));
                if (item != null) {
                    item.addProperty("slot", i);
                    ec.add(item);
                }
            }
            if (ec.size() > 0) o.add("enderChest", ec);
        } catch (Throwable ignored) {}

        // statistics (curated; each guarded so an odd one can't sink the rest)
        try {
            ServerStatsCounter sc = p.getStats();
            JsonObject stats = new JsonObject();
            stat(stats, sc, "playtimeHours", Stats.PLAY_TIME, 1.0 / (20.0 * 60.0 * 60.0));
            stat(stats, sc, "deaths", Stats.DEATHS, 1.0);
            stat(stats, sc, "mobKills", Stats.MOB_KILLS, 1.0);
            stat(stats, sc, "playerKills", Stats.PLAYER_KILLS, 1.0);
            stat(stats, sc, "jumps", Stats.JUMP, 1.0);
            stat(stats, sc, "damageDealt", Stats.DAMAGE_DEALT, 0.1);
            stat(stats, sc, "damageTaken", Stats.DAMAGE_TAKEN, 0.1);
            stat(stats, sc, "walkedBlocks", Stats.WALK_ONE_CM, 0.01);
            stat(stats, sc, "sprintedBlocks", Stats.SPRINT_ONE_CM, 0.01);
            stat(stats, sc, "flownBlocks", Stats.FLY_ONE_CM, 0.01);
            if (stats.size() > 0) o.add("statistics", stats);
        } catch (Throwable ignored) {}

        // op flag is intentionally omitted on Fabric — the 1.21 op check wants a
        // NameAndId and a non-public server handle; the UI shows the badge only
        // when the field is present, so leaving it off is harmless.

        return o;
    }

    private static JsonObject item(ItemStack stack) {
        if (stack == null || stack.isEmpty() || stack.getCount() <= 0) return null;
        JsonObject o = new JsonObject();
        try {
            o.addProperty("type", stripNs(BuiltInRegistries.ITEM.getKey(stack.getItem()).toString()));
        } catch (Throwable t) {
            return null;
        }
        o.addProperty("count", stack.getCount());
        // custom name (only when the item actually carries one)
        try {
            Component cn = stack.getComponents().get(DataComponents.CUSTOM_NAME);
            if (cn != null) {
                String s = cn.getString();
                if (s != null && !s.isEmpty()) o.addProperty("name", s);
            }
        } catch (Throwable ignored) {}
        // enchantments
        try {
            ItemEnchantments ench = stack.getEnchantments();
            if (ench != null && !ench.isEmpty()) {
                JsonObject je = new JsonObject();
                for (Object2IntMap.Entry<Holder<Enchantment>> e : ench.entrySet()) {
                    je.addProperty(stripNs(e.getKey().getRegisteredName()), e.getIntValue());
                }
                if (je.size() > 0) o.add("enchants", je);
            }
        } catch (Throwable ignored) {}
        return o;
    }

    private static void addItem(JsonObject parent, String key, ItemStack stack) {
        JsonObject item = item(stack);
        if (item != null) parent.add(key, item);
    }

    private static void stat(JsonObject out, ServerStatsCounter sc, String key,
                             net.minecraft.resources.Identifier custom, double scale) {
        try {
            int raw = sc.getValue(Stats.CUSTOM.get(custom));
            if (scale == 1.0) out.addProperty(key, (long) raw);
            else out.addProperty(key, round1(raw * scale));
        } catch (Throwable ignored) {}
    }

    /** "minecraft:diamond_sword" → "diamond_sword"; leaves other namespaces intact after the colon. */
    private static String stripNs(String id) {
        if (id == null) return "";
        int c = id.indexOf(':');
        return c >= 0 ? id.substring(c + 1) : id;
    }

    static double round1(double v) { return Math.round(v * 10.0) / 10.0; }
    static double round2(double v) { return Math.round(v * 100.0) / 100.0; }

    private FabricPlayerDetailBuilder() {}
}
