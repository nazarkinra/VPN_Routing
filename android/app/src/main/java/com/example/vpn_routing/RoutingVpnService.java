package com.example.vpn_routing;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.ConnectivityManager;
import android.net.LinkProperties;
import android.net.Network;
import android.net.VpnService;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import libv2ray.CoreCallbackHandler;
import libv2ray.CoreController;
import libv2ray.Libv2ray;

/**
 * VPN-служба: туннель Android (VpnService) + ядро Xray.
 * Аналог связки служб xray и sing-box на Raspberry Pi и команды update-vpn.
 */
public class RoutingVpnService extends VpnService {

    private static final String TAG = "RoutingVpn";

    /** Включить основной VPN (VPN МГТУ, если работает, не трогается). */
    public static final String ACTION_START = "com.example.vpn_routing.START";
    /** Выключить всё: и основной VPN, и VPN МГТУ (уведомление, «Отключить»). */
    public static final String ACTION_STOP = "com.example.vpn_routing.STOP";
    /** Выключить основной VPN (VPN МГТУ, если включён, продолжает работать). */
    public static final String ACTION_STOP_MAIN = "com.example.vpn_routing.STOP_MAIN";
    /** Включить VPN МГТУ (основной VPN не трогается). */
    public static final String ACTION_START_CORP = "com.example.vpn_routing.START_CORP";
    /** Выключить VPN МГТУ (основной VPN не трогается). */
    public static final String ACTION_STOP_CORP = "com.example.vpn_routing.STOP_CORP";
    /** Пересобрать конфиг (после изменения исключений), туннель не закрывается. */
    public static final String ACTION_RELOAD = "com.example.vpn_routing.RELOAD";
    /** Изменилась маршрутизация по приложениям: пересоздать туннель с новым списком приложений. */
    public static final String ACTION_RELOAD_APPS = "com.example.vpn_routing.RELOAD_APPS";
    /** Применить новый ключ: при ошибке вернуть прежний (как update-vpn с восстановлением снимка). */
    public static final String ACTION_APPLY_KEY = "com.example.vpn_routing.APPLY_KEY";
    public static final String EXTRA_URL = "url";
    /** Изменились логин, пароль или файл МГТУ: переподключить туннель, если он включён. */
    public static final String ACTION_BMSTU_SYNC = "com.example.vpn_routing.BMSTU_SYNC";
    /** Новый .ovpn МГТУ: проверить подключением, при ошибке вернуть прежний. */
    public static final String ACTION_BMSTU_OVPN = "com.example.vpn_routing.BMSTU_OVPN";
    public static final String EXTRA_OVPN = "ovpn";

    /** 1400, а не 1500: меньше риск фрагментации в сетях с туннелями и корпоративными шлюзами. */
    public static final int MTU = 1400;
    private static final String TUN_ADDRESS = "172.19.0.1";
    private static final String TUN_ADDRESS6 = "fdfe:dcba:9876::1";
    private static final String DELAY_TEST_URL = "https://www.gstatic.com/generate_204";

    private static final String CHANNEL_ID = "vpn";
    private static final int NOTIFICATION_ID = 1;

    /** Состояние основного VPN. У VPN МГТУ своё состояние — BmstuManager.getState() и isCorpOn(). */
    public enum State { STOPPED, CONNECTING, CONNECTED, ERROR }

    public interface Listener {
        void onStateChanged(State state, String message);
    }

