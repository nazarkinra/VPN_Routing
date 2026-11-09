package com.example.vpn_routing;

import android.content.Context;
import android.content.SharedPreferences;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

/** Серверы (отдельные ссылки и серверы из подписок) и подписки. */
public final class ServerStore {

    public static final class Server {
        public final String id;
        public final String url;
        /** null — сервер добавлен отдельной ссылкой. */
        public final String subscriptionId;
        public final ProxyLink link;

        Server(String id, String url, String subscriptionId) {
            this.id = id;
            this.url = url;
            this.subscriptionId = subscriptionId;
            this.link = ProxyLink.parse(url);
        }

        public String title() {
            return link.name.isEmpty() ? link.server : link.name;
        }

        /** «VLESS · Reality · TCP», «Hysteria2 · QUIC» */
        public String subtitle() {
            if (link.isHysteria()) {
                return "Hysteria2 · QUIC" + (link.obfs.isEmpty() ? "" : " · Salamander");
            }
            String security = link.security.equals("none") ? "" : " · " + capitalize(link.security);
            return "VLESS" + security + " · " + link.network.toUpperCase(java.util.Locale.ROOT);
        }

        private static String capitalize(String s) {
            return s.equals("tls") ? "TLS" : Character.toUpperCase(s.charAt(0)) + s.substring(1);
        }
    }

    public static final class Subscription {
        public final String id;
        public final String url;
        public String name = "";
        public long updatedAt;
        /** Из заголовка subscription-userinfo, байты; total 0 — безлимит. */
        public long upload;
        public long download;
        public long total;
        /** Окончание подписки, секунды Unix; 0 — бессрочно. */
        public long expire;
        public int updateIntervalHours;
        public String announce = "";
        public String error = "";
        public int unsupported;

        Subscription(String id, String url) {
            this.id = id;
            this.url = url;
        }

        public String title() {
            if (!name.isEmpty()) return name;
            try {
                String host = android.net.Uri.parse(url).getHost();
                return host == null ? url : host;
            } catch (RuntimeException e) {
                return url;
            }
        }

        public boolean isStale() {
            long hours = updateIntervalHours > 0 ? updateIntervalHours : 12;
            return System.currentTimeMillis() - updatedAt > hours * 3600_000L;
        }
    }

    private static final String PREFS = "servers";
    private static final String KEY_SERVERS = "servers";
    private static final String KEY_SUBSCRIPTIONS = "subscriptions";

    private static ServerStore instance;

    private final SharedPreferences prefs;
    private final List<Server> servers = new ArrayList<>();
    private final List<Subscription> subscriptions = new ArrayList<>();
    /** Результаты пинга в мс; -1 — не ответил. Только в памяти. */
    private final Map<String, Long> pings = new ConcurrentHashMap<>();

    public static synchronized ServerStore get(Context context) {
        if (instance == null) instance = new ServerStore(context.getApplicationContext());
        return instance;
    }

    private ServerStore(Context context) {
        prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        load();
        // Ключ из первой версии приложения становится отдельным сервером
        String legacy = new Settings(context).getVlessUrl();
        if (servers.isEmpty() && !legacy.isEmpty()) {
            try {
                servers.add(new Server(newId(), legacy, null));
                save();
            } catch (IllegalArgumentException ignored) {
            }
        }
    }

    private static String newId() {
        return UUID.randomUUID().toString();
    }

    // ---------- Чтение ----------

    public synchronized List<Server> servers() {
        return new ArrayList<>(servers);
    }

    public synchronized List<Subscription> subscriptions() {
        return new ArrayList<>(subscriptions);
    }

    public synchronized List<Server> serversOf(String subscriptionId) {
        List<Server> result = new ArrayList<>();
        for (Server s : servers) {
            if (subscriptionId == null ? s.subscriptionId == null : subscriptionId.equals(s.subscriptionId)) {
                result.add(s);
            }
        }
        return result;
    }

    public synchronized Server findByUrl(String url) {
        for (Server s : servers) if (s.url.equals(url)) return s;
        return null;
    }

    public synchronized Server find(String id) {
        for (Server s : servers) if (s.id.equals(id)) return s;
        return null;
    }

    public synchronized Subscription subscription(String id) {
        for (Subscription s : subscriptions) if (s.id.equals(id)) return s;
        return null;
    }

    public Long ping(String serverId) {
        return pings.get(serverId);
    }

    public void setPing(String serverId, long ms) {
        pings.put(serverId, ms);
    }

    // ---------- Изменение ----------

    /** Добавляет отдельный сервер. Возвращает уже существующий, если ссылка такая же. */
    public synchronized Server addServer(String url) throws IllegalArgumentException {
        String clean = url.replaceAll("\\s+", "");
        Server existing = findByUrl(clean);
        if (existing != null) return existing;
        Server server = new Server(newId(), clean, null);
        servers.add(server);
        save();
        return server;
    }

