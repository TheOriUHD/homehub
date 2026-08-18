package dev.homehub.companion.paper;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import org.bukkit.GameMode;
import org.bukkit.Location;
import org.bukkit.Material;
import org.bukkit.Statistic;
import org.bukkit.enchantments.Enchantment;
import org.bukkit.entity.Player;
import org.bukkit.inventory.ItemStack;
import org.bukkit.inventory.PlayerInventory;
import org.bukkit.inventory.meta.ItemMeta;
import org.bukkit.potion.PotionEffect;

import java.util.Map;

/**
 * Builds the full live snapshot for one player as a JSON object: position,
 * vitals, gamemode, effects, the whole inventory (hotbar/main/armor/offhand),
 * ender chest, and a curated set of statistics. Everything is wrapped in
 * try/catch per section so a single version-sensitive API can never blank
 * the whole document. Runs on the server main thread.
 */
final class PlayerDetailBuilder {

    static JsonObject build(Player p) {
        JsonObject o = new JsonObject();
        o.addProperty("name", p.getName());
        o.addProperty("uuid", p.getUniqueId().toString());

        try {
            o.addProperty("gameMode", p.getGameMode().name().toLowerCase());
        } catch (Throwable ignored) {}

        // position
        try {
            Location l = p.getLocation();
            JsonObject pos = new JsonObject();
            if (l.getWorld() != null) pos.addProperty("world", l.getWorld().getName());
            pos.addProperty("x", round2(l.getX()));
            pos.addProperty("y", round2(l.getY()));
            pos.addProperty("z", round2(l.getZ()));
            pos.addProperty("yaw", round1(l.getYaw()));
            pos.addProperty("pitch", round1(l.getPitch()));
            o.add("position", pos);
        } catch (Throwable ignored) {}

        // vitals
        try {
            JsonObject v = new JsonObject();
            v.addProperty("health", round1(p.getHealth()));
            try { v.addProperty("maxHealth", round1(p.getMaxHealth())); } catch (Throwable ignored) {}
            v.addProperty("food", p.getFoodLevel());
            v.addProperty("saturation", round1(p.getSaturation()));
            v.addProperty("level", p.getLevel());
            v.addProperty("exp", round2(p.getExp()));
            v.addProperty("totalExperience", p.getTotalExperience());
            v.addProperty("remainingAir", p.getRemainingAir());
            v.addProperty("maximumAir", p.getMaximumAir());
            Integer ping = ping(p);
            if (ping != null) v.addProperty("pingMs", ping.intValue());
            o.add("vitals", v);
        } catch (Throwable ignored) {}

        // potion effects
        try {
            JsonArray effects = new JsonArray();
            for (PotionEffect e : p.getActivePotionEffects()) {
                JsonObject je = new JsonObject();
                je.addProperty("type", e.getType().getName().toLowerCase());
                je.addProperty("amplifier", e.getAmplifier());
                je.addProperty("durationTicks", e.getDuration());
                effects.add(je);
            }
            if (effects.size() > 0) o.add("effects", effects);
        } catch (Throwable ignored) {}

        // inventory
        try {
            PlayerInventory inv = p.getInventory();
            JsonObject invObj = new JsonObject();
            invObj.addProperty("heldSlot", inv.getHeldItemSlot());

            JsonArray items = new JsonArray();
            ItemStack[] contents = inv.getContents();
            for (int i = 0; i < contents.length; i++) {
                JsonObject item = item(contents[i]);
                if (item != null) {
                    item.addProperty("slot", i);
                    items.add(item);
                }
            }
            invObj.add("items", items);

            JsonObject armor = new JsonObject();
            addItem(armor, "helmet", inv.getHelmet());
            addItem(armor, "chestplate", inv.getChestplate());
            addItem(armor, "leggings", inv.getLeggings());
            addItem(armor, "boots", inv.getBoots());
            invObj.add("armor", armor);

            JsonObject off = item(inv.getItemInOffHand());
            if (off != null) invObj.add("offHand", off);

            o.add("inventory", invObj);
        } catch (Throwable ignored) {}

        // ender chest
        try {
            JsonArray ec = new JsonArray();
            ItemStack[] contents = p.getEnderChest().getContents();
            for (int i = 0; i < contents.length; i++) {
                JsonObject item = item(contents[i]);
                if (item != null) {
                    item.addProperty("slot", i);
                    ec.add(item);
                }
            }
            if (ec.size() > 0) o.add("enderChest", ec);
        } catch (Throwable ignored) {}

        // statistics (a curated set; any missing enum is skipped)
        try {
            JsonObject stats = new JsonObject();
            stat(stats, p, "playtimeHours", "PLAY_ONE_MINUTE", 1.0 / (20.0 * 60.0 * 60.0));
            stat(stats, p, "deaths", "DEATHS", 1.0);
            stat(stats, p, "mobKills", "MOB_KILLS", 1.0);
            stat(stats, p, "playerKills", "PLAYER_KILLS", 1.0);
            stat(stats, p, "jumps", "JUMP", 1.0);
            stat(stats, p, "damageDealt", "DAMAGE_DEALT", 0.1);
            stat(stats, p, "damageTaken", "DAMAGE_TAKEN", 0.1);
            stat(stats, p, "walkedBlocks", "WALK_ONE_CM", 0.01);
            stat(stats, p, "sprintedBlocks", "SPRINT_ONE_CM", 0.01);
            stat(stats, p, "flownBlocks", "FLY_ONE_CM", 0.01);
            if (stats.size() > 0) o.add("statistics", stats);
        } catch (Throwable ignored) {}

        try {
            o.addProperty("firstPlayed", p.getFirstPlayed());
            o.addProperty("lastPlayed", lastPlayed(p));
            o.addProperty("op", p.isOp());
        } catch (Throwable ignored) {}

        return o;
    }

