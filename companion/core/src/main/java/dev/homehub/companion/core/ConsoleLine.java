package dev.homehub.companion.core;

/** One console line: unix-millis timestamp, optional level, text. */
public final class ConsoleLine {
    public final long ts;
    public final String level; // INFO|WARN|ERROR|... or null when unknown
    public final String text;

    public ConsoleLine(long ts, String level, String text) {
        this.ts = ts;
        this.level = level;
        this.text = text == null ? "" : text;
    }
}
