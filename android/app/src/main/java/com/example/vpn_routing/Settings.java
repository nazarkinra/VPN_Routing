package com.example.vpn_routing;

import android.content.Context;
import android.content.SharedPreferences;

import org.json.JSONArray;
import org.json.JSONException;

import java.util.ArrayList;
import java.util.List;

/** Хранение ключа и исключений (аналог /etc/xray/vless.txt и /etc/sing-box/local-exceptions.list). */
public final class Settings {

    private static final String PREFS = "routing";
    private static final String KEY_VLESS = "vless_url";
    private static final String KEY_EXCEPTIONS = "exceptions";

    private final SharedPreferences prefs;

    public Settings(Context context) {
        prefs = context.getApplicationContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    public String getVlessUrl() {
        return prefs.getString(KEY_VLESS, "");
    }

    public void setVlessUrl(String url) {
        prefs.edit().putString(KEY_VLESS, url.trim()).apply();
    }

    /** Сохранённый ключ в разобранном виде или null, если ключа нет или он испорчен. */
    public ProxyLink getLink() {
        String url = getVlessUrl();
        if (url.isEmpty()) return null;
        try {
            return ProxyLink.parse(url);
        } catch (IllegalArgumentException e) {
            return null;
        }
    }

    public List<String> getExceptions() {
        List<String> result = new ArrayList<>();
        try {
            JSONArray array = new JSONArray(prefs.getString(KEY_EXCEPTIONS, "[]"));
            for (int i = 0; i < array.length(); i++) result.add(array.getString(i));
        } catch (JSONException ignored) {
        }
        return result;
    }

    private void saveExceptions(List<String> items) {
        prefs.edit().putString(KEY_EXCEPTIONS, new JSONArray(items).toString()).apply();
    }

    /**
     * Добавляет исключение. Возвращает нормализованное значение или null, если адрес уже идёт напрямую
     * (тот же домен или его родитель есть во встроенных правилах или среди своих исключений).
     * Добавленный домен заменяет свои же поддомены: при example.com запись sub.example.com не нужна.
     */
    public String addException(String raw) throws IllegalArgumentException {
        String value = Exceptions.normalize(raw);
        List<String> items = getExceptions();
        if (items.contains(value)) return null;
        if (!Exceptions.isCidr(value)) {
            if (isBuiltinActive(value) || RoutingRules.coveredBy(RoutingRules.canonical(value),
                    domainsOf(items))) {
                return null;
            }
            String parent = RoutingRules.canonical(value);
            List<String> kept = new ArrayList<>();
            for (String item : items) {
                boolean child = !Exceptions.isCidr(item) && RoutingRules.canonical(item).endsWith("." + parent);
                if (!child) kept.add(item);
            }
            items = kept;
        }
        items.add(value);
        saveExceptions(items);
        return value;
    }

    /**
     * Убирает из своих исключений домены, которые уже покрыты встроенными правилами или другими
     * исключениями (например, ya.ru при встроенной зоне .ru). Возвращает число удалённых записей.
     */
    public int compactExceptions() {
        List<String> items = getExceptions();
        List<String> domains = RoutingRules.compact(domainsOf(items));
        List<String> kept = new ArrayList<>();
        for (String item : items) {
            if (Exceptions.isCidr(item)) {
                if (!kept.contains(item)) kept.add(item);
                continue;
            }
            String c = RoutingRules.canonical(item);
            boolean keep = domains.contains(c) && !isBuiltinActive(item);
            if (keep) {
                domains.remove(c); // повтор того же домена в другом написании (.рф и xn--p1ai) не сохраняем
                kept.add(item);
            }
        }
        int removed = items.size() - kept.size();
        if (removed > 0) saveExceptions(kept);
        return removed;
    }

    private static List<String> domainsOf(List<String> items) {
        List<String> result = new ArrayList<>();
        for (String item : items) if (!Exceptions.isCidr(item)) result.add(item);
        return result;
    }

    public boolean removeException(String value) {
        List<String> items = getExceptions();
        boolean removed = items.remove(value);
        if (removed) saveExceptions(items);
        return removed;
    }

    // ---------- Встроенные правила, отключённые пользователем ----------

    private static final String KEY_DISABLED_BUILTINS = "disabled_builtins";

    /** Встроенные домены (из RoutingRules), которые пользователь удалил со вкладки «Напрямую». */
    public List<String> getDisabledBuiltins() {
        List<String> result = new ArrayList<>();
        try {
            JSONArray array = new JSONArray(prefs.getString(KEY_DISABLED_BUILTINS, "[]"));
            for (int i = 0; i < array.length(); i++) result.add(array.getString(i));
        } catch (JSONException ignored) {
        }
        return result;
    }

    public void disableBuiltin(String domain) {
        List<String> items = getDisabledBuiltins();
        String c = RoutingRules.canonical(domain);
        if (!items.contains(c)) items.add(c);
        prefs.edit().putString(KEY_DISABLED_BUILTINS, new JSONArray(items).toString()).apply();
    }

    /** Возвращает все встроенные правила. */
    public void restoreBuiltins() {
        prefs.edit().remove(KEY_DISABLED_BUILTINS).apply();
    }

    /** Действующие встроенные домены напрямую (зоны, сайты и сервисы) без отключённых. */
    public List<String> getActiveDirectDomains() {
        return RoutingRules.without(RoutingRules.DIRECT_DOMAINS, getDisabledBuiltins());
    }

    /** Действующие домены Cloudflare Tunnel без отключённых. */
    public List<String> getActiveTunnelDomains() {
        return RoutingRules.without(RoutingRules.DIRECT_TUNNEL_DOMAINS, getDisabledBuiltins());
    }

    /** Адрес уже идёт напрямую по действующим встроенным правилам. */
    public boolean isBuiltinActive(String value) {
        return RoutingRules.coveredBy(RoutingRules.canonical(value), getActiveDirectDomains());
    }

    // ---------- VPN МГТУ (аналог bmstu.auth и bmstu-source.ovpn) ----------

    private static final String KEY_BMSTU_ENABLED = "bmstu_enabled";
    private static final String KEY_BMSTU_USER = "bmstu_user";
    private static final String KEY_BMSTU_PASS = "bmstu_pass";
    private static final String KEY_BMSTU_OVPN = "bmstu_ovpn";
    private static final String KEY_BMSTU_OVPN_PREV = "bmstu_ovpn_prev";

    /** VPN МГТУ включён кнопкой на вкладке МГТУ (независимо от основного VPN). */
    public boolean isBmstuEnabled() {
        return prefs.getBoolean(KEY_BMSTU_ENABLED, false);
    }

    public void setBmstuEnabled(boolean enabled) {
        prefs.edit().putBoolean(KEY_BMSTU_ENABLED, enabled).apply();
    }

    private static final String KEY_MAIN_ON = "main_on";

    /** Основной VPN был включён (для перезапуска службы системой; VPN МГТУ — isBmstuEnabled). */
    public boolean wasMainOn() {
        return prefs.getBoolean(KEY_MAIN_ON, true);
    }

    public void setMainOn(boolean on) {
        prefs.edit().putBoolean(KEY_MAIN_ON, on).apply();
    }

    public String getBmstuUser() {
        return prefs.getString(KEY_BMSTU_USER, "");
    }

    public String getBmstuPassword() {
        return prefs.getString(KEY_BMSTU_PASS, "");
    }

    public void setBmstuCredentials(String user, String password) {
        prefs.edit().putString(KEY_BMSTU_USER, user).putString(KEY_BMSTU_PASS, password).apply();
    }

    public boolean hasBmstuCredentials() {
        return !getBmstuUser().isEmpty() && !getBmstuPassword().isEmpty();
    }

    /** Загруженный пользователем .ovpn или null, если файл ещё не импортирован. */
    public String getBmstuCustomOvpn() {
        return prefs.getString(KEY_BMSTU_OVPN, null);
    }

    public boolean hasBmstuPreviousOvpn() {
        return prefs.contains(KEY_BMSTU_OVPN_PREV);
    }

    /** Импортирован ли файл профиля МГТУ. */
    public boolean hasBmstuOvpn() {
        return getBmstuCustomOvpn() != null;
    }

    /**
     * Действующий .ovpn или null. Встроенного файла нет: профиль университета не публикуется
     * вместе с приложением, его импортируют на вкладке МГТУ.
     */
    public String getBmstuOvpn(Context context) {
        return getBmstuCustomOvpn();
    }

    /** Устанавливает новый .ovpn, прежний сохраняется для отката. */
    public void installBmstuOvpn(Context context, String text) {
        String current = getBmstuOvpn(context);
        SharedPreferences.Editor e = prefs.edit().putString(KEY_BMSTU_OVPN, text);
        if (current != null && !current.equals(text)) e.putString(KEY_BMSTU_OVPN_PREV, current);
        e.apply();
    }

    /** Возвращает предыдущий .ovpn. */
    public boolean restoreBmstuOvpn() {
        String previous = prefs.getString(KEY_BMSTU_OVPN_PREV, null);
        if (previous == null) return false;
        String current = getBmstuCustomOvpn();
        SharedPreferences.Editor e = prefs.edit().putString(KEY_BMSTU_OVPN, previous);
        if (current != null) e.putString(KEY_BMSTU_OVPN_PREV, current);
        else e.remove(KEY_BMSTU_OVPN_PREV);
        e.apply();
        return true;
    }
}
