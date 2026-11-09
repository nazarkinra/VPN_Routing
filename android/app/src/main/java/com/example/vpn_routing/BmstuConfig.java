package com.example.vpn_routing;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Проверка и очистка .ovpn МГТУ (аналог build из routing/scripts/bmstu-config.py).
 * Из файла берутся только настройки из белого списка, остальные пропускаются.
 */
public final class BmstuConfig {

    static final int MAX_SIZE = 100000;

    private static final Set<String> ALLOWED_OPTIONS = new HashSet<>(Arrays.asList(
            "client", "tls-client", "remote", "port", "rport", "proto", "remote-random", "remote-cert-tls",
            "verify-x509-name", "reneg-sec", "tun-mtu", "mssfix", "fragment", "cipher", "ncp-ciphers",
            "data-ciphers", "data-ciphers-fallback", "auth", "tls-version-min", "tls-version-max", "tls-cipher",
            "tls-ciphersuites", "key-direction", "comp-lzo", "compress", "allow-compression",
            "explicit-exit-notify", "sndbuf", "rcvbuf", "connect-retry", "connect-timeout", "hand-window",
            "tran-window"));
    private static final Set<String> ALLOWED_BLOCKS = new HashSet<>(Arrays.asList(
            "ca", "cert", "key", "extra-certs", "tls-auth", "tls-crypt", "tls-crypt-v2"));
    /** На телефоне задаются приложением, поэтому о них незачем предупреждать. */
    private static final Set<String> REPLACED_OPTIONS = new HashSet<>(Arrays.asList(
            "dev", "dev-type", "auth-user-pass", "nobind", "resolv-retry", "ping", "ping-restart", "verb",
            "persist-key", "persist-tun", "pull", "auth-nocache", "route-noexec", "pull-filter",
            "script-security", "up", "down", "mute", "mute-replay-warnings"));
    /** Не поддерживаются клиентом в приложении (minivpn). */
    private static final Set<String> UNSUPPORTED_BLOCKS = new HashSet<>(Arrays.asList(
            "tls-auth", "tls-crypt", "tls-crypt-v2"));

    private static final Pattern NOT_AFTER = Pattern.compile("#\\s*Not After\\s*:\\s*(.+)");
    private static final Pattern BLOCK = Pattern.compile("<([A-Za-z0-9-]+)>");

    public final String text;
    public final List<String> skipped;
    public final String remoteHost;
    public final String notAfter;

    private BmstuConfig(String text, List<String> skipped, String remoteHost, String notAfter) {
        this.text = text;
        this.skipped = skipped;
        this.remoteHost = remoteHost;
        this.notAfter = notAfter;
    }

    /** Срок сертификата для экрана: «21.05.2027» (в файле — «May 21 00:00:00 2027 GMT»). */
    public String notAfterDate() {
        if (notAfter == null) return null;
        try {
            java.text.SimpleDateFormat in = new java.text.SimpleDateFormat("MMM d HH:mm:ss yyyy zzz", Locale.ENGLISH);
            java.util.Date d = in.parse(notAfter.replaceAll("\\s+", " "));
            if (d != null) return new java.text.SimpleDateFormat("dd.MM.yyyy", Locale.ROOT).format(d);
        } catch (java.text.ParseException ignored) {
        }
        return notAfter;
    }

    public static BmstuConfig parse(String source) throws IllegalArgumentException {
        if (source == null || source.isEmpty()) throw new IllegalArgumentException("Файл пуст.");
        if (source.length() > MAX_SIZE) {
            throw new IllegalArgumentException("Файл слишком большой для конфига OpenVPN.");
        }
        String bom = String.valueOf((char) 0xFEFF);
        String[] lines = source.replace(bom, "").replace("\r\n", "\n").replace('\r', '\n').split("\n");
        List<String> comments = new ArrayList<>();
        List<String> options = new ArrayList<>();
        List<String> blocks = new ArrayList<>();
        List<String> skipped = new ArrayList<>();
        String remoteHost = null;
        String notAfter = null;
        String blockName = null;
        StringBuilder blockText = new StringBuilder();

        for (String raw : lines) {
            String line = raw.trim();
            if (blockName != null) {
                if (line.equalsIgnoreCase("</" + blockName + ">")) {
                    if (UNSUPPORTED_BLOCKS.contains(blockName)) {
                        throw new IllegalArgumentException("Блок <" + blockName
                                + "> пока не поддерживается клиентом OpenVPN в приложении.");
                    }
                    if (ALLOWED_BLOCKS.contains(blockName)) {
                        blocks.add("<" + blockName + ">\n" + blockText + "</" + blockName + ">");
                    } else {
                        skipped.add("<" + blockName + ">");
                    }
                    blockName = null;
                    blockText.setLength(0);
                } else {
                    blockText.append(line).append('\n');
                }
                continue;
            }
            if (line.isEmpty()) continue;
            if (line.charAt(0) == '#' || line.charAt(0) == ';') {
                // Срок сертификата из шапки показывается в приложении
                Matcher m = NOT_AFTER.matcher(line);
                if (m.matches()) {
                    notAfter = m.group(1).trim();
                    comments.add(line);
                }
                continue;
            }
            Matcher block = BLOCK.matcher(line);
            if (block.matches()) {
                blockName = block.group(1).toLowerCase(Locale.ROOT);
                continue;
            }
            String[] parts = line.split("\\s+");
            String name = parts[0].toLowerCase(Locale.ROOT);
            while (name.startsWith("-")) name = name.substring(1);
            if (Arrays.asList("ca", "cert", "key", "tls-auth", "tls-crypt").contains(name)) {
                throw new IllegalArgumentException("Настройка " + name
                        + " ссылается на отдельный файл. Нужен .ovpn со встроенными сертификатами.");
            }
            if (ALLOWED_OPTIONS.contains(name)) {
                options.add(line);
                if (name.equals("remote") && remoteHost == null && parts.length >= 2) remoteHost = parts[1];
            } else if (!REPLACED_OPTIONS.contains(name)) {
                skipped.add(name);
            }
        }
        if (blockName != null) {
            throw new IllegalArgumentException("Блок <" + blockName + "> не закрыт: файл повреждён.");
        }
        if (remoteHost == null) {
            throw new IllegalArgumentException("В файле нет адреса сервера (remote). Похоже, это не конфиг OpenVPN.");
        }
        boolean hasCa = false;
        for (String b : blocks) if (b.startsWith("<ca>")) hasCa = true;
        if (!hasCa) {
            throw new IllegalArgumentException("В файле нет сертификата <ca>. Похоже, это не конфиг OpenVPN.");
        }

        StringBuilder out = new StringBuilder();
        for (String c : comments) out.append(c).append('\n');
        for (String o : options) out.append(o).append('\n');
        for (String b : blocks) out.append(b).append('\n');
        return new BmstuConfig(out.toString(), skipped, remoteHost, notAfter);
    }
}
