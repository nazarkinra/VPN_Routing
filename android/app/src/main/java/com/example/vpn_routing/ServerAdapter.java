package com.example.vpn_routing;

import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.ImageButton;
import android.widget.ImageView;
import android.widget.ProgressBar;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.core.content.ContextCompat;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.card.MaterialCardView;
import com.google.android.material.progressindicator.LinearProgressIndicator;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/** Список на вкладке VPN: панель подключения, группы (свои серверы и подписки) и серверы. */
final class ServerAdapter extends RecyclerView.Adapter<RecyclerView.ViewHolder> {

    interface Callbacks {
        void onServerClick(ServerStore.Server server);

        void onServerMore(ServerStore.Server server, View anchor);

        void onGroupRefresh(ServerStore.Subscription sub);

        void onGroupMore(ServerStore.Subscription sub, View anchor);
    }

    private static final int TYPE_PANEL = 0;
    private static final int TYPE_GROUP = 1;
    private static final int TYPE_SERVER = 2;
    private static final int TYPE_EMPTY = 3;

    private static final class Row {
        final int type;
        final ServerStore.Server server;
        final ServerStore.Subscription sub;

        Row(int type, ServerStore.Server server, ServerStore.Subscription sub) {
            this.type = type;
            this.server = server;
            this.sub = sub;
        }
    }

    private final View panel;
    private final Callbacks callbacks;
    private final List<Row> rows = new ArrayList<>();
    private final Set<String> refreshing = new HashSet<>();
    private String activeUrl = "";
    private ServerStore store;

    ServerAdapter(View panel, Callbacks callbacks) {
        this.panel = panel;
        this.callbacks = callbacks;
    }

    void setRefreshing(String subscriptionId, boolean on) {
        if (on) refreshing.add(subscriptionId);
        else refreshing.remove(subscriptionId);
        for (int i = 0; i < rows.size(); i++) {
            Row r = rows.get(i);
            if (r.type == TYPE_GROUP && r.sub != null && r.sub.id.equals(subscriptionId)) notifyItemChanged(i);
        }
    }

    /** Пересобрать список из хранилища. */
    void submit(ServerStore store, String activeUrl) {
        this.store = store;
        this.activeUrl = activeUrl == null ? "" : activeUrl;
        rows.clear();
        rows.add(new Row(TYPE_PANEL, null, null));
        List<ServerStore.Server> own = store.serversOf(null);
        List<ServerStore.Subscription> subs = store.subscriptions();
        if (own.isEmpty() && subs.isEmpty()) {
            rows.add(new Row(TYPE_EMPTY, null, null));
        }
        if (!own.isEmpty()) {
            rows.add(new Row(TYPE_GROUP, null, null));
            for (ServerStore.Server s : own) rows.add(new Row(TYPE_SERVER, s, null));
        }
        for (ServerStore.Subscription sub : subs) {
            rows.add(new Row(TYPE_GROUP, null, sub));
            for (ServerStore.Server s : store.serversOf(sub.id)) rows.add(new Row(TYPE_SERVER, s, sub));
        }
        notifyDataSetChanged();
    }

    /** Обновить один сервер (результат пинга). */
    void notifyServer(String serverId) {
        for (int i = 0; i < rows.size(); i++) {
            Row r = rows.get(i);
            if (r.type == TYPE_SERVER && r.server.id.equals(serverId)) notifyItemChanged(i);
        }
    }

    @Override
    public int getItemViewType(int position) {
        return rows.get(position).type;
    }

    @Override
    public int getItemCount() {
        return rows.size();
    }

