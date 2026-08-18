package dev.homehub.companion.core;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

import java.util.List;

/**
 * Thin encode/decode layer over Gson for the HomeHub agent protocol
 * (mirror of homehub/internal/integrations/minecraft/protocol.go). One JSON
 * object per WebSocket text frame; every frame carries a "type".
 *
 * Uses only Gson APIs present since 2.8.0 so the same bytecode runs against
 * the Gson every Minecraft server from 1.16.5 onward ships.
 */
public final class Frames {
    private Frames() {}

    // ---- agent → HomeHub ---------------------------------------------------

    public static String pair(String code, String serverName, String loader,
                              String mcVersion, String companion) {
        JsonObject o = ident(serverName, loader, mcVersion, companion);
        o.addProperty("type", "pair");
        o.addProperty("code", code);
        return o.toString();
    }

    public static String hello(String token, String serverName, String loader,
                               String mcVersion, String companion) {
        JsonObject o = ident(serverName, loader, mcVersion, companion);
        o.addProperty("type", "hello");
        o.addProperty("token", token);
        return o.toString();
    }

    private static JsonObject ident(String serverName, String loader,
                                    String mcVersion, String companion) {
        JsonObject o = new JsonObject();
        if (serverName != null) o.addProperty("serverName", serverName);
        if (loader != null) o.addProperty("loader", loader);
        if (mcVersion != null) o.addProperty("mcVersion", mcVersion);
        if (companion != null) o.addProperty("companion", companion);
        return o;
    }

    public static String console(List<ConsoleLine> lines) {
        return linesFrame("console", lines);
    }

    public static String backfill(List<ConsoleLine> lines) {
        return linesFrame("backfill", lines);
    }

    private static String linesFrame(String type, List<ConsoleLine> lines) {
        JsonObject o = new JsonObject();
        o.addProperty("type", type);
        JsonArray arr = new JsonArray();
        for (ConsoleLine ln : lines) {
            JsonObject jo = new JsonObject();
            jo.addProperty("ts", ln.ts);
            if (ln.level != null && !ln.level.isEmpty()) jo.addProperty("level", ln.level);
            jo.addProperty("text", ln.text);
            arr.add(jo);
        }
        o.add("lines", arr);
        return o.toString();
    }

    public static String status(StatusSnapshot s) {
        JsonObject st = new JsonObject();
        if (s.tps != null) st.addProperty("tps", s.tps);
        if (s.mspt != null) st.addProperty("mspt", s.mspt);
        st.addProperty("playersOnline", s.playersOnline);
        st.addProperty("playersMax", s.playersMax);
        st.addProperty("heapUsedMb", s.heapUsedMb);
        st.addProperty("heapMaxMb", s.heapMaxMb);
        if (s.downstream != null) {
            JsonArray ds = new JsonArray();
            for (DownstreamInfo d : s.downstream) {
                JsonObject jd = new JsonObject();
                jd.addProperty("name", d.name);
                jd.addProperty("players", d.players);
                ds.add(jd);
            }
            st.add("downstream", ds);
        }
        JsonObject o = new JsonObject();
        o.addProperty("type", "status");
        o.add("status", st);
        return o.toString();
    }

    public static String players(List<PlayerInfo> players) {
        JsonObject o = new JsonObject();
        o.addProperty("type", "players");
        JsonArray arr = new JsonArray();
        for (PlayerInfo p : players) {
            JsonObject jp = new JsonObject();
            jp.addProperty("name", p.name);
            if (p.uuid != null) jp.addProperty("uuid", p.uuid);
            if (p.world != null) jp.addProperty("world", p.world);
            if (p.hasPos) {
                jp.addProperty("x", round2(p.x));
                jp.addProperty("y", round2(p.y));
                jp.addProperty("z", round2(p.z));
            }
            if (p.health != null) jp.addProperty("health", round1(p.health));
            if (p.maxHealth != null) jp.addProperty("maxHealth", round1(p.maxHealth));
            if (p.food != null) jp.addProperty("food", p.food.intValue());
            if (p.level != null) jp.addProperty("level", p.level.intValue());
            if (p.gameMode != null) jp.addProperty("gameMode", p.gameMode);
            if (p.pingMs != null) jp.addProperty("pingMs", p.pingMs.intValue());
            if (p.heldSlot != null) jp.addProperty("heldSlot", p.heldSlot.intValue());
            arr.add(jp);
        }
        o.add("players", arr);
        return o.toString();
    }

    /** Wrap a companion-built detail object under the result envelope. The
     *  detail string is raw JSON; a null detail reports the player offline. */
    public static String playerDetailResult(long id, String detailJson) {
        JsonObject o = new JsonObject();
        o.addProperty("type", "player_detail_result");
        o.addProperty("id", id);
        if (detailJson == null) {
            o.addProperty("ok", false);
            o.addProperty("message", "player is not online");
        } else {
            o.addProperty("ok", true);
            o.add("detail", com.google.gson.JsonParser.parseString(detailJson));
        }
        return o.toString();
    }

    private static double round1(double v) { return Math.round(v * 10.0) / 10.0; }
    private static double round2(double v) { return Math.round(v * 100.0) / 100.0; }

    public static String cmdResult(long id, boolean ok, String message) {
        JsonObject o = new JsonObject();
        o.addProperty("type", "cmd_result");
        o.addProperty("id", id);
        o.addProperty("ok", ok);
        if (message != null && !message.isEmpty()) o.addProperty("message", message);
        return o.toString();
    }

    public static String pong() {
        JsonObject o = new JsonObject();
        o.addProperty("type", "pong");
        return o.toString();
    }

    // ---- HomeHub → agent ---------------------------------------------------

    /** Parses a raw frame; returns null when the payload is not a JSON object. */
    @SuppressWarnings("deprecation") // instance JsonParser: only API on Gson 2.8.0 (MC 1.16.5)
    public static JsonObject parse(String raw) {
        try {
            com.google.gson.JsonElement el = new JsonParser().parse(raw);
            return el != null && el.isJsonObject() ? el.getAsJsonObject() : null;
        } catch (RuntimeException e) {
            return null;
        }
    }

    public static String str(JsonObject o, String key) {
        return o.has(key) && o.get(key).isJsonPrimitive() ? o.get(key).getAsString() : null;
    }

    public static long num(JsonObject o, String key) {
        return o.has(key) && o.get(key).isJsonPrimitive() ? o.get(key).getAsLong() : 0L;
    }
}
