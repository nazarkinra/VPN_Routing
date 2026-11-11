package com.example.vpn_routing;

import java.net.IDN;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.List;
import java.util.Locale;

/** Встроенные правила маршрутизации — те же, что в routing/configs/sing-box.json.template. */
public final class RoutingRules {

    private RoutingRules() {
    }

    /**
     * Домены напрямую (вместе с поддоменами) и через DNS домашней сети.
     * Сайты в зонах .ru/.su/.рф/.moscow сюда не входят: их уже покрывает зона (ya.ru, gosuslugi.ru и т. п.).
     */
    public static final List<String> DIRECT_DOMAINS = Arrays.asList(
            // Доменные зоны (xn--p1ai — это .рф)
            "ru", "su", "xn--p1ai", "moscow",
            // Яндекс
            "yandex.net", "yandex.com",
            // VK и его CDN
            "vk.com", "vk.me", "userapi.com", "vk-portal.net", "vk-apps.com", "vkuser.net", "vkuserlive.com",
            "vkuseraudio.net", "vkuseraudio.com", "vkuservideo.net", "vkuservideo.com",
            // Банки, госсервисы, прочее
            "sberbank.com", "emias.info", "habr.com", "kaspersky.com", "drweb.com", "crealitycloud.com");

    /** Туннели Cloudflare работают только напрямую. */
    public static final List<String> DIRECT_TUNNEL_DOMAINS = Arrays.asList("argotunnel.com", "trycloudflare.com");

    /** Локальные и служебные сети. */
    public static final List<String> PRIVATE_CIDRS = Arrays.asList(
            "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
            "169.254.0.0/16", "100.64.0.0/10", "224.0.0.0/4", "255.255.255.255/32",
            "fc00::/7", "fe80::/10", "::1/128");

    /** Порты VPN-протоколов и Cloudflare Tunnel: их трафик не заворачивается в прокси. */
    public static final String DIRECT_PORTS = "2100,7844,1194,51820,500,4500,1701,1723,10808,10809";

    /**
     * Запасные DNS для российских доменов и исключений, напрямую в обход VPN: на случай, если DNS сети
     * не определился или не отвечает. Только TCP и DoH по IP: в корпоративных сетях (МГТУ) обычный
     * DNS по UDP 53 на внешние серверы заблокирован, работает лишь DNS, выданный сетью.
     * «+local» — запрос идёт с устройства напрямую, мимо правил маршрутизации.
     */
    public static final List<String> FALLBACK_LOCAL_DNS = Arrays.asList(
            "tcp+local://77.88.8.8", "https+local://1.1.1.1/dns-query");

    /** DNS для остальных доменов: через VPN, по TCP, затем DoH — от политики сети не зависит. */
    public static final List<String> PROXY_DNS = Arrays.asList(
            "tcp://8.8.8.8", "https://1.1.1.1/dns-query");

    /** Сколько ждать ответа одного DNS-сервера, прежде чем спросить следующий. */
    public static final int DNS_TIMEOUT_MS = 2500;

    /** Исключение уже покрыто встроенными правилами (домен или его родитель в списке). */
    public static boolean isBuiltinDirect(String value) {
        return coveredBy(canonical(value), DIRECT_DOMAINS);
    }

    /** Домен в одном виде: без точки в начале и в конце, строчными, кириллица — в punycode (рф → xn--p1ai). */
    public static String canonical(String domain) {
        String d = domain.trim().toLowerCase(Locale.ROOT);
        while (d.startsWith(".")) d = d.substring(1);
        while (d.endsWith(".")) d = d.substring(0, d.length() - 1);
        try {
            d = IDN.toASCII(d, IDN.ALLOW_UNASSIGNED).toLowerCase(Locale.ROOT);
        } catch (IllegalArgumentException ignored) {
            // оставляем как есть
        }
        return d;
    }

    /** Список без отключённых записей (сравнение в каноническом виде). */
    public static List<String> without(List<String> domains, Collection<String> disabled) {
        List<String> off = new ArrayList<>();
        for (String d : disabled) off.add(canonical(d));
        List<String> result = new ArrayList<>();
        for (String d : domains) if (!off.contains(canonical(d))) result.add(d);
        return result;
    }

    /** domain совпадает с одним из parents или является его поддоменом. */
    static boolean coveredBy(String domain, Collection<String> parents) {
        for (String p : parents) {
            String parent = canonical(p);
            if (!parent.isEmpty() && (domain.equals(parent) || domain.endsWith("." + parent))) return true;
        }
        return false;
    }

    /**
     * Список доменов без повторов: каждый домен в каноническом виде, а домены, у которых в списке
     * есть родитель (ya.ru при ru), выбрасываются. Порядок первых вхождений сохраняется.
     */
    public static List<String> compact(Collection<String> domains) {
        List<String> canon = new ArrayList<>();
        for (String d : domains) {
            String c = canonical(d);
            if (!c.isEmpty() && !canon.contains(c)) canon.add(c);
        }
        List<String> result = new ArrayList<>();
        for (String d : canon) {
            boolean covered = false;
            for (String other : canon) {
                if (!other.equals(d) && d.endsWith("." + other)) {
                    covered = true;
                    break;
                }
            }
            if (!covered) result.add(d);
        }
        return result;
    }
}
