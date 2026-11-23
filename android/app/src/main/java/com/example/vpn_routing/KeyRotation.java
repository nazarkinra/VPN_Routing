package com.example.vpn_routing;

import android.content.Context;
import android.util.Log;

import java.util.List;

/**
 * Плановая смена ключей на сервере: подписка присылает тот же сервер с новым UUID, а старый
 * ключ ещё какое-то время работает. Здесь — фоновое обновление подписок, пока VPN включён, и
 * переход выбранного сервера на новый ключ без участия человека.
 */
final class KeyRotation {

    private static final String TAG = "KeyRotation";
    /** Как часто служба проверяет, не устарели ли подписки (интервал задаёт сервер, обычно 4 ч). */
    static final long CHECK_EVERY_MS = 30 * 60 * 1000L;

    private KeyRotation() {
    }

    /**
     * Тот же сервер с другим ключом: совпадают протокол, адрес, порт, транспорт, путь и название, а
     * ссылка другая. null — замены нет или она неоднозначна (тогда ничего не трогаем).
     */
    static String replacementFor(String oldUrl, List<ServerStore.Server> servers) {
        ProxyLink old;
        try {
            old = ProxyLink.parse(oldUrl);
        } catch (IllegalArgumentException e) {
            return null;
        }
        String found = null;
        for (ServerStore.Server s : servers) {
            if (s.url.equals(oldUrl)) return null; // ссылка на месте — менять нечего
            ProxyLink l = s.link;
            if (l.protocol.equals(old.protocol) && l.server.equalsIgnoreCase(old.server) && l.port == old.port
                    && l.network.equals(old.network) && l.path.equals(old.path) && l.name.equals(old.name)) {
                if (found != null) return null;
                found = s.url;
            }
        }
        return found;
    }

    /**
     * Обновить устаревшие подписки и, если ключ выбранного сервера сменился, вернуть ссылку для
     * перехода (null — переходить не нужно). Вызывать не в UI-потоке.
     */
    static String refreshStale(Context context) {
        ServerStore store = ServerStore.get(context);
        String hwid = DeviceId.get(context);
        for (ServerStore.Subscription sub : store.subscriptions()) {
            if (!sub.isStale()) continue;
            try {
                store.applySubscription(sub.id, SubscriptionFetcher.fetchStored(sub.url, hwid));
            } catch (Exception e) {
                store.setSubscriptionError(sub.id, e.getMessage() == null ? e.getClass().getSimpleName() : e.getMessage());
                Log.w(TAG, "subscription refresh failed: " + e);
            }
        }
        String active = new Settings(context).getVlessUrl();
        if (active == null || active.isEmpty() || store.findByUrl(active) != null) return null;
        return replacementFor(active, store.servers());
    }
}
