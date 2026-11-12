package com.example.vpn_routing;

import java.math.BigInteger;
import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.List;

/**
 * Маршруты туннеля: весь интернет, кроме локальных и служебных сетей.
 *
 * Раньше в туннель шёл весь трафик (0.0.0.0/0 и ::/0), и Xray отправлял локальные адреса «напрямую» через
 * Wi-Fi или мобильную сеть. Соединения в сети Wi-Fi Direct (Mi Share, Nearby Share, Quick Share, печать —
 * адреса 192.168.49.x на интерфейсе p2p) так не работают: устройство находится, но файлы не передаются.
 * Теперь эти сети в туннель не попадают вовсе, как с «Обходом LAN» у других клиентов. Xray и так
 * отправлял их напрямую (RoutingRules.PRIVATE_CIDRS), так что для остального трафика ничего не меняется.
 *
 * Builder.excludeRoute есть только с Android 13, поэтому маршруты — дополнение списка исключений до всего
 * адресного пространства.
 */
public final class TunRoutes {

    private TunRoutes() {
    }

    /** Мимо туннеля: локальные сети, link-local, multicast (у IPv6 — ещё ff00::/8). */
    static List<String> excluded() {
        List<String> r = new ArrayList<>(RoutingRules.PRIVATE_CIDRS);
        // Петлю и широковещательный адрес система и так не отправляет в туннель, а исключение одного
        // адреса дало бы десятки лишних маршрутов (::1 — 128)
        r.remove("::1/128");
        r.remove("255.255.255.255/32");
        r.add("ff00::/8");
        return r;
    }

    /** Маршрут: адрес и длина префикса. */
    public static final class Route {
        public final String address;
        public final int prefix;

        Route(String address, int prefix) {
            this.address = address;
            this.prefix = prefix;
        }

        @Override
        public String toString() {
            return address + "/" + prefix;
        }
    }

    /** IPv4-маршруты в туннель (без исключённых сетей). */
    public static List<Route> ipv4() {
        return complement(excluded(), 32);
    }

    /** IPv6-маршруты в туннель (без исключённых сетей). */
    public static List<Route> ipv6() {
        return complement(excluded(), 128);
    }

    /** Все адреса семейства (bits = 32 или 128), кроме сетей из excluded (другое семейство пропускается). */
    static List<Route> complement(List<String> excluded, int bits) {
        List<BigInteger[]> ranges = new ArrayList<>();
        for (String cidr : excluded) {
            BigInteger[] r = range(cidr, bits);
            if (r != null) ranges.add(r);
        }
        List<Route> out = new ArrayList<>();
        split(BigInteger.ZERO, 0, bits, ranges, out);
        return out;
    }

    /** Сеть base/len: целиком вне исключений — маршрут, целиком внутри — пропуск, иначе делится пополам. */
    private static void split(BigInteger base, int len, int bits, List<BigInteger[]> excluded, List<Route> out) {
        BigInteger last = base.add(BigInteger.ONE.shiftLeft(bits - len)).subtract(BigInteger.ONE);
        boolean overlaps = false;
        for (BigInteger[] e : excluded) {
            if (e[0].compareTo(base) <= 0 && e[1].compareTo(last) >= 0) return; // внутри исключения
            if (e[0].compareTo(last) <= 0 && e[1].compareTo(base) >= 0) overlaps = true;
        }
        if (!overlaps) {
            out.add(new Route(format(base, bits), len));
            return;
        }
        split(base, len + 1, bits, excluded, out);
        split(base.setBit(bits - len - 1), len + 1, bits, excluded, out);
    }

    /** Первый и последний адрес сети; null — сеть другого семейства или запись с ошибкой. */
    private static BigInteger[] range(String cidr, int bits) {
        int slash = cidr.indexOf('/');
        if (slash < 0) return null;
        byte[] raw;
        try {
            raw = InetAddress.getByName(cidr.substring(0, slash)).getAddress();
        } catch (UnknownHostException e) {
            return null;
        }
        if (raw.length * 8 != bits) return null;
        int len = Integer.parseInt(cidr.substring(slash + 1));
        BigInteger hostMask = BigInteger.ONE.shiftLeft(bits - len).subtract(BigInteger.ONE);
        BigInteger first = new BigInteger(1, raw).andNot(hostMask);
        return new BigInteger[]{first, first.or(hostMask)};
    }

    private static String format(BigInteger value, int bits) {
        byte[] raw = new byte[bits / 8];
        byte[] v = value.toByteArray();
        int n = Math.min(v.length, raw.length);
        System.arraycopy(v, v.length - n, raw, raw.length - n, n);
        try {
            return InetAddress.getByAddress(raw).getHostAddress();
        } catch (UnknownHostException e) {
            throw new IllegalStateException(e);
        }
    }
}
