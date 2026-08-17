package dev.homehub.companion.velocity;

import dev.homehub.companion.core.Text;
import org.apache.logging.log4j.LogManager;
import org.apache.logging.log4j.core.LogEvent;
import org.apache.logging.log4j.core.Logger;
import org.apache.logging.log4j.core.appender.AbstractAppender;

import java.io.PrintWriter;
import java.io.StringWriter;

/**
 * Log4j2 appender on the root logger — sees every console line the proxy
 * prints. Values are extracted inside {@link #append} because async log4j
 * configurations recycle LogEvent instances after the call returns.
 */
final class ConsoleCapture extends AbstractAppender {
    interface Sink {
        void line(long ts, String level, String text);
    }

    private final Sink sink;

    @SuppressWarnings("deprecation") // 4-arg ctor exists on every log4j2 Velocity ships
    ConsoleCapture(Sink sink) {
        super("HomeHubCompanion", null, null, true);
        this.sink = sink;
    }

    @Override
    public void append(LogEvent event) {
        try {
            long ts = event.getTimeMillis();
            String level = event.getLevel() == null ? null : event.getLevel().name();
            String msg = event.getMessage() == null ? "" : event.getMessage().getFormattedMessage();
            emit(ts, level, msg);
            Throwable thrown = event.getThrown();
            if (thrown != null) {
                StringWriter sw = new StringWriter();
                thrown.printStackTrace(new PrintWriter(sw));
                emit(ts, level, sw.toString());
            }
        } catch (Throwable t) {
            // Never let console capture break the proxy's logging.
        }
    }

    private void emit(long ts, String level, String block) {
        for (String part : Text.strip(block).split("\r?\n")) {
            if (!part.isEmpty()) sink.line(ts, level, part);
        }
    }

    void attach() {
        start();
        ((Logger) LogManager.getRootLogger()).addAppender(this);
    }

    void detach() {
        ((Logger) LogManager.getRootLogger()).removeAppender(this);
        stop();
    }
}
