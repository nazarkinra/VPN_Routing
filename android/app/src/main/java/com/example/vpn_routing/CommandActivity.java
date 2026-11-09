package com.example.vpn_routing;

import android.app.Activity;
import android.content.Intent;
import android.net.VpnService;
import android.os.Bundle;
import android.widget.Toast;

/**
 * Быстрые команды без интерфейса: ярлыки на иконке приложения и вызовы из сценариев
 * (Samsung «Режимы и сценарии», MacroDroid, Tasker и т. п.).
 *
 * Действия:
 *   com.example.vpn_routing.action.CONNECT    — включить VPN
 *   com.example.vpn_routing.action.DISCONNECT — выключить VPN
 *   com.example.vpn_routing.action.TOGGLE     — переключить
 */
public class CommandActivity extends Activity {

    public static final String ACTION_CONNECT = "com.example.vpn_routing.action.CONNECT";
    public static final String ACTION_DISCONNECT = "com.example.vpn_routing.action.DISCONNECT";
    public static final String ACTION_TOGGLE = "com.example.vpn_routing.action.TOGGLE";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        String action = getIntent() == null ? null : getIntent().getAction();
        if (ACTION_TOGGLE.equals(action)) action = isActive() ? ACTION_DISCONNECT : ACTION_CONNECT;

        if (ACTION_DISCONNECT.equals(action)) {
            if (isActive()) {
                RoutingVpnService.send(this, RoutingVpnService.ACTION_STOP);
                toast("VPN выключается");
            } else {
                toast("VPN уже выключен");
            }
        } else if (ACTION_CONNECT.equals(action)) {
            connect();
        }
        finish();
    }

    private void connect() {
        if (isActive()) {
            toast("VPN уже включён");
            return;
        }
        if (new Settings(this).getLink() == null) {
            toast("Сервер не выбран: откройте приложение и добавьте сервер");
            startActivity(new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
            return;
        }
        if (VpnService.prepare(this) != null) {
            // Разрешение на VPN ещё не выдано: системный запрос показывается из главного экрана
            startActivity(new Intent(this, MainActivity.class)
                    .setAction(MainActivity.ACTION_CONNECT)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP));
            return;
        }
        RoutingVpnService.send(this, RoutingVpnService.ACTION_START);
        toast("VPN включается");
    }

    private static boolean isActive() {
        RoutingVpnService.State s = RoutingVpnService.getState();
        return s == RoutingVpnService.State.CONNECTED || s == RoutingVpnService.State.CONNECTING;
    }

    private void toast(String text) {
        Toast.makeText(getApplicationContext(), text, Toast.LENGTH_SHORT).show();
    }
}
