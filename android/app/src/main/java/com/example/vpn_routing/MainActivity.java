package com.example.vpn_routing;

import android.app.Activity;
import android.content.Intent;
import android.net.VpnService;
import android.os.Bundle;
import android.widget.Toast;

import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.appcompat.app.AppCompatActivity;
import androidx.fragment.app.Fragment;
import androidx.fragment.app.FragmentManager;
import androidx.fragment.app.FragmentTransaction;

import com.google.android.material.bottomnavigation.BottomNavigationView;

/** Главный экран: три вкладки — VPN, МГТУ и «Напрямую». */
public class MainActivity extends AppCompatActivity {

    private static final String TAG_VPN = "vpn";
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
        if (savedInstanceState == null) handleCommand(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        handleCommand(intent);
    }

    private void handleCommand(Intent intent) {
        if (intent != null && ACTION_CONNECT.equals(intent.getAction()) && !isActive()) {
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
        if (itemId == R.id.nav_corp) return TAG_CORP;
        if (itemId == R.id.nav_direct) return TAG_DIRECT;
        return TAG_VPN;
    }

    private void show(String tag) {
        FragmentManager fm = getSupportFragmentManager();
        FragmentTransaction tx = fm.beginTransaction();
        for (String t : new String[]{TAG_VPN, TAG_CORP, TAG_DIRECT}) {
            Fragment f = fm.findFragmentByTag(t);
            if (f == null) continue;
            if (t.equals(tag)) tx.show(f);
            else tx.hide(f);
        }
        tx.commitNow();
    }

    // ---------- Подключение ----------

    private boolean isActive() {
        RoutingVpnService.State s = RoutingVpnService.getState();
        return s == RoutingVpnService.State.CONNECTED || s == RoutingVpnService.State.CONNECTING;
    }

    /** Кнопка питания на вкладке VPN. */
    void toggleVpn() {
        if (isActive()) {
            RoutingVpnService.send(this, RoutingVpnService.ACTION_STOP);
            return;
        }
        if (settings.getLink() == null) {
            toast("Сначала добавьте сервер: кнопка «Добавить» внизу.");
            return;
        }
        withVpnPermission(() -> RoutingVpnService.send(this, RoutingVpnService.ACTION_START));
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
