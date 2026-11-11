package com.example.vpn_routing;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;

/**
 * Конфиг Xray для телефона.
 *
 * На Raspberry Pi трафик принимал sing-box (TUN) и отдавал в Xray (VLESS). На Android одно ядро Xray
 * делает и то и другое: принимает пакеты из VpnService (inbound tun), определяет домен по TLS/HTTP
 * (sniffing) и раскладывает трафик по тем же правилам, что и sing-box на Pi.
 */
public final class XrayConfig {

    /** Адрес DNS внутри туннеля: его получают приложения, запросы к нему перехватываются. */
    public static final String TUN_DNS = "172.19.0.2";

    /** Домены, которые идут через VPN МГТУ (как правило bmstu-active на Pi). */
    public static final String BMSTU_DOMAIN = "bmstu.ru";

    private XrayConfig() {
    }

    /** Локальный SOCKS-прокси туннеля МГТУ. */
    public static final class BmstuRoute {
        final int port;
        final String user;
        final String password;

        public BmstuRoute(int port, String user, String password) {
            this.port = port;
            this.user = user;
            this.password = password;
        }
    }

    /**
     * @param link       разобранная ссылка vless://
     * @param serverIp   IP сервера (имя разрешено заранее, как в update-vpn)
     * @param localDns   DNS текущей сети (Wi-Fi роутер или оператор) для российских доменов, может быть null
     * @param exceptions исключения пользователя (домены и подсети)
     * @param bmstu      прокси туннеля МГТУ или null, если VPN МГТУ выключен
     */
    public static String build(ProxyLink link, String serverIp, String localDns, Collection<String> exceptions,
                               BmstuRoute bmstu) throws JSONException {
        return build(link, serverIp, localDns, exceptions, bmstu,
                RoutingRules.DIRECT_DOMAINS, RoutingRules.DIRECT_TUNNEL_DOMAINS);
    }

    /**
     * @param directDomains встроенные домены напрямую без отключённых пользователем
     * @param tunnelDomains домены Cloudflare Tunnel без отключённых пользователем
     */
    public static String build(ProxyLink link, String serverIp, String localDns, Collection<String> exceptions,
                               BmstuRoute bmstu, List<String> directDomains, List<String> tunnelDomains)
            throws JSONException {
        List<String> userDomains = new ArrayList<>();
        List<String> userCidrs = new ArrayList<>();
        for (String item : exceptions) {
            if (Exceptions.isCidr(item)) userCidrs.add(item);
            else userDomains.add(item);
        }

        JSONObject root = new JSONObject();
        // В отладочной сборке — журнал соединений и DNS (logcat, тег GoLog)
        root.put("log", BuildConfig.DEBUG
                ? new JSONObject().put("loglevel", "info").put("dnsLog", true)
                : new JSONObject().put("loglevel", "warning").put("access", "none"));
        root.put("dns", dns(localDns, directDomains, userDomains));
        root.put("inbounds", new JSONArray().put(tunInbound()));
        JSONArray outbounds = new JSONArray()
                .put(proxyOutbound(link, serverIp))   // первый outbound — маршрут по умолчанию, как final: proxy
                .put(new JSONObject().put("tag", "direct").put("protocol", "freedom")
                        .put("streamSettings", new JSONObject()
                                .put("sockopt", new JSONObject().put("domainStrategy", "UseIPv4"))))
                .put(new JSONObject().put("tag", "block").put("protocol", "blackhole"))
                .put(new JSONObject().put("tag", "dns-out").put("protocol", "dns")
                        .put("settings", new JSONObject().put("nonIPQuery", "reject")));
        if (bmstu != null) outbounds.put(bmstuOutbound(bmstu));
        root.put("outbounds", outbounds);
        root.put("routing", routing(localDns, directDomains, tunnelDomains, userDomains, userCidrs, bmstu != null));
        return root.toString(2);
    }

    /** Конфиг для замера задержки сервера: только VLESS-выход, без туннеля и правил. */
    public static String buildPing(ProxyLink link, String serverIp) throws JSONException {
        JSONObject root = new JSONObject();
        root.put("log", new JSONObject().put("loglevel", "none"));
        root.put("outbounds", new JSONArray().put(proxyOutbound(link, serverIp)));
        return root.toString();
    }

