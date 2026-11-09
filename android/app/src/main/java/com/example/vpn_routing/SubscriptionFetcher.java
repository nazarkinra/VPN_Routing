package com.example.vpn_routing;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * Загрузка подписки: список ссылок (открытым текстом или в base64) и стандартные заголовки
 * profile-title, subscription-userinfo, profile-update-interval, announce.
 */
public final class SubscriptionFetcher {

    private static final int MAX_SIZE = 2_000_000;
    /**
     * Панели подписок (Remnawave, Marzban и др.) выбирают формат по User-Agent и на незнакомый
     * отвечают 404. С «v2rayN» они отдают список ссылок в base64 — его и разбирает приложение.
     */
    private static final String USER_AGENT = "v2rayN/7.0 (VPNrouting)";

    public static final class Result {
        public final List<String> links = new ArrayList<>();
        public int unsupported;
        public String title = "";
        public long upload;
        public long download;
        public long total;
        public long expire;
        public int updateIntervalHours;
        public String announce = "";
    }

    private SubscriptionFetcher() {
    }

    public static boolean isSubscriptionUrl(String text) {
        String t = text.trim().toLowerCase(Locale.ROOT);
        return t.startsWith("https://") || t.startsWith("http://");
    }

    /** Загружает подписку. Вызывать не в UI-потоке. */
    public static Result fetch(String url) throws IOException {
        HttpURLConnection c = (HttpURLConnection) new URL(url.trim()).openConnection();
        c.setConnectTimeout(15000);
        c.setReadTimeout(20000);
        c.setInstanceFollowRedirects(true);
        c.setRequestProperty("User-Agent", USER_AGENT);
        c.setRequestProperty("Accept", "*/*");
        try {
            int code = c.getResponseCode();
            if (code == 404) throw new IOException("сервер не нашёл подписку (404): проверьте адрес");
            if (code / 100 != 2) throw new IOException("сервер ответил " + code);
            String body;
            try (InputStream in = c.getInputStream()) {
                body = readLimited(in);
            }
            Result r = parseBody(body);
            applyHeaders(r, c.getHeaderField("profile-title"), c.getHeaderField("subscription-userinfo"),
                    c.getHeaderField("profile-update-interval"), c.getHeaderField("announce"));
            if (r.links.isEmpty()) {
                throw new IOException(r.unsupported > 0
                        ? "в подписке нет серверов VLESS или Hysteria2 (других: " + r.unsupported + ")"
                        : "в подписке нет серверов");
            }
            return r;
        } finally {
            c.disconnect();
        }
    }

    private static String readLimited(InputStream in) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] buf = new byte[8192];
        int n;
        while ((n = in.read(buf)) > 0) {
            out.write(buf, 0, n);
            if (out.size() > MAX_SIZE) throw new IOException("подписка слишком большая");
        }
        return new String(out.toByteArray(), StandardCharsets.UTF_8);
    }

    /** Тело подписки: ссылки построчно, возможно целиком в base64. */
    static Result parseBody(String body) {
        Result r = new Result();
        String text = body.trim();
        if (!text.contains("://")) {
            String decoded = decodeBase64(text);
            if (decoded != null) text = decoded;
        }
        for (String raw : text.split("[\\r\\n]+")) {
            String line = raw.trim();
            if (line.isEmpty() || line.startsWith("#")) continue;
            if (ProxyLink.isSupportedScheme(line)) {
                try {
                    ProxyLink.parse(line);
                    if (!r.links.contains(line)) r.links.add(line);
                } catch (IllegalArgumentException e) {
                    r.unsupported++;
                }
            } else if (line.contains("://")) {
                r.unsupported++; // vmess, trojan, ss и т. п.
            }
        }
        return r;
    }

    static void applyHeaders(Result r, String title, String userinfo, String interval, String announce) {
        if (title != null) r.title = decodeHeader(title).trim();
        if (announce != null) r.announce = decodeHeader(announce).trim();
        if (interval != null) {
            try {
                r.updateIntervalHours = Integer.parseInt(interval.trim());
            } catch (NumberFormatException ignored) {
            }
        }
        if (userinfo != null) {
            for (String part : userinfo.split(";")) {
                String[] kv = part.trim().split("=", 2);
                if (kv.length != 2) continue;
                long v;
                try {
                    v = (long) Double.parseDouble(kv[1].trim());
                } catch (NumberFormatException e) {
                    continue;
                }
                switch (kv[0].trim().toLowerCase(Locale.ROOT)) {
                    case "upload": r.upload = v; break;
                    case "download": r.download = v; break;
                    case "total": r.total = v; break;
                    case "expire": r.expire = v; break;
                    default: break;
                }
            }
        }
    }

    /** Значение заголовка может быть в виде base64:…, иначе — как есть. */
    private static String decodeHeader(String value) {
        String v = value.trim();
        if (v.regionMatches(true, 0, "base64:", 0, 7)) {
            String decoded = decodeBase64(v.substring(7));
            return decoded == null ? "" : decoded;
        }
        return v;
    }

    /** Base64 (обычный и URL-safe, с отступами и без). null, если это не base64. */
    static String decodeBase64(String text) {
        String s = text.replaceAll("\\s+", "").replace('-', '+').replace('_', '/');
        while (s.endsWith("=")) s = s.substring(0, s.length() - 1);
        if (s.isEmpty() || !s.matches("[A-Za-z0-9+/]+") || s.length() % 4 == 1) return null;
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        int buffer = 0, bits = 0;
        for (int i = 0; i < s.length(); i++) {
            char ch = s.charAt(i);
            int v = ch >= 'A' && ch <= 'Z' ? ch - 'A' : ch >= 'a' && ch <= 'z' ? ch - 'a' + 26
                    : ch >= '0' && ch <= '9' ? ch - '0' + 52 : ch == '+' ? 62 : 63;
            buffer = (buffer << 6) | v;
            bits += 6;
            if (bits >= 8) {
                bits -= 8;
                out.write((buffer >> bits) & 0xFF);
            }
        }
        return new String(out.toByteArray(), StandardCharsets.UTF_8);
    }
}
