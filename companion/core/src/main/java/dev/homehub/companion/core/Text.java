package dev.homehub.companion.core;

import java.util.regex.Pattern;

/** Console text hygiene shared by capture and backfill. */
public final class Text {
    private Text() {}

    private static final char ESC = (char) 0x1B;
    // ANSI CSI sequences (colors, cursor moves) and legacy §x color codes.
    private static final Pattern ANSI = Pattern.compile("\\u001B\\[[0-9;]*[A-Za-z]");
    private static final Pattern SECTION = Pattern.compile("§.");

    public static String strip(String s) {
        if (s == null) return "";
        if (s.indexOf(ESC) >= 0) s = ANSI.matcher(s).replaceAll("");
        if (s.indexOf('§') >= 0) s = SECTION.matcher(s).replaceAll("");
        return s;
    }
}