    /** Аналог outbound bmstu (bind_interface bmstu0) на Pi: прокси сам выбирает туннель или прямой выход. */
    private static JSONObject bmstuOutbound(BmstuRoute r) throws JSONException {
        return new JSONObject()
                .put("tag", "bmstu")
                .put("protocol", "socks")
                .put("settings", new JSONObject().put("servers", new JSONArray().put(new JSONObject()
                        .put("address", "127.0.0.1")
                        .put("port", r.port)
                        .put("users", new JSONArray().put(new JSONObject()
                                .put("user", r.user)
                                .put("pass", r.password))))));
    }

    private static JSONObject tunInbound() throws JSONException {
        return new JSONObject()
                .put("tag", "tun-in")
                .put("port", 0)
                .put("protocol", "tun")
                .put("settings", new JSONObject().put("name", "xray0").put("mtu", RoutingVpnService.MTU))
                // Аналог sniff в sing-box: домен берётся из TLS SNI / HTTP Host, адрес назначения не меняется
                .put("sniffing", new JSONObject()
                        .put("enabled", true)
                        .put("destOverride", new JSONArray().put("http").put("tls").put("quic"))
                        .put("routeOnly", true));
    }

    private static JSONObject dns(String localDns, List<String> directDomains, List<String> userDomains)
            throws JSONException {
        JSONArray domains = domainList(directDomains, userDomains);
        JSONArray servers = new JSONArray();
        // Российские домены и исключения — через DNS текущей сети (как local-dns на Pi): он работает
        // в любой сети, в том числе корпоративной. Запасные — по TCP и DoH, не по UDP 53.
        // Без доменов такие серверы стали бы общими, поэтому при пустом списке они не добавляются.
        if (domains.length() > 0) {
            if (localDns != null && !localDns.isEmpty()) {
                servers.put(dnsServer(localDns).put("port", 53).put("domains", domains).put("skipFallback", true));
            }
            for (String address : RoutingRules.FALLBACK_LOCAL_DNS) {
                servers.put(dnsServer(address).put("domains", domains).put("skipFallback", true));
            }
        }
        // Остальное — через VPN: Google DNS по TCP, затем DoH Cloudflare
        for (String address : RoutingRules.PROXY_DNS) servers.put(dnsServer(address));
        return new JSONObject()
                .put("tag", "dns-internal")
                .put("queryStrategy", "UseIPv4")
                .put("servers", servers);
    }

    private static JSONObject dnsServer(String address) throws JSONException {
        return new JSONObject().put("address", address).put("timeoutMs", RoutingRules.DNS_TIMEOUT_MS);
    }

    /** DNS сети: его запросы идут напрямую по правилу маршрутизации (у запасных — «+local»). */
    private static Set<String> localDnsServers(String localDns) {
        Set<String> result = new LinkedHashSet<>();
        if (localDns != null && !localDns.isEmpty()) result.add(localDns);
        return result;
    }

