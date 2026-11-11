package com.example.vpn_routing;

import android.net.Network;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.URL;
import java.net.URLEncoder;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.regex.Pattern;

/**
 * IP сервера VPN. Порядок, работающий и в корпоративных сетях, где обычный DNS по UDP 53 на внешние
 * серверы закрыт (МГТУ):
 *  1. адрес в ключе уже IP — берётся как есть, без DNS;
 *  2. системный резолвер (DNS, выданный сетью), не дольше {@link #SYSTEM_TIMEOUT_MS};
 *  3. DoH по IP (HTTPS 443): Cloudflare, затем Google — им не нужен DNS для своего имени;
 *  4. DNS по TCP 53 (Google, Яндекс) — если сеть подменяет HTTPS-сертификаты и DoH не проходит.
 * Запросы идут через реальную сеть (Wi-Fi или мобильную), мимо туннеля.
 */
final class ServerResolver {

    private static final String TAG = "ServerResolver";
    static final int SYSTEM_TIMEOUT_MS = 4000;
    private static final int DOH_TIMEOUT_MS = 5000;
    static final String[] DOH_URLS = {
            "https://1.1.1.1/dns-query?type=A&name=",
            "https://8.8.8.8/resolve?type=A&name=",
    };

    static final String[] TCP_DNS = {"8.8.8.8", "77.88.8.8"};

    private static final Pattern IPV4 = Pattern.compile("^\\d{1,3}(\\.\\d{1,3}){3}$");
    private static final ExecutorService lookups = Executors.newCachedThreadPool();

    /** Результат: адрес и чем он получен (для журнала и сообщений об ошибке). */
    static final class Result {
        final String ip;
        final String method;

        Result(String ip, String method) {
            this.ip = ip;
            this.method = method;
        }
    }

    private ServerResolver() {
    }

    static boolean isIpLiteral(String host) {
        return IPV4.matcher(host).matches() || host.contains(":");
    }

    /**
     * @param network сеть для запросов (null — сеть по умолчанию для приложения; приложение
     *                исключено из VPN, так что это тоже физическая сеть)
     */
    static Result resolve(String host, Network network) throws Exception {
        String h = host.startsWith("[") && host.endsWith("]") ? host.substring(1, host.length() - 1) : host;
        if (isIpLiteral(h)) return new Result(h, "IP из ключа");

        StringBuilder errors = new StringBuilder();
        try {
            String ip = system(h, network);
            if (ip != null) return new Result(ip, "DNS сети");
            errors.append("DNS сети: нет адреса");
        } catch (Exception e) {
            errors.append("DNS сети: ").append(shortError(e));
        }
        for (String base : DOH_URLS) {
            String name = new URL(base).getHost();
            try {
                String ip = doh(base + URLEncoder.encode(h, "UTF-8"), network);
                if (ip != null) return new Result(ip, "DoH " + name);
                errors.append("; DoH ").append(name).append(": нет адреса");
            } catch (Exception e) {
                errors.append("; DoH ").append(name).append(": ").append(shortError(e));
            }
        }
        // DNS по TCP: не зависит от сертификатов (сеть с проверкой HTTPS ломает DoH)
        for (String server : TCP_DNS) {
            try {
                String ip = tcpDns(h, server, network);
                if (ip != null) return new Result(ip, "DNS по TCP " + server);
                errors.append("; TCP ").append(server).append(": нет адреса");
            } catch (Exception e) {
                errors.append("; TCP ").append(server).append(": ").append(shortError(e));
            }
        }
        Log.w(TAG, host + ": " + errors);
        throw new IllegalStateException("Не удалось разрешить имя сервера '" + host + "' (" + errors
                + "). Проверьте подключение к интернету.");
    }

