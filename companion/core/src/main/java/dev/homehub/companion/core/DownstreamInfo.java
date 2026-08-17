package dev.homehub.companion.core;

/** One backend server behind a proxy (Velocity), mirrored in status frames. */
public final class DownstreamInfo {
    public final String name;
    public final int players;

    public DownstreamInfo(String name, int players) {
        this.name = name;
        this.players = players;
    }
}
