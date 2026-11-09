package com.example.vpn_routing;

import android.content.Context;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

import bmstuvpn.Bmstuvpn;
import bmstuvpn.Proxy;
import bmstuvpn.Tunnel;

/**
 * Туннель OpenVPN МГТУ (аналог службы bmstu-vpn и bmstu-vpn-hook.sh на Pi).
 *
 * Xray отправляет bmstu.ru в локальный SOCKS-прокси. Прокси работает всё время, пока включён VPN:
 * при поднятом туннеле соединения идут через OpenVPN, иначе напрямую — как .ru на Pi,
 * когда туннель опущен. Поэтому подъём и падение туннеля не требуют перезапуска Xray.
 */
final class BmstuManager {

    private static final String TAG = "BmstuVpn";
    /** Сайт для проверки связи через туннель. */
    static final String PROBE_HOST = "bmstu.ru";
    /** После отказа в доступе — повтор не раньше чем через 5 минут, чтобы не заблокировали учётную запись. */
    private static final long RETRY_AUTH_MS = TimeUnit.MINUTES.toMillis(5);
    private static final long RETRY_MIN_MS = TimeUnit.SECONDS.toMillis(30);
    private static final long RETRY_MAX_MS = TimeUnit.MINUTES.toMillis(5);
    /**
     * Сервер МГТУ допускает одну сессию на учётную запись и держит упавшую сессию до своего таймаута.
     * Поэтому после падения туннеля — пауза, а отказ сразу после падения повторяется через 90 с.
     */
    private static final long RECONNECT_AFTER_DOWN_MS = TimeUnit.SECONDS.toMillis(30);
    private static final long RETRY_AFTER_DROP_MS = TimeUnit.SECONDS.toMillis(90);
    private static final long DROP_WINDOW_MS = TimeUnit.MINUTES.toMillis(3);
    /** Время последнего падения туннеля (не по команде пользователя). */
    private long lastDropAt;
    /** Отказов во входе подряд; сбрасывается при успешном подключении. */
    private int authFailures;

    enum State { OFF, CONNECTING, UP, WAITING }

    interface Listener {
        void onBmstuState(State state, String message);
    }

    private static final List<Listener> listeners = new CopyOnWriteArrayList<>();
    private static final Handler mainHandler = new Handler(Looper.getMainLooper());
    private static volatile State state = State.OFF;
    private static volatile String message = "";

    static void addListener(Listener l) {
        listeners.add(l);
        l.onBmstuState(state, message);
    }

    static void removeListener(Listener l) {
        listeners.remove(l);
    }

    static State getState() {
        return state;
    }

    // ---------- Статистика для экрана (как в OpenVPN Connect) ----------

    private static volatile Tunnel current;
    private static volatile long upSince;
    private static volatile Tunnel lastTunnel;

    /** Момент подъёма туннеля, 0 — не поднят. */
    static long getUpSince() {
        return upSince;
    }

    static long getBytesIn() {
        Tunnel t = current;
        return t == null ? 0 : t.bytesIn();
    }

    static long getBytesOut() {
        Tunnel t = current;
        return t == null ? 0 : t.bytesOut();
    }

    static String getLocalIp() {
        Tunnel t = current;
        return t == null ? "" : t.localIP();
    }

    /** Журнал текущего или последнего туннеля. */
    static String getLog() {
        Tunnel t = current != null ? current : lastTunnel;
        return t == null ? "" : t.log();
    }

    private final Context context;
    private final Settings settings;
    private final ScheduledExecutorService exec = Executors.newSingleThreadScheduledExecutor();
    private Proxy proxy;
    private Tunnel tunnel;
    private ScheduledFuture<?> pending;
    private long backoffMs = RETRY_MIN_MS;
    private boolean active;

    BmstuManager(Context context) {
        this.context = context.getApplicationContext();
        this.settings = new Settings(context);
    }

    private static void setState(State newState, String newMessage) {
        state = newState;
        message = newMessage;
        Log.i(TAG, newState + ": " + newMessage);
        mainHandler.post(() -> {
            for (Listener l : listeners) l.onBmstuState(state, message);
        });
    }

