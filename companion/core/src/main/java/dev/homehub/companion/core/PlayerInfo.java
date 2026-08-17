package dev.homehub.companion.core;

/** One online player. */
public final class PlayerInfo {
    public final String name;
    public final String uuid; // may be null

    public PlayerInfo(String name, String uuid) {
        this.name = name;
        this.uuid = uuid;
    }
}
