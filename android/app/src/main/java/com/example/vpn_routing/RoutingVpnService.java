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

    public static final String ACTION_START = "com.example.vpn_routing.START";
    public static final String ACTION_STOP = "com.example.vpn_routing.STOP";
    /** Пересобрать конфиг (после изменения исключений), туннель не закрывается. */
    public static final String ACTION_RELOAD = "com.example.vpn_routing.RELOAD";
    /** Применить новый ключ: при ошибке вернуть прежний (как update-vpn с восстановлением снимка). */
    public static final String ACTION_APPLY_KEY = "com.example.vpn_routing.APPLY_KEY";
    public static final String EXTRA_URL = "url";
    /** Изменились настройки VPN МГТУ (включён, логин, пароль): обновить маршруты и переподключить туннель. */
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

    private final ExecutorService worker = Executors.newSingleThreadExecutor();
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
    /** В действующем конфиге Xray есть маршрут bmstu.ru в туннель МГТУ. */
    private boolean appliedBmstuRoute;
    /** Сервер запущенного ядра — для диагностики, если он не отвечает. */
    private volatile ProxyLink appliedLink;
    private volatile String appliedServerIp;
    private volatile String appliedResolveMethod;

    // ---------- Состояние для интерфейса ----------

    public static State getState() {
        return state;
    }

    public static String getMessage() {
        return message;
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
        if (newState == State.CONNECTED && state != State.CONNECTED) connectedSince = System.currentTimeMillis();
        if (newState == State.STOPPED || newState == State.ERROR) {
            connectedSince = 0;
            lastDelay = 0;
        }
        boolean changed = state != newState;
        state = newState;
        message = newMessage == null ? "" : newMessage;
        Log.i(TAG, newState + ": " + message);
        if (changed) VpnTileService.refresh(this);
        mainHandler.post(() -> {
            for (Listener l : listeners) l.onStateChanged(state, message);
            if (state == State.CONNECTING || state == State.CONNECTED) updateNotification();
        });
    }

    public static void send(Context context, String action) {
        Intent intent = new Intent(context, RoutingVpnService.class).setAction(action);
        if (ACTION_START.equals(action) || ACTION_APPLY_KEY.equals(action)) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(intent);
            else context.startService(intent);
        } else if (instance != null) {
            context.startService(intent);
        }
    }

    /** Передать службе новый .ovpn МГТУ. Возвращает false, если VPN не запущен. */
    public static boolean applyBmstuOvpn(Context context, String text) {
        if (instance == null) return false;
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
        if (controller == null || !controller.getIsRunning()) throw new IllegalStateException("VPN не подключён.");
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
        // null — перезапуск системой, SERVICE_INTERFACE — запуск «Постоянной VPN» (always-on)
        if (action == null || SERVICE_INTERFACE.equals(action)) action = ACTION_START;

        if (ACTION_STOP.equals(action)) {
            worker.execute(this::stopEverything);
            return START_NOT_STICKY;
        }
        startForeground(NOTIFICATION_ID, buildNotification());
        if (ACTION_APPLY_KEY.equals(action)) {
            String url = intent.getStringExtra(EXTRA_URL);
            worker.execute(() -> applyKeyInternal(url));
        } else if (ACTION_RELOAD.equals(action)) {
            worker.execute(() -> {
                if (core != null) restartCore("Правила обновлены");
            });
        } else if (ACTION_BMSTU_SYNC.equals(action)) {
            worker.execute(() -> {
                if (core == null) return;
                // Маршрут bmstu.ru добавляется в Xray или убирается из него только при включении и выключении
                if (appliedBmstuRoute != settings.isBmstuEnabled()) restartCore("Маршруты МГТУ обновлены");
                bmstu.restart();
            });
        } else if (ACTION_BMSTU_OVPN.equals(action)) {
            String text = intent.getStringExtra(EXTRA_OVPN);
            if (text != null) bmstu.applyOvpn(text);
        } else {
            worker.execute(this::startInternal);
        }
        return START_STICKY;
    }

    @Override
    public void onRevoke() {
        // Пользователь включил другой VPN или отозвал разрешение
        worker.execute(this::stopEverything);
    }

    @Override
    public void onDestroy() {
        worker.execute(this::stopEverything);
        worker.shutdown();
        bmstu.shutdown();
        if (instance == this) {
            instance = null;
            bmstuvpn.Bmstuvpn.setProtector(null);
        }
        super.onDestroy();
    }

    // ---------- Работа в фоновом потоке ----------

    private void startInternal() {
        if (core != null && core.getIsRunning()) {
            setState(State.CONNECTED, message);
            return;
        }
        setState(State.CONNECTING, "Подключение…");
        watchNetwork();
        try {
            startCore(settings.getVlessUrl());
            setState(State.CONNECTED, "Подключено");
            checkConnection();
            bmstu.sync();
        } catch (Exception e) {
            Log.e(TAG, "start failed", e);
            stopEverything();
            setState(State.ERROR, errorText(e));
        }
    }

    private void applyKeyInternal(String url) {
        String previous = settings.getVlessUrl();
        boolean wasRunning = core != null;
        setState(State.CONNECTING, "Применение ключа…");
        watchNetwork();
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
            if (wasRunning && !previous.isEmpty()) {
                try {
                    startCore(previous);
                    setState(State.CONNECTED, error + " Работает прежний ключ.");
                    return;
                } catch (Exception e2) {
                    Log.e(TAG, "rollback failed", e2);
                }
            }
            stopEverything();
            setState(State.ERROR, error);
        }
    }

    private void restartCore(String okMessage) {
        try {
            startCore(settings.getVlessUrl());
            setState(State.CONNECTED, okMessage);
        } catch (Exception e) {
            Log.e(TAG, "restart failed", e);
            stopEverything();
            setState(State.ERROR, errorText(e));
        }
    }

    /** Разбор ключа, разрешение имени сервера, сборка конфига и (пере)запуск ядра. */
    private void startCore(String url) throws Exception {
        ProxyLink link = ProxyLink.parse(url);
        ServerResolver.Result resolved = ServerResolver.resolve(link.server, physicalNetwork());
        String serverIp = resolved.ip;
        Log.i(TAG, "server " + link.server + " -> " + serverIp + " (" + resolved.method + ")");
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
        appliedBmstuRoute = route != null;
        appliedLink = link;
        appliedServerIp = serverIp;
        appliedResolveMethod = resolved.method;
        Log.i(TAG, "Xray started: " + link.describe() + " via " + serverIp + ", local DNS " + dns);
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
                .addRoute("0.0.0.0", 0)
                .addAddress(TUN_ADDRESS6, 126)
                // IPv6 тоже в туннель, иначе он уходил бы мимо VPN
                .addRoute("::", 0)
                .addDnsServer(XrayConfig.TUN_DNS);
        try {
            // Соединения самого Xray (к VPS и напрямую) идут мимо туннеля — как sockopt.interface на Pi
            builder.addDisallowedApplication(getPackageName());
        } catch (PackageManager.NameNotFoundException e) {
            throw new IllegalStateException(e);
        }
        Intent open = new Intent(this, MainActivity.class);
        builder.setConfigureIntent(PendingIntent.getActivity(this, 0, open, PendingIntent.FLAG_IMMUTABLE));

        ParcelFileDescriptor pfd = builder.establish();
        if (pfd == null) {
            throw new IllegalStateException("Нет разрешения на VPN. Откройте приложение и подключитесь вручную.");
        }
        return pfd;
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

    private void stopEverything() {
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
        if (state != State.ERROR) setState(State.STOPPED, "Отключено");
        mainHandler.post(() -> {
            stopForeground(true);
            stopSelf();
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
        worker.execute(() -> {
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
        ProxyLink link = settings.getLink();
        String title = state == State.CONNECTED ? "VPN подключён" : "Подключение VPN…";
        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL_ID) : new Notification.Builder(this);
        return b.setSmallIcon(R.drawable.ic_vpn)
                .setContentTitle(title)
                .setContentText(link == null ? message : link.server)
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
