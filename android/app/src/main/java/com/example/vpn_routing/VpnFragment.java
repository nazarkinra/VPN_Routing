package com.example.vpn_routing;

import android.animation.ObjectAnimator;
import android.animation.PropertyValuesHolder;
import android.animation.ValueAnimator;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.res.ColorStateList;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.PopupMenu;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import androidx.core.content.ContextCompat;
import androidx.core.graphics.ColorUtils;
import androidx.fragment.app.Fragment;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;
import androidx.recyclerview.widget.SimpleItemAnimator;

import com.google.android.material.appbar.MaterialToolbar;
import com.google.android.material.bottomsheet.BottomSheetDialog;
import com.google.android.material.card.MaterialCardView;
import com.google.android.material.textfield.TextInputEditText;
import com.google.android.material.textfield.TextInputLayout;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Вкладка основного VPN: кнопка подключения, текущий сервер, серверы и подписки. */
public class VpnFragment extends Fragment implements RoutingVpnService.Listener, ServerAdapter.Callbacks {

    private static final ExecutorService io = Executors.newCachedThreadPool();

    private Settings settings;
    private ServerStore store;
    private ServerAdapter adapter;
    private final Handler ticker = new Handler(Looper.getMainLooper());
    private boolean pinging;

    private View panel;
    private View halo;
    private MaterialCardView powerButton;
    private ImageView powerIcon;
    private TextView statusTitle;
    private TextView statusDetail;
    private TextView currentName;
    private TextView currentInfo;
    private MaterialToolbar toolbar;
    private RecyclerView list;
    private ObjectAnimator pulse;

    private final Runnable tick = new Runnable() {
        @Override
        public void run() {
            updateDetail();
            ticker.postDelayed(this, 1000);
        }
    };

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        View root = inflater.inflate(R.layout.fragment_vpn, container, false);
        settings = new Settings(requireContext());
        store = ServerStore.get(requireContext());

