package com.example.vpn_routing;

import android.Manifest;
import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.pm.PermissionInfo;
import android.content.pm.ResolveInfo;
import android.graphics.drawable.Drawable;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.LayoutInflater;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;
import android.widget.CheckBox;
import android.widget.ImageView;
import android.widget.TextView;
import android.widget.Toast;

import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.annotation.NonNull;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.content.ContextCompat;
import androidx.appcompat.widget.SearchView;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.appbar.MaterialToolbar;
import com.google.android.material.button.MaterialButtonToggleGroup;

import java.text.Collator;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Маршрутизация по приложениям (как в Happ): все приложения по правилам VPN, только выбранные по правилам
 * (остальные напрямую) или все, кроме выбранных (выбранные напрямую). У каждого режима свой список.
 * Изменения сохраняются сразу, туннель пересоздаётся при уходе с экрана.
 */
public class AppsActivity extends AppCompatActivity {

    /**
     * Выбор приложений дополнительного туннеля (версия 2.1): режимов нет, отмеченные приложения целиком идут
     * через туннель. Приложение может быть только в одном туннеле — у занятых другим видно, в каком.
     */
    public static final String EXTRA_TUNNEL_ID = "tunnel_id";

    private static final ExecutorService io = Executors.newSingleThreadExecutor();

    /** Установленное приложение с доступом в интернет. */
    static final class App {
        final String pkg;
        final String label;
        /** Системное без значка в меню приложений (по умолчанию скрыто). */
        final boolean hiddenSystem;

        App(String pkg, String label, boolean hiddenSystem) {
            this.pkg = pkg;
            this.label = label;
            this.hiddenSystem = hiddenSystem;
        }
    }

    private Settings settings;
    private String mode;
    private final Set<String> include = new LinkedHashSet<>();
    private final Set<String> exclude = new LinkedHashSet<>();
    /** Режим туннеля: id туннеля, его приложения и где заняты остальные (пакет -> название туннеля). */
    private String tunnelId;
    private final Set<String> tunnelApps = new LinkedHashSet<>();
    private final Map<String, String> otherTunnels = new HashMap<>();
    private boolean changed;
    private boolean showSystem;
    private String query = "";
    private String tunnelName = "";
    private List<App> apps = new ArrayList<>();

