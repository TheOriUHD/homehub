package dev.homehub.companion.core;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.Properties;

/**
 * <server>/homehub-companion.properties — keys: url, token, code.
 * A virgin install carries url+code; after pairing succeeds the token is
 * written and the code line removed.
 */
public final class CompanionConfig {
    private final File file;
    private volatile String url;
    private volatile String token;
    private volatile String code;

    public CompanionConfig(File file) {
        this.file = file;
    }

    public File file() { return file; }
    public String url() { return url; }
    public String token() { return token; }
    public String code() { return code; }

    public boolean hasToken() { return url != null && token != null; }
    public boolean hasCode() { return url != null && code != null; }

    public synchronized void load() throws IOException {
        url = null;
        token = null;
        code = null;
        if (!file.isFile()) return;
        Properties p = new Properties();
        InputStream in = new FileInputStream(file);
        try {
            p.load(in);
        } finally {
            in.close();
        }
        url = trimToNull(p.getProperty("url"));
        token = trimToNull(p.getProperty("token"));
        code = trimToNull(p.getProperty("code"));
        if (url != null && url.endsWith("/")) {
            url = url.substring(0, url.length() - 1);
        }
    }

    /** Persists the token issued at pairing and removes the spent code. */
    public synchronized void storeToken(String token) throws IOException {
        this.token = token;
        this.code = null;
        store();
    }

    /** Records a fresh url+code pair (from /homehub pair) and drops any stale token. */
    public synchronized void storePairing(String url, String code) throws IOException {
        this.url = url != null && url.endsWith("/") ? url.substring(0, url.length() - 1) : url;
        this.code = code;
        this.token = null;
        store();
    }

    private void store() throws IOException {
        Properties p = new Properties();
        if (url != null) p.setProperty("url", url);
        if (token != null) p.setProperty("token", token);
        if (code != null) p.setProperty("code", code);
        OutputStream out = new FileOutputStream(file);
        try {
            p.store(out, "HomeHub Companion — do not share the token");
        } finally {
            out.close();
        }
    }

    private static String trimToNull(String s) {
        if (s == null) return null;
        s = s.trim();
        return s.isEmpty() ? null : s;
    }
}