        panel = inflater.inflate(R.layout.view_connect_panel, null, false);
        panel.setLayoutParams(new RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT));
        halo = panel.findViewById(R.id.powerHalo);
        powerButton = panel.findViewById(R.id.powerButton);
        powerIcon = panel.findViewById(R.id.powerIcon);
        statusTitle = panel.findViewById(R.id.statusTitle);
        statusDetail = panel.findViewById(R.id.statusDetail);
        currentName = panel.findViewById(R.id.currentName);
        currentInfo = panel.findViewById(R.id.currentInfo);
        powerButton.setOnClickListener(v -> ((MainActivity) requireActivity()).toggleVpn());
        panel.findViewById(R.id.currentServer).setOnClickListener(v -> list.smoothScrollToPosition(1));

        toolbar = root.findViewById(R.id.toolbar);
        toolbar.inflateMenu(R.menu.vpn);
        toolbar.setOnMenuItemClickListener(item -> {
            int id = item.getItemId();
            if (id == R.id.action_ping) pingAll();
            else if (id == R.id.action_update) refreshAll(false);
            else if (id == R.id.action_always_on) openVpnSettings();
            return true;
        });

        list = root.findViewById(R.id.list);
        list.setLayoutManager(new LinearLayoutManager(requireContext()));
        RecyclerView.ItemAnimator animator = list.getItemAnimator();
        if (animator instanceof SimpleItemAnimator) ((SimpleItemAnimator) animator).setSupportsChangeAnimations(false);
        adapter = new ServerAdapter(panel, this);
        list.setAdapter(adapter);
        root.findViewById(R.id.addButton).setOnClickListener(v -> showAddSheet());

        reload();
        refreshAll(true);
        return root;
    }

    @Override
    public void onStart() {
        super.onStart();
        RoutingVpnService.addListener(this);
        ticker.post(tick);
    }

    @Override
    public void onStop() {
        RoutingVpnService.removeListener(this);
        ticker.removeCallbacks(tick);
        super.onStop();
    }

    private void reload() {
        adapter.submit(store, settings.getVlessUrl());
        updateCurrent();
    }

    // ---------- Состояние подключения ----------

    @Override
    public void onStateChanged(RoutingVpnService.State state, String message) {
        if (!isAdded()) return;
        Context c = requireContext();
        int core, icon, haloColor;
        float haloAlpha;
        switch (state) {
            case CONNECTED:
                core = ContextCompat.getColor(c, R.color.status_ok);
                icon = ContextCompat.getColor(c, R.color.white);
                haloColor = core;
                haloAlpha = 0.18f;
                statusTitle.setText(R.string.status_connected);
                break;
            case CONNECTING:
                core = ContextCompat.getColor(c, R.color.status_wait);
                icon = ContextCompat.getColor(c, R.color.white);
                haloColor = core;
                haloAlpha = 0.18f;
                statusTitle.setText(R.string.status_connecting);
                break;
            case ERROR:
                core = ContextCompat.getColor(c, R.color.surface);
                icon = ContextCompat.getColor(c, R.color.status_error);
                haloColor = ContextCompat.getColor(c, R.color.status_error);
                haloAlpha = 0.14f;
                statusTitle.setText(R.string.status_error);
                break;
            default:
                // Белая кнопка на сером круге: выглядит нажимаемой, а не «выключенной»
                core = ContextCompat.getColor(c, R.color.surface);
                icon = ContextCompat.getColor(c, R.color.accent);
                haloColor = ContextCompat.getColor(c, R.color.surface_variant);
                haloAlpha = 1f;
                statusTitle.setText(R.string.status_stopped);
                break;
        }
        boolean idle = state == RoutingVpnService.State.STOPPED || state == RoutingVpnService.State.ERROR;
        powerButton.setStrokeColor(ContextCompat.getColor(c, R.color.outline));
        powerButton.setStrokeWidth(idle ? Math.round(getResources().getDisplayMetrics().density) : 0);
        powerButton.setCardElevation(idle ? 6 * getResources().getDisplayMetrics().density : 0);
        powerButton.setCardBackgroundColor(core);
        powerIcon.setImageTintList(ColorStateList.valueOf(icon));
        halo.setBackgroundTintList(ColorStateList.valueOf(
                ColorUtils.setAlphaComponent(haloColor, Math.round(haloAlpha * 255))));
        setPulsing(state == RoutingVpnService.State.CONNECTING);
        updateDetail();
        // Ключ мог смениться (переключение сервера или откат после ошибки)
        reload();
    }

    private void setPulsing(boolean on) {
        if (on && pulse == null) {
            pulse = ObjectAnimator.ofPropertyValuesHolder(halo,
                    PropertyValuesHolder.ofFloat(View.SCALE_X, 0.86f, 1f),
                    PropertyValuesHolder.ofFloat(View.SCALE_Y, 0.86f, 1f));
            pulse.setDuration(900);
            pulse.setRepeatCount(ValueAnimator.INFINITE);
            pulse.setRepeatMode(ValueAnimator.REVERSE);
            pulse.start();
        } else if (!on && pulse != null) {
            pulse.cancel();
            pulse = null;
            halo.setScaleX(1f);
            halo.setScaleY(1f);
        }
    }

    private void updateDetail() {
        if (statusDetail == null) return;
        RoutingVpnService.State state = RoutingVpnService.getState();
        String message = RoutingVpnService.getMessage();
        int color = R.color.text_secondary;
        String text;
        switch (state) {
            case CONNECTED: {
                List<String> parts = new ArrayList<>();
                long since = RoutingVpnService.getConnectedSince();
                if (since > 0) parts.add(Format.duration(System.currentTimeMillis() - since));
                long delay = RoutingVpnService.getLastDelay();
                if (delay > 0) parts.add(delay + " мс");
                else if (delay < 0) {
                    parts.add("сервер не отвечает");
                    color = R.color.status_error;
                }
                text = android.text.TextUtils.join(" · ", parts);
                break;
            }
            case CONNECTING:
                text = message;
                break;
            case ERROR:
                text = message;
                color = R.color.status_error;
                break;
            default:
                text = getString(R.string.tap_to_connect);
                break;
        }
        statusDetail.setText(text);
        statusDetail.setTextColor(ContextCompat.getColor(requireContext(), color));
    }

    private void updateCurrent() {
        ServerStore.Server s = store.findByUrl(settings.getVlessUrl());
        ProxyLink link = s != null ? s.link : settings.getLink();
        if (link == null) {
            currentName.setText(R.string.server_none);
            currentInfo.setText(R.string.add_from_clipboard_hint);
        } else if (s != null) {
            currentName.setText(s.title());
            currentInfo.setText(s.subtitle());
        } else {
            currentName.setText(link.name.isEmpty() ? link.server : link.name);
            currentInfo.setText(link.describe());
        }
    }

    // ---------- Серверы ----------

    @Override
    public void onServerClick(ServerStore.Server server) {
        if (server.url.equals(settings.getVlessUrl())) return;
        if (isVpnActive()) {
            // Служба проверит сервер запуском и при ошибке вернёт прежний
            RoutingVpnService.applyKey(requireContext(), server.url);
            toast("Переключение на " + server.title());
        } else {
            settings.setVlessUrl(server.url);
            reload();
        }
    }

    @Override
    public void onServerMore(ServerStore.Server server, View anchor) {
        PopupMenu menu = new PopupMenu(requireContext(), anchor);
        menu.getMenu().add(0, 1, 0, "Проверить пинг");
        menu.getMenu().add(0, 2, 1, "Копировать ссылку");
        if (server.subscriptionId == null) menu.getMenu().add(0, 3, 2, R.string.remove);
        menu.setOnMenuItemClickListener(item -> {
            switch (item.getItemId()) {
                case 1:
                    List<ServerStore.Server> one = new ArrayList<>();
                    one.add(server);
                    Pinger.ping(requireContext(), store, one, (id, ms, done) -> adapter.notifyServer(id));
                    break;
                case 2:
                    copy(server.url);
                    break;
                case 3:
                    confirm("Удалить сервер «" + server.title() + "»?", () -> {
                        store.removeServer(server.id);
                        reload();
                    });
                    break;
                default:
                    break;
            }
            return true;
        });
        menu.show();
    }

    @Override
    public void onGroupRefresh(ServerStore.Subscription sub) {
        refresh(sub, false);
    }

    @Override
    public void onGroupMore(ServerStore.Subscription sub, View anchor) {
        PopupMenu menu = new PopupMenu(requireContext(), anchor);
        menu.getMenu().add(0, 1, 0, "Переименовать");
        menu.getMenu().add(0, 2, 1, "Копировать адрес подписки");
        menu.getMenu().add(0, 3, 2, "Удалить подписку");
        menu.setOnMenuItemClickListener(item -> {
            switch (item.getItemId()) {
                case 1:
                    prompt("Название подписки", "", sub.title(), text -> {
                        store.renameSubscription(sub.id, text);
                        reload();
                    });
                    break;
                case 2:
                    copy(sub.url);
                    break;
                case 3:
                    confirm("Удалить подписку «" + sub.title() + "» и все её серверы?", () -> {
                        store.removeSubscription(sub.id);
                        reload();
                    });
                    break;
                default:
                    break;
            }
            return true;
        });
        menu.show();
    }

    private void pingAll() {
        if (pinging) return;
        List<ServerStore.Server> all = store.servers();
        if (all.isEmpty()) {
            toast("Список серверов пуст.");
            return;
        }
        pinging = true;
        toolbar.getMenu().findItem(R.id.action_ping).setEnabled(false);
        Pinger.ping(requireContext(), store, all, (id, ms, done) -> {
            if (id != null) adapter.notifyServer(id);
            if (done) {
                pinging = false;
                if (isAdded()) toolbar.getMenu().findItem(R.id.action_ping).setEnabled(true);
            }
        });
    }

    // ---------- Подписки ----------

    /** Обновить подписки; onlyStale — только устаревшие (при открытии приложения). */
    private void refreshAll(boolean onlyStale) {
        List<ServerStore.Subscription> subs = store.subscriptions();
        if (subs.isEmpty() && !onlyStale) toast("Подписок нет.");
        for (ServerStore.Subscription sub : subs) {
            if (!onlyStale || sub.isStale()) refresh(sub, onlyStale);
        }
    }

    private void refresh(ServerStore.Subscription sub, boolean quiet) {
        adapter.setRefreshing(sub.id, true);
        io.execute(() -> {
            String error = null;
            int count = 0;
            try {
                SubscriptionFetcher.Result r = SubscriptionFetcher.fetch(sub.url);
                store.applySubscription(sub.id, r);
                count = r.links.size();
            } catch (Exception e) {
                error = e.getMessage() == null ? e.getClass().getSimpleName() : e.getMessage();
                store.setSubscriptionError(sub.id, error);
            }
            String err = error;
            int n = count;
            ticker.post(() -> {
                if (!isAdded()) return;
                adapter.setRefreshing(sub.id, false);
                selectFirstIfNone();
                reload();
                if (err != null && !quiet) toast("Подписка не обновилась: " + err);
                else if (err == null && !quiet) toast(sub.title() + ": серверов " + n);
            });
        });
    }

    // ---------- Добавление ----------

    private void showAddSheet() {
        BottomSheetDialog sheet = new BottomSheetDialog(requireContext());
        View v = getLayoutInflater().inflate(R.layout.sheet_add, null, false);
        v.findViewById(R.id.addPaste).setOnClickListener(x -> {
            sheet.dismiss();
            String text = clipboardText();
            if (text.isEmpty()) toast("Буфер обмена пуст.");
            else addInput(text);
        });
        v.findViewById(R.id.addManual).setOnClickListener(x -> {
            sheet.dismiss();
            prompt(getString(R.string.add), getString(R.string.add_manual_hint), "", this::addInput);
        });
        sheet.setContentView(v);
        sheet.show();
    }

    /** Ссылки vless:// и hysteria2:// и адреса подписок — по одной на строке. */
    private void addInput(String input) {
        int added = 0;
        List<String> errors = new ArrayList<>();
        List<ServerStore.Subscription> newSubs = new ArrayList<>();
        for (String raw : input.split("[\\r\\n\\s]+")) {
            String line = raw.trim();
            if (line.isEmpty()) continue;
            if (SubscriptionFetcher.isSubscriptionUrl(line)) {
                newSubs.add(store.addSubscription(line));
            } else if (ProxyLink.isSupportedScheme(line)) {
                try {
                    store.addServer(line);
                    added++;
                } catch (IllegalArgumentException e) {
                    errors.add(e.getMessage());
                }
            } else if (line.contains("://")) {
                errors.add("Протокол " + line.substring(0, line.indexOf("://")) + " пока не поддерживается.");
            } else {
                errors.add("Не похоже на ссылку: " + (line.length() > 40 ? line.substring(0, 40) + "…" : line));
            }
        }
        selectFirstIfNone();
        reload();
        for (ServerStore.Subscription sub : newSubs) refresh(sub, false);
        if (!errors.isEmpty()) {
            new MaterialAlertDialogBuilder(requireContext()).setTitle(added > 0 ? "Добавлено не всё" : "Не добавлено")
                    .setMessage(android.text.TextUtils.join("\n", errors))
                    .setPositiveButton(android.R.string.ok, null).show();
        } else if (added > 0) {
            toast(added == 1 ? "Сервер добавлен" : "Добавлено серверов: " + added);
        } else if (newSubs.isEmpty()) {
            toast("Ссылок не найдено.");
        }
    }

    /** Если сервер ещё не выбран, выбрать первый из списка. */
    private void selectFirstIfNone() {
        if (store.findByUrl(settings.getVlessUrl()) != null || isVpnActive()) return;
        List<ServerStore.Server> all = store.servers();
        if (!all.isEmpty()) settings.setVlessUrl(all.get(0).url);
    }

    // ---------- Вспомогательное ----------

    private boolean isVpnActive() {
        RoutingVpnService.State s = RoutingVpnService.getState();
        return s == RoutingVpnService.State.CONNECTED || s == RoutingVpnService.State.CONNECTING;
    }

    private void openVpnSettings() {
        try {
            startActivity(new Intent(android.provider.Settings.ACTION_VPN_SETTINGS));
        } catch (Exception e) {
            toast("Не удалось открыть настройки VPN.");
        }
    }

    private String clipboardText() {
        ClipboardManager cm = (ClipboardManager) requireContext().getSystemService(Context.CLIPBOARD_SERVICE);
        ClipData clip = cm == null ? null : cm.getPrimaryClip();
        if (clip == null || clip.getItemCount() == 0) return "";
        CharSequence text = clip.getItemAt(0).coerceToText(requireContext());
        return text == null ? "" : text.toString().trim();
    }

    private void copy(String text) {
        ClipboardManager cm = (ClipboardManager) requireContext().getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm != null) cm.setPrimaryClip(ClipData.newPlainText("link", text));
        toast("Скопировано");
    }

    interface TextResult {
        void accept(String text);
    }

    private void prompt(String title, String hint, String value, TextResult result) {
        Context c = requireContext();
        View frame = getLayoutInflater().inflate(R.layout.dialog_input, null, false);
        TextInputLayout layout = frame.findViewById(R.id.inputLayout);
        TextInputEditText input = frame.findViewById(R.id.input);
        layout.setHint(hint.isEmpty() ? title : hint);
        input.setText(value);
        new MaterialAlertDialogBuilder(c).setTitle(title).setView(frame)
                .setPositiveButton(android.R.string.ok, (d, w) -> {
                    String text = input.getText() == null ? "" : input.getText().toString().trim();
                    if (!text.isEmpty()) result.accept(text);
                })
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void confirm(String message, Runnable action) {
        new MaterialAlertDialogBuilder(requireContext()).setMessage(message)
                .setPositiveButton(R.string.remove, (d, w) -> action.run())
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void toast(String text) {
        if (isAdded()) Toast.makeText(requireContext(), text, Toast.LENGTH_SHORT).show();
    }
}
