package dev.homehub.companion.core;

import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;

/**
 * Bounded queue of live console lines flushed every ~250ms as one console
 * frame. Beyond 10k pending, the oldest lines are dropped and the next
 * flushed batch opens with a synthetic note line reporting the loss.
 */
final class ConsoleBatcher {
    interface Sink {
        /** @return true when the batch was handed to a connected socket. */
        boolean sendConsole(List<ConsoleLine> lines);
    }

    static final int MAX_PENDING = 10_000;
    static final long FLUSH_INTERVAL_MS = 250;

    private final Object lock = new Object();
    private final ArrayDeque<ConsoleLine> pending = new ArrayDeque<ConsoleLine>();
    private long dropped;
    private final Sink sink;
    private Thread thread;
    private volatile boolean running;

    ConsoleBatcher(Sink sink) {
        this.sink = sink;
    }

    void start() {
        running = true;
        thread = new Thread(new Runnable() {
            @Override public void run() { loop(); }
        }, "HomeHub-Console-Batcher");
        thread.setDaemon(true);
        thread.start();
    }

    void stop() {
        running = false;
        if (thread != null) thread.interrupt();
    }

    void add(ConsoleLine line) {
        synchronized (lock) {
            pending.addLast(line);
            while (pending.size() > MAX_PENDING) {
                pending.removeFirst();
                dropped++;
            }
        }
    }

    /** Forget everything queued (called right before a backfill re-covers history). */
    void clear() {
        synchronized (lock) {
            pending.clear();
            dropped = 0;
        }
    }

    private void loop() {
        while (running) {
            try {
                Thread.sleep(FLUSH_INTERVAL_MS);
            } catch (InterruptedException e) {
                if (!running) return;
            }
            List<ConsoleLine> batch;
            long droppedNow;
            synchronized (lock) {
                if (pending.isEmpty()) continue;
                batch = new ArrayList<ConsoleLine>(pending.size() + 1);
                droppedNow = dropped;
                if (droppedNow > 0) {
                    batch.add(new ConsoleLine(System.currentTimeMillis(), "WARN",
                            "[HomeHub Companion] dropped " + droppedNow
                                    + " console line(s) — send queue overflowed"));
                }
                batch.addAll(pending);
                pending.clear();
                dropped = 0;
            }
            if (!sink.sendConsole(batch)) {
                // Not connected: re-queue at the front so nothing is lost
                // (the bound still applies).
                synchronized (lock) {
                    List<ConsoleLine> requeue = batch;
                    if (droppedNow > 0) requeue = batch.subList(1, batch.size());
                    for (int i = requeue.size() - 1; i >= 0; i--) {
                        pending.addFirst(requeue.get(i));
                    }
                    dropped += droppedNow;
                    while (pending.size() > MAX_PENDING) {
                        pending.removeFirst();
                        dropped++;
                    }
                }
            }
        }
    }
}