    @NonNull
    @Override
    public RecyclerView.ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
        LayoutInflater inf = LayoutInflater.from(parent.getContext());
        switch (viewType) {
            case TYPE_PANEL:
                if (panel.getParent() != null) ((ViewGroup) panel.getParent()).removeView(panel);
                return new RecyclerView.ViewHolder(panel) {
                };
            case TYPE_GROUP:
                return new GroupHolder(inf.inflate(R.layout.item_server_group, parent, false));
            case TYPE_EMPTY:
                View empty = inf.inflate(R.layout.item_empty, parent, false);
                ((TextView) empty.findViewById(R.id.emptyText)).setText(R.string.empty_servers);
                return new RecyclerView.ViewHolder(empty) {
                };
            default:
                return new ServerHolder(inf.inflate(R.layout.item_server, parent, false));
        }
    }

    @Override
    public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int position) {
        Row row = rows.get(position);
        if (holder instanceof GroupHolder) ((GroupHolder) holder).bind(row.sub);
        else if (holder instanceof ServerHolder) ((ServerHolder) holder).bind(row.server);
    }

    private final class GroupHolder extends RecyclerView.ViewHolder {
        final TextView title = itemView.findViewById(R.id.groupTitle);
        final TextView info = itemView.findViewById(R.id.groupInfo);
        final TextView announce = itemView.findViewById(R.id.groupAnnounce);
        final LinearProgressIndicator traffic = itemView.findViewById(R.id.groupTraffic);
        final ProgressBar progress = itemView.findViewById(R.id.groupProgress);
        final ImageButton refresh = itemView.findViewById(R.id.groupRefresh);
        final ImageButton more = itemView.findViewById(R.id.groupMore);

        GroupHolder(View v) {
            super(v);
        }

        void bind(ServerStore.Subscription sub) {
            if (sub == null) {
                title.setText(R.string.my_servers);
                info.setVisibility(View.GONE);
                announce.setVisibility(View.GONE);
                traffic.setVisibility(View.GONE);
                progress.setVisibility(View.GONE);
                refresh.setVisibility(View.GONE);
                more.setVisibility(View.GONE);
                return;
            }
            title.setText(sub.title());
            List<String> parts = new ArrayList<>();
            if (sub.total > 0) {
                parts.add(Format.bytes(sub.upload + sub.download) + " из " + Format.bytes(sub.total));
            } else if (sub.upload + sub.download > 0) {
                parts.add("использовано " + Format.bytes(sub.upload + sub.download));
            }
            if (sub.expire > 0) parts.add("до " + Format.date(sub.expire));
            parts.add(Format.ago(sub.updatedAt));
            if (sub.unsupported > 0) parts.add("не поддерживаются: " + sub.unsupported);
            boolean error = !sub.error.isEmpty();
            info.setText(error ? "Ошибка: " + sub.error : android.text.TextUtils.join(" · ", parts));
            info.setTextColor(ContextCompat.getColor(itemView.getContext(),
                    error ? R.color.status_error : R.color.text_secondary));
            info.setVisibility(View.VISIBLE);

            if (sub.total > 0) {
                traffic.setVisibility(View.VISIBLE);
                traffic.setMax(1000);
                traffic.setProgress((int) Math.min(1000, (sub.upload + sub.download) * 1000 / sub.total));
            } else {
                traffic.setVisibility(View.GONE);
            }
            announce.setVisibility(sub.announce.isEmpty() ? View.GONE : View.VISIBLE);
            announce.setText(sub.announce);

            boolean busy = refreshing.contains(sub.id);
            progress.setVisibility(busy ? View.VISIBLE : View.GONE);
            refresh.setVisibility(busy ? View.GONE : View.VISIBLE);
            more.setVisibility(View.VISIBLE);
            refresh.setOnClickListener(v -> callbacks.onGroupRefresh(sub));
            more.setOnClickListener(v -> callbacks.onGroupMore(sub, v));
        }
    }

    private final class ServerHolder extends RecyclerView.ViewHolder {
        final MaterialCardView card = (MaterialCardView) itemView;
        final View radio = itemView.findViewById(R.id.radio);
        final ImageView check = itemView.findViewById(R.id.radioCheck);
        final TextView name = itemView.findViewById(R.id.serverName);
        final TextView info = itemView.findViewById(R.id.serverInfo);
        final TextView ping = itemView.findViewById(R.id.serverPing);
        final ImageButton more = itemView.findViewById(R.id.serverMore);

        ServerHolder(View v) {
            super(v);
        }

        void bind(ServerStore.Server s) {
            boolean selected = s.url.equals(activeUrl);
            radio.setBackgroundResource(selected ? R.drawable.bg_radio_on : R.drawable.bg_radio_off);
            check.setVisibility(selected ? View.VISIBLE : View.GONE);
            card.setStrokeColor(ContextCompat.getColor(itemView.getContext(),
                    selected ? R.color.accent : R.color.outline));
            card.setStrokeWidth(dp(selected ? 2 : 1));
            name.setText(s.title());
            info.setText(s.subtitle());

            Long ms = store == null ? null : store.ping(s.id);
            if (ms == null) {
                ping.setVisibility(View.GONE);
            } else {
                ping.setVisibility(View.VISIBLE);
                ping.setText(ms < 0 ? "нет ответа" : ms + " мс");
                ping.setTextColor(ContextCompat.getColor(itemView.getContext(), Format.pingColor(ms)));
            }
            card.setOnClickListener(v -> callbacks.onServerClick(s));
            more.setOnClickListener(v -> callbacks.onServerMore(s, v));
        }

        private int dp(int v) {
            return Math.round(v * itemView.getResources().getDisplayMetrics().density);
        }
    }
}