    private static JSONObject routing(String localDns, List<String> directDomains, List<String> tunnelDomains,
                                      List<String> userDomains, List<String> userCidrs,
                                      boolean bmstu) throws JSONException {
        JSONArray rules = new JSONArray();

        // Запросы самого DNS-модуля к DNS сети идут напрямую, к 8.8.8.8 — через VPN (правило по умолчанию)
        JSONArray localDnsIps = new JSONArray();
        for (String address : localDnsServers(localDns)) localDnsIps.put(address);
        if (localDnsIps.length() > 0) {
            rules.put(rule().put("inboundTag", new JSONArray().put("dns-internal"))
                    .put("ip", localDnsIps).put("outboundTag", "direct"));
        }

        // hijack-dns: любой DNS-запрос из туннеля обрабатывает DNS-модуль Xray
        rules.put(rule().put("inboundTag", new JSONArray().put("tun-in"))
                .put("port", "53").put("outboundTag", "dns-out"));

        // Сайты МГТУ — через туннель университета. Как на Pi, маршрутизируются только сайты:
        // домен известен из HTTP и TLS, поэтому только TCP
        if (bmstu) {
            rules.put(rule().put("domain", new JSONArray().put("domain:" + BMSTU_DOMAIN))
                    .put("network", "tcp").put("outboundTag", "bmstu"));
            // QUIC (HTTP/3) к сайтам МГТУ отклоняется: прокси туннеля только TCP, браузер перейдёт на TCP
            rules.put(rule().put("domain", new JSONArray().put("domain:" + BMSTU_DOMAIN))
                    .put("network", "udp").put("port", "443").put("outboundTag", "block"));
        }

        rules.put(rule().put("ip", toArray(RoutingRules.PRIVATE_CIDRS)).put("outboundTag", "direct"));
        rules.put(rule().put("port", RoutingRules.DIRECT_PORTS).put("outboundTag", "direct"));
        // Правило с пустым списком доменов не добавляется (все встроенные могли быть удалены)
        JSONArray tunnels = domainList(tunnelDomains, null);
        if (tunnels.length() > 0) rules.put(rule().put("domain", tunnels).put("outboundTag", "direct"));
        JSONArray direct = domainList(directDomains, userDomains);
        if (direct.length() > 0) rules.put(rule().put("domain", direct).put("outboundTag", "direct"));
        if (!userCidrs.isEmpty()) {
            rules.put(rule().put("ip", toArray(userCidrs)).put("outboundTag", "direct"));
        }
        // QUIC к зарубежным ресурсам отклоняется: приложения сразу переходят на TCP
        rules.put(rule().put("network", "udp").put("port", "443").put("outboundTag", "block"));

        return new JSONObject().put("domainStrategy", "AsIs").put("rules", rules);
    }

    private static JSONObject rule() throws JSONException {
        return new JSONObject().put("type", "field");
    }

    private static JSONArray toArray(Collection<String> items) {
        JSONArray array = new JSONArray();
        for (String item : items) array.put(item);
        return array;
    }

    /** domain:example.com в Xray — сам домен и все поддомены (как domain_suffix в sing-box). */
    private static JSONArray domainList(List<String> builtin, List<String> extra) {
        Set<String> all = new LinkedHashSet<>(builtin);
        if (extra != null) all.addAll(extra);
        JSONArray array = new JSONArray();
        // Без повторов: ya.ru не нужен, если есть ru
        for (String name : RoutingRules.compact(all)) array.put("domain:" + name);
        return array;
    }

    /** VLESS-выход: то же, что update_xray в update-vpn.py. */
    private static JSONObject proxyOutbound(ProxyLink p, String serverIp) throws JSONException {
        if (p.isHysteria()) return hysteriaOutbound(p, serverIp);
        JSONObject user = new JSONObject().put("id", p.uuid)
                .put("encryption", p.encryption.isEmpty() ? "none" : p.encryption);
        if (!p.flow.isEmpty()) user.put("flow", p.flow);

        JSONObject stream = new JSONObject().put("network", p.network).put("security", p.security);
        if (p.security.equals("reality")) {
            stream.put("realitySettings", new JSONObject()
                    .put("show", false)
                    .put("fingerprint", p.fp.isEmpty() ? "chrome" : p.fp)
                    .put("serverName", !p.sni.isEmpty() ? p.sni : p.server)
                    .put("publicKey", p.pbk)
                    .put("shortId", p.sid)
                    .put("spiderX", ""));
        } else if (p.security.equals("tls")) {
            JSONObject tls = new JSONObject()
                    .put("serverName", firstNonEmpty(p.sni, p.host, p.server))
                    .put("fingerprint", p.fp.isEmpty() ? "chrome" : p.fp);
            if (!p.alpn.isEmpty()) tls.put("alpn", alpnList(p.alpn));
            applyCertChecks(tls, p);
            stream.put("tlsSettings", tls);
        }
        // Параметр fm: фрагментация TLS Hello и другие маски поверх транспорта
        JSONObject finalmask = jsonObjectOrNull(p.finalmask);
        if (finalmask != null) stream.put("finalmask", finalmask);

        switch (p.network) {
            case "ws":
            case "httpupgrade":
            case "xhttp": {
                JSONObject transport = new JSONObject().put("path", p.path.isEmpty() ? "/" : p.path);
                String host = firstNonEmpty(p.host, p.sni, "");
                if (!host.isEmpty()) transport.put("host", host);
                if (p.network.equals("xhttp") && !p.mode.isEmpty()) transport.put("mode", p.mode);
                // extra: xmux, паддинг и прочие настройки xhttp от панели
                JSONObject extra = p.network.equals("xhttp") ? jsonObjectOrNull(p.extra) : null;
                if (extra != null) transport.put("extra", extra);
                stream.put(p.network + "Settings", transport);
                break;
            }
            case "grpc":
                stream.put("grpcSettings", new JSONObject().put("serviceName", p.serviceName));
                break;
            default:
                break;
        }

        JSONObject outbound = new JSONObject()
                .put("tag", "proxy")
                .put("protocol", "vless")
                .put("settings", new JSONObject().put("vnext", new JSONArray().put(new JSONObject()
                        .put("address", serverIp)
                        .put("port", p.port)
                        .put("users", new JSONArray().put(user)))))
                .put("streamSettings", stream);

        // Mux: меньше рукопожатий с VPS. Несовместим с flow xtls-rprx-vision, а XHTTP мультиплексирует
        // соединения сам (xmux) — Mux поверх него сервер обрывает (EOF)
        if (p.flow.isEmpty() && !p.network.equals("xhttp")) {
            outbound.put("mux", new JSONObject()
                    .put("enabled", true)
                    .put("concurrency", 8)
                    .put("xudpConcurrency", 16)
                    .put("xudpProxyUDP443", "reject"));
        }
        return outbound;
    }

