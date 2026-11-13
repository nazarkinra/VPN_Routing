package com.example.vpn_routing;

import android.content.Context;
import android.content.Intent;
import android.content.res.ColorStateList;
import android.os.Bundle;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.core.content.ContextCompat;
import androidx.fragment.app.Fragment;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import com.google.android.material.floatingactionbutton.ExtendedFloatingActionButton;
import com.google.android.material.switchmaterial.SwitchMaterial;
import com.google.android.material.textfield.TextInputEditText;
import com.google.android.material.textfield.TextInputLayout;

import java.util.ArrayList;
import java.util.List;

/**
 * Вкладка «Туннели» (версия 2.1): основной VPN и дополнительные туннели — каждый со своим сервером,
 * приложениями и доменами. Изменения применяются на ходу: ядро перезапускается с новым конфигом.
 */
public class TunnelsFragment extends Fragment implements RoutingVpnService.Listener {

    /** Цвета номеров туннелей (номер 1 — основной VPN); после десятого цвета идут по кругу. */
    private static final int[] COLORS = {
            0xFF3F63F5, 0xFF8E44D9, 0xFFE0721E, 0xFF1E9EA0, 0xFFD93B8C,
            0xFF5A8F1E, 0xFFB5651D, 0xFF2E7BD6, 0xFFC0392B, 0xFF6D5BD0};

    static int color(int number) {
        return COLORS[Math.max(0, number - 1) % COLORS.length];
    }

