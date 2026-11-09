package com.example.vpn_routing;

import android.text.TextUtils;

import java.io.UnsupportedEncodingException;
import java.net.URLDecoder;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * Ссылка на сервер: vless:// (аналог parse_vless из routing/scripts/update-vpn.py) или hysteria2:// (hy2://).
 */
public final class ProxyLink {

    public static final String VLESS = "vless";
    public static final String HYSTERIA2 = "hysteria2";

    public static final List<String> SUPPORTED_NETWORKS =
            Arrays.asList("tcp", "raw", "ws", "grpc", "httpupgrade", "xhttp");
    public static final List<String> SUPPORTED_SECURITY = Arrays.asList("none", "tls", "reality");

    /** VLESS или HYSTERIA2. */
    public final String protocol;
    /** VLESS: UUID; Hysteria2: строка авторизации. */
    public final String uuid;
    public final String server;
    public final int port;
    public final String security;
    public final String network;
    public final String flow;
    public final String sni;
    public final String pbk;
    public final String sid;
    public final String fp;
    public final String path;
    public final String host;
    public final String serviceName;
    public final String mode;
    public final String name;
    /** ALPN для TLS, через запятую: «h2,http/1.1». */
    public final String alpn;
    /** Шифрование VLESS: «none» или ключ ML-KEM. */
    public final String encryption;
    /** JSON-объект extra для xhttp (xmux, паддинг) или пусто. */
    public final String extra;
    /** JSON-объект finalmask (параметр fm: фрагментация TLS и т. п.) или пусто. */
    public final String finalmask;
    /** Проверять сертификат по этому имени вместо SNI (vcn). */
    public final String verifyName;
    /** SHA-256 сертификата сервера в hex, через запятую (pcs / pinSHA256). */
    public final String pinnedCert;
    /** Hysteria2: обфускация («salamander») и её пароль. */
    public final String obfs;
    public final String obfsPassword;

    private ProxyLink(String protocol, String uuid, String server, int port, Map<String, String> q, String name) {
        boolean hy2 = protocol.equals(HYSTERIA2);
        this.protocol = protocol;
        this.uuid = uuid;
        this.server = server;
        this.port = port;
        this.security = hy2 ? "tls" : get(q, "security", "none");
        this.network = hy2 ? "quic" : get(q, "type", "tcp");
        this.flow = get(q, "flow", "");
        this.sni = get(q, "sni", get(q, "peer", ""));
        this.pbk = get(q, "pbk", "");
        this.sid = get(q, "sid", "");
        this.fp = get(q, "fp", hy2 ? "" : "chrome");
        this.path = get(q, "path", "/");
        this.host = get(q, "host", "");
        this.serviceName = get(q, "serviceName", "");
        this.mode = get(q, "mode", "");
        this.name = name;
        this.alpn = get(q, "alpn", "");
        this.encryption = get(q, "encryption", "none");
        this.extra = get(q, "extra", "");
        this.finalmask = get(q, "fm", "");
        this.verifyName = get(q, "vcn", "");
        this.pinnedCert = normalizePin(get(q, "pcs", get(q, "pinSHA256", "")));
        this.obfs = get(q, "obfs", "");
        this.obfsPassword = get(q, "obfs-password", "");
    }

    public boolean isHysteria() {
        return protocol.equals(HYSTERIA2);
    }

    /** Поддерживает ли приложение ссылку с такой схемой. */
    public static boolean isSupportedScheme(String text) {
        String t = text.trim().toLowerCase(Locale.ROOT);
        return t.startsWith("vless://") || t.startsWith("hysteria2://") || t.startsWith("hy2://");
    }

    private static String get(Map<String, String> q, String key, String def) {
        String v = q.get(key);
        return v == null || v.isEmpty() ? def : v;
    }

    /** «AB:CD:…» или «abcd…» → «abcd…» (Xray ждёт hex без разделителей). */
    private static String normalizePin(String pin) {
        return pin.replace(":", "").replace(" ", "").toLowerCase(Locale.ROOT);
    }

