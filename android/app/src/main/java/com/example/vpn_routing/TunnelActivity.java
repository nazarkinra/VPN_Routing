package com.example.vpn_routing;

import android.content.Context;
import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.res.ColorStateList;
import android.os.Bundle;
import android.text.InputType;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.ImageView;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.content.ContextCompat;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.appbar.MaterialToolbar;
import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import com.google.android.material.switchmaterial.SwitchMaterial;
import com.google.android.material.textfield.TextInputEditText;
import com.google.android.material.textfield.TextInputLayout;

import java.text.Collator;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * Настройка дополнительного туннеля: сервер, приложения (целиком через туннель) и домены или подсети.
 * Изменения сохраняются сразу, VPN перезапускается с новым конфигом при уходе с экрана.
 */
public class TunnelActivity extends AppCompatActivity implements RoutingVpnService.Listener {

    public static final String EXTRA_ID = "tunnel_id";

    private TunnelStore store;
    private ServerStore servers;
    private Settings settings;
    private String tunnelId;
    private TunnelStore.Tunnel tunnel;
    private boolean changed;
    private MaterialToolbar toolbar;
    private final Adapter adapter = new Adapter();

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_tunnel);
        store = TunnelStore.get(this);
        servers = ServerStore.get(this);
        settings = new Settings(this);
        tunnelId = getIntent().getStringExtra(EXTRA_ID);
        if (tunnelId == null || store.find(tunnelId) == null) {
            finish();
            return;
        }
        toolbar = findViewById(R.id.toolbar);
        toolbar.setNavigationOnClickListener(v -> finish());
        toolbar.inflateMenu(R.menu.tunnel);
        toolbar.setOnMenuItemClickListener(item -> {
            if (item.getItemId() == R.id.action_rename) rename();
            else if (item.getItemId() == R.id.action_delete) delete();
            return true;
        });
        RecyclerView list = findViewById(R.id.list);
        list.setLayoutManager(new LinearLayoutManager(this));
        list.setAdapter(adapter);
    }

    @Override
    protected void onStart() {
        super.onStart();
        RoutingVpnService.addListener(this);
    }

    @Override
    protected void onStop() {
        RoutingVpnService.removeListener(this);
        super.onStop();
    }

    @Override
    protected void onResume() {
        super.onResume();
        render(); // вернулись с выбора приложений
    }

    @Override
    protected void onPause() {
        super.onPause();
        // Конфиг ядра пересобирается один раз — после всех изменений на экране
        if (changed) {
            changed = false;
            RoutingVpnService.send(this, RoutingVpnService.ACTION_RELOAD_TUNNELS);
        }
    }

    @Override
    public void onStateChanged(RoutingVpnService.State state, String message) {
        render();
    }

    private void save() {
        store.update(tunnel);
        changed = true;
        render();
    }

    // ---------- Действия ----------

    private void rename() {
        prompt(getString(R.string.tunnel_rename), getString(R.string.tunnel_name), tunnel.name, null, text -> {
            tunnel.name = text;
            save();
        });
    }

    private void delete() {
        new MaterialAlertDialogBuilder(this)
                .setMessage("Удалить туннель «" + tunnel.name + "»? Его приложения и домены пойдут через основной VPN.")
                .setPositiveButton(R.string.remove, (d, w) -> {
                    store.remove(tunnel.id);
                    RoutingVpnService.send(this, RoutingVpnService.ACTION_RELOAD_TUNNELS);
                    changed = false;
                    finish();
                })
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void chooseServer() {
        List<ServerStore.Server> all = servers.servers();
        if (all.isEmpty()) {
            toast("Список серверов пуст: добавьте серверы на вкладке VPN.");
            return;
        }
        String main = settings.getVlessUrl();
        CharSequence[] items = new CharSequence[all.size()];
        int checked = -1;
        for (int i = 0; i < all.size(); i++) {
            ServerStore.Server s = all.get(i);
            items[i] = s.title() + (s.url.equals(main) ? "  (основной)" : "");
            if (s.url.equals(tunnel.serverUrl)) checked = i;
        }
        new MaterialAlertDialogBuilder(this).setTitle(R.string.tunnel_server)
                .setSingleChoiceItems(items, checked, (d, which) -> {
                    d.dismiss();
                    ServerStore.Server s = all.get(which);
                    if (s.url.equals(tunnel.serverUrl)) return;
                    tunnel.serverUrl = s.url;
                    save();
                    if (s.url.equals(main)) toast("Это сервер основного VPN: трафик туннеля пойдёт туда же.");
                    List<ServerStore.Server> one = new ArrayList<>();
                    one.add(s);
                    Pinger.ping(this, servers, one, (id, ms, done) -> render());
                })
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void chooseApps() {
        startActivity(new Intent(this, AppsActivity.class).putExtra(AppsActivity.EXTRA_TUNNEL_ID, tunnel.id));
    }

    private void removeApp(String pkg) {
        tunnel.apps.remove(pkg);
        save();
    }

    private void addDomain() {
        prompt(getString(R.string.tunnel_domain_add), getString(R.string.add_site_hint), "",
                "Сайт и все его поддомены пойдут через туннель «" + tunnel.name + "» из любых приложений. "
                        + "Можно вставить адрес страницы целиком или IP-подсеть.", raw -> {
                    String value;
                    try {
                        TunnelStore.Tunnel other = store.ownerOfDomain(Exceptions.normalize(raw), tunnel.id);
                        if (other != null) {
                            alert("Не добавлено", "Этот адрес уже идёт через туннель «" + other.name
                                    + "». Уберите его там, если хотите перенести сюда.");
                            return;
                        }
                        value = store.addDomain(tunnel.id, raw);
                    } catch (IllegalArgumentException e) {
                        alert("Не удалось добавить", e.getMessage());
                        return;
                    }
                    if (value == null) {
                        toast("Этот адрес уже есть в туннеле.");
                        return;
                    }
                    tunnel = store.find(tunnelId);
                    changed = true;
                    render();
                });
    }

    private void removeDomain(String value) {
        store.removeDomain(tunnel.id, value);
        tunnel = store.find(tunnelId);
        changed = true;
        render();
    }

    private void setEnabled(boolean on) {
        if (on && tunnel.link() == null) {
            toast("Сначала выберите сервер туннеля.");
            render();
            return;
        }
        tunnel.enabled = on;
        save();
    }

    // ---------- Отрисовка ----------

    private void render() {
        if (tunnelId == null) return;
        TunnelStore.Tunnel fresh = store.find(tunnelId);
        if (fresh == null) {
            finish();
            return;
        }
        tunnel = fresh;
        int number = store.number(tunnelId) + 1; // 1 — основной VPN
        toolbar.setTitle(tunnel.name);
        // Номер нужен, только если название его не содержит («Туннель 2»)
        toolbar.setSubtitle(tunnel.name.equals("Туннель " + number) ? "Дополнительный туннель"
                : "Туннель " + number);

        List<Row> rows = new ArrayList<>();
        // Включение и состояние
        Row power = new Row(R.drawable.ic_power, getString(R.string.tunnel_enabled), statusText());
        power.iconTint = tunnel.enabled ? TunnelsFragment.color(number) : 0;
        power.switchOn = tunnel.enabled;
        power.onSwitch = this::setEnabled;
        group(rows, null, null, single(power));

        // Сервер
        ServerStore.Server s = tunnel.serverUrl.isEmpty() ? null : servers.findByUrl(tunnel.serverUrl);
        ProxyLink link = tunnel.link();
        Row server;
        if (s != null) {
            server = new Row(R.drawable.ic_dns, s.title(), s.subtitle());
            Long ms = servers.ping(s.id);
            if (ms != null) {
                server.chip = ms < 0 ? "нет ответа" : ms + " мс";
                server.chipColor = Format.pingColor(ms);
            }
        } else if (link != null) {
            server = new Row(R.drawable.ic_dns, link.server, "Сервера больше нет в списке — выберите другой");
        } else {
            server = new Row(R.drawable.ic_dns, getString(R.string.tunnel_server_none),
                    getString(R.string.tunnel_server_choose));
        }
        server.actionIcon = R.drawable.ic_arrow_forward;
        server.onClick = this::chooseServer;
        server.onAction = this::chooseServer;
        group(rows, getString(R.string.tunnel_server), null, single(server));

        // Приложения
        List<Row> apps = new ArrayList<>();
        Row pick = new Row(R.drawable.ic_apps, getString(R.string.tunnel_apps_choose),
                getString(R.string.tunnel_apps_hint));
        pick.actionIcon = R.drawable.ic_arrow_forward;
        pick.onClick = this::chooseApps;
        pick.onAction = this::chooseApps;
        apps.add(pick);
        PackageManager pm = getPackageManager();
        List<Row> appRows = new ArrayList<>();
        for (String pkg : tunnel.apps) {
            String label = pkg;
            boolean installed = true;
            try {
                ApplicationInfo info = pm.getApplicationInfo(pkg, 0);
                label = String.valueOf(pm.getApplicationLabel(info));
            } catch (PackageManager.NameNotFoundException e) {
                installed = false;
            }
            Row r = new Row(0, label, installed ? pkg : pkg + " · не установлено");
            r.appPkg = pkg;
            r.actionIcon = R.drawable.ic_delete;
            r.onAction = () -> removeApp(pkg);
            appRows.add(r);
        }
        Collator collator = Collator.getInstance(new Locale("ru"));
        appRows.sort((a, b) -> collator.compare(a.title, b.title));
        apps.addAll(appRows);
        group(rows, getString(R.string.tunnel_apps), String.valueOf(tunnel.apps.size()), apps);

        // Домены и подсети
        List<Row> domains = new ArrayList<>();
        Row add = new Row(R.drawable.ic_add, getString(R.string.tunnel_domain_add),
                getString(R.string.tunnel_domain_hint));
        add.iconTint = ContextCompat.getColor(this, R.color.accent);
        add.onClick = this::addDomain;
        domains.add(add);
        for (String d : tunnel.domains) {
            boolean cidr = Exceptions.isCidr(d);
            Row r = new Row(cidr ? R.drawable.ic_dns : R.drawable.ic_globe, DirectAdapter.display(d),
                    cidr ? "IP-подсеть" : "сайт и все поддомены");
            r.actionIcon = R.drawable.ic_delete;
            r.onAction = () -> removeDomain(d);
            domains.add(r);
        }
        group(rows, getString(R.string.tunnel_domains), String.valueOf(tunnel.domains.size()), domains);

        Row note = new Row(0, null, getString(R.string.tunnel_note));
        note.type = TYPE_NOTE;
        rows.add(note);
        adapter.submit(rows);
    }

    /** Состояние туннеля под переключателем. */
    private String statusText() {
        if (!tunnel.enabled) return "Выключен: приложения и домены идут через основной VPN";
        if (tunnel.link() == null) return "Выберите сервер";
        if (tunnel.apps.isEmpty() && tunnel.domains.isEmpty()) return "Добавьте приложения или домены";
        if (!RoutingVpnService.isMainActive()) return "Заработает, когда включится основной VPN";
        RoutingVpnService.TunnelStatus st = RoutingVpnService.getTunnelStatus(tunnel.id);
        return st == null ? "Применится после выхода с экрана" : st.text;
    }

    private static List<Row> single(Row r) {
        List<Row> list = new ArrayList<>();
        list.add(r);
        return list;
    }

    private void group(List<Row> rows, String title, String count, List<Row> items) {
        if (title != null) {
            Row section = new Row(0, title, count);
            section.type = TYPE_SECTION;
            rows.add(section);
        } else {
            Row spacer = new Row(0, null, null);
            spacer.type = TYPE_SPACER;
            rows.add(spacer);
        }
        for (int i = 0; i < items.size(); i++) {
            items.get(i).first = i == 0;
            items.get(i).last = i == items.size() - 1;
            rows.add(items.get(i));
        }
    }

    // ---------- Список ----------

    private static final int TYPE_ROW = 0;
    private static final int TYPE_SECTION = 1;
    private static final int TYPE_NOTE = 2;
    private static final int TYPE_SPACER = 3;

    interface Toggle {
        void set(boolean on);
    }

    private static final class Row {
        int type = TYPE_ROW;
        final int icon;
        final String title;
        final String subtitle;
        String appPkg;
        int iconTint;
        String chip;
        int chipColor;
        int actionIcon;
        Runnable onClick;
        Runnable onAction;
        Boolean switchOn;
        Toggle onSwitch;
        boolean first, last;

        Row(int icon, String title, String subtitle) {
            this.icon = icon;
            this.title = title;
            this.subtitle = subtitle;
        }
    }

    private final class Adapter extends RecyclerView.Adapter<RecyclerView.ViewHolder> {
        private final List<Row> rows = new ArrayList<>();

        void submit(List<Row> list) {
            rows.clear();
            rows.addAll(list);
            notifyDataSetChanged();
        }

        @Override
        public int getItemCount() {
            return rows.size();
        }

        @Override
        public int getItemViewType(int position) {
            return rows.get(position).type;
        }

        @NonNull
        @Override
        public RecyclerView.ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
            LayoutInflater inf = LayoutInflater.from(parent.getContext());
            View v;
            if (viewType == TYPE_SECTION) {
                v = inf.inflate(R.layout.item_section, parent, false);
            } else if (viewType == TYPE_NOTE || viewType == TYPE_SPACER) {
                TextView t = new TextView(parent.getContext(), null, 0, R.style.Caption);
                float d = parent.getResources().getDisplayMetrics().density;
                int pad = Math.round(4 * d);
                t.setPadding(pad, viewType == TYPE_NOTE ? Math.round(16 * d) : Math.round(8 * d), pad, 0);
                t.setTextSize(12);
                t.setLineSpacing(0, 1.15f);
                t.setLayoutParams(new RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT,
                        ViewGroup.LayoutParams.WRAP_CONTENT));
                v = t;
            } else {
                v = inf.inflate(R.layout.item_tunnel_row, parent, false);
            }
            return new RecyclerView.ViewHolder(v) {
            };
        }

        @Override
        public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int position) {
            Row row = rows.get(position);
            View v = holder.itemView;
            Context c = v.getContext();
            if (row.type == TYPE_SECTION) {
                ((TextView) v.findViewById(R.id.sectionTitle)).setText(row.title);
                ((TextView) v.findViewById(R.id.sectionCount)).setText(row.subtitle == null ? "" : row.subtitle);
                return;
            }
            if (row.type == TYPE_NOTE || row.type == TYPE_SPACER) {
                ((TextView) v).setText(row.subtitle);
                return;
            }
            ImageView icon = v.findViewById(R.id.rowIcon);
            ImageView appIcon = v.findViewById(R.id.rowAppIcon);
            View iconBg = v.findViewById(R.id.rowIconBg);
            if (row.appPkg != null) {
                appIcon.setVisibility(View.VISIBLE);
                icon.setVisibility(View.GONE);
                iconBg.setVisibility(View.GONE);
                try {
                    appIcon.setImageDrawable(c.getPackageManager().getApplicationIcon(row.appPkg));
                } catch (PackageManager.NameNotFoundException e) {
                    appIcon.setImageDrawable(c.getPackageManager().getDefaultActivityIcon());
                }
            } else {
                appIcon.setVisibility(View.GONE);
                icon.setVisibility(View.VISIBLE);
                iconBg.setVisibility(View.VISIBLE);
                icon.setImageResource(row.icon);
                if (row.iconTint != 0) {
                    icon.setImageTintList(ColorStateList.valueOf(ContextCompat.getColor(c, R.color.white)));
                    iconBg.setBackgroundTintList(ColorStateList.valueOf(row.iconTint));
                } else {
                    icon.setImageTintList(ColorStateList.valueOf(ContextCompat.getColor(c, R.color.text_secondary)));
                    iconBg.setBackgroundTintList(ColorStateList.valueOf(
                            ContextCompat.getColor(c, R.color.surface_variant)));
                }
            }
            ((TextView) v.findViewById(R.id.rowTitle)).setText(row.title);
            TextView sub = v.findViewById(R.id.rowSubtitle);
            sub.setText(row.subtitle);
            sub.setVisibility(row.subtitle == null || row.subtitle.isEmpty() ? View.GONE : View.VISIBLE);

            TextView chip = v.findViewById(R.id.rowChip);
            chip.setVisibility(row.chip == null ? View.GONE : View.VISIBLE);
            if (row.chip != null) {
                chip.setText(row.chip);
                chip.setTextColor(ContextCompat.getColor(c, row.chipColor));
            }

            View action = v.findViewById(R.id.rowAction);
            action.setVisibility(row.actionIcon == 0 ? View.GONE : View.VISIBLE);
            if (row.actionIcon != 0) {
                ((ImageView) action).setImageResource(row.actionIcon);
                action.setOnClickListener(x -> row.onAction.run());
                action.setContentDescription(row.actionIcon == R.drawable.ic_delete ? getString(R.string.remove)
                        : row.title);
            }

            SwitchMaterial sw = v.findViewById(R.id.rowSwitch);
            sw.setOnCheckedChangeListener(null);
            sw.setVisibility(row.switchOn == null ? View.GONE : View.VISIBLE);
            if (row.switchOn != null) {
                sw.setChecked(row.switchOn);
                sw.setOnCheckedChangeListener((b, on) -> row.onSwitch.set(on));
            }

            if (row.onClick != null) v.setOnClickListener(x -> row.onClick.run());
            else if (row.switchOn != null) v.setOnClickListener(x -> sw.toggle());
            else v.setOnClickListener(null);
            v.setClickable(row.onClick != null || row.switchOn != null);
            v.setBackground(GroupBackground.create(c, row.first, row.last));
        }
    }

    // ---------- Вспомогательное ----------

    interface TextResult {
        void accept(String text);
    }

    private void prompt(String title, String hint, String value, String message, TextResult result) {
        View frame = getLayoutInflater().inflate(R.layout.dialog_input, null, false);
        TextInputLayout layout = frame.findViewById(R.id.inputLayout);
        TextInputEditText input = frame.findViewById(R.id.input);
        layout.setHint(hint);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        input.setSingleLine(true);
        input.setText(value);
        MaterialAlertDialogBuilder b = new MaterialAlertDialogBuilder(this).setTitle(title).setView(frame)
                .setPositiveButton(android.R.string.ok, (d, w) -> {
                    String text = input.getText() == null ? "" : input.getText().toString().trim();
                    if (!text.isEmpty()) result.accept(text);
                })
                .setNegativeButton(android.R.string.cancel, null);
        if (message != null) b.setMessage(message);
        b.show();
    }

    private void alert(String title, String message) {
        new MaterialAlertDialogBuilder(this).setTitle(title).setMessage(message)
                .setPositiveButton(android.R.string.ok, null).show();
    }

    private void toast(String text) {
        Toast.makeText(this, text, Toast.LENGTH_SHORT).show();
    }
}
