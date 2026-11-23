package com.example.vpn_routing;

import android.annotation.SuppressLint;
import android.content.Context;
import android.provider.Settings;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/**
 * Идентификатор устройства для заголовка X-Hwid (как у Happ): сервер подписок привязывает
 * зашифрованную ссылку к первому устройству и отказывает остальным.
 *
 * Основа — ANDROID_ID: постоянен для приложения на этом телефоне, переживает переустановку
 * с тем же ключом подписи, меняется после сброса телефона (тогда доступ перепривязывает
 * администратор). Наружу уходит не он сам, а хеш с солью приложения.
 */
public final class DeviceId {

    private DeviceId() {
    }

    @SuppressLint("HardwareIds")
    public static String get(Context context) {
        String id = Settings.Secure.getString(context.getContentResolver(), Settings.Secure.ANDROID_ID);
        if (id == null || id.isEmpty()) {
            // Без ANDROID_ID (редкие прошивки) все такие телефоны получили бы один и тот же хеш —
            // берём случайный, сохранённый в настройках приложения
            android.content.SharedPreferences p = context.getSharedPreferences("device", Context.MODE_PRIVATE);
            id = p.getString("id", "");
            if (id.isEmpty()) {
                id = java.util.UUID.randomUUID().toString();
                p.edit().putString("id", id).apply();
            }
        }
        return fromPlatformId(id);
    }

    /** 32 шестнадцатеричных знака — проходят проверку формата X-Hwid на сервере. */
    static String fromPlatformId(String platformId) {
        try {
            byte[] h = MessageDigest.getInstance("SHA-256")
                    .digest(("vpnrouting-hwid-v1:" + platformId).getBytes(StandardCharsets.UTF_8));
            StringBuilder sb = new StringBuilder();
            for (int i = 0; i < 16; i++) sb.append(String.format("%02x", h[i]));
            return sb.toString();
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }
}