    private static String system(String host, Network network) throws Exception {
        Future<InetAddress[]> f = lookups.submit(() ->
                network != null ? network.getAllByName(host) : InetAddress.getAllByName(host));
        InetAddress[] all;
        try {
            all = f.get(SYSTEM_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        } catch (java.util.concurrent.TimeoutException e) {
            f.cancel(true);
            throw new IllegalStateException("нет ответа за " + SYSTEM_TIMEOUT_MS / 1000 + " с");
        } catch (java.util.concurrent.ExecutionException e) {
            throw e.getCause() instanceof Exception ? (Exception) e.getCause() : e;
        }
        for (InetAddress a : all) if (a instanceof Inet4Address) return a.getHostAddress();
        return all.length > 0 ? all[0].getHostAddress() : null;
    }

    private static String doh(String url, Network network) throws Exception {
        URL u = new URL(url);
        HttpURLConnection c = (HttpURLConnection) (network != null ? network.openConnection(u) : u.openConnection());
        try {
            c.setConnectTimeout(DOH_TIMEOUT_MS);
            c.setReadTimeout(DOH_TIMEOUT_MS);
            c.setRequestProperty("Accept", "application/dns-json");
            if (c.getResponseCode() != 200) throw new IllegalStateException("HTTP " + c.getResponseCode());
            try (InputStream in = c.getInputStream()) {
                ByteArrayOutputStream out = new ByteArrayOutputStream();
                byte[] buf = new byte[4096];
                int n;
                while ((n = in.read(buf)) > 0 && out.size() < 65536) out.write(buf, 0, n);
                return parseDohJson(out.toString("UTF-8"));
            }
        } finally {
            c.disconnect();
        }
    }

    private static String tcpDns(String host, String server, Network network) throws Exception {
        try (java.net.Socket s = new java.net.Socket()) {
            if (network != null) network.bindSocket(s);
            s.connect(new java.net.InetSocketAddress(server, 53), DOH_TIMEOUT_MS);
            s.setSoTimeout(DOH_TIMEOUT_MS);
            byte[] q = dnsQuery(host, 0x5a5a);
            java.io.OutputStream out = s.getOutputStream();
            out.write(new byte[]{(byte) (q.length >> 8), (byte) q.length});
            out.write(q);
            out.flush();
            java.io.DataInputStream in = new java.io.DataInputStream(s.getInputStream());
            byte[] resp = new byte[in.readUnsignedShort()];
            in.readFully(resp);
            return parseDnsResponse(resp);
        }
    }

    /** DNS-запрос записи A (RFC 1035). */
    static byte[] dnsQuery(String host, int id) {
        ByteArrayOutputStream b = new ByteArrayOutputStream();
        b.write(id >> 8);
        b.write(id);
        b.write(0x01); // RD: рекурсивный запрос
        b.write(0x00);
        b.write(0);
        b.write(1);    // QDCOUNT = 1
        for (int i = 0; i < 6; i++) b.write(0);
        for (String label : host.split("\\.")) {
            if (label.isEmpty()) continue;
            byte[] bytes = label.getBytes(java.nio.charset.StandardCharsets.US_ASCII);
            b.write(bytes.length);
            b.write(bytes, 0, bytes.length);
        }
        b.write(0);
        b.write(0);
        b.write(1);    // QTYPE A
        b.write(0);
        b.write(1);    // QCLASS IN
        return b.toByteArray();
    }

    /** Первый IPv4 из ответа DNS; null, если записей A нет. */
    static String parseDnsResponse(byte[] r) {
        if (r.length < 12 || (r[3] & 0x0f) != 0) return null; // RCODE != 0
        int qd = u16(r, 4);
        int an = u16(r, 6);
        int p = 12;
        for (int i = 0; i < qd; i++) p = skipName(r, p) + 4;
        for (int i = 0; i < an && p + 10 <= r.length; i++) {
            p = skipName(r, p);
            int type = u16(r, p);
            int len = u16(r, p + 8);
            p += 10;
            if (type == 1 && len == 4 && p + 4 <= r.length) {
                return (r[p] & 0xff) + "." + (r[p + 1] & 0xff) + "." + (r[p + 2] & 0xff) + "." + (r[p + 3] & 0xff);
            }
            p += len;
        }
        return null;
    }

    private static int skipName(byte[] r, int p) {
        while (p < r.length) {
            int len = r[p] & 0xff;
            if (len == 0) return p + 1;
            if ((len & 0xc0) == 0xc0) return p + 2; // ссылка на имя выше
            p += len + 1;
        }
        return p;
    }

    private static int u16(byte[] r, int p) {
        return ((r[p] & 0xff) << 8) | (r[p + 1] & 0xff);
    }

    /** Первый IPv4 из ответа DoH в формате JSON (Cloudflare и Google отвечают одинаково). */
    static String parseDohJson(String body) throws JSONException {
        JSONArray answers = new JSONObject(body).optJSONArray("Answer");
        if (answers == null) return null;
        for (int i = 0; i < answers.length(); i++) {
            JSONObject a = answers.getJSONObject(i);
            String data = a.optString("data");
            if (a.optInt("type") == 1 && IPV4.matcher(data).matches()) return data;
        }
        return null;
    }

    private static String shortError(Throwable e) {
        String m = e.getMessage();
        return m == null || m.isEmpty() ? e.getClass().getSimpleName() : m;
    }
}
