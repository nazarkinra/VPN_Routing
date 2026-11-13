package com.example.vpn_routing;

import android.app.Activity;
import android.content.Intent;
import android.net.VpnService;
import android.os.Bundle;
import android.view.View;
import android.widget.TextView;
import android.widget.Toast;

import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.appcompat.app.AlertDialog;
import androidx.appcompat.app.AppCompatActivity;

import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import com.google.android.material.progressindicator.LinearProgressIndicator;

import java.io.File;
import java.io.IOException;
import java.util.concurrent.atomic.AtomicBoolean;
import androidx.fragment.app.Fragment;
import androidx.fragment.app.FragmentManager;
import androidx.fragment.app.FragmentTransaction;

import com.google.android.material.bottomnavigation.BottomNavigationView;

/** Главный экран: три вкладки — VPN, МГТУ и «Напрямую». */
public class MainActivity extends AppCompatActivity {

    private static final String TAG_VPN = "vpn";
    private static final String TAG_TUNNELS = "tunnels";
    private static final String TAG_CORP = "corp";
    private static final String TAG_DIRECT = "direct";
    private static final String STATE_TAB = "tab";
    /** Подключиться сразу (команда из ярлыка, когда нужно системное разрешение на VPN). */
    public static final String ACTION_CONNECT = "com.example.vpn_routing.action.CONNECT_UI";

    private Settings settings;
    private Runnable afterPermission;