    private Settings settings;
    private TunnelStore store;
    private Adapter adapter;
    private ExtendedFloatingActionButton addButton;

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        View v = inflater.inflate(R.layout.fragment_tunnels, container, false);
        settings = new Settings(requireContext());
        store = TunnelStore.get(requireContext());
        RecyclerView list = v.findViewById(R.id.list);
        list.setLayoutManager(new LinearLayoutManager(requireContext()));
        adapter = new Adapter();
        list.setAdapter(adapter);
        addButton = v.findViewById(R.id.addButton);
        addButton.setOnClickListener(x -> addTunnel());
        return v;
    }

    @Override
    public void onStart() {
        super.onStart();
        RoutingVpnService.addListener(this); // сразу вызывает onStateChanged — список отрисуется
    }

    @Override
    public void onStop() {
        RoutingVpnService.removeListener(this);
        super.onStop();
    }

    @Override
    public void onResume() {
        super.onResume();
        render(); // вернулись с экрана туннеля
    }

    @Override
    public void onHiddenChanged(boolean hidden) {
        super.onHiddenChanged(hidden);
        if (!hidden) render(); // сервер основного VPN могли сменить на вкладке VPN
    }

    @Override
    public void onStateChanged(RoutingVpnService.State state, String message) {
        if (isAdded()) render();
    }

    private void render() {
        if (adapter == null) return;
        adapter.tunnels = store.all();
        adapter.notifyDataSetChanged();
    }

    private void addTunnel() {
        Context c = requireContext();
        View frame = getLayoutInflater().inflate(R.layout.dialog_input, null, false);
        TextInputLayout layout = frame.findViewById(R.id.inputLayout);
        TextInputEditText input = frame.findViewById(R.id.input);
        layout.setHint(getString(R.string.tunnel_name));
        input.setSingleLine(true);
        input.setInputType(android.text.InputType.TYPE_CLASS_TEXT | android.text.InputType.TYPE_TEXT_FLAG_CAP_SENTENCES);
        input.setText("Туннель " + (store.all().size() + 2));
        input.selectAll();
        new MaterialAlertDialogBuilder(c).setTitle(R.string.tunnel_add)
                .setMessage("Например: «Стриминг», «Работа», «Игры». Сервер, приложения и домены выберете дальше.")
                .setView(frame)
                .setPositiveButton(R.string.add, (d, w) -> {
                    String name = input.getText() == null ? "" : input.getText().toString().trim();
                    TunnelStore.Tunnel t = store.add(name.isEmpty() ? "Туннель" : name);
                    startActivity(new Intent(c, TunnelActivity.class).putExtra(TunnelActivity.EXTRA_ID, t.id));
                })
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void setEnabled(TunnelStore.Tunnel t, boolean on) {
        if (on && t.link() == null) {
            toast("Сначала выберите сервер туннеля.");
            render();
            return;
        }
        t.enabled = on;
        store.update(t);
        RoutingVpnService.send(requireContext(), RoutingVpnService.ACTION_RELOAD_TUNNELS);
        render();
    }

    private void toast(String text) {
        if (isAdded()) Toast.makeText(requireContext(), text, Toast.LENGTH_SHORT).show();
    }

    // ---------- Список ----------

    private static final int TYPE_INTRO = 0;
    private static final int TYPE_TUNNEL = 1;
    private static final int TYPE_EMPTY = 2;

    private final class Adapter extends RecyclerView.Adapter<RecyclerView.ViewHolder> {
        List<TunnelStore.Tunnel> tunnels = new ArrayList<>();

        @Override
        public int getItemCount() {
            return 1 + Math.max(1, tunnels.size());
        }

        @Override
        public int getItemViewType(int position) {
            if (position == 0) return TYPE_INTRO;
            return tunnels.isEmpty() ? TYPE_EMPTY : TYPE_TUNNEL;
        }

        @NonNull
        @Override
        public RecyclerView.ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
            LayoutInflater inf = LayoutInflater.from(parent.getContext());
            int layout = viewType == TYPE_INTRO ? R.layout.item_tunnel_intro
                    : viewType == TYPE_EMPTY ? R.layout.item_empty : R.layout.item_tunnel;
            return new RecyclerView.ViewHolder(inf.inflate(layout, parent, false)) {
            };
        }

        @Override
        public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int position) {
            View v = holder.itemView;
            int type = getItemViewType(position);
            if (type == TYPE_INTRO) bindIntro(v);
            else if (type == TYPE_EMPTY) ((TextView) v.findViewById(R.id.emptyText)).setText(R.string.tunnels_empty);
            else bindTunnel(v, tunnels.get(position - 1), position + 1);
        }

        private void bindIntro(View v) {
            Context c = v.getContext();
            ServerStore.Server s = ServerStore.get(c).findByUrl(settings.getVlessUrl());
            ProxyLink link = settings.getLink();
            ((TextView) v.findViewById(R.id.mainServer)).setText(s != null ? s.title()
                    : link != null ? link.server : getString(R.string.server_none));
            RoutingVpnService.State state = RoutingVpnService.getState();
            String status;
            int color;
            switch (state) {
                case CONNECTED:
                    status = "Подключено · весь остальной трафик";
                    color = R.color.status_ok;
                    break;
                case CONNECTING:
                    status = "Подключение…";
                    color = R.color.status_wait;
                    break;
                case ERROR:
                    status = RoutingVpnService.getMessage();
                    color = R.color.status_error;
                    break;
                default:
                    status = "Выключен — дополнительные туннели тоже не работают";
                    color = R.color.status_idle;
                    break;
            }
            TextView st = v.findViewById(R.id.mainStatus);
            st.setText(status);
            st.setTextColor(ContextCompat.getColor(c, state == RoutingVpnService.State.STOPPED
                    ? R.color.text_secondary : color));
            v.findViewById(R.id.mainNumber).setBackgroundTintList(
                    ColorStateList.valueOf(ContextCompat.getColor(c, color)));
            ((TextView) v.findViewById(R.id.tunnelCount)).setText(String.valueOf(tunnels.size()));
        }

        private void bindTunnel(View v, TunnelStore.Tunnel t, int number) {
            Context c = v.getContext();
            TextView num = v.findViewById(R.id.tunnelNumber);
            num.setText(String.valueOf(number));
            num.setBackgroundTintList(ColorStateList.valueOf(t.enabled ? color(number)
                    : ContextCompat.getColor(c, R.color.status_idle)));
            ((TextView) v.findViewById(R.id.tunnelName)).setText(t.name);

            ServerStore.Server server = t.serverUrl.isEmpty() ? null : ServerStore.get(c).findByUrl(t.serverUrl);
            ProxyLink link = t.link();
            String serverText = server != null ? server.title() + " · " + server.subtitle()
                    : link != null ? link.server : getString(R.string.tunnel_server_none);
            ((TextView) v.findViewById(R.id.tunnelServer)).setText(serverText);

            List<String> rules = new ArrayList<>();
            if (!t.apps.isEmpty()) rules.add(Format.apps(t.apps.size()));
            if (!t.domains.isEmpty()) rules.add(Format.plural(t.domains.size(), "домен", "домена", "доменов"));
            ((TextView) v.findViewById(R.id.tunnelRules)).setText(rules.isEmpty()
                    ? "Нет приложений и доменов" : android.text.TextUtils.join(" · ", rules));

            TextView status = v.findViewById(R.id.tunnelStatus);
            String text;
            int color;
            RoutingVpnService.TunnelStatus st = RoutingVpnService.getTunnelStatus(t.id);
            if (!t.enabled) {
                text = "Выключен";
                color = R.color.text_secondary;
            } else if (link == null) {
                text = "Выберите сервер";
                color = R.color.status_wait;
            } else if (t.apps.isEmpty() && t.domains.isEmpty()) {
                text = "Добавьте приложения или домены";
                color = R.color.status_wait;
            } else if (!RoutingVpnService.isMainActive()) {
                text = "Ждёт включения основного VPN";
                color = R.color.text_secondary;
            } else if (st == null) {
                text = "Подключение…";
                color = R.color.status_wait;
            } else {
                text = st.text;
                color = st.ok ? (st.delay == 0 ? R.color.status_wait : R.color.status_ok) : R.color.status_error;
            }
            status.setText(text);
            status.setTextColor(ContextCompat.getColor(c, color));

            SwitchMaterial sw = v.findViewById(R.id.tunnelSwitch);
            sw.setOnCheckedChangeListener(null);
            sw.setChecked(t.enabled);
            sw.setOnCheckedChangeListener((b, on) -> setEnabled(t, on));
            v.setOnClickListener(x -> startActivity(new Intent(c, TunnelActivity.class)
                    .putExtra(TunnelActivity.EXTRA_ID, t.id)));
        }
    }
}
