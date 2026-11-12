package com.example.vpn_routing;

import android.content.res.ColorStateList;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.ImageButton;
import android.widget.ImageView;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.core.content.ContextCompat;
import androidx.recyclerview.widget.RecyclerView;

import java.net.IDN;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/** Списки адресов, которые идут напрямую: свои исключения и встроенные правила. */
final class DirectAdapter extends RecyclerView.Adapter<RecyclerView.ViewHolder> {

    interface Callbacks {
        /** Удалить своё исключение. */
        void onRemove(String value);

        /** Отключить встроенное правило (домен, как в RoutingRules); title — как он показан. */
        void onRemoveBuiltin(String domain, String title);

        /** Открыть маршрутизацию по приложениям. */
        void onApps();
    }

    static final int TYPE_INTRO = 0;
    static final int TYPE_SECTION = 1;
    static final int TYPE_ENTRY = 2;
    static final int TYPE_EMPTY = 3;
    /** Карточка «Приложения»: открывает маршрутизацию по приложениям. */
    static final int TYPE_APPS = 4;

    /** Строка списка. */
    static final class Row {
        final int type;
        final String title;
        final String subtitle;
        final int icon;
        /** Значение, которое можно удалить; null — закреплённая запись (сети, МГТУ). */
        final String removable;
        /** Удаление отключает встроенное правило, а не своё исключение. */
        boolean builtin;
        /** Подсветка значка (МГТУ — синий, свои — акцент). */
        final int tint;
        /** Скругление: первая и последняя строки группы. */
        boolean first, last;

        Row(int type, String title, String subtitle, int icon, String removable, int tint) {
            this.type = type;
            this.title = title;
            this.subtitle = subtitle;
            this.icon = icon;
            this.removable = removable;
            this.tint = tint;
        }
    }

    private final Callbacks callbacks;
    private final List<Row> rows = new ArrayList<>();

    DirectAdapter(Callbacks callbacks) {
        this.callbacks = callbacks;
    }

    /** Пересобрать список; query — строка поиска (пустая — показать всё). */
    void submit(List<String> userItems, boolean bmstuEnabled, List<String> directDomains,
                List<String> tunnelDomains, String appsTitle, String appsSubtitle, String query) {
        String q = query == null ? "" : query.trim().toLowerCase(Locale.ROOT);
        rows.clear();
        if (q.isEmpty()) {
            rows.add(new Row(TYPE_INTRO, null, null, 0, null, 0));
            rows.add(new Row(TYPE_SECTION, null, "", R.string.apps_section, null, 0));
            Row apps = new Row(TYPE_APPS, appsTitle, appsSubtitle, R.drawable.ic_apps, null, R.color.accent);
            apps.first = apps.last = true;
            rows.add(apps);
        }

        List<Row> user = new ArrayList<>();
        for (String item : userItems) {
            user.add(entry(display(item), describeUser(item), Exceptions.isCidr(item) ? R.drawable.ic_dns
                    : item.startsWith(".") ? R.drawable.ic_globe : R.drawable.ic_link, item, R.color.accent));
        }
        addSection(R.string.direct_user, user, q, q.isEmpty() ? R.string.direct_user_empty : 0);

        List<Row> bmstu = new ArrayList<>();
        bmstu.add(entry(XrayConfig.BMSTU_DOMAIN, bmstuEnabled
                ? "через корпоративный VPN; пока он не подключён — напрямую"
                : "VPN МГТУ выключен: идёт напрямую, как остальные .ru", R.drawable.ic_school, null, R.color.corp));
        addSection(R.string.direct_bmstu, bmstu, q, 0);

        List<Row> zones = new ArrayList<>();
        List<Row> sites = new ArrayList<>();
        for (String d : RoutingRules.compact(directDomains)) {
            if (d.contains(".")) {
                sites.add(builtin(display(d), "сайт и все поддомены", R.drawable.ic_link, d));
            } else {
                zones.add(builtin("." + display(d), "все сайты в зоне", R.drawable.ic_globe, d));
            }
        }
        for (String d : tunnelDomains) {
            sites.add(builtin(d, "Cloudflare Tunnel", R.drawable.ic_link, d));
        }
        addSection(R.string.direct_zones, zones, q, 0);
        addSection(R.string.direct_sites, sites, q, 0);

        List<Row> nets = new ArrayList<>();
        nets.add(entry("Локальная сеть", "роутер, принтеры, NAS: 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12",
                R.drawable.ic_dns, null, 0));
        nets.add(entry("Служебные адреса", "127.0.0.0/8, 169.254.0.0/16, 100.64.0.0/10, multicast, IPv6 ULA",
                R.drawable.ic_dns, null, 0));
        nets.add(entry("VPN-протоколы", "порты " + RoutingRules.DIRECT_PORTS.replace(",", ", "),
                R.drawable.ic_shield, null, 0));
        addSection(R.string.direct_networks, nets, q, 0);

        if (!q.isEmpty() && rows.isEmpty()) {
            rows.add(new Row(TYPE_EMPTY, null, null, R.string.nothing_found, null, 0));
        }
        notifyDataSetChanged();
    }