    private final ActivityResultLauncher<Intent> vpnPermission = registerForActivityResult(
            new ActivityResultContracts.StartActivityForResult(), result -> {
                Runnable action = afterPermission;
                afterPermission = null;
                if (result.getResultCode() == Activity.RESULT_OK && action != null) {
                    action.run();
                } else {
                    toast("Без разрешения на VPN приложение не сможет направлять трафик.");
                }
            });

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);
        settings = new Settings(this);

        FragmentManager fm = getSupportFragmentManager();
        if (fm.findFragmentByTag(TAG_VPN) == null) {
            // Все вкладки создаются сразу и только скрываются: состояние списков сохраняется
            fm.beginTransaction()
                    .add(R.id.fragmentContainer, new VpnFragment(), TAG_VPN)
                    .add(R.id.fragmentContainer, new TunnelsFragment(), TAG_TUNNELS)
                    .add(R.id.fragmentContainer, new CorpFragment(), TAG_CORP)
                    .add(R.id.fragmentContainer, new DirectFragment(), TAG_DIRECT)
                    .commitNow();
        }

        BottomNavigationView nav = findViewById(R.id.bottomNav);
        nav.setOnItemSelectedListener(item -> {
            show(tagFor(item.getItemId()));
            return true;
        });
        int tab = savedInstanceState == null ? R.id.nav_vpn : savedInstanceState.getInt(STATE_TAB, R.id.nav_vpn);
        nav.setSelectedItemId(tab);
        show(tagFor(tab));
        if (savedInstanceState == null) {
            handleCommand(getIntent());
            if (UpdateManager.isAutoCheckDue(this)) checkForUpdates(false);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        // Вернулись из настроек «Установка неизвестных приложений» — продолжить установку
        if (pendingApk != null && UpdateManager.canInstall(this)) {
            File apk = pendingApk;
            pendingApk = null;
            startActivity(UpdateManager.installIntent(this, apk));
        }
    }

    // ---------- Обновления ----------

    /** Скачанный APK, ждущий разрешения на установку. */
    private File pendingApk;
    private boolean checkingUpdates;

    /** Проверить обновление; manual — по кнопке (сообщать и об отсутствии обновлений, и об ошибках). */
    void checkForUpdates(boolean manual) {
        if (checkingUpdates) return;
        checkingUpdates = true;
        if (manual) toast("Проверка обновлений…");
        new Thread(() -> {
            UpdateManager.Info info = null;
            String error = null;
            try {
                info = UpdateManager.fetch(this);
            } catch (IOException e) {
                error = e.getMessage();
            }
            UpdateManager.Info result = info;
            String err = error;
            runOnUiThread(() -> {
                checkingUpdates = false;
                if (isFinishing()) return;
                if (result == null) {
                    if (manual) toast("Не удалось проверить обновления: " + err);
                } else if (!result.isNewer()) {
                    if (manual) toast("Установлена последняя версия " + BuildConfig.VERSION_NAME);
                } else if (manual || !UpdateManager.isSkipped(this, result)) {
                    offerUpdate(result);
                }
            });
        }, "update-check").start();
    }

    private void offerUpdate(UpdateManager.Info info) {
        StringBuilder text = new StringBuilder("Установлена версия " + BuildConfig.VERSION_NAME + ".");
        if (info.size > 0) text.append("\nРазмер: ").append(Format.bytes(info.size)).append('.');
        if (!info.notes.isEmpty()) text.append("\n\nЧто нового:\n").append(info.notes);
        new MaterialAlertDialogBuilder(this)
                .setTitle("Доступна версия " + info.versionName)
                .setMessage(text)
                .setPositiveButton("Обновить", (d, w) -> downloadUpdate(info))
                .setNegativeButton("Позже", (d, w) -> UpdateManager.skip(this, info.versionCode))
                .show();
    }

    private void downloadUpdate(UpdateManager.Info info) {
        View view = getLayoutInflater().inflate(R.layout.dialog_download, null, false);
        LinearProgressIndicator bar = view.findViewById(R.id.downloadProgress);
        TextView label = view.findViewById(R.id.downloadText);
        label.setText("Подключение…");
        AtomicBoolean cancelled = new AtomicBoolean();
        AlertDialog dialog = new MaterialAlertDialogBuilder(this)
                .setTitle("Загрузка версии " + info.versionName)
                .setView(view)
                .setCancelable(false)
                .setNegativeButton(android.R.string.cancel, (d, w) -> cancelled.set(true))
                .show();

        new Thread(() -> {
            long[] lastUi = {0};
            try {
                File apk = UpdateManager.download(this, info, new UpdateManager.Progress() {
                    @Override
                    public void onProgress(long done, long total) {
                        long now = System.currentTimeMillis();
                        if (now - lastUi[0] < 200 && done != total) return;
                        lastUi[0] = now;
                        runOnUiThread(() -> {
                            if (total > 0) {
                                bar.setIndeterminate(false);
                                bar.setMax(1000);
                                bar.setProgress((int) (done * 1000 / total));
                                label.setText(Format.bytes(done) + " из " + Format.bytes(total));
                            } else {
                                label.setText(Format.bytes(done));
                            }
                        });
                    }

                    @Override
                    public boolean isCancelled() {
                        return cancelled.get();
                    }
                });
                runOnUiThread(() -> {
                    dialog.dismiss();
                    install(apk);
                });
            } catch (IOException e) {
                runOnUiThread(() -> {
                    dialog.dismiss();
                    if (!cancelled.get()) {
                        new MaterialAlertDialogBuilder(this).setTitle("Обновление не скачалось")
                                .setMessage(String.valueOf(e.getMessage()))
                                .setPositiveButton(android.R.string.ok, null).show();
                    }
                });
            }
        }, "update-download").start();
    }

    private void install(File apk) {
        if (UpdateManager.canInstall(this)) {
            startActivity(UpdateManager.installIntent(this, apk));
            return;
        }
        pendingApk = apk;
        new MaterialAlertDialogBuilder(this)
                .setTitle("Нужно разрешение")
                .setMessage("Чтобы установить обновление, разрешите этому приложению устанавливать приложения. "
                        + "После этого вернитесь назад — установка продолжится.")
                .setPositiveButton("Открыть настройки",
                        (d, w) -> startActivity(UpdateManager.installPermissionIntent(this)))
                .setNegativeButton(android.R.string.cancel, (d, w) -> pendingApk = null)
                .show();
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        handleCommand(intent);
    }

    private void handleCommand(Intent intent) {
        if (intent != null && ACTION_CONNECT.equals(intent.getAction()) && !RoutingVpnService.isMainActive()) {
            setIntent(new Intent(this, MainActivity.class)); // при повороте экрана не повторять
            toggleVpn();
        }
    }

    @Override
    protected void onSaveInstanceState(Bundle outState) {
        super.onSaveInstanceState(outState);
        outState.putInt(STATE_TAB, ((BottomNavigationView) findViewById(R.id.bottomNav)).getSelectedItemId());
    }

    private static String tagFor(int itemId) {
        if (itemId == R.id.nav_tunnels) return TAG_TUNNELS;
        if (itemId == R.id.nav_corp) return TAG_CORP;
        if (itemId == R.id.nav_direct) return TAG_DIRECT;
        return TAG_VPN;
    }

    private void show(String tag) {
        FragmentManager fm = getSupportFragmentManager();
        FragmentTransaction tx = fm.beginTransaction();
        for (String t : new String[]{TAG_VPN, TAG_TUNNELS, TAG_CORP, TAG_DIRECT}) {
            Fragment f = fm.findFragmentByTag(t);
            if (f == null) continue;
            if (t.equals(tag)) tx.show(f);
            else tx.hide(f);
        }
        tx.commitNow();
    }

    // ---------- Подключение ----------

    /** Кнопка питания на вкладке VPN: только основной VPN, VPN МГТУ не трогается. */
    void toggleVpn() {
        if (RoutingVpnService.isMainActive()) {
            RoutingVpnService.send(this, RoutingVpnService.ACTION_STOP_MAIN);
            return;
        }
        if (settings.getLink() == null) {
            toast("Сначала добавьте сервер: кнопка «Добавить» внизу.");
            return;
        }
        withVpnPermission(() -> RoutingVpnService.send(this, RoutingVpnService.ACTION_START));
    }

    /** Кнопка питания на вкладке МГТУ: только VPN МГТУ, основной VPN не трогается. */
    void toggleCorp() {
        if (RoutingVpnService.isCorpOn()) {
            RoutingVpnService.send(this, RoutingVpnService.ACTION_STOP_CORP);
            return;
        }
        withVpnPermission(() -> RoutingVpnService.send(this, RoutingVpnService.ACTION_START_CORP));
    }

    private void withVpnPermission(Runnable action) {
        Intent prepare = VpnService.prepare(this);
        if (prepare == null) {
            action.run();
        } else {
            afterPermission = action;
            vpnPermission.launch(prepare);
        }
    }

    private void toast(String text) {
        Toast.makeText(this, text, Toast.LENGTH_LONG).show();
    }
}
