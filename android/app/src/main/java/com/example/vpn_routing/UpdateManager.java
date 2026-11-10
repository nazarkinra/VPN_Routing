package com.example.vpn_routing;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.Uri;
import android.os.Build;

import androidx.core.content.FileProvider;

import org.json.JSONException;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Locale;

/**
 * Обновление приложения из репозитория на GitHub.
 *
 * Рядом с VPN_routing.apk в корне репозитория лежит version.json (его пишет android/publish_apk.ps1):
 * {"versionCode": 3, "versionName": "1.2", "apk": "VPN_routing.apk", "size": 44400058,
 *  "sha256": "…", "notes": "что нового"}
 * Приложение сравнивает versionCode со своим, скачивает APK, сверяет размер и SHA-256 и запускает
 * системную установку. Подпись нового APK проверяет сам Android: поверх ставится только APK,
 * подписанный тем же ключом.
 */
final class UpdateManager {

    /** Сведения о версии из version.json. */
    static final class Info {
        final int versionCode;
        final String versionName;
        final String apkUrl;
        final long size;
        final String sha256;
        final String notes;

        Info(int versionCode, String versionName, String apkUrl, long size, String sha256, String notes) {
            this.versionCode = versionCode;
            this.versionName = versionName;
            this.apkUrl = apkUrl;
            this.size = size;
            this.sha256 = sha256;
            this.notes = notes;
        }

        boolean isNewer() {
            return versionCode > BuildConfig.VERSION_CODE;
        }
    }

    interface Progress {
        /** Вызывается из фонового потока. total — 0, если размер неизвестен. */
        void onProgress(long done, long total);

        boolean isCancelled();
    }

    private static final String PREFS = "updates";
    private static final String KEY_LAST_CHECK = "last_check";
    private static final String KEY_SKIPPED = "skipped_version";
    /** Автоматическая проверка — не чаще раза в 12 часов. */
    private static final long AUTO_INTERVAL_MS = 12 * 3600_000L;
    private static final int MAX_MANIFEST = 64 * 1024;

    private UpdateManager() {
    }