    private static Row entry(String title, String subtitle, int icon, String removable, int tint) {
        return new Row(TYPE_ENTRY, title, subtitle, icon, removable, tint);
    }

    /** Встроенное правило: можно удалить (отключить), вернуть — через меню «Вернуть встроенные». */
    private static Row builtin(String title, String subtitle, int icon, String domain) {
        Row r = new Row(TYPE_ENTRY, title, subtitle, icon, domain, 0);
        r.builtin = true;
        return r;
    }

    private void addSection(int titleRes, List<Row> items, String q, int emptyRes) {
        List<Row> matched = new ArrayList<>();
        for (Row r : items) {
            if (q.isEmpty() || r.title.toLowerCase(Locale.ROOT).contains(q)
                    || r.subtitle.toLowerCase(Locale.ROOT).contains(q)) {
                matched.add(r);
            }
        }
        if (matched.isEmpty() && emptyRes == 0) return;
        rows.add(new Row(TYPE_SECTION, null, matched.isEmpty() ? "" : String.valueOf(matched.size()),
                titleRes, null, 0));
        if (matched.isEmpty()) {
            rows.add(new Row(TYPE_EMPTY, null, null, emptyRes, null, 0));
            return;
        }
        matched.get(0).first = true;
        matched.get(matched.size() - 1).last = true;
        rows.addAll(matched);
    }

    /** Кириллические домены показываются по-русски. */
    static String display(String value) {
        if (Exceptions.isCidr(value)) {
            return value.endsWith("/32") || value.endsWith("/128") ? value.substring(0, value.lastIndexOf('/')) : value;
        }
        String prefix = value.startsWith(".") ? "." : "";
        try {
            return prefix + IDN.toUnicode(value.substring(prefix.length()));
        } catch (IllegalArgumentException e) {
            return value;
        }
    }

