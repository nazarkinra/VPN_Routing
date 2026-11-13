package com.example.vpn_routing;

import android.content.Context;
import android.content.SharedPreferences;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

/**
 * Дополнительные туннели (версия 2.1). Каждый туннель — свой сервер, параллельный основному VPN: в него
 * уходят выбранные приложения целиком и выбранные домены. Всё остальное идёт по правилам основного VPN.
 *
 * Под капотом это не отдельные VpnService (Android держит только один VPN), а дополнительные выходы
 * того же ядра Xray: туннель Android один, трафик раскладывается правилами маршрутизации — по UID
 * приложения (ProcessFinder) и по домену (sniffing).
 */
public final class TunnelStore {

    public static final class Tunnel {
        public final String id;
        public String name;
        /** Ссылка сервера (как ключ основного VPN: переживает обновление подписки). Пусто — не выбран. */
        public String serverUrl = "";
        public boolean enabled = true;
        /** Пакеты приложений, которые целиком идут через этот туннель. */
        public final List<String> apps = new ArrayList<>();
        /** Домены (с поддоменами) и IP-подсети, которые идут через этот туннель. */
        public final List<String> domains = new ArrayList<>();

        Tunnel(String id, String name) {
            this.id = id;
            this.name = name;
        }

        /** Сервер разобран, туннель можно запускать. */
        public ProxyLink link() {
            if (serverUrl.isEmpty()) return null;
            try {
                return ProxyLink.parse(serverUrl);
            } catch (IllegalArgumentException e) {
                return null;
            }
        }

        /** Туннелю есть что делать: включён, выбран сервер и есть хотя бы одно правило. */
        public boolean isActive() {
            return enabled && link() != null && (!apps.isEmpty() || !domains.isEmpty());
        }
    }

    private static final String PREFS = "tunnels";
    private static final String KEY_TUNNELS = "tunnels";

    private static TunnelStore instance;

    private final SharedPreferences prefs;
    private final List<Tunnel> tunnels = new ArrayList<>();

    public static synchronized TunnelStore get(Context context) {
        if (instance == null) instance = new TunnelStore(context.getApplicationContext());
        return instance;
    }

    private TunnelStore(Context context) {
        prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        load();
    }

    // ---------- Чтение ----------

    public synchronized List<Tunnel> all() {
        List<Tunnel> result = new ArrayList<>();
        for (Tunnel t : tunnels) result.add(copy(t));
        return result;
    }

    public synchronized Tunnel find(String id) {
        for (Tunnel t : tunnels) if (t.id.equals(id)) return copy(t);
        return null;
    }

    /** Номер туннеля для подписи и цвета (1, 2, …), 0 — не найден. */
    public synchronized int number(String id) {
        for (int i = 0; i < tunnels.size(); i++) if (tunnels.get(i).id.equals(id)) return i + 1;
        return 0;
    }

    /** Туннель, к которому уже привязано приложение (кроме exceptId), или null. */
    public synchronized Tunnel ownerOfApp(String pkg, String exceptId) {
        for (Tunnel t : tunnels) {
            if (!t.id.equals(exceptId) && t.apps.contains(pkg)) return copy(t);
        }
        return null;
    }

    /** Туннель, в котором уже есть этот домен (или его родитель), кроме exceptId, или null. */
    public synchronized Tunnel ownerOfDomain(String value, String exceptId) {
        boolean cidr = Exceptions.isCidr(value);
        String c = cidr ? value : RoutingRules.canonical(value);
        for (Tunnel t : tunnels) {
            if (t.id.equals(exceptId)) continue;
            for (String d : t.domains) {
                if (cidr ? d.equals(value)
                        : !Exceptions.isCidr(d) && covers(RoutingRules.canonical(d), c)) return copy(t);
            }
        }
        return null;
    }

    /** Пакеты приложений всех туннелей, которые сейчас работают (для списка приложений туннеля Android). */
    public synchronized List<String> activeApps() {
        List<String> result = new ArrayList<>();
        for (Tunnel t : tunnels) if (t.isActive()) result.addAll(t.apps);
        return result;
    }

    // ---------- Изменение ----------

    /**
     * Новый туннель. Числа туннелей не ограничено: простаивающий туннель не держит соединений, а приложение
     * соединения проверяется один раз (кеш в UidFinder), сколько бы туннелей ни было.
     */
    public synchronized Tunnel add(String name) {
        Tunnel t = new Tunnel(UUID.randomUUID().toString(), name.trim());
        tunnels.add(t);
        save();
        return copy(t);
    }

