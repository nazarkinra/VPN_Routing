package com.example.vpn_routing;

import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;

import libv2ray.Libv2ray;

/** Замер задержки серверов (реальный запрос через VLESS, как «пинг» в клиентах Xray). */
final class Pinger {

    interface Callback {
        /** Вызывается в UI-потоке после каждого сервера; done — замер всех закончен. */
        void onPing(String serverId, long ms, boolean done);
    }

    private static final String TEST_URL = "https://www.gstatic.com/generate_204";
    private static final ExecutorService pool = Executors.newFixedThreadPool(6);
    private static final Handler main = new Handler(Looper.getMainLooper());

    private Pinger() {
    }

    static void ping(android.content.Context context, ServerStore store, List<ServerStore.Server> servers,
                     Callback callback) {
        RoutingVpnService.ensureCoreEnv(context);
        if (servers.isEmpty()) {
            main.post(() -> callback.onPing(null, 0, true));
            return;
        }
        AtomicInteger left = new AtomicInteger(servers.size());
        for (ServerStore.Server s : servers) {
            pool.execute(() -> {
                long ms = measure(s);
                store.setPing(s.id, ms);
                boolean done = left.decrementAndGet() == 0;
                main.post(() -> callback.onPing(s.id, ms, done));
            });
        }
    }

    /** Задержка в мс или -1. */
    static long measure(ServerStore.Server s) {
        try {
            String ip = resolve(s.link.server);
            long ms = Libv2ray.measureOutboundDelay(XrayConfig.buildPing(s.link, ip), TEST_URL);
            return ms > 0 ? ms : -1;
        } catch (Exception e) {
            Log.i("Pinger", s.title() + ": " + e.getMessage());
            return -1;
        }
    }

    private static String resolve(String host) throws Exception {
        return ServerResolver.resolve(host, null).ip;
    }
}
