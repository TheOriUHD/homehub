package dev.homehub.companion.core;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Calendar;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Parses logs/latest.log into ConsoleLines for the one-shot backfill frame.
 *
 * Handles both log file styles seen across loaders:
 *   vanilla/Spigot: {@code [12:34:56] [Server thread/INFO]: message}
 *   Paper:          {@code [12:34:56 INFO]: message}
 * Timestamps are HH:MM:SS only, resolved against today's date (latest.log is
 * always the current boot). Unmatched lines (stack traces, banners) inherit
 * the previous line's timestamp/level.
 */
public final class BackfillReader {
    private BackfillReader() {}

    /** Last 10k lines, and never let one frame threaten the 4MB server read limit. */
    public static final int MAX_LINES = 10_000;
    private static final long MAX_TOTAL_CHARS = 2_500_000;
    private static final int MAX_LINE_CHARS = 2000;

    private static final Pattern VANILLA =
            Pattern.compile("^\\[(\\d{2}):(\\d{2}):(\\d{2})\\] \\[([^\\]]*)\\]:? ?(.*)$");
    private static final Pattern PAPER =
            Pattern.compile("^\\[(\\d{2}):(\\d{2}):(\\d{2}) ([A-Z]+)\\]:? ?(.*)$");

    public static List<ConsoleLine> read(File logFile) {
        List<String> tail = tailLines(logFile);
        List<ConsoleLine> out = new ArrayList<ConsoleLine>(tail.size());
        long now = System.currentTimeMillis();
        long lastTs = now;
        String lastLevel = null;
        for (String raw : tail) {
            String line = Text.strip(raw);
            if (line.length() > MAX_LINE_CHARS) line = line.substring(0, MAX_LINE_CHARS) + "…";
            long ts;
            String level;
            String text;
            Matcher m = PAPER.matcher(line);
            if (m.matches()) {
                ts = todayAt(m.group(1), m.group(2), m.group(3), now);
                level = m.group(4);
                text = m.group(5);
            } else {
                m = VANILLA.matcher(line);
                if (m.matches()) {
                    ts = todayAt(m.group(1), m.group(2), m.group(3), now);
                    level = levelOf(m.group(4));
                    text = m.group(5);
                } else {
                    // Continuation (stack trace etc.) — keep the raw text.
                    ts = lastTs;
                    level = lastLevel;
                    text = line;
                }
            }
            lastTs = ts;
            lastLevel = level;
            out.add(new ConsoleLine(ts, level, text));
        }
        return out;
    }

    /** "Server thread/INFO" → "INFO"; a marker without a slash is taken verbatim. */
    private static String levelOf(String marker) {
        int slash = marker.lastIndexOf('/');
        String lvl = slash >= 0 ? marker.substring(slash + 1) : marker;
        lvl = lvl.trim();
        return lvl.isEmpty() ? null : lvl;
    }

    private static long todayAt(String hh, String mm, String ss, long now) {
        try {
            Calendar c = Calendar.getInstance();
            c.setTimeInMillis(now);
            c.set(Calendar.HOUR_OF_DAY, Integer.parseInt(hh));
            c.set(Calendar.MINUTE, Integer.parseInt(mm));
            c.set(Calendar.SECOND, Integer.parseInt(ss));
            c.set(Calendar.MILLISECOND, 0);
            return c.getTimeInMillis();
        } catch (RuntimeException e) {
            return now;
        }
    }

    private static List<String> tailLines(File logFile) {
        ArrayDeque<String> keep = new ArrayDeque<String>(1024);
        long totalChars = 0;
        BufferedReader r = null;
        try {
            r = new BufferedReader(new InputStreamReader(
                    new FileInputStream(logFile), StandardCharsets.UTF_8));
            String line;
            while ((line = r.readLine()) != null) {
                keep.addLast(line);
                totalChars += Math.min(line.length(), MAX_LINE_CHARS);
                while (keep.size() > MAX_LINES || (totalChars > MAX_TOTAL_CHARS && keep.size() > 1)) {
                    String evicted = keep.removeFirst();
                    totalChars -= Math.min(evicted.length(), MAX_LINE_CHARS);
                }
            }
        } catch (IOException e) {
            // Missing/unreadable log — backfill is best-effort.
        } finally {
            if (r != null) {
                try { r.close(); } catch (IOException ignored) {}
            }
        }
        return new ArrayList<String>(keep);
    }
}
