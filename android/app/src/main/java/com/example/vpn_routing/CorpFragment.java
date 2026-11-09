package com.example.vpn_routing;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.res.ColorStateList;
import android.net.Uri;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.TextView;
import android.widget.Toast;

import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import androidx.core.content.ContextCompat;
import androidx.fragment.app.Fragment;

import com.google.android.material.appbar.MaterialToolbar;
import com.google.android.material.switchmaterial.SwitchMaterial;
import com.google.android.material.textfield.TextInputEditText;
import com.google.android.material.textfield.TextInputLayout;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Locale;

/** Вкладка корпоративного VPN МГТУ: профиль с переключателем, статистика, учётные данные, файл, журнал. */
public class CorpFragment extends Fragment implements BmstuManager.Listener, RoutingVpnService.Listener {

    private Settings settings;
    private final Handler ticker = new Handler(Looper.getMainLooper());

    private SwitchMaterial profileSwitch;
    private TextView profileServer;
    private View statusDot;
    private TextView statusText;
    private TextView statusDetail;
    private View statsSection;
    private TextView statDuration, statAddress, statIn, statOut, statInSpeed, statOutSpeed;
    private TextView credUser, credPass;
    private TextView fileName, fileInfo;
    private View fileRestore;
    private TextView logText;
    private MaterialToolbar toolbar;

    private long lastIn = -1, lastOut = -1;
    private boolean bindingSwitch;

    private final ActivityResultLauncher<String[]> pickOvpn = registerForActivityResult(
            new ActivityResultContracts.OpenDocument(), uri -> {
                if (uri != null) loadOvpn(uri);
            });

    private final Runnable tick = new Runnable() {
        @Override
        public void run() {
            updateStats();
            ticker.postDelayed(this, 1000);
        }
    };

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        View v = inflater.inflate(R.layout.fragment_corp, container, false);
        settings = new Settings(requireContext());

        toolbar = v.findViewById(R.id.toolbar);
        toolbar.inflateMenu(R.menu.corp);
        toolbar.setOnMenuItemClickListener(item -> {
            if (item.getItemId() == R.id.action_import) pickOvpn.launch(new String[]{"*/*"});
            else if (item.getItemId() == R.id.action_restore) restoreOvpn();
            return true;
        });

        profileSwitch = v.findViewById(R.id.profileSwitch);
        profileServer = v.findViewById(R.id.profileServer);
        statusDot = v.findViewById(R.id.statusDot);
        statusText = v.findViewById(R.id.statusText);
        statusDetail = v.findViewById(R.id.statusDetail);
        statsSection = v.findViewById(R.id.statsSection);
        statDuration = v.findViewById(R.id.statDuration);
        statAddress = v.findViewById(R.id.statAddress);
        statIn = v.findViewById(R.id.statIn);
        statOut = v.findViewById(R.id.statOut);
        statInSpeed = v.findViewById(R.id.statInSpeed);
        statOutSpeed = v.findViewById(R.id.statOutSpeed);
        credUser = v.findViewById(R.id.credUser);
        credPass = v.findViewById(R.id.credPass);
        fileName = v.findViewById(R.id.fileName);
        fileInfo = v.findViewById(R.id.fileInfo);
        fileRestore = v.findViewById(R.id.fileRestore);
        logText = v.findViewById(R.id.logText);

        profileSwitch.setOnCheckedChangeListener((b, checked) -> {
            if (!bindingSwitch) setEnabled(checked);
        });
        v.findViewById(R.id.profileCard).setOnClickListener(x -> profileSwitch.toggle());
        v.findViewById(R.id.credEdit).setOnClickListener(x -> editCredentials());
        v.findViewById(R.id.fileImport).setOnClickListener(x -> pickOvpn.launch(new String[]{"*/*"}));
        fileRestore.setOnClickListener(x -> restoreOvpn());
        v.findViewById(R.id.logCopy).setOnClickListener(x -> copyLog());

