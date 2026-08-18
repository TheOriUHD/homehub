package dev.homehub.companion.core;

/**
 * One online player in the streaming list. name + uuid are always set; the
 * rest are cheap live fields (position, vitals, gamemode, ping) that the
 * loader adapter fills in when it can — all nullable so a serialized frame
 * only carries what is known. Heavy data (inventory, statistics) is fetched
 * separately via player_detail.
 */
public final class PlayerInfo {
    public final String name;
    public final String uuid; // may be null

    public String world;
    public boolean hasPos;
    public double x, y, z;
    public Double health, maxHealth;
    public Integer food, level, pingMs;
    public Integer heldSlot;
    public String gameMode;

    public PlayerInfo(String name, String uuid) {
        this.name = name;
        this.uuid = uuid;
    }
}