    private static String describeUser(String value) {
        if (Exceptions.isCidr(value)) {
            return value.endsWith("/32") || value.endsWith("/128") ? "IP-адрес" : "подсеть " + value;
        }
        return value.startsWith(".") ? "доменная зона: все сайты в ней" : "сайт и все поддомены";
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
            case TYPE_INTRO: {
                View v = inf.inflate(R.layout.item_empty, parent, false);
                TextView t = v.findViewById(R.id.emptyText);
                t.setText(R.string.direct_intro);
                t.setGravity(android.view.Gravity.START);
                ViewGroup.MarginLayoutParams lp = (ViewGroup.MarginLayoutParams) v.getLayoutParams();
                lp.topMargin = Math.round(4 * parent.getResources().getDisplayMetrics().density);
                return new RecyclerView.ViewHolder(v) {
                };
            }
            case TYPE_SECTION:
                return new SectionHolder(inf.inflate(R.layout.item_section, parent, false));
            case TYPE_EMPTY: {
                View v = inf.inflate(R.layout.item_empty, parent, false);
                ViewGroup.MarginLayoutParams lp = (ViewGroup.MarginLayoutParams) v.getLayoutParams();
                lp.topMargin = 0;
                return new EmptyHolder(v);
            }
            default:
                return new EntryHolder(inf.inflate(R.layout.item_direct, parent, false));
        }
    }

    @Override
    public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int position) {
        Row row = rows.get(position);
        if (holder instanceof SectionHolder) {
            ((SectionHolder) holder).title.setText(row.icon);
            ((SectionHolder) holder).count.setText(row.subtitle);
        } else if (holder instanceof EmptyHolder) {
            ((EmptyHolder) holder).text.setText(row.icon);
        } else if (holder instanceof EntryHolder) {
            ((EntryHolder) holder).bind(row);
        }
    }

    private static final class SectionHolder extends RecyclerView.ViewHolder {
        final TextView title = itemView.findViewById(R.id.sectionTitle);
        final TextView count = itemView.findViewById(R.id.sectionCount);

        SectionHolder(View v) {
            super(v);
        }
    }

    private static final class EmptyHolder extends RecyclerView.ViewHolder {
        final TextView text = itemView.findViewById(R.id.emptyText);

        EmptyHolder(View v) {
            super(v);
        }
    }

    private final class EntryHolder extends RecyclerView.ViewHolder {
        final View iconBg = itemView.findViewById(R.id.directIconBg);
        final ImageView icon = itemView.findViewById(R.id.directIcon);
        final TextView title = itemView.findViewById(R.id.directTitle);
        final TextView subtitle = itemView.findViewById(R.id.directSubtitle);
        final ImageButton action = itemView.findViewById(R.id.directAction);
        final ImageView lock = itemView.findViewById(R.id.directLock);

        EntryHolder(View v) {
            super(v);
        }

        void bind(Row row) {
            title.setText(row.title);
            subtitle.setText(row.subtitle);
            icon.setImageResource(row.icon);
            int tint = row.tint != 0 ? ContextCompat.getColor(itemView.getContext(), row.tint)
                    : ContextCompat.getColor(itemView.getContext(), R.color.text_secondary);
            icon.setImageTintList(ColorStateList.valueOf(tint));
            iconBg.setBackgroundTintList(ColorStateList.valueOf(row.tint != 0
                    ? androidx.core.graphics.ColorUtils.setAlphaComponent(tint, 40)
                    : ContextCompat.getColor(itemView.getContext(), R.color.surface_variant)));
            itemView.setBackground(GroupBackground.create(itemView.getContext(), row.first, row.last));
            if (row.type == TYPE_APPS) {
                // Карточка-ссылка: стрелка вместо замка, нажатие открывает экран приложений
                action.setVisibility(View.VISIBLE);
                action.setImageResource(R.drawable.ic_arrow_forward);
                action.setContentDescription(row.title);
                action.setOnClickListener(v -> callbacks.onApps());
                lock.setVisibility(View.GONE);
                itemView.setOnClickListener(v -> callbacks.onApps());
                return;
            }
            itemView.setOnClickListener(null);
            itemView.setClickable(false);
            action.setImageResource(R.drawable.ic_delete);
            action.setContentDescription(itemView.getContext().getString(R.string.remove));
            boolean removable = row.removable != null;
            action.setVisibility(removable ? View.VISIBLE : View.GONE);
            lock.setVisibility(removable ? View.GONE : View.VISIBLE);
            action.setOnClickListener(!removable ? null : row.builtin
                    ? v -> callbacks.onRemoveBuiltin(row.removable, row.title)
                    : v -> callbacks.onRemove(row.removable));
        }
    }
}