        bindStatic();
        return v;
    }

    @Override
    public void onStart() {
        super.onStart();
        BmstuManager.addListener(this);
        RoutingVpnService.addListener(this);
        ticker.post(tick);
    }

    @Override
    public void onStop() {
        BmstuManager.removeListener(this);
        RoutingVpnService.removeListener(this);
        ticker.removeCallbacks(tick);
        super.onStop();
    }

    @Override
    public void onHiddenChanged(boolean hidden) {
        super.onHiddenChanged(hidden);
        if (!hidden) bindStatic();
    }

    // ---------- Состояние ----------

    @Override
    public void onStateChanged(RoutingVpnService.State state, String message) {
        if (isAdded()) render();
    }

    @Override
    public void onBmstuState(BmstuManager.State state, String message) {
        lastMessage = message == null ? "" : message;
        if (isAdded()) render();
    }

    private boolean vpnActive() {
        RoutingVpnService.State s = RoutingVpnService.getState();
        return s == RoutingVpnService.State.CONNECTED || s == RoutingVpnService.State.CONNECTING;
    }

    private void render() {
        bindingSwitch = true;
        profileSwitch.setChecked(settings.isBmstuEnabled());
        bindingSwitch = false;

        BmstuManager.State state = BmstuManager.getState();
        String title;
        String detail;
        int color;
        if (!settings.isBmstuEnabled()) {
            title = "Отключено";
            detail = "Включите, чтобы сайты МГТУ шли через корпоративную сеть";
            color = R.color.status_idle;
        } else if (!settings.hasBmstuOvpn()) {
            title = "Нужен файл профиля";
            detail = "Импортируйте .ovpn от университета в разделе «Файл профиля»";
            color = R.color.status_wait;
        } else if (!settings.hasBmstuCredentials()) {
            title = "Нужны логин и пароль";
            detail = "Укажите их в разделе «Учётные данные»";
            color = R.color.status_wait;
        } else if (!vpnActive()) {
            title = "Ожидает основной VPN";
            detail = "Подключится вместе с основным VPN на вкладке VPN";
            color = R.color.status_idle;
        } else if (state == BmstuManager.State.UP) {
            title = "Подключено";
            detail = "bmstu.ru идёт через сеть МГТУ";
            color = R.color.status_ok;
        } else if (state == BmstuManager.State.CONNECTING) {
            title = "Подключение…";
            detail = "Рукопожатие с сервером OpenVPN";
            color = R.color.status_wait;
        } else if (state == BmstuManager.State.WAITING) {
            title = "Нет соединения";
            detail = currentMessage();
            color = R.color.status_error;
        } else {
            title = "Отключено";
            detail = currentMessage();
            color = R.color.status_idle;
        }
        statusText.setText(title);
        statusDetail.setText(detail);
        statusDot.setBackgroundTintList(ColorStateList.valueOf(ContextCompat.getColor(requireContext(), color)));
        statsSection.setVisibility(state == BmstuManager.State.UP && vpnActive() ? View.VISIBLE : View.GONE);
        updateStats();
        String log = BmstuManager.getLog();
        logText.setText(log.isEmpty() ? getString(R.string.bmstu_log_empty) : log);
    }

    /** Последнее сообщение менеджера туннеля (причина ошибки, время повтора). */
    private String lastMessage = "";

    private String currentMessage() {
        return lastMessage;
    }

    private void updateStats() {
        if (statsSection == null || statsSection.getVisibility() != View.VISIBLE) {
            lastIn = lastOut = -1;
            return;
        }
        long since = BmstuManager.getUpSince();
        statDuration.setText(since > 0 ? Format.duration(System.currentTimeMillis() - since) : "—");
        String ip = BmstuManager.getLocalIp();
        statAddress.setText(ip.isEmpty() ? "—" : ip);
        long in = BmstuManager.getBytesIn(), out = BmstuManager.getBytesOut();
        statIn.setText(Format.bytes(in));
        statOut.setText(Format.bytes(out));
        statInSpeed.setText(lastIn >= 0 ? Format.bytes(Math.max(0, in - lastIn)) + "/с" : "");
        statOutSpeed.setText(lastOut >= 0 ? Format.bytes(Math.max(0, out - lastOut)) + "/с" : "");
        lastIn = in;
        lastOut = out;
    }

    /** Учётные данные и файл профиля. */
    private void bindStatic() {
        String user = settings.getBmstuUser();
        credUser.setText(user.isEmpty() ? getString(R.string.bmstu_no_credentials) : user);
        credPass.setText(settings.hasBmstuCredentials() ? "Пароль: ••••••••" : "Логин и пароль не заданы");

        if (!settings.hasBmstuOvpn()) {
            fileName.setText("Файл .ovpn не загружен");
            fileInfo.setText("Нажмите «Импорт» и выберите .ovpn, выданный университетом");
            profileServer.setText("Профиль не загружен");
        } else {
            fileName.setText("Загруженный .ovpn");
            try {
                BmstuConfig cfg = BmstuConfig.parse(settings.getBmstuOvpn(requireContext()));
                profileServer.setText(cfg.remoteHost + " · " + protoAndPort(cfg.text));
                fileInfo.setText(cfg.notAfter == null ? "Срок сертификата не указан"
                        : "Сертификат действует до " + cfg.notAfterDate());
            } catch (IllegalArgumentException e) {
                profileServer.setText("Файл профиля повреждён");
                fileInfo.setText(e.getMessage());
            }
        }
        fileRestore.setVisibility(settings.hasBmstuPreviousOvpn() ? View.VISIBLE : View.GONE);
        toolbar.getMenu().findItem(R.id.action_restore).setVisible(settings.hasBmstuPreviousOvpn());
        render();
    }

    /** «UDP 2100» из очищенного .ovpn. */
    private static String protoAndPort(String text) {
        String proto = "UDP", port = "1194";
        for (String line : text.split("\n")) {
            String[] p = line.trim().split("\\s+");
            if (p.length == 0) continue;
            String key = p[0].toLowerCase(Locale.ROOT);
            if (key.equals("proto") && p.length > 1) proto = p[1].toLowerCase(Locale.ROOT).startsWith("tcp") ? "TCP" : "UDP";
            if ((key.equals("port") || key.equals("rport")) && p.length > 1) port = p[1];
            if (key.equals("remote") && p.length > 2) port = p[2];
            if (key.equals("remote") && p.length > 3) proto = p[3].toLowerCase(Locale.ROOT).startsWith("tcp") ? "TCP" : "UDP";
        }
        return proto + " " + port;
    }

    // ---------- Действия ----------

    private void setEnabled(boolean enabled) {
        settings.setBmstuEnabled(enabled);
        if (enabled && !settings.hasBmstuCredentials()) editCredentials();
        RoutingVpnService.send(requireContext(), RoutingVpnService.ACTION_BMSTU_SYNC);
        render();
    }

    private void editCredentials() {
        View v = getLayoutInflater().inflate(R.layout.dialog_credentials, null, false);
        TextInputEditText user = v.findViewById(R.id.credUserInput);
        TextInputEditText pass = v.findViewById(R.id.credPassInput);
        TextInputLayout passLayout = v.findViewById(R.id.credPassLayout);
        user.setText(settings.getBmstuUser());
        if (settings.hasBmstuCredentials()) passLayout.setHint(getString(R.string.bmstu_password_saved));
        new MaterialAlertDialogBuilder(requireContext()).setTitle(R.string.bmstu_credentials).setView(v)
                .setPositiveButton(android.R.string.ok, (d, w) -> {
                    String u = user.getText() == null ? "" : user.getText().toString().trim();
                    String p = pass.getText() == null ? "" : pass.getText().toString();
                    if (p.isEmpty()) p = settings.getBmstuPassword();
                    if (u.isEmpty() || p.isEmpty()) {
                        toast("Нужны и логин, и пароль.");
                        return;
                    }
                    settings.setBmstuCredentials(u, p);
                    if (!settings.isBmstuEnabled()) settings.setBmstuEnabled(true);
                    RoutingVpnService.send(requireContext(), RoutingVpnService.ACTION_BMSTU_SYNC);
                    bindStatic();
                })
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void loadOvpn(Uri uri) {
        String text;
        try (InputStream in = requireContext().getContentResolver().openInputStream(uri)) {
            if (in == null) throw new IOException("файл недоступен");
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int n;
            while ((n = in.read(buf)) > 0) {
                out.write(buf, 0, n);
                if (out.size() > BmstuConfig.MAX_SIZE) break;
            }
            text = out.toString("UTF-8");
        } catch (IOException | SecurityException e) {
            error("Не удалось прочитать файл", String.valueOf(e.getMessage()));
            return;
        }
        BmstuConfig cfg;
        try {
            cfg = BmstuConfig.parse(text);
        } catch (IllegalArgumentException e) {
            error("Файл не подходит", e.getMessage());
            return;
        }
        String note = cfg.skipped.isEmpty() ? ""
                : " Пропущены настройки: " + android.text.TextUtils.join(", ", cfg.skipped) + ".";
        if (RoutingVpnService.applyBmstuOvpn(requireContext(), text)) {
            // Служба проверит файл подключением и при ошибке вернёт прежний
            toast("Профиль импортирован, проверка подключения…" + note);
        } else {
            settings.installBmstuOvpn(requireContext(), text);
            toast("Профиль импортирован." + note);
        }
        ticker.postDelayed(this::bindStatic, 300);
    }

    private void restoreOvpn() {
        if (!settings.restoreBmstuOvpn()) {
            toast("Предыдущего файла нет.");
            return;
        }
        toast("Возвращён предыдущий файл профиля.");
        RoutingVpnService.send(requireContext(), RoutingVpnService.ACTION_BMSTU_SYNC);
        bindStatic();
    }

    private void copyLog() {
        ClipboardManager cm = (ClipboardManager) requireContext().getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm != null) cm.setPrimaryClip(ClipData.newPlainText("log", BmstuManager.getLog()));
        toast("Журнал скопирован");
    }

    private void error(String title, String text) {
        new MaterialAlertDialogBuilder(requireContext()).setTitle(title).setMessage(text)
                .setPositiveButton(android.R.string.ok, null).show();
    }

    private void toast(String text) {
        if (isAdded()) Toast.makeText(requireContext(), text, Toast.LENGTH_LONG).show();
    }
}