    private static SharedPreferences prefs(Context c) {
        return c.getApplicationContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    /** Пора ли автоматической проверке (при запуске приложения). */
    static boolean isAutoCheckDue(Context c) {
        return System.currentTimeMillis() - prefs(c).getLong(KEY_LAST_CHECK, 0) > AUTO_INTERVAL_MS;
    }

    /** Пользователь нажал «Позже» для этой версии: при автопроверке её больше не предлагать. */
    static void skip(Context c, int versionCode) {
        prefs(c).edit().putInt(KEY_SKIPPED, versionCode).apply();
    }

    static boolean isSkipped(Context c, Info info) {
        return prefs(c).getInt(KEY_SKIPPED, 0) >= info.versionCode;
    }

    /** Читает version.json. Вызывать не в UI-потоке. */
    static Info fetch(Context c) throws IOException {
        HttpURLConnection conn = open(BuildConfig.UPDATE_MANIFEST_URL + "?t=" + System.currentTimeMillis());
        try {
            int code = conn.getResponseCode();
            if (code == 404) throw new IOException("файл версии не найден (репозиторий закрыт или ещё не опубликован)");
            if (code / 100 != 2) throw new IOException("сервер ответил " + code);
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            try (InputStream in = conn.getInputStream()) {
                byte[] buf = new byte[8192];
                int n;
                while ((n = in.read(buf)) > 0) {
                    out.write(buf, 0, n);
                    if (out.size() > MAX_MANIFEST) throw new IOException("файл версии слишком большой");
                }
            }
            prefs(c).edit().putLong(KEY_LAST_CHECK, System.currentTimeMillis()).apply();
            return parse(new String(out.toByteArray(), StandardCharsets.UTF_8), BuildConfig.UPDATE_APK_BASE);
        } finally {
            conn.disconnect();
        }
    }

    /** Разбор version.json; apk — имя файла относительно apkBase или полный адрес https://. */
    static Info parse(String json, String apkBase) throws IOException {
        try {
            JSONObject o = new JSONObject(json);
            String apk = o.optString("apk", "VPN_routing.apk");
            String url = apk.startsWith("https://") || apk.startsWith("http://") ? apk : apkBase + apk;
            String sha = o.optString("sha256", "").toLowerCase(Locale.ROOT);
            if (!sha.matches("[0-9a-f]{64}")) throw new IOException("в файле версии нет контрольной суммы");
            return new Info(o.getInt("versionCode"), o.optString("versionName", "?"), url,
                    o.optLong("size", 0), sha, o.optString("notes", "").trim());
        } catch (JSONException e) {
            throw new IOException("файл версии повреждён");
        }
    }

    /** Скачивает APK в кэш приложения и проверяет его. Вызывать не в UI-потоке. */
    static File download(Context c, Info info, Progress progress) throws IOException {
        File dir = new File(c.getCacheDir(), "updates");
        if (!dir.isDirectory() && !dir.mkdirs()) throw new IOException("нет места для загрузки");
        for (File old : dir.listFiles() == null ? new File[0] : dir.listFiles()) {
            //noinspection ResultOfMethodCallIgnored
            old.delete();
        }
        File file = new File(dir, "VPN_routing-" + info.versionName + ".apk");

        MessageDigest sha;
        try {
            sha = MessageDigest.getInstance("SHA-256");
        } catch (NoSuchAlgorithmException e) {
            throw new IOException(e);
        }
        HttpURLConnection conn = open(info.apkUrl);
        try {
            int code = conn.getResponseCode();
            if (code / 100 != 2) throw new IOException("сервер ответил " + code);
            long total = info.size > 0 ? info.size : Math.max(0, conn.getContentLength());
            long done = 0;
            try (InputStream in = conn.getInputStream(); OutputStream out = new FileOutputStream(file)) {
                byte[] buf = new byte[64 * 1024];
                int n;
                while ((n = in.read(buf)) > 0) {
                    if (progress.isCancelled()) throw new IOException("загрузка отменена");
                    out.write(buf, 0, n);
                    sha.update(buf, 0, n);
                    done += n;
                    progress.onProgress(done, total);
                }
            }
            if (info.size > 0 && done != info.size) {
                throw new IOException("файл скачался не полностью (" + done + " из " + info.size + " байт)");
            }
        } catch (IOException e) {
            //noinspection ResultOfMethodCallIgnored
            file.delete();
            throw e;
        } finally {
            conn.disconnect();
        }
        String actual = hex(sha.digest());
        if (!actual.equals(info.sha256)) {
            //noinspection ResultOfMethodCallIgnored
            file.delete();
            throw new IOException("контрольная сумма не совпала: файл повреждён или подменён");
        }
        return file;
    }

    /** Может ли приложение запустить установку (Android 8+: разрешение «Установка неизвестных приложений»). */
    static boolean canInstall(Context c) {
        return Build.VERSION.SDK_INT < Build.VERSION_CODES.O || c.getPackageManager().canRequestPackageInstalls();
    }

    /** Экран настроек, где разрешается установка из этого приложения. */
    static Intent installPermissionIntent(Context c) {
        return new Intent(android.provider.Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                Uri.parse("package:" + c.getPackageName()));
    }

    /** Системный установщик для скачанного APK. */
    static Intent installIntent(Context c, File apk) {
        Uri uri = FileProvider.getUriForFile(c, c.getPackageName() + ".files", apk);
        return new Intent(Intent.ACTION_VIEW)
                .setDataAndType(uri, "application/vnd.android.package-archive")
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_ACTIVITY_NEW_TASK);
    }

    private static HttpURLConnection open(String url) throws IOException {
        HttpURLConnection conn = (HttpURLConnection) new URL(url).openConnection();
        conn.setConnectTimeout(15000);
        conn.setReadTimeout(30000);
        conn.setInstanceFollowRedirects(true); // github.com/.../raw → media.githubusercontent.com (Git LFS)
        conn.setRequestProperty("User-Agent", "VPNrouting/" + BuildConfig.VERSION_NAME);
        conn.setRequestProperty("Cache-Control", "no-cache");
        return conn;
    }

    static String hex(byte[] bytes) {
        StringBuilder sb = new StringBuilder();
        for (byte b : bytes) sb.append(String.format(Locale.ROOT, "%02x", b));
        return sb.toString();
    }
}
