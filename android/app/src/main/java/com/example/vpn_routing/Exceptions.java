package com.example.vpn_routing;

import android.net.Uri;

import java.net.IDN;
import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.Locale;
import java.util.regex.Pattern;

/**
 * Приведение исключений к единому виду (аналог normalize из routing/scripts/add_exception.sh):
 * адрес сайта сводится к имени хоста, кириллица — к punycode, IP — к подсети.
 */
public final class Exceptions {

    private static final Pattern DOMAIN_RE = Pattern.compile(
            "^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$");
    private static final Pattern LABEL_RE = Pattern.compile("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$");
    private static final Pattern IPV4_RE = Pattern.compile(
            "^(25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)(\\.(25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)){3}$");
    private static final Pattern IPV6_CHARS = Pattern.compile("^[0-9a-f:.]+$");

    private Exceptions() {
    }

    public static boolean isCidr(String value) {
        return parseCidr(value) != null;
    }

    /** Возвращает нормализованное исключение: подсеть (1.2.3.0/24) или домен (example.com, .example). */
    public static String normalize(String raw) throws IllegalArgumentException {
        String text = raw == null ? "" : raw.trim().toLowerCase(Locale.ROOT);
        if (text.isEmpty()) throw new IllegalArgumentException("Укажите домен или IP-адрес.");

        String cidr = parseCidr(text);
        if (cidr != null) return cidr;

        if (text.contains("://")) {
            String host = Uri.parse(text).getHost();
            text = host == null ? "" : host;
        } else {
            text = text.split("/", 2)[0];
            if (!text.startsWith("[")) text = text.split(":", 2)[0];
        }
        if (text.startsWith("[") && text.endsWith("]")) text = text.substring(1, text.length() - 1);
        cidr = parseCidr(text);
        if (cidr != null) return cidr;

        // Домен верхнего уровня можно указать с точкой в начале: .example
        String prefix = text.startsWith(".") ? "." : "";
        String host = text.substring(prefix.length());
        while (host.endsWith(".")) host = host.substring(0, host.length() - 1);
        try {
            host = IDN.toASCII(host, IDN.ALLOW_UNASSIGNED).toLowerCase(Locale.ROOT);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("Некорректный домен: " + raw);
        }
        boolean ok = prefix.isEmpty() ? DOMAIN_RE.matcher(host).matches() : LABEL_RE.matcher(host).matches();
        if (!ok) throw new IllegalArgumentException("Некорректный домен или IP: " + raw);
        return prefix + host;
    }

    /** Подсеть в каноническом виде (биты хоста обнуляются, как ip_network(strict=False)) или null. */
    static String parseCidr(String text) {
        String addr = text;
        int prefix = -1;
        int slash = text.indexOf('/');
        if (slash >= 0) {
            addr = text.substring(0, slash);
            try {
                prefix = Integer.parseInt(text.substring(slash + 1));
            } catch (NumberFormatException e) {
                return null;
            }
        }
        boolean v4 = IPV4_RE.matcher(addr).matches();
        boolean v6 = !v4 && addr.contains(":") && IPV6_CHARS.matcher(addr).matches();
        if (!v4 && !v6) return null;

        byte[] bytes;
        try {
            // Строка уже проверена как IP-литерал, поэтому DNS-запроса не будет
            bytes = InetAddress.getByName(addr).getAddress();
        } catch (UnknownHostException e) {
            return null;
        }
        int bits = bytes.length * 8;
        if (prefix < 0) prefix = bits;
        if (prefix > bits) return null;
        for (int i = 0; i < bytes.length; i++) {
            int keep = Math.max(0, Math.min(8, prefix - i * 8));
            bytes[i] &= (byte) (0xFF << (8 - keep));
        }
        try {
            String host = InetAddress.getByAddress(bytes).getHostAddress();
            if (bytes.length == 16) host = compressIpv6(host);
            return host + "/" + prefix;
        } catch (UnknownHostException e) {
            return null;
        }
    }

    /** Java пишет IPv6 без сокращения нулей (2001:db8:0:0:0:0:0:0): приводим к виду 2001:db8::. */
    private static String compressIpv6(String full) {
        String[] parts = full.split(":");
        int bestStart = -1, bestLen = 0;
        for (int i = 0; i < parts.length; ) {
            if (parts[i].equals("0")) {
                int j = i;
                while (j < parts.length && parts[j].equals("0")) j++;
                if (j - i > bestLen && j - i > 1) {
                    bestStart = i;
                    bestLen = j - i;
                }
                i = j;
            } else {
                i++;
            }
        }
        if (bestStart < 0) return full;
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < bestStart; i++) sb.append(i > 0 ? ":" : "").append(parts[i]);
        sb.append("::");
        for (int i = bestStart + bestLen; i < parts.length; i++) {
            sb.append(parts[i]);
            if (i < parts.length - 1) sb.append(":");
        }
        return sb.toString();
    }
}
