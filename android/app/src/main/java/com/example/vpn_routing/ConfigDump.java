package com.example.vpn_routing;

import android.content.Context;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Set;

/**
 * Копия рабочего конфига Xray для проверки: Android/data/com.example.vpn_routing/files/xray_config.json.
 * UUID, ключи Reality, пароли и логины заменены звёздочками — файл можно показать кому угодно.
 */
final class ConfigDump {

    static final String FILE_NAME = "xray_config.json";
    private static final Set<String> SECRET_KEYS = new HashSet<>(Arrays.asList(
            "id", "publicKey", "shortId", "password", "pass", "user", "auth", "seed"));

    private ConfigDump() {
    }

    static void save(Context context, String config) {
        try {
            File dir = context.getExternalFilesDir(null);
            if (dir == null) dir = context.getFilesDir();
            File file = new File(dir, FILE_NAME);
            try (FileOutputStream out = new FileOutputStream(file)) {
                out.write(mask(config).getBytes(StandardCharsets.UTF_8));
            }
            Log.i("ConfigDump", "config saved: " + file);
        } catch (Exception e) {
            Log.w("ConfigDump", "save failed", e);
        }
    }

    static String mask(String config) throws JSONException {
        JSONObject root = new JSONObject(config);
        maskObject(root);
        return root.toString(2);
    }

    private static void maskObject(JSONObject o) throws JSONException {
        Iterator<String> keys = o.keys();
        while (keys.hasNext()) {
            String k = keys.next();
            Object v = o.get(k);
            if (v instanceof JSONObject) maskObject((JSONObject) v);
            else if (v instanceof JSONArray) maskArray((JSONArray) v);
            else if (SECRET_KEYS.contains(k) && v instanceof String && !((String) v).isEmpty()) o.put(k, "***");
        }
    }

    private static void maskArray(JSONArray a) throws JSONException {
        for (int i = 0; i < a.length(); i++) {
            Object v = a.get(i);
            if (v instanceof JSONObject) maskObject((JSONObject) v);
            else if (v instanceof JSONArray) maskArray((JSONArray) v);
        }
    }
}