    public static ProxyLink parse(String raw) throws IllegalArgumentException {
        if (raw == null) throw new IllegalArgumentException("Ссылка не указана.");
        String url = raw.replaceAll("\\s+", "");
        String lower = url.toLowerCase(Locale.ROOT);
        String protocol;
        String rest;
        if (lower.startsWith("vless://")) {
            protocol = VLESS;
            rest = url.substring("vless://".length());
        } else if (lower.startsWith("hysteria2://")) {
            protocol = HYSTERIA2;
            rest = url.substring("hysteria2://".length());
        } else if (lower.startsWith("hy2://")) {
            protocol = HYSTERIA2;
            rest = url.substring("hy2://".length());
        } else {
            throw new IllegalArgumentException("Ссылка должна начинаться с vless:// или hysteria2://");
        }

        String name = "";
        int hash = rest.indexOf('#');
        if (hash >= 0) {
            name = decode(rest.substring(hash + 1));
            rest = rest.substring(0, hash);
        }
        String query = "";
        int qm = rest.indexOf('?');
        if (qm >= 0) {
            query = rest.substring(qm + 1);
            rest = rest.substring(0, qm);
        }
        int slash = rest.indexOf('/');
        if (slash >= 0) rest = rest.substring(0, slash);

        int at = rest.lastIndexOf('@');
        if (at <= 0) throw missing(protocol);
        String user = decode(rest.substring(0, at));
        String hostPort = rest.substring(at + 1);

        String server;
        String portText;
        if (hostPort.startsWith("[")) {
            int close = hostPort.indexOf(']');
            if (close < 0) throw missing(protocol);
            server = hostPort.substring(1, close);
            portText = close + 1 < hostPort.length() && hostPort.charAt(close + 1) == ':'
                    ? hostPort.substring(close + 2) : "";
        } else {
            int colon = hostPort.lastIndexOf(':');
            server = colon > 0 ? hostPort.substring(0, colon) : hostPort;
            portText = colon > 0 ? hostPort.substring(colon + 1) : "";
        }
        // У Hysteria2 порт необязателен (443)
        if (portText.isEmpty() && protocol.equals(HYSTERIA2)) portText = "443";
        if (portText.contains(",") || portText.contains("-")) {
            throw new IllegalArgumentException("Смена портов (port hopping) пока не поддерживается.");
        }
        int port;
        try {
            port = Integer.parseInt(portText);
        } catch (NumberFormatException e) {
            throw missing(protocol);
        }
        if (user.isEmpty() || server.isEmpty() || port <= 0 || port > 65535) throw missing(protocol);

        Map<String, String> q = new HashMap<>();
        for (String pair : query.split("&")) {
            if (pair.isEmpty()) continue;
            int eq = pair.indexOf('=');
            String k = decode(eq >= 0 ? pair.substring(0, eq) : pair);
            String v = eq >= 0 ? decode(pair.substring(eq + 1)) : "";
            if (!q.containsKey(k)) q.put(k, v); // как parse_qs()[0]: берётся первое значение
        }

        ProxyLink link = new ProxyLink(protocol, user, server.toLowerCase(Locale.ROOT), port, q, name);
        if (link.isHysteria()) {
            if (!link.obfs.isEmpty() && !link.obfs.equals("salamander")) {
                throw new IllegalArgumentException("Обфускация '" + link.obfs + "' не поддерживается: salamander.");
            }
            if (link.obfs.equals("salamander") && link.obfsPassword.isEmpty()) {
                throw new IllegalArgumentException("Для salamander в ссылке нужен obfs-password.");
            }
            return link;
        }
        if (!SUPPORTED_NETWORKS.contains(link.network)) {
            throw new IllegalArgumentException("Транспорт '" + link.network + "' не поддерживается: "
                    + TextUtils.join(", ", SUPPORTED_NETWORKS) + ".");
        }
        if (!SUPPORTED_SECURITY.contains(link.security)) {
            throw new IllegalArgumentException("Тип защиты '" + link.security + "' не поддерживается: "
                    + TextUtils.join(", ", SUPPORTED_SECURITY) + ".");
        }
        if (link.security.equals("reality") && link.pbk.isEmpty()) {
            throw new IllegalArgumentException("Для Reality в ссылке нужен открытый ключ (pbk).");
        }
        return link;
    }

    private static IllegalArgumentException missing(String protocol) {
        return new IllegalArgumentException(protocol.equals(HYSTERIA2)
                ? "Отсутствуют обязательные параметры (пароль, сервер)."
                : "Отсутствуют обязательные параметры (uuid, server, port).");
    }

    private static String decode(String s) {
        try {
            return URLDecoder.decode(s, "UTF-8");
        } catch (UnsupportedEncodingException | IllegalArgumentException e) {
            return s;
        }
    }

    /** Короткое описание для интерфейса: без UUID и ключей. */
    public String describe() {
        String where = server.contains(":") ? "[" + server + "]:" + port : server + ":" + port;
        String title = name.isEmpty() ? "" : name + " — ";
        return title + where + " (" + (isHysteria() ? "hysteria2" : network + ", " + security) + ")";
    }
}