    /**
     * Hysteria2 (QUIC): тот же вид, что панель отдаёт клиентам Xray — protocol hysteria версии 2,
     * транспорт hysteria с auth, TLS с ALPN h3, обфускация salamander — маска finalmask.udp.
     */
    private static JSONObject hysteriaOutbound(ProxyLink p, String serverIp) throws JSONException {
        JSONObject tls = new JSONObject()
                .put("serverName", firstNonEmpty(p.sni, p.server))
                .put("alpn", alpnList(p.alpn.isEmpty() ? "h3" : p.alpn));
        if (!p.fp.isEmpty()) tls.put("fingerprint", p.fp);
        applyCertChecks(tls, p);

        JSONObject stream = new JSONObject()
                .put("network", "hysteria")
                .put("hysteriaSettings", new JSONObject().put("version", 2).put("auth", p.uuid))
                .put("security", "tls")
                .put("tlsSettings", tls);

        JSONObject finalmask = jsonObjectOrNull(p.finalmask);
        if (p.obfs.equals("salamander")) {
            if (finalmask == null) finalmask = new JSONObject();
            JSONArray udp = finalmask.optJSONArray("udp");
            if (udp == null) udp = new JSONArray();
            udp.put(new JSONObject().put("type", "salamander")
                    .put("settings", new JSONObject().put("password", p.obfsPassword)));
            finalmask.put("udp", udp);
        }
        if (finalmask != null) stream.put("finalmask", finalmask);

        return new JSONObject()
                .put("tag", "proxy")
                .put("protocol", "hysteria")
                .put("settings", new JSONObject().put("address", serverIp).put("port", p.port).put("version", 2))
                .put("streamSettings", stream);
    }

    /** Проверка сертификата: по другому имени (vcn) и/или по отпечатку (pcs, pinSHA256). */
    private static void applyCertChecks(JSONObject tls, ProxyLink p) throws JSONException {
        if (!p.verifyName.isEmpty()) tls.put("verifyPeerCertByName", p.verifyName);
        if (!p.pinnedCert.isEmpty()) tls.put("pinnedPeerCertSha256", p.pinnedCert);
    }

    private static JSONArray alpnList(String alpn) {
        JSONArray list = new JSONArray();
        for (String a : alpn.split(",")) if (!a.trim().isEmpty()) list.put(a.trim());
        return list;
    }

    /** JSON-объект из параметра ссылки; null, если параметра нет или он испорчен. */
    private static JSONObject jsonObjectOrNull(String text) {
        if (text == null || text.trim().isEmpty()) return null;
        try {
            return new JSONObject(text);
        } catch (JSONException e) {
            return null;
        }
    }

    private static String firstNonEmpty(String... values) {
        for (String v : values) if (v != null && !v.isEmpty()) return v;
        return "";
    }
}
