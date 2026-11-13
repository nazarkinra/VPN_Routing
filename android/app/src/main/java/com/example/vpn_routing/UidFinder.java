package com.example.vpn_routing;

import android.net.ConnectivityManager;
import android.os.Build;
import android.system.OsConstants;
import android.util.Log;

import java.io.BufferedReader;
import java.io.FileReader;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;

import libv2ray.ProcessFinder;

/**
 * Какое приложение открыло соединение (для правил дополнительных туннелей). Ядро Xray вызывает его
 * для каждого нового соединения, дошедшего до правила process, и сравнивает UID с UID приложений туннеля.
 *
 * Android 10+: ConnectivityManager.getConnectionOwnerUid — его разрешено вызывать активному VPN-приложению.
 * Android 7–9: таблицы /proc/net/tcp|udp (на этих версиях они ещё доступны приложениям).
 */
final class UidFinder implements ProcessFinder {

    private static final String TAG = "UidFinder";
    private static final long CACHE_MS = 15_000;
    private static final int CACHE_SIZE = 512;

    private final ConnectivityManager connectivity;
    /** Одно соединение проверяется несколькими правилами (по одному на туннель) — кеш на 15 с. */
    private final Map<String, long[]> cache = new LinkedHashMap<String, long[]>(64, 0.75f, true) {
        @Override
        protected boolean removeEldestEntry(Map.Entry<String, long[]> eldest) {
            return size() > CACHE_SIZE;
        }
    };

    UidFinder(ConnectivityManager connectivity) {
        this.connectivity = connectivity;
    }

    @Override
    public long findProcessByConnection(String network, String srcIP, long srcPort, String destIP, long destPort) {
        String key = network + " " + srcIP + ":" + srcPort + " " + destIP + ":" + destPort;
        long now = System.currentTimeMillis();
        synchronized (cache) {
            long[] hit = cache.get(key);
            if (hit != null && now - hit[1] < CACHE_MS) return hit[0];
        }
        int uid;
        try {
            uid = Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q
                    ? ownerUid(network, srcIP, (int) srcPort, destIP, (int) destPort)
                    : procNetUid(network, srcIP, (int) srcPort);
        } catch (Exception e) {
            Log.w(TAG, key + ": " + e);
            uid = -1;
        }
        if (BuildConfig.DEBUG) Log.d(TAG, key + " -> uid " + uid);
        synchronized (cache) {
            cache.put(key, new long[]{uid, now});
        }
        return uid;
    }

    @android.annotation.TargetApi(Build.VERSION_CODES.Q)
    private int ownerUid(String network, String srcIP, int srcPort, String destIP, int destPort) throws Exception {
        int protocol = "udp".equals(network) ? OsConstants.IPPROTO_UDP : OsConstants.IPPROTO_TCP;
        InetSocketAddress local = new InetSocketAddress(InetAddress.getByName(srcIP), srcPort);
        InetSocketAddress remote = new InetSocketAddress(InetAddress.getByName(destIP), destPort);
        return connectivity.getConnectionOwnerUid(protocol, local, remote);
    }

    /** Строка таблицы /proc/net, где локальный адрес — srcIP:srcPort; UID — восьмая колонка. */
    private static int procNetUid(String network, String srcIP, int srcPort) throws Exception {
        String[] files = "udp".equals(network)
                ? new String[]{"/proc/net/udp", "/proc/net/udp6"}
                : new String[]{"/proc/net/tcp", "/proc/net/tcp6"};
        String port = String.format(Locale.ROOT, "%04X", srcPort);
        byte[] ip = InetAddress.getByName(srcIP).getAddress();
        String v4 = ip.length == 4 ? hexWord(ip, 0) : null;
        for (String file : files) {
            try (BufferedReader r = new BufferedReader(new FileReader(file))) {
                r.readLine(); // заголовок
                String line;
                while ((line = r.readLine()) != null) {
                    String[] f = line.trim().split("\\s+");
                    if (f.length < 8) continue;
                    String local = f[1];
                    int colon = local.indexOf(':');
                    if (colon < 0 || !local.substring(colon + 1).equalsIgnoreCase(port)) continue;
                    String addr = local.substring(0, colon);
                    boolean match;
                    if (addr.length() == 8) {
                        match = v4 != null && addr.equalsIgnoreCase(v4);
                    } else if (v4 != null) {
                        // IPv4 на сокете IPv6: ::ffff:a.b.c.d
                        match = addr.equalsIgnoreCase("0000000000000000FFFF0000" + v4);
                    } else {
                        match = addr.equalsIgnoreCase(hexWord(ip, 0) + hexWord(ip, 4) + hexWord(ip, 8)
                                + hexWord(ip, 12));
                    }
                    if (match) return Integer.parseInt(f[7]);
                }
            }
        }
        return -1;
    }

    /** 4 байта адреса так, как их пишет ядро в /proc/net (порядок байт little-endian). */
    private static String hexWord(byte[] b, int off) {
        return String.format(Locale.ROOT, "%02X%02X%02X%02X",
                b[off + 3] & 0xff, b[off + 2] & 0xff, b[off + 1] & 0xff, b[off] & 0xff);
    }
}