    private static final List<Listener> listeners = new CopyOnWriteArrayList<>();
    private static final Handler mainHandler = new Handler(Looper.getMainLooper());
    private static volatile State state = State.STOPPED;
    private static volatile String message = "";
    private static volatile RoutingVpnService instance;
    private static boolean coreEnvReady;
    /**
     * Включённые части. Служба работает, пока включена хотя бы одна; обе живут в одном ядре Xray:
     * основной — выход на сервер, МГТУ — маршрут bmstu.ru в туннель университета. Флаг МГТУ хранится
     * в настройках (isBmstuEnabled), основного — тоже (для перезапуска службы системой).
     */
    private static volatile boolean mainOn;
    private static volatile boolean corpOn;

    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    /** Служба уничтожена (onDestroy): оставшиеся задачи только освобождают её ресурсы. */
    private volatile boolean destroyed;
    /** Последняя команда службе (onStartCommand, главный поток): остановка не отменяет более новую. */
    private volatile int lastStartId;
    private ParcelFileDescriptor tun;
    private CoreController core;
    private Settings settings;
    private ConnectivityManager connectivity;
    private ConnectivityManager.NetworkCallback networkCallback;
    /** DNS физической сети (Wi-Fi роутер или оператор) — аналог шлюза, который определял update-vpn. */
    private volatile String networkDns;
    private String appliedDns;
    private volatile Network lastNetwork;
    private BmstuManager bmstu;
    /** Сервер запущенного ядра — для диагностики, если он не отвечает (null — основной выключен). */
    private volatile ProxyLink appliedLink;
    private volatile String appliedServerIp;
    private volatile String appliedResolveMethod;

    // ---------- Состояние для интерфейса ----------

    /**
     * Состояние основного VPN. Службы нет — значит, ничего не включено, что бы ни осталось в state
     * (иначе кнопка «отключала» бы несуществующую службу и VPN не включался до перезапуска приложения).
     */
    public static State getState() {
        State s = state;
        if (instance == null && (s == State.CONNECTED || s == State.CONNECTING)) return State.STOPPED;
        return s;
    }

    public static String getMessage() {
        return message;
    }

    /** Основной VPN подключён или подключается. */
    public static boolean isMainActive() {
        State s = getState();
        return s == State.CONNECTED || s == State.CONNECTING;
    }

    /** VPN МГТУ включён (туннель подключён, подключается или ждёт повтора). */
    public static boolean isCorpOn() {
        return corpOn && instance != null;
    }

    public static void addListener(Listener l) {
        listeners.add(l);
        l.onStateChanged(state, message);
    }

    public static void removeListener(Listener l) {
        listeners.remove(l);
    }

    /** Момент подключения (для таймера), 0 — не подключено. */
    public static long getConnectedSince() {
        return connectedSince;
    }

    /** Последняя задержка через VPN в мс, -1 — не отвечает, 0 — не измерялась. */
    public static long getLastDelay() {
        return lastDelay;
    }

    private static volatile long connectedSince;
    private static volatile long lastDelay;

    private void setState(State newState, String newMessage) {
        // Задача уже уничтоженной службы не должна менять общее состояние (его может вести новая служба)
        if (destroyed) return;
        if (newState == State.CONNECTED && state != State.CONNECTED) connectedSince = System.currentTimeMillis();
        if (newState == State.STOPPED || newState == State.ERROR) {
            connectedSince = 0;
            lastDelay = 0;
        }
        boolean changed = state != newState;
        state = newState;
        message = newMessage == null ? "" : newMessage;
        Log.i(TAG, newState + ": " + message);
        notifyListeners(changed);
    }

    /** Сообщить интерфейсу, плитке и уведомлению (в том числе о включении и выключении МГТУ). */
    private void notifyListeners(boolean tile) {
        if (tile) VpnTileService.refresh(this);
        mainHandler.post(() -> {
            for (Listener l : listeners) l.onStateChanged(state, message);
            if (core != null) updateNotification();
        });
    }

