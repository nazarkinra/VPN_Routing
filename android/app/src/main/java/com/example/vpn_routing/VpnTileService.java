package com.example.vpn_routing;

import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.Build;
import android.service.quicksettings.Tile;
import android.service.quicksettings.TileService;

/**
 * Плитка в панели быстрых настроек (шторка уведомлений): включает и выключает VPN одним нажатием.
 * Если сервер не выбран или разрешение на VPN ещё не выдано — открывает приложение.
 */
public class VpnTileService extends TileService implements RoutingVpnService.Listener {

    /** Попросить систему обновить плитку, даже если шторка сейчас закрыта. */
    static void refresh(Context context) {
        try {
            requestListeningState(context, new ComponentName(context, VpnTileService.class));
        } catch (RuntimeException ignored) {
            // Плитка не добавлена в панель или система не поддерживает запрос — не страшно
        }
    }

    @Override
    public void onStartListening() {
        RoutingVpnService.addListener(this); // сразу вызывает onStateChanged с текущим состоянием
    }

    @Override
    public void onStopListening() {
        RoutingVpnService.removeListener(this);
    }

    @Override
    public void onStateChanged(RoutingVpnService.State state, String message) {
        Tile tile = getQsTile();
        if (tile == null) return;
        boolean on = state == RoutingVpnService.State.CONNECTED;
        boolean busy = state == RoutingVpnService.State.CONNECTING;
        tile.setState(on || busy ? Tile.STATE_ACTIVE : Tile.STATE_INACTIVE);
        tile.setLabel(getString(R.string.tile_label));
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            String sub;
            if (on) sub = "Подключено";
            else if (busy) sub = "Подключение…";
            else if (state == RoutingVpnService.State.ERROR) sub = "Ошибка";
            else sub = "Выключено";
            tile.setSubtitle(sub);
        }
        tile.updateTile();
    }

    @Override
    public void onClick() {
        RoutingVpnService.State s = RoutingVpnService.getState();
        // Плитка — основной VPN, как кнопка на вкладке VPN (VPN МГТУ не трогается)
        if (s == RoutingVpnService.State.CONNECTED || s == RoutingVpnService.State.CONNECTING) {
            RoutingVpnService.send(this, RoutingVpnService.ACTION_STOP_MAIN);
            return;
        }
        if (new Settings(this).getLink() == null) {
            openApp(new Intent(this, MainActivity.class));
            return;
        }
        if (VpnService.prepare(this) != null) {
            // Системный запрос разрешения на VPN показывается только из окна приложения
            openApp(new Intent(this, MainActivity.class).setAction(MainActivity.ACTION_CONNECT));
            return;
        }
        RoutingVpnService.send(this, RoutingVpnService.ACTION_START);
        Tile tile = getQsTile();
        if (tile != null) {
            tile.setState(Tile.STATE_ACTIVE);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) tile.setSubtitle("Подключение…");
            tile.updateTile();
        }
    }

    private void openApp(Intent intent) {
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        startActivityAndCollapse(intent);
    }
}