    /** Сохраняет изменённую копию туннеля. */
    public synchronized void update(Tunnel changed) {
        for (int i = 0; i < tunnels.size(); i++) {
            if (tunnels.get(i).id.equals(changed.id)) {
                tunnels.set(i, copy(changed));
                save();
                return;
            }
        }
    }

    public synchronized void remove(String id) {
        for (int i = 0; i < tunnels.size(); i++) {
            if (tunnels.get(i).id.equals(id)) {
                tunnels.remove(i);
                save();
                return;
            }
        }
    }

    /**
     * Задаёт приложения туннеля. Приложение может быть только в одном туннеле: из других оно убирается.
     */
    public synchronized void setApps(String id, List<String> apps) {
        for (Tunnel t : tunnels) {
            if (t.id.equals(id)) {
                t.apps.clear();
                t.apps.addAll(apps);
            } else {
                t.apps.removeAll(apps);
            }
        }
        save();
    }

    /**
     * Добавляет домен, адрес сайта или IP-подсеть. Возвращает нормализованное значение или null, если он
     * уже есть в этом туннеле. Вложенные домены заменяются родителем (при example.com запись sub.example.com
     * не нужна).
     */
    public synchronized String addDomain(String id, String raw) throws IllegalArgumentException {
        String value = Exceptions.normalize(raw);
        Tunnel t = null;
        for (Tunnel x : tunnels) if (x.id.equals(id)) t = x;
        if (t == null) return null;
        if (t.domains.contains(value)) return null;
        if (!Exceptions.isCidr(value)) {
            String c = RoutingRules.canonical(value);
            for (String d : t.domains) {
                if (!Exceptions.isCidr(d) && covers(RoutingRules.canonical(d), c)) return null;
            }
            List<String> kept = new ArrayList<>();
            for (String d : t.domains) {
                if (Exceptions.isCidr(d) || !RoutingRules.canonical(d).endsWith("." + c)) kept.add(d);
            }
            t.domains.clear();
            t.domains.addAll(kept);
        }
        t.domains.add(value);
        save();
        return value;
    }

    public synchronized void removeDomain(String id, String value) {
        for (Tunnel t : tunnels) {
            if (t.id.equals(id) && t.domains.remove(value)) save();
        }
    }

    private static boolean covers(String parent, String child) {
        return child.equals(parent) || child.endsWith("." + parent);
    }

    // ---------- Хранение ----------

    private static Tunnel copy(Tunnel t) {
        Tunnel c = new Tunnel(t.id, t.name);
        c.serverUrl = t.serverUrl;
        c.enabled = t.enabled;
        c.apps.addAll(t.apps);
        c.domains.addAll(t.domains);
        return c;
    }

    private void load() {
        try {
            JSONArray list = new JSONArray(prefs.getString(KEY_TUNNELS, "[]"));
            for (int i = 0; i < list.length(); i++) {
                tunnels.add(fromJson(list.getJSONObject(i)));
            }
        } catch (JSONException ignored) {
        }
    }

    private void save() {
        JSONArray list = new JSONArray();
        try {
            for (Tunnel t : tunnels) list.put(toJson(t));
        } catch (JSONException ignored) {
        }
        prefs.edit().putString(KEY_TUNNELS, list.toString()).apply();
    }

    static JSONObject toJson(Tunnel t) throws JSONException {
        return new JSONObject()
                .put("id", t.id).put("name", t.name).put("server", t.serverUrl).put("enabled", t.enabled)
                .put("apps", new JSONArray(t.apps)).put("domains", new JSONArray(t.domains));
    }

    static Tunnel fromJson(JSONObject o) throws JSONException {
        Tunnel t = new Tunnel(o.getString("id"), o.optString("name"));
        t.serverUrl = o.optString("server");
        t.enabled = o.optBoolean("enabled", true);
        JSONArray apps = o.optJSONArray("apps");
        if (apps != null) for (int i = 0; i < apps.length(); i++) t.apps.add(apps.getString(i));
        JSONArray domains = o.optJSONArray("domains");
        if (domains != null) for (int i = 0; i < domains.length(); i++) t.domains.add(domains.getString(i));
        return t;
    }
}