    /** Прокси для конфига Xray: порт и случайные логин/пароль. Создаётся один раз на время работы VPN. */
    synchronized Proxy ensureProxy() throws Exception {
        if (proxy == null) proxy = Bmstuvpn.newProxy();
        return proxy;
    }

    /** Привести туннель в соответствие с настройками (включён ли, есть ли логин и пароль). */
    void sync() {
        run(() -> {
            active = true;
            if (!settings.isBmstuEnabled()) {
                stopTunnel();
                setState(State.OFF, "Выключен");
            } else if (tunnel == null && (pending == null || pending.isDone())) {
                connect();
            }
        });
    }

    /** Переподключиться (сменилась сеть, логин или пароль). */
    void restart() {
        run(() -> {
            active = true;
            stopTunnel();
            backoffMs = RETRY_MIN_MS;
            connect();
        });
    }

    /** Установить новый .ovpn. Если туннель с ним не поднимется — вернуть прежний (как в панели Pi). */
    void applyOvpn(String text) {
        run(() -> {
            settings.installBmstuOvpn(context, text);
            if (!active || !settings.isBmstuEnabled() || !settings.hasBmstuCredentials()) {
                setState(state, "Новый файл .ovpn сохранён");
                return;
            }
            stopTunnel();
            String error = tryConnect();
            if (error == null) return;
            settings.restoreBmstuOvpn();
            boolean auth = Bmstuvpn.isAuthError(error);
            long delay = auth ? RETRY_AUTH_MS : RETRY_MIN_MS;
            setState(State.WAITING, "Новый файл .ovpn не подошёл: "
                    + (auth ? lowerFirst(authText(error)) : error)
                    + ". Возвращён прежний, повтор через " + formatDelay(delay) + ".");
            schedule(delay);
        });
    }

    /** Остановить туннель и прокси (VPN выключается). */
    void stopAll() {
        // Прокси закрывается сразу, а не в очереди: иначе при быстром перезапуске VPN
        // запоздавшая задача закрыла бы уже новый прокси, указанный в конфиге Xray
        synchronized (this) {
            if (proxy != null) {
                proxy.close();
                proxy = null;
            }
        }
        run(() -> {
            active = false;
            stopTunnel();
            setState(State.OFF, settings.isBmstuEnabled() ? "VPN отключён" : "Выключен");
        });
    }

    void shutdown() {
        stopAll();
        exec.shutdown();
    }

    /** Задача в поток туннеля. После shutdown задачи молча отбрасываются. */
    private void run(Runnable task) {
        try {
            exec.execute(task);
        } catch (RejectedExecutionException ignored) {
        }
    }

    // ---------- В потоке exec ----------

    private void connect() {
        cancelPending();
        if (!active) return;
        if (!settings.isBmstuEnabled()) {
            setState(State.OFF, "Выключен");
            return;
        }
        if (!settings.hasBmstuOvpn()) {
            setState(State.OFF, "Импортируйте файл профиля .ovpn");
            return;
        }
        if (!settings.hasBmstuCredentials()) {
            setState(State.OFF, "Укажите логин и пароль");
            return;
        }
        String error = tryConnect();
        if (error == null) return;
        boolean auth = Bmstuvpn.isAuthError(error);
        // Первый отказ подряд — скорее всего, сервер ещё держит прежнюю сессию (туннель упал, приложение
        // было выгружено системой, телефон перезагружен): один повтор через 90 с. Дальше — раз в 5 минут,
        // как на Pi, чтобы не заблокировали учётную запись.
        boolean firstAuthFailure = auth && authFailures == 0;
        if (auth) authFailures++;
        boolean afterDrop = lastDropAt > 0 && System.currentTimeMillis() - lastDropAt < DROP_WINDOW_MS;
        long delay = firstAuthFailure ? RETRY_AFTER_DROP_MS : auth ? RETRY_AUTH_MS : backoffMs;
        lastDropAt = 0;
        if (!auth) backoffMs = Math.min(backoffMs * 2, RETRY_MAX_MS);
        String text = firstAuthFailure
                ? (afterDrop ? "Сервер ещё держит прежнюю сессию" : authText(error))
                : auth ? authText(error) : "Не подключается: " + error;
        setState(State.WAITING, text + ". Повтор через " + formatDelay(delay) + ".");
        schedule(delay);
    }

