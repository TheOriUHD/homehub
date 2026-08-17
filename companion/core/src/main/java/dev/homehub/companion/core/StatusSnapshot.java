package dev.homehub.companion.core;

/**
 * Periodic performance snapshot. {@code tps}/{@code mspt} are nullable —
 * when the platform cannot provide them the keys are omitted from the wire
 * frame entirely (the HomeHub side treats absence as "unavailable").
 */
public final class StatusSnapshot {
    public final Double tps;   // nullable
    public final Double mspt;  // nullable
    public final int playersOnline;
    public final int playersMax;
    public final double heapUsedMb;
    public final double heapMaxMb;
    /** Proxy loaders (Velocity) report their backends here; null elsewhere. */
    public final java.util.List<DownstreamInfo> downstream; // nullable

    public StatusSnapshot(Double tps, Double mspt, int playersOnline, int playersMax,
                          double heapUsedMb, double heapMaxMb) {
        this(tps, mspt, playersOnline, playersMax, heapUsedMb, heapMaxMb, null);
    }

    public StatusSnapshot(Double tps, Double mspt, int playersOnline, int playersMax,
                          double heapUsedMb, double heapMaxMb,
                          java.util.List<DownstreamInfo> downstream) {
        this.tps = tps;
        this.mspt = mspt;
        this.playersOnline = playersOnline;
        this.playersMax = playersMax;
        this.heapUsedMb = heapUsedMb;
        this.heapMaxMb = heapMaxMb;
        this.downstream = downstream;
    }
}
