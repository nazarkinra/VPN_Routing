package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.math.BigInteger;
import java.net.InetAddress;
import java.util.List;

/** Маршруты туннеля: весь интернет без локальных сетей (Wi-Fi Direct для Mi Share и т. п.). */
public class TunRoutesTest {

    private static BigInteger ip(String s) throws Exception {
        return new BigInteger(1, InetAddress.getByName(s).getAddress());
    }

    private static boolean routed(List<TunRoutes.Route> routes, String address, int bits) throws Exception {
        BigInteger a = ip(address);
        for (TunRoutes.Route r : routes) {
            BigInteger base = ip(r.address);
            BigInteger size = BigInteger.ONE.shiftLeft(bits - r.prefix);
            if (a.compareTo(base) >= 0 && a.compareTo(base.add(size)) < 0) return true;
        }
        return false;
    }

    /** Сумма размеров маршрутов: без пересечений она равна всему пространству минус исключения. */
    private static BigInteger covered(List<TunRoutes.Route> routes, int bits) throws Exception {
        BigInteger sum = BigInteger.ZERO;
        BigInteger prevEnd = BigInteger.ZERO;
        for (TunRoutes.Route r : routes) {
            BigInteger base = ip(r.address);
            assertTrue("маршруты по порядку и без пересечений: " + r, base.compareTo(prevEnd) >= 0);
            BigInteger size = BigInteger.ONE.shiftLeft(bits - r.prefix);
            assertEquals("адрес сети без битов хоста: " + r, BigInteger.ZERO, base.mod(size));
            prevEnd = base.add(size);
            sum = sum.add(size);
        }
        return sum;
    }

    @Test
    public void ipv4SkipsLocalNetworks() throws Exception {
        List<TunRoutes.Route> v4 = TunRoutes.ipv4();
        for (String local : new String[]{"192.168.49.1", "192.168.1.10", "10.1.2.3", "172.16.0.1",
                "172.31.255.255", "169.254.1.1", "224.0.0.251", "239.255.255.250", "127.0.0.1", "100.64.0.1"}) {
            assertFalse(local, routed(v4, local, 32));
        }
        for (String internet : new String[]{"8.8.8.8", "1.1.1.1", "77.88.8.8", "172.15.255.255",
                "172.32.0.0", "192.167.255.255", "192.169.0.0", "11.0.0.0", "223.255.255.255", "0.0.0.1"}) {
            assertTrue(internet, routed(v4, internet, 32));
        }
        // Всё пространство минус исключения: 10/8, 172.16/12, 192.168/16, 127/8, 169.254/16, 100.64/10,
        // 224/4
        BigInteger excluded = BigInteger.valueOf((1L << 24) + (1L << 20) + (1L << 16) + (1L << 24) + (1L << 16)
                + (1L << 22) + (1L << 28));
        assertEquals(BigInteger.ONE.shiftLeft(32).subtract(excluded), covered(v4, 32));
        assertTrue("разумное число маршрутов: " + v4.size(), v4.size() < 64);
    }

    @Test
    public void ipv6SkipsLocalNetworks() throws Exception {
        List<TunRoutes.Route> v6 = TunRoutes.ipv6();
        for (String local : new String[]{"fe80::1", "fd00::1", "fdfe:dcba:9876::1", "ff02::fb"}) {
            assertFalse(local, routed(v6, local, 128));
        }
        for (String internet : new String[]{"2001:4860:4860::8888", "2a00:1450::1", "fe00::1", "fec0::1"}) {
            assertTrue(internet, routed(v6, internet, 128));
        }
        assertTrue("разумное число маршрутов: " + v6.size(), v6.size() < 200);
        covered(v6, 128);
    }
}