    public static void send(Context context, String action) {
        Intent intent = new Intent(context, RoutingVpnService.class).setAction(action);
        if (ACTION_START.equals(action) || ACTION_START_CORP.equals(action) || ACTION_APPLY_KEY.equals(action)) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(intent);
            else context.startService(intent);
        } else if (instance != null) {
            context.startService(intent);
        }
    }

    /** Передать службе новый .ovpn МГТУ. Возвращает false, если VPN МГТУ не включён. */
    public static boolean applyBmstuOvpn(Context context, String text) {
        if (instance == null || !corpOn) return false;
        context.startService(new Intent(context, RoutingVpnService.class)
                .setAction(ACTION_BMSTU_OVPN).putExtra(EXTRA_OVPN, text));
        return true;
    }

    public static void applyKey(Context context, String url) {
        Intent intent = new Intent(context, RoutingVpnService.class)
                .setAction(ACTION_APPLY_KEY).putExtra(EXTRA_URL, url);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(intent);
        else context.startService(intent);
    }

    /** Проверка связи через VPN-сервер, как «адрес через прокси» в check-vpn. Вызывать не в UI-потоке. */
    public static long measureDelay() throws Exception {
        RoutingVpnService service = instance;
        CoreController controller = service == null ? null : service.core;
        if (controller == null || !controller.getIsRunning() || !mainOn) {
            throw new IllegalStateException("VPN не подключён.");
        }
        try {
            lastDelay = controller.measureDelay(DELAY_TEST_URL);
        } catch (Exception e) {
            lastDelay = -1;
            throw e;
        }
        return lastDelay;
    }

    // ---------- Жизненный цикл ----------

    @Override
    public void onCreate() {
        super.onCreate();
        instance = this;
        settings = new Settings(this);
        bmstu = new BmstuManager(this);
        // Каждый сокет ядра выводится из туннеля, иначе соединение к VPN-серверу уйдёт в него же (петля).
        // addDisallowedApplication на части устройств и в эмуляторах этого не гарантирует.
        bmstuvpn.Bmstuvpn.setProtector(fd -> protect((int) fd));
        connectivity = (ConnectivityManager) getSystemService(Context.CONNECTIVITY_SERVICE);
        createChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();
        lastStartId = startId;

        if (ACTION_STOP.equals(action)) {
            run(() -> stopEverything(true));
            return START_NOT_STICKY;
        }
        if (ACTION_STOP_MAIN.equals(action)) {
            run(() -> {
                mainOn = false;
                apply();
            });
            return START_STICKY;
        }
        if (ACTION_STOP_CORP.equals(action)) {
            run(() -> {
                settings.setBmstuEnabled(false);
                apply();
            });
            return START_STICKY;
        }
        startForeground(NOTIFICATION_ID, buildNotification());
        if (action == null || SERVICE_INTERFACE.equals(action)) {
            // null — перезапуск системой: включить части, как были. SERVICE_INTERFACE — «Постоянная VPN»
            // (always-on): основной VPN и МГТУ, если он был включён
            boolean main = action != null || settings.wasMainOn();
            run(() -> {
                mainOn = main;
                apply();
            });
        } else if (ACTION_START_CORP.equals(action)) {
            run(() -> {
                // Всё было выключено: включается только МГТУ
                if (core == null) mainOn = false;
                settings.setBmstuEnabled(true);
                apply();
            });
        } else if (ACTION_APPLY_KEY.equals(action)) {
            String url = intent.getStringExtra(EXTRA_URL);
            run(() -> applyKeyInternal(url));
        } else if (ACTION_RELOAD.equals(action)) {
            run(() -> {
                if (core != null) restartCore("Правила обновлены");
            });
        } else if (ACTION_RELOAD_APPS.equals(action)) {
            run(this::reloadApps);
        } else if (ACTION_BMSTU_SYNC.equals(action)) {
            run(() -> {
                if (core != null && settings.isBmstuEnabled()) bmstu.restart();
            });
        } else if (ACTION_BMSTU_OVPN.equals(action)) {
            String text = intent.getStringExtra(EXTRA_OVPN);
            if (text != null) bmstu.applyOvpn(text);
        } else {
            run(() -> {
                // Всё было выключено: включается только основной (флаг МГТУ мог остаться от прошлого раза)
                if (core == null) settings.setBmstuEnabled(false);
                mainOn = true;
                apply();
            });
        }
        return START_STICKY;
    }

    /**
     * Задача в фоновом потоке службы. Неожиданная ошибка не должна оставить VPN в «Подключение…»
     * навсегда, а задача после onDestroy (сеть сменилась в момент остановки) — уронить приложение.
     */
    private void run(Runnable task) {
        try {
            worker.execute(() -> {
                try {
                    task.run();
                } catch (Throwable t) {
                    Log.e(TAG, "task failed", t);
                    try {
                        fail(new Exception(t), errorText(t));
                    } catch (Throwable t2) {
                        Log.e(TAG, "fail() failed", t2);
                    }
                }
                // Ничего не включено (например, «Обновить правила» сразу после отключения) — служба не нужна
                if (!destroyed && core == null && !mainOn && !settings.isBmstuEnabled()) postStop();
            });
        } catch (java.util.concurrent.RejectedExecutionException e) {
            Log.w(TAG, "service destroyed, task dropped");
        }
    }

    @Override
    public void onRevoke() {
        // Пользователь включил другой VPN или отозвал разрешение
        run(() -> stopEverything(true));
    }

    @Override
    public void onDestroy() {
        destroyed = true;
        run(() -> stopEverything(false));
        worker.shutdown();
        bmstu.shutdown();
        if (instance == this) {
            instance = null;
            bmstuvpn.Bmstuvpn.setProtector(null);
            // Сбросить состояние сразу, не дожидаясь фонового потока (он может быть занят): службы больше нет
            mainOn = false;
            corpOn = false;
            if (state == State.CONNECTED || state == State.CONNECTING) {
                state = State.STOPPED;
                message = "Отключено";
                connectedSince = 0;
                lastDelay = 0;
            }
            VpnTileService.refresh(this);
            mainHandler.post(() -> {
                for (Listener l : listeners) l.onStateChanged(getState(), message);
            });
        }
        super.onDestroy();
    }

    // ---------- Работа в фоновом потоке ----------

    /**
     * Привести ядро в соответствие с включёнными частями: обе выключены — всё останавливается, иначе
     * конфиг Xray пересобирается (выход на сервер — если включён основной, bmstu.ru — если МГТУ).
     */
    private void apply() {
        // Команда пришла, а служба тем временем остановлена: туннель уничтоженной службы не поднимать
        // (следующее нажатие запустит новую службу)
        if (destroyed) return;
        boolean corp = settings.isBmstuEnabled();
        settings.setMainOn(mainOn);
        if (!mainOn && !corp) {
            stopEverything(true);
            return;
        }
        if (mainOn && (!isMainActive() || appliedLink == null)) setState(State.CONNECTING, "Подключение…");
        watchNetwork();
        try {
            startCore(settings.getVlessUrl());
        } catch (Exception e) {
            fail(e, errorText(e));
            return;
        }
        corpOn = corp;
        if (mainOn && state != State.CONNECTED) {
            setState(State.CONNECTED, "Подключено");
            checkConnection();
        } else if (!mainOn && state != State.ERROR) {
            setState(State.STOPPED, "Отключено");
        } else {
            notifyListeners(false);
        }
        bmstu.sync();
    }

    /** Ошибка запуска. Если не поднялся основной VPN, а МГТУ включён, МГТУ продолжает работать один. */
    private void fail(Exception e, String text) {
        Log.e(TAG, "start failed", e);
        boolean main = mainOn;
        if (main && settings.isBmstuEnabled()) {
            mainOn = false;
            settings.setMainOn(false);
            try {
                startCore(null);
                corpOn = true;
                setState(State.ERROR, text);
                bmstu.sync();
                return;
            } catch (Exception e2) {
                Log.e(TAG, "BMSTU-only fallback failed", e2);
            }
        }
        stopEverything(true);
        if (main) setState(State.ERROR, text);
        else bmstu.reportError(text);
    }

    private void applyKeyInternal(String url) {
        String previous = settings.getVlessUrl();
        // Основной VPN выключен: новый сервер просто запоминается
        if (core == null || !mainOn) {
            settings.setVlessUrl(url);
            notifyListeners(false);
            return;
        }
        setState(State.CONNECTING, "Применение ключа…");
        try {
            startCore(url);
            // Ключ сохраняется только после успешного запуска
            settings.setVlessUrl(url);
            setState(State.CONNECTED, "Новый ключ применён");
            checkConnection();
            bmstu.sync();
        } catch (Exception e) {
            Log.e(TAG, "new key failed", e);
            String error = "Ключ не применён: " + errorText(e);
            if (!previous.isEmpty()) {
                try {
                    startCore(previous);
                    setState(State.CONNECTED, error + " Работает прежний ключ.");
                    return;
                } catch (Exception e2) {
                    Log.e(TAG, "rollback failed", e2);
                }
            }
            fail(e, error);
        }
    }

    /**
     * Список приложений задаётся при создании туннеля (VpnService.Builder), поэтому туннель создаётся заново:
     * новый заменяет прежний, ядро перезапускается на нём, затем прежний закрывается.
     */
    private void reloadApps() {
        if (core == null) return;
        ParcelFileDescriptor old = tun;
        try {
            tun = establishTun();
        } catch (Exception e) {
            tun = old;
            fail(e, errorText(e));
            return;
        }
        restartCore("Приложения обновлены");
        if (old != null && old != tun) {
            try {
                old.close();
            } catch (Exception ignored) {
            }
        }
    }

    private void restartCore(String okMessage) {
        try {
            startCore(settings.getVlessUrl());
            if (mainOn) setState(State.CONNECTED, okMessage);
        } catch (Exception e) {
            fail(e, errorText(e));
        }
    }

    /**
     * Разбор ключа, разрешение имени сервера, сборка конфига и (пере)запуск ядра.
     * Сервер url — только если включён основной VPN; маршрут bmstu.ru — если включён VPN МГТУ.
     */
    private void startCore(String url) throws Exception {
        ProxyLink link = null;
        String serverIp = null;
        ServerResolver.Result resolved = null;
        if (mainOn) {
            link = ProxyLink.parse(url);
            resolved = ServerResolver.resolve(link.server, physicalNetwork());
            serverIp = resolved.ip;
            Log.i(TAG, "server " + link.server + " -> " + serverIp + " (" + resolved.method + ")");
        }
        String dns = networkDns != null ? networkDns : detectDns(connectivity.getActiveNetwork());
        XrayConfig.BmstuRoute route = null;
        if (settings.isBmstuEnabled()) {
            bmstuvpn.Proxy proxy = bmstu.ensureProxy();
            route = new XrayConfig.BmstuRoute((int) proxy.port(), proxy.user(), proxy.password());
        }
        String config = XrayConfig.build(link, serverIp, dns, settings.getExceptions(), route,
                settings.getActiveDirectDomains(), settings.getActiveTunnelDomains());

        stopCore();
        if (tun == null) tun = establishTun();

        ensureCoreEnv(this);
        CoreController controller = Libv2ray.newCoreController(new CoreCallback());
        controller.startLoop(config, tun.getFd());
        core = controller;
        appliedDns = dns;
        appliedLink = link;
        appliedServerIp = serverIp;
        appliedResolveMethod = resolved == null ? null : resolved.method;
        Log.i(TAG, "Xray started: " + (link == null ? "без основного VPN" : link.describe() + " via " + serverIp)
                + (route != null ? ", BMSTU" : "") + ", local DNS " + dns);
        ConfigDump.save(this, config);
    }

    /** Физическая сеть (Wi-Fi или мобильная), не наш VPN; null — пусть решает система. */
    private Network physicalNetwork() {
        if (lastNetwork != null) return lastNetwork;
        Network active = connectivity.getActiveNetwork();
        if (active == null) return null;
        android.net.NetworkCapabilities caps = connectivity.getNetworkCapabilities(active);
        return caps != null && !caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN) ? active : null;
    }

    /** Окружение ядра Xray (один раз на процесс): нужно и службе, и замеру задержки серверов. */
    static synchronized void ensureCoreEnv(Context context) {
        if (!coreEnvReady) {
            Libv2ray.initCoreEnv(context.getFilesDir().getAbsolutePath(), "");
            coreEnvReady = true;
        }
    }

    private ParcelFileDescriptor establishTun() throws Exception {
        Builder builder = new Builder()
                .setSession(getString(R.string.app_name))
                .setMtu(MTU)
                .addAddress(TUN_ADDRESS, 30)
                .addAddress(TUN_ADDRESS6, 126)
                .addDnsServer(XrayConfig.TUN_DNS);
        // В туннель — весь интернет (IPv6 тоже, иначе он уходил бы мимо VPN), кроме локальных сетей:
        // Wi-Fi Direct (Mi Share, Quick Share) через туннель не работает, см. TunRoutes
        for (TunRoutes.Route r : TunRoutes.ipv4()) builder.addRoute(r.address, r.prefix);
        for (TunRoutes.Route r : TunRoutes.ipv6()) builder.addRoute(r.address, r.prefix);
        // Сеть самого туннеля (в ней DNS 172.19.0.2) — внутри исключённых 172.16.0.0/12 и fc00::/7
        // (адрес сети без битов хоста: старые Android иначе отвечают «Bad address»)
        builder.addRoute("172.19.0.0", 30);
        builder.addRoute("fdfe:dcba:9876::", 126);
        applyApps(builder);
        Intent open = new Intent(this, MainActivity.class);
        builder.setConfigureIntent(PendingIntent.getActivity(this, 0, open, PendingIntent.FLAG_IMMUTABLE));

        ParcelFileDescriptor pfd = builder.establish();
        if (pfd == null) {
            throw new IllegalStateException("Нет разрешения на VPN. Откройте приложение и подключитесь вручную.");
        }
        return pfd;
    }

    /**
     * Маршрутизация по приложениям. Приложение вне туннеля идёт напрямую в сеть — мимо всех правил,
     * в том числе мимо VPN МГТУ. Само приложение VPN routing в туннель не попадает никогда: соединения Xray
     * (к серверу и напрямую) идут мимо него — как sockopt.interface на Pi.
     */
    private void applyApps(Builder builder) {
        String mode = settings.getAppsMode();
        List<String> selected = settings.getActiveApps();
        int added = 0;
        if (Settings.APPS_INCLUDE.equals(mode) && !selected.isEmpty()) {
            // В туннеле — только выбранные (себя среди разрешённых нет, значит, и мы вне туннеля)
            for (String pkg : selected) {
                if (pkg.equals(getPackageName())) continue;
                try {
                    builder.addAllowedApplication(pkg);
                    added++;
                } catch (PackageManager.NameNotFoundException e) {
                    Log.i(TAG, "app not installed: " + pkg);
                }
            }
            if (added > 0) {
                Log.i(TAG, "apps: only " + added + " selected via VPN");
                return;
            }
            // Ни одно из выбранных не установлено: пустой список разрешённых означал бы «все»
        }
        disallow(builder, getPackageName());
        if (Settings.APPS_EXCLUDE.equals(mode)) {
            for (String pkg : selected) {
                if (!pkg.equals(getPackageName()) && disallow(builder, pkg)) added++;
            }
            Log.i(TAG, "apps: " + added + " selected bypass VPN");
        }
    }

    private static boolean disallow(Builder builder, String pkg) {
        try {
            builder.addDisallowedApplication(pkg);
            return true;
        } catch (PackageManager.NameNotFoundException e) {
            Log.i(TAG, "app not installed: " + pkg);
            return false;
        }
    }

    private void stopCore() {
        CoreController controller = core;
        core = null;
        if (controller != null) {
            try {
                controller.stopLoop();
            } catch (Exception e) {
                Log.w(TAG, "stopLoop", e);
            }
        }
    }

    /**
     * Остановить ядро, туннель МГТУ и службу. resetFlags — обе части выключены (иначе — уничтожение
     * службы системой: флаги остаются, чтобы при перезапуске включить части, как были).
     */
    private void stopEverything(boolean resetFlags) {
        // Общие флаги трогает только живая служба: после onDestroy их могла уже выставить новая
        if (!destroyed) {
            mainOn = false;
            corpOn = false;
            if (resetFlags) {
                settings.setMainOn(false);
                settings.setBmstuEnabled(false);
            }
        }
        appliedLink = null;
        bmstu.stopAll();
        stopCore();
        if (tun != null) {
            try {
                tun.close();
            } catch (Exception ignored) {
            }
            tun = null;
        }
        unwatchNetwork();
        if (destroyed) return;
        if (state != State.ERROR) setState(State.STOPPED, "Отключено");
        else notifyListeners(true);
        postStop();
    }

    /**
     * Остановить службу, только если после этого не пришла новая команда («Отключить» и сразу «Подключить»):
     * иначе stopSelf() уничтожил бы службу вместе с новым подключением. Если новая команда ничего не
     * включила, после неё run() вызовет postStop() снова.
     */
    private void postStop() {
        int stopId = lastStartId;
        mainHandler.post(() -> {
            if (destroyed || stopId != lastStartId) return;
            stopForeground(true);
            stopSelf(stopId);
        });
    }

    /** Проверка связи после подключения: показывает задержку или предупреждение. */
    private void checkConnection() {
        new Thread(() -> {
            try {
                long ms = measureDelay();
                if (state == State.CONNECTED) setState(State.CONNECTED, message + ". Задержка: " + ms + " мс");
            } catch (Exception e) {
                if (state == State.CONNECTED) {
                    setState(State.CONNECTED, message + ". Сервер не отвечает: " + errorText(e)
                            + diagnose());
                }
            }
        }, "delay-check").start();
    }

    /**
     * Где сломалось: (а) чем получен адрес сервера, (б) открывается ли до него TCP-соединение.
     * Для UDP-протоколов (Hysteria2) TCP не проверить — сообщается, что сеть может блокировать UDP.
     */
    private String diagnose() {
        ProxyLink link = appliedLink;
        String ip = appliedServerIp;
        if (link == null || ip == null) return "";
        String where = " Адрес " + ip + " (" + appliedResolveMethod + ")";
        if (link.isHysteria()) {
            Log.i(TAG, "diagnose: hysteria over UDP, " + ip + ":" + link.port);
            return where + ". Hysteria2 работает по UDP: корпоративные сети его часто блокируют,"
                    + " выберите сервер VLESS (TCP).";
        }
        java.net.Socket socket = new java.net.Socket();
        try {
            protect(socket);
            socket.connect(new java.net.InetSocketAddress(ip, link.port), 4000);
            Log.i(TAG, "diagnose: TCP " + ip + ":" + link.port + " ok");
            return where + ", TCP-порт " + link.port + " открыт: соединение есть, но сервер не принимает ключ"
                    + " или не выпускает в интернет.";
        } catch (Exception e) {
            Log.i(TAG, "diagnose: TCP " + ip + ":" + link.port + " failed: " + e);
            return where + ", TCP-порт " + link.port + " недоступен из этой сети.";
        } finally {
            try {
                socket.close();
            } catch (Exception ignored) {
            }
        }
    }

    // ---------- Слежение за сетью ----------

    private void watchNetwork() {
        if (networkCallback != null) return;
        networkCallback = new ConnectivityManager.NetworkCallback() {
            @Override
            public void onAvailable(Network network) {
                onNetwork(network, connectivity.getLinkProperties(network));
            }

            @Override
            public void onLinkPropertiesChanged(Network network, LinkProperties lp) {
                onNetwork(network, lp);
            }
        };
        try {
            // Приложение исключено из VPN, поэтому для него сеть по умолчанию — физическая (Wi-Fi или мобильная)
            connectivity.registerDefaultNetworkCallback(networkCallback);
        } catch (RuntimeException e) {
            Log.w(TAG, "registerDefaultNetworkCallback", e);
            networkCallback = null;
        }
    }

    private void unwatchNetwork() {
        if (networkCallback == null) return;
        try {
            connectivity.unregisterNetworkCallback(networkCallback);
        } catch (RuntimeException ignored) {
        }
        networkCallback = null;
    }

    private void onNetwork(Network network, LinkProperties lp) {
        // На части версий Android обратный вызов сообщает и о самом нашем VPN: это не смена сети
        // (иначе ядро и туннель МГТУ перезапускаются, а VPN становится «подлежащей сетью» сам себе)
        android.net.NetworkCapabilities caps = connectivity.getNetworkCapabilities(network);
        if (caps == null || caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN)) return;
        String dns = detectDns(lp);
        networkDns = dns;
        setUnderlyingNetworks(new Network[]{network});
        Network previous = lastNetwork;
        lastNetwork = network;
        boolean networkChanged = previous != null && !previous.equals(network);
        // Сменилась сеть (дом -> мобильная): российские домены должны резолвиться через DNS новой сети
        run(() -> {
            if (core != null && !equalsNullable(dns, appliedDns)) {
                Log.i(TAG, "network DNS changed: " + appliedDns + " -> " + dns);
                restartCore("Подключено (сменилась сеть)");
            }
            // UDP-соединение OpenVPN было привязано к прежней сети
            if (core != null && networkChanged && settings.isBmstuEnabled()) bmstu.restart();
        });
    }

    private String detectDns(Network network) {
        return network == null ? null : detectDns(connectivity.getLinkProperties(network));
    }

    private static String detectDns(LinkProperties lp) {
        if (lp == null) return null;
        for (InetAddress a : lp.getDnsServers()) {
            if (a instanceof Inet4Address && !a.getHostAddress().equals(XrayConfig.TUN_DNS)) {
                return a.getHostAddress();
            }
        }
        return null;
    }

    private static boolean equalsNullable(String a, String b) {
        return a == null ? b == null : a.equals(b);
    }

    private static String errorText(Throwable e) {
        String text = e.getMessage();
        return text == null || text.isEmpty() ? e.getClass().getSimpleName() : text;
    }

    // ---------- Уведомление ----------

    private void createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(CHANNEL_ID,
                    getString(R.string.notification_channel), NotificationManager.IMPORTANCE_LOW);
            getSystemService(NotificationManager.class).createNotificationChannel(channel);
        }
    }

    private Notification buildNotification() {
        PendingIntent open = PendingIntent.getActivity(this, 0,
                new Intent(this, MainActivity.class), PendingIntent.FLAG_IMMUTABLE);
        PendingIntent stop = PendingIntent.getService(this, 1,
                new Intent(this, RoutingVpnService.class).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE);
        // Основной VPN и VPN МГТУ — каждый своей строкой состояния
        boolean main = mainOn || state == State.CONNECTING;
        String title;
        if (main && corpOn) title = state == State.CONNECTED ? "VPN и VPN МГТУ включены" : "Подключение VPN…";
        else if (main) title = state == State.CONNECTED ? "VPN подключён" : "Подключение VPN…";
        else if (corpOn) title = "VPN МГТУ включён";
        else title = "Подключение…";
        ProxyLink link = main ? settings.getLink() : null;
        String text = link != null ? link.server : corpOn ? "bmstu.ru — через сеть МГТУ" : message;
        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL_ID) : new Notification.Builder(this);
        return b.setSmallIcon(R.drawable.ic_vpn)
                .setContentTitle(title)
                .setContentText(text)
                .setContentIntent(open)
                .setOngoing(true)
                .addAction(new Notification.Action.Builder(null, "Отключить", stop).build())
                .build();
    }

    private void updateNotification() {
        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        nm.notify(NOTIFICATION_ID, buildNotification());
    }

    private static final class CoreCallback implements CoreCallbackHandler {
        @Override
        public long onEmitStatus(long code, String s) {
            Log.i(TAG, "core: " + s);
            return 0;
        }

        @Override
        public long shutdown() {
            return 0;
        }

        @Override
        public long startup() {
            return 0;
        }
    }
}