    /** Одна попытка подключения. Возвращает null при успехе или текст ошибки. */
    private String tryConnect() {
        setState(State.CONNECTING, "Подключение…");
        Tunnel t = null;
        try {
            BmstuConfig cfg = BmstuConfig.parse(settings.getBmstuOvpn(context));
            String remoteIp = resolve(cfg.remoteHost);
            String probe = "";
            try {
                probe = resolve(PROBE_HOST) + ":443";
            } catch (Exception e) {
                Log.w(TAG, "probe host", e);
            }
            DownListener listener = new DownListener();
            Tunnel created = Bmstuvpn.newTunnel(listener);
            listener.owner = created;
            t = created;
            created.start(cfg.text, settings.getBmstuUser(), settings.getBmstuPassword(), remoteIp, probe,
                    ensureProxy());
            tunnel = created;
            current = created;
            lastTunnel = created;
            upSince = System.currentTimeMillis();
            backoffMs = RETRY_MIN_MS;
            authFailures = 0;
            setState(State.UP, "Подключён");
            return null;
        } catch (Exception e) {
            if (t != null) {
                lastTunnel = t;
                Log.w(TAG, "tunnel log:\n" + t.log());
                t.stop();
            }
            Log.e(TAG, "connect failed", e);
            String text = e.getMessage();
            return text == null || text.isEmpty() ? e.getClass().getSimpleName() : text;
        }
    }

    /** Сообщение Go-туннеля о падении. Помнит свой туннель: старые туннели не влияют на текущий. */
    private final class DownListener implements bmstuvpn.Listener {
        volatile Tunnel owner;

        @Override
        public void onDown(String reason) {
            Tunnel which = owner;
            run(() -> BmstuManager.this.onDown(which, reason));
        }
    }

    private void onDown(Tunnel which, String reason) {
        if (which == null || which != tunnel) return;
        Log.w(TAG, "tunnel log:\n" + which.log());
        tunnel = null;
        current = null;
        upSince = 0;
        if (!active) return;
        lastDropAt = System.currentTimeMillis();
        setState(State.WAITING, "Туннель упал (" + reason + "). Переподключение через "
                + formatDelay(RECONNECT_AFTER_DOWN_MS) + "…");
        schedule(RECONNECT_AFTER_DOWN_MS);
    }

    private void stopTunnel() {
        cancelPending();
        if (tunnel != null) {
            tunnel.stop();
            tunnel = null;
        }
        current = null;
        upSince = 0;
    }

    private void schedule(long delayMs) {
        cancelPending();
        pending = exec.schedule(this::connect, delayMs, TimeUnit.MILLISECONDS);
    }

    private void cancelPending() {
        if (pending != null) {
            pending.cancel(false);
            pending = null;
        }
    }

    private static String resolve(String host) throws Exception {
        try {
            InetAddress[] all = InetAddress.getAllByName(host);
            for (InetAddress a : all) if (a instanceof Inet4Address) return a.getHostAddress();
            return all[0].getHostAddress();
        } catch (Exception e) {
            throw new IllegalStateException("не удалось разрешить имя " + host);
        }
    }

    /**
     * Отказ сервера (AUTH_FAILED). Если сервер назвал причину (например, «Data channel cipher negotiation
     * failed»), показываем её: дело может быть не в пароле.
     */
    static String authText(String error) {
        int i = error.indexOf("AUTH_FAILED:");
        String reason = i >= 0 ? error.substring(i + "AUTH_FAILED:".length()).trim() : "";
        if (reason.isEmpty()) {
            // Сервер не назвал причину: неверные данные, уже есть подключение с этой учётной записью
            // (например, с Raspberry Pi) или временная блокировка за частые попытки
            return "Сервер отказал во входе. Проверьте логин и пароль и не подключена ли эта учётная "
                    + "запись с другого устройства";
        }
        return "Сервер отказал во входе: " + reason;
    }

    private static String lowerFirst(String s) {
        return s.isEmpty() ? s : Character.toLowerCase(s.charAt(0)) + s.substring(1);
    }

    private static String formatDelay(long ms) {
        long sec = ms / 1000;
        return sec < 60 ? sec + " с" : (sec / 60) + " мин";
    }
}
