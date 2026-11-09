package com.example.vpn_routing;

import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;

/** Форматирование чисел, времени и объёмов для экрана. */
final class Format {

    private Format() {
    }

    /** 1536 -> «1,5 КБ». */
    static String bytes(long b) {
        if (b < 1024) return b + " Б";
        String[] units = {"КБ", "МБ", "ГБ", "ТБ"};
        double v = b;
        int i = -1;
        do {
            v /= 1024;
            i++;
        } while (v >= 1024 && i < units.length - 1);
        return String.format(Locale.forLanguageTag("ru"), v >= 100 ? "%.0f %s" : "%.1f %s", v, units[i]);
    }

    /** Длительность: «12:34» или «1:02:03». */
    static String duration(long ms) {
        long s = Math.max(0, ms / 1000);
        long h = s / 3600, m = (s % 3600) / 60, sec = s % 60;
        return h > 0 ? String.format(Locale.ROOT, "%d:%02d:%02d", h, m, sec)
                : String.format(Locale.ROOT, "%02d:%02d", m, sec);
    }

    /** «только что», «5 мин назад», «3 ч назад», «12.11.2026». */
    static String ago(long timeMs) {
        if (timeMs <= 0) return "не обновлялась";
        long diff = (System.currentTimeMillis() - timeMs) / 1000;
        if (diff < 60) return "обновлено только что";
        if (diff < 3600) return "обновлено " + diff / 60 + " мин назад";
        if (diff < 86400) return "обновлено " + diff / 3600 + " ч назад";
        return "обновлено " + date(timeMs / 1000);
    }

    /** Дата из секунд Unix. */
    static String date(long unixSeconds) {
        return new SimpleDateFormat("dd.MM.yyyy", Locale.ROOT).format(new Date(unixSeconds * 1000));
    }

    /** Цвет пинга: зелёный до 300 мс, оранжевый до 800, красный — нет ответа. */
    static int pingColor(long ms) {
        if (ms < 0) return R.color.status_error;
        if (ms < 300) return R.color.status_ok;
        if (ms < 800) return R.color.status_wait;
        return R.color.status_error;
    }
}
