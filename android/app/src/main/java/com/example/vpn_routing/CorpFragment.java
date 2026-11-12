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
import com.google.android.material.card.MaterialCardView;
import com.google.android.material.textfield.TextInputEditText;
import com.google.android.material.textfield.TextInputLayout;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Locale;

/**
 * Вкладка корпоративного VPN МГТУ: кнопка питания (как на вкладке VPN, но включает только туннель
 * университета, независимо от основного VPN), профиль, статистика, учётные данные, файл, журнал.
 */
public class CorpFragment extends Fragment implements BmstuManager.Listener, RoutingVpnService.Listener {

    private Settings settings;
    private final Handler ticker = new Handler(Looper.getMainLooper());

    private View powerHalo;
    private MaterialCardView powerButton;
    private android.widget.ImageView powerIcon;
    private android.animation.ObjectAnimator pulse;
    private TextView profileServer;
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

        powerHalo = v.findViewById(R.id.powerHalo);
        powerButton = v.findViewById(R.id.powerButton);
        powerIcon = v.findViewById(R.id.powerIcon);
        powerButton.setOnClickListener(x -> togglePower());
        profileServer = v.findViewById(R.id.profileServer);
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

    private enum Power { OFF, CONNECTING, ON, ERROR }

    private void render() {
        // Состояние только VPN МГТУ: основной VPN на эту кнопку не влияет
        boolean on = RoutingVpnService.isCorpOn();
        BmstuManager.State state = BmstuManager.getState();
        Power power;
        String title;
        String detail;
        if (on && state == BmstuManager.State.UP) {
            power = Power.ON;
            title = getString(R.string.status_connected);
            detail = "bmstu.ru идёт через сеть МГТУ";
        } else if (on && state == BmstuManager.State.WAITING) {
            power = Power.CONNECTING;
            title = "Нет соединения";
            detail = currentMessage();
        } else if (on) {
            power = Power.CONNECTING;
            title = getString(R.string.status_connecting);
            detail = "Рукопожатие с сервером OpenVPN";
        } else if (state == BmstuManager.State.ERROR) {
            power = Power.ERROR;
            title = getString(R.string.status_error);
            detail = currentMessage();
        } else {
            power = Power.OFF;
            title = getString(R.string.status_stopped);
            detail = !settings.hasBmstuOvpn() ? "Сначала импортируйте .ovpn от университета"
                    : !settings.hasBmstuCredentials() ? "Сначала укажите логин и пароль"
                    : getString(R.string.tap_to_connect);
        }
        statusText.setText(title);
        statusDetail.setText(detail);
        statusDetail.setTextColor(ContextCompat.getColor(requireContext(),
                power == Power.ERROR || state == BmstuManager.State.WAITING && on
                        ? R.color.status_error : R.color.text_secondary));
        paintPower(power);

        statsSection.setVisibility(on && state == BmstuManager.State.UP ? View.VISIBLE : View.GONE);
        updateStats();
        String log = BmstuManager.getLog();
        logText.setText(log.isEmpty() ? getString(R.string.bmstu_log_empty) : log);
    }

    /** Кнопка питания — те же цвета, что на вкладке VPN. */
    private void paintPower(Power power) {
        Context c = requireContext();
        int core, icon, haloColor;
        float haloAlpha;
        switch (power) {
            case ON:
            case CONNECTING:
                core = ContextCompat.getColor(c, power == Power.ON ? R.color.status_ok : R.color.status_wait);
                icon = ContextCompat.getColor(c, R.color.white);
                haloColor = core;
                haloAlpha = 0.18f;
                break;
            case ERROR:
                core = ContextCompat.getColor(c, R.color.surface);
                icon = ContextCompat.getColor(c, R.color.status_error);
                haloColor = ContextCompat.getColor(c, R.color.status_error);
                haloAlpha = 0.14f;
                break;
            default:
                core = ContextCompat.getColor(c, R.color.surface);
                icon = ContextCompat.getColor(c, R.color.accent);
                haloColor = ContextCompat.getColor(c, R.color.surface_variant);
                haloAlpha = 1f;
                break;
        }
        boolean idle = power == Power.OFF || power == Power.ERROR;
        float density = getResources().getDisplayMetrics().density;
        powerButton.setStrokeColor(ContextCompat.getColor(c, R.color.outline));
        powerButton.setStrokeWidth(idle ? Math.round(density) : 0);
        powerButton.setCardElevation(idle ? 6 * density : 0);
        powerButton.setCardBackgroundColor(core);
        powerIcon.setImageTintList(ColorStateList.valueOf(icon));
        powerHalo.setBackgroundTintList(ColorStateList.valueOf(
                androidx.core.graphics.ColorUtils.setAlphaComponent(haloColor, Math.round(haloAlpha * 255))));
        boolean pulsing = power == Power.CONNECTING;
        if (pulsing && pulse == null) {
            pulse = android.animation.ObjectAnimator.ofPropertyValuesHolder(powerHalo,
                    android.animation.PropertyValuesHolder.ofFloat(View.SCALE_X, 0.86f, 1f),
                    android.animation.PropertyValuesHolder.ofFloat(View.SCALE_Y, 0.86f, 1f));
            pulse.setDuration(900);
            pulse.setRepeatCount(android.animation.ValueAnimator.INFINITE);
            pulse.setRepeatMode(android.animation.ValueAnimator.REVERSE);
            pulse.start();
        } else if (!pulsing && pulse != null) {
            pulse.cancel();
            pulse = null;
            powerHalo.setScaleX(1f);
            powerHalo.setScaleY(1f);
        }
    }

    /** Кнопка питания VPN МГТУ. Без файла профиля или учётных данных — сначала они. */
    private void togglePower() {
        if (RoutingVpnService.isCorpOn()) {
            ((MainActivity) requireActivity()).toggleCorp();
        } else if (!settings.hasBmstuOvpn()) {
            toast("Сначала импортируйте файл профиля .ovpn от университета.");
            pickOvpn.launch(new String[]{"*/*"});
        } else if (!settings.hasBmstuCredentials()) {
            editCredentials(true);
        } else {
            ((MainActivity) requireActivity()).toggleCorp();
        }
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

    private void editCredentials() {
        editCredentials(false);
    }

    /** Диалог логина и пароля; connectAfter — после сохранения включить VPN МГТУ (нажата кнопка питания). */
    private void editCredentials(boolean connectAfter) {
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
                    // Включённый туннель переподключится с новыми данными
                    RoutingVpnService.send(requireContext(), RoutingVpnService.ACTION_BMSTU_SYNC);
                    bindStatic();
                    if (connectAfter && settings.hasBmstuOvpn()) ((MainActivity) requireActivity()).toggleCorp();
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