    public synchronized Subscription addSubscription(String url) {
        for (Subscription s : subscriptions) if (s.url.equals(url)) return s;
        Subscription sub = new Subscription(newId(), url.trim());
        subscriptions.add(sub);
        save();
        return sub;
    }

    /** Заменяет серверы подписки свежим списком и сохраняет сведения о ней. */
    public synchronized void applySubscription(String subscriptionId, SubscriptionFetcher.Result result) {
        Subscription sub = subscription(subscriptionId);
        if (sub == null) return;
        // Серверы, которые остались в подписке, сохраняют свои id (и результаты пинга)
        List<Server> old = serversOf(subscriptionId);
        List<Server> fresh = new ArrayList<>();
        for (String url : result.links) {
            Server keep = null;
            for (Server s : old) if (s.url.equals(url)) keep = s;
            try {
                fresh.add(keep != null ? keep : new Server(newId(), url, subscriptionId));
            } catch (IllegalArgumentException ignored) {
                // ссылка с неподдерживаемыми параметрами
            }
        }
        int insertAt = servers.size();
        for (int i = 0; i < servers.size(); i++) {
            if (subscriptionId.equals(servers.get(i).subscriptionId)) {
                insertAt = i;
                break;
            }
        }
        servers.removeAll(old);
        servers.addAll(Math.min(insertAt, servers.size()), fresh);

        if (!result.title.isEmpty()) sub.name = result.title;
        sub.upload = result.upload;
        sub.download = result.download;
        sub.total = result.total;
        sub.expire = result.expire;
        sub.updateIntervalHours = result.updateIntervalHours;
        sub.announce = result.announce;
        sub.unsupported = result.unsupported + (result.links.size() - fresh.size());
        sub.updatedAt = System.currentTimeMillis();
        sub.error = "";
        save();
    }

    public synchronized void setSubscriptionError(String subscriptionId, String error) {
        Subscription sub = subscription(subscriptionId);
        if (sub == null) return;
        sub.error = error;
        save();
    }

    public synchronized void renameSubscription(String subscriptionId, String name) {
        Subscription sub = subscription(subscriptionId);
        if (sub == null) return;
        sub.name = name.trim();
        save();
    }

    public synchronized void removeServer(String id) {
        Server s = find(id);
        if (s == null) return;
        servers.remove(s);
        pings.remove(id);
        save();
    }

    public synchronized void removeSubscription(String id) {
        Subscription sub = subscription(id);
        if (sub == null) return;
        for (Server s : serversOf(id)) {
            servers.remove(s);
            pings.remove(s.id);
        }
        subscriptions.remove(sub);
        save();
    }

    // ---------- Хранение ----------

    private void load() {
        try {
            JSONArray subs = new JSONArray(prefs.getString(KEY_SUBSCRIPTIONS, "[]"));
            for (int i = 0; i < subs.length(); i++) {
                JSONObject o = subs.getJSONObject(i);
                Subscription s = new Subscription(o.getString("id"), o.getString("url"));
                s.name = o.optString("name");
                s.updatedAt = o.optLong("updatedAt");
                s.upload = o.optLong("upload");
                s.download = o.optLong("download");
                s.total = o.optLong("total");
                s.expire = o.optLong("expire");
                s.updateIntervalHours = o.optInt("interval");
                s.announce = o.optString("announce");
                s.error = o.optString("error");
                s.unsupported = o.optInt("unsupported");
                subscriptions.add(s);
            }
            JSONArray list = new JSONArray(prefs.getString(KEY_SERVERS, "[]"));
            for (int i = 0; i < list.length(); i++) {
                JSONObject o = list.getJSONObject(i);
                try {
                    servers.add(new Server(o.getString("id"), o.getString("url"),
                            o.isNull("sub") ? null : o.optString("sub", null)));
                } catch (IllegalArgumentException ignored) {
                }
            }
        } catch (JSONException ignored) {
        }
    }

    private void save() {
        try {
            JSONArray subs = new JSONArray();
            for (Subscription s : subscriptions) {
                subs.put(new JSONObject()
                        .put("id", s.id).put("url", s.url).put("name", s.name)
                        .put("updatedAt", s.updatedAt).put("upload", s.upload).put("download", s.download)
                        .put("total", s.total).put("expire", s.expire).put("interval", s.updateIntervalHours)
                        .put("announce", s.announce).put("error", s.error).put("unsupported", s.unsupported));
            }
            JSONArray list = new JSONArray();
            for (Server s : servers) {
                list.put(new JSONObject().put("id", s.id).put("url", s.url)
                        .put("sub", s.subscriptionId == null ? JSONObject.NULL : s.subscriptionId));
            }
            prefs.edit().putString(KEY_SUBSCRIPTIONS, subs.toString())
                    .putString(KEY_SERVERS, list.toString()).apply();
        } catch (JSONException ignored) {
        }
    }
}