    private static JsonObject item(ItemStack stack) {
        if (stack == null || stack.getType() == Material.AIR || stack.getAmount() <= 0) return null;
        JsonObject o = new JsonObject();
        o.addProperty("type", stack.getType().name().toLowerCase());
        o.addProperty("count", stack.getAmount());
        try {
            if (stack.hasItemMeta()) {
                ItemMeta meta = stack.getItemMeta();
                if (meta != null && meta.hasDisplayName()) o.addProperty("name", meta.getDisplayName());
            }
        } catch (Throwable ignored) {}
        try {
            Map<Enchantment, Integer> ench = stack.getEnchantments();
            if (!ench.isEmpty()) {
                JsonObject je = new JsonObject();
                for (Map.Entry<Enchantment, Integer> e : ench.entrySet()) {
                    je.addProperty(enchName(e.getKey()), e.getValue());
                }
                o.add("enchants", je);
            }
        } catch (Throwable ignored) {}
        return o;
    }

    private static void addItem(JsonObject parent, String key, ItemStack stack) {
        JsonObject item = item(stack);
        if (item != null) parent.add(key, item);
    }

    private static String enchName(Enchantment e) {
        try {
            return e.getKey().getKey(); // 1.13+ namespaced key, e.g. "sharpness"
        } catch (Throwable t) {
            return e.getName().toLowerCase();
        }
    }

    // Statistic lookup by enum name so a build against one MC version still
    // compiles and simply skips stats that do not exist elsewhere.
    private static void stat(JsonObject out, Player p, String key, String enumName, double scale) {
        try {
            Statistic st = Statistic.valueOf(enumName);
            int raw = p.getStatistic(st);
            double v = raw * scale;
            if (scale == 1.0) out.addProperty(key, (long) raw);
            else out.addProperty(key, round1(v));
        } catch (Throwable ignored) {}
    }

    private static Integer ping(Player p) {
        try {
            return (Integer) Player.class.getMethod("getPing").invoke(p); // Paper 1.16+
        } catch (Throwable t) {
            return null;
        }
    }

    private static long lastPlayed(Player p) {
        try {
            return (Long) Player.class.getMethod("getLastSeen").invoke(p); // Paper newer
        } catch (Throwable t) {
            try {
                return p.getLastPlayed(); // deprecated but present on older
            } catch (Throwable t2) {
                return 0L;
            }
        }
    }

    static double round1(double v) { return Math.round(v * 10.0) / 10.0; }
    static double round2(double v) { return Math.round(v * 100.0) / 100.0; }

    private PlayerDetailBuilder() {}
}