    private TextView hint;
    private View progress;
    private Adapter adapter;
    private final Map<String, Drawable> icons = new HashMap<>();
    private final Handler main = new Handler(Looper.getMainLooper());

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_apps);
        settings = new Settings(this);
        mode = settings.getAppsMode();
        include.addAll(settings.getApps(Settings.APPS_INCLUDE));
        exclude.addAll(settings.getApps(Settings.APPS_EXCLUDE));

        MaterialToolbar toolbar = findViewById(R.id.toolbar);
        toolbar.setNavigationOnClickListener(v -> finish());
        toolbar.inflateMenu(R.menu.apps);
        toolbar.setOnMenuItemClickListener(this::onMenu);
        SearchView search = (SearchView) toolbar.getMenu().findItem(R.id.action_search).getActionView();
        search.setQueryHint("Название приложения");
        search.setOnQueryTextListener(new SearchView.OnQueryTextListener() {
            @Override
            public boolean onQueryTextSubmit(String q) {
                return true;
            }

            @Override
            public boolean onQueryTextChange(String q) {
                query = q == null ? "" : q.trim().toLowerCase(Locale.ROOT);
                render();
                return true;
            }
        });

        hint = findViewById(R.id.modeHint);
        progress = findViewById(R.id.progress);
        MaterialButtonToggleGroup group = findViewById(R.id.modeGroup);

        tunnelId = getIntent().getStringExtra(EXTRA_TUNNEL_ID);
        if (tunnelId != null) {
            TunnelStore.Tunnel t = TunnelStore.get(this).find(tunnelId);
            if (t == null) {
                finish();
                return;
            }
            tunnelName = t.name;
            tunnelApps.addAll(t.apps);
            for (TunnelStore.Tunnel other : TunnelStore.get(this).all()) {
                if (other.id.equals(tunnelId)) continue;
                for (String pkg : other.apps) otherTunnels.put(pkg, other.name);
            }
            toolbar.setTitle(getString(R.string.apps_title) + ": " + t.name);
            group.setVisibility(View.GONE);
            RecyclerView list = findViewById(R.id.list);
            list.setLayoutManager(new LinearLayoutManager(this));
            adapter = new Adapter();
            list.setAdapter(adapter);
            render();
            if (needsMiuiAppsPermission()) appsPermission.launch(MIUI_GET_INSTALLED_APPS);
            else load();
            return;
        }
        group.check(Settings.APPS_INCLUDE.equals(mode) ? R.id.modeInclude
                : Settings.APPS_EXCLUDE.equals(mode) ? R.id.modeExclude : R.id.modeAll);
        group.addOnButtonCheckedListener((g, id, checked) -> {
            if (!checked) return;
            mode = id == R.id.modeInclude ? Settings.APPS_INCLUDE
                    : id == R.id.modeExclude ? Settings.APPS_EXCLUDE : Settings.APPS_ALL;
            save();
            render();
        });

        RecyclerView list = findViewById(R.id.list);
        list.setLayoutManager(new LinearLayoutManager(this));
        adapter = new Adapter();
        list.setAdapter(adapter);
        render();
        if (needsMiuiAppsPermission()) appsPermission.launch(MIUI_GET_INSTALLED_APPS);
        else load();
    }

    @Override
    protected void onPause() {
        super.onPause();
        // Список приложений задаётся при создании туннеля: пересоздать его один раз, после всех отметок
        if (changed) {
            changed = false;
            RoutingVpnService.send(this, tunnelId != null ? RoutingVpnService.ACTION_RELOAD_TUNNELS
                    : RoutingVpnService.ACTION_RELOAD_APPS);
        }
    }

    private boolean onMenu(MenuItem item) {
        if (item.getItemId() == R.id.action_system) {
            showSystem = !item.isChecked();
            item.setChecked(showSystem);
            render();
        } else if (item.getItemId() == R.id.action_clear) {
            Set<String> set = selectedSet();
            if (set != null) {
                set.clear();
                save();
                render();
            }
        }
        return true;
    }

    /**
     * MIUI / HyperOS (Xiaomi): список установленных приложений — отдельное разрешение «Доступ к списку
     * приложений». Без него система отдаёт только часть приложений, и в списке не хватает установленных.
     */
    private static final String MIUI_GET_INSTALLED_APPS = "com.android.permission.GET_INSTALLED_APPS";

    private final ActivityResultLauncher<String> appsPermission = registerForActivityResult(
            new ActivityResultContracts.RequestPermission(), granted -> {
                if (!granted) {
                    Toast.makeText(this, "Без доступа к списку приложений система показывает не все "
                            + "установленные приложения", Toast.LENGTH_LONG).show();
                }
                load();
            });

    private boolean needsMiuiAppsPermission() {
        try {
            PermissionInfo p = getPackageManager().getPermissionInfo(MIUI_GET_INSTALLED_APPS, 0);
            return p != null && "com.lbe.security.miui".equals(p.packageName)
                    && ContextCompat.checkSelfPermission(this, MIUI_GET_INSTALLED_APPS)
                    != PackageManager.PERMISSION_GRANTED;
        } catch (PackageManager.NameNotFoundException e) {
            return false; // не Xiaomi: список доступен по QUERY_ALL_PACKAGES
        }
    }

    /**
     * Список установленных приложений — в фоне: подписи читаются долго. Показываются все приложения со
     * значком в меню (даже без разрешения INTERNET: например, Gemini ходит в сеть через приложение Google),
     * а без значка — только с доступом в интернет.
     */
    private void load() {
        io.execute(() -> {
            PackageManager pm = getPackageManager();
            Set<String> launchable = new HashSet<>();
            Intent launcherIntent = new Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER);
            for (ResolveInfo ri : pm.queryIntentActivities(launcherIntent, 0)) {
                launchable.add(ri.activityInfo.packageName);
            }
            List<App> result = new ArrayList<>();
            for (ApplicationInfo info : pm.getInstalledApplications(0)) {
                if (info.packageName.equals(getPackageName())) continue;
                boolean launcher = launchable.contains(info.packageName)
                        || pm.getLaunchIntentForPackage(info.packageName) != null;
                if (!launcher && pm.checkPermission(Manifest.permission.INTERNET, info.packageName)
                        != PackageManager.PERMISSION_GRANTED) continue;
                boolean system = (info.flags & ApplicationInfo.FLAG_SYSTEM) != 0;
                result.add(new App(info.packageName, String.valueOf(pm.getApplicationLabel(info)), system && !launcher));
            }
            Collator collator = Collator.getInstance(new Locale("ru"));
            result.sort((a, b) -> collator.compare(a.label, b.label));
            main.post(() -> {
                if (isFinishing()) return;
                apps = result;
                progress.setVisibility(View.GONE);
                render();
            });
        });
    }

    private Set<String> selectedSet() {
        if (tunnelId != null) return tunnelApps;
        return Settings.APPS_INCLUDE.equals(mode) ? include : Settings.APPS_EXCLUDE.equals(mode) ? exclude : null;
    }

    private void save() {
        if (tunnelId != null) {
            // Отмеченное здесь приложение убирается из других туннелей
            TunnelStore.get(this).setApps(tunnelId, new ArrayList<>(tunnelApps));
            for (String pkg : tunnelApps) otherTunnels.remove(pkg);
        } else {
            settings.setApps(mode, new ArrayList<>(include), new ArrayList<>(exclude));
        }
        changed = true;
    }

    private void toggle(App app) {
        Set<String> set = selectedSet();
        if (set == null) {
            Toast.makeText(this, "Сначала выберите режим: «Только выбранные» или «Кроме выбранных»",
                    Toast.LENGTH_SHORT).show();
            return;
        }
        if (!set.remove(app.pkg)) set.add(app.pkg);
        save();
        render();
    }

    private void render() {
        if (tunnelId != null) hint.setText(getString(R.string.apps_hint_tunnel, tunnelName));
        else hint.setText(Settings.APPS_INCLUDE.equals(mode) ? R.string.apps_hint_include
                : Settings.APPS_EXCLUDE.equals(mode) ? R.string.apps_hint_exclude : R.string.apps_hint_all);
        Set<String> set = selectedSet();
        List<App> selected = new ArrayList<>();
        List<App> other = new ArrayList<>();
        for (App a : apps) {
            boolean on = set != null && set.contains(a.pkg);
            if (!on && a.hiddenSystem && !showSystem) continue;
            if (!query.isEmpty() && !a.label.toLowerCase(Locale.ROOT).contains(query)
                    && !a.pkg.toLowerCase(Locale.ROOT).contains(query)) continue;
            (on ? selected : other).add(a);
        }
        adapter.submit(selected, other, set);
    }

    // ---------- Список ----------

    private static final int TYPE_SECTION = 0;
    private static final int TYPE_APP = 1;

    private static final class Row {
        final int type;
        final App app;
        final String title;
        final String count;
        boolean first, last;

        Row(int type, App app, String title, String count) {
            this.type = type;
            this.app = app;
            this.title = title;
            this.count = count;
        }
    }

    private final class Adapter extends RecyclerView.Adapter<RecyclerView.ViewHolder> {
        private final List<Row> rows = new ArrayList<>();
        private Set<String> selected;

        void submit(List<App> on, List<App> off, Set<String> selected) {
            this.selected = selected;
            rows.clear();
            group(getString(R.string.apps_selected), on);
            group(getString(selected == null && on.isEmpty() ? R.string.apps_title : R.string.apps_other), off);
            notifyDataSetChanged();
        }

        private void group(String title, List<App> items) {
            if (items.isEmpty()) return;
            rows.add(new Row(TYPE_SECTION, null, title, String.valueOf(items.size())));
            int start = rows.size();
            for (App a : items) rows.add(new Row(TYPE_APP, a, null, null));
            rows.get(start).first = true;
            rows.get(rows.size() - 1).last = true;
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
            if (viewType == TYPE_SECTION) {
                return new RecyclerView.ViewHolder(inf.inflate(R.layout.item_section, parent, false)) {
                };
            }
            return new RecyclerView.ViewHolder(inf.inflate(R.layout.item_app, parent, false)) {
            };
        }

        @Override
        public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int position) {
            Row row = rows.get(position);
            View v = holder.itemView;
            if (row.type == TYPE_SECTION) {
                ((TextView) v.findViewById(R.id.sectionTitle)).setText(row.title);
                ((TextView) v.findViewById(R.id.sectionCount)).setText(row.count);
                return;
            }
            App app = row.app;
            ((TextView) v.findViewById(R.id.appName)).setText(app.label);
            String elsewhere = otherTunnels.get(app.pkg);
            TextView pkgText = v.findViewById(R.id.appPackage);
            pkgText.setText(elsewhere != null ? "сейчас в туннеле «" + elsewhere + "»" : app.pkg);
            CheckBox check = v.findViewById(R.id.appCheck);
            check.setChecked(selected != null && selected.contains(app.pkg));
            check.setEnabled(selected != null);
            v.setAlpha(selected == null ? 0.6f : 1f);
            ImageView icon = v.findViewById(R.id.appIcon);
            icon.setImageDrawable(icon(app.pkg));
            v.setOnClickListener(x -> toggle(app));
            v.setBackground(GroupBackground.create(v.getContext(), row.first, row.last));
        }
    }

    private Drawable icon(String pkg) {
        Drawable d = icons.get(pkg);
        if (d == null) {
            try {
                d = getPackageManager().getApplicationIcon(pkg);
            } catch (PackageManager.NameNotFoundException e) {
                d = getPackageManager().getDefaultActivityIcon();
            }
            icons.put(pkg, d);
        }
        return d;
    }
}
