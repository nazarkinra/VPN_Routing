package com.example.vpn_routing;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.KeyFactory;
import java.security.PrivateKey;
import java.security.interfaces.RSAPrivateKey;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.Locale;

import javax.crypto.Cipher;

/**
 * Зашифрованные ссылки-подписки: vpnrouting://crypt1/&lt;base64url&gt;.
 *
 * Внутри — адрес подписки (https://…), зашифрованный открытым ключом RSA-4096 (PKCS#1 v1.5,
 * блоками по 501 байту, как happ://crypt4). Закрытый ключ встраивается при сборке
 * (BuildConfig.LINK_PRIVATE_KEY, см. app/build.gradle) и в репозитории не лежит. Ссылку нельзя
 * прочитать, отредактировать или переслать как обычный адрес; сервер к тому же привязывает её
 * к первому устройству (заголовок X-Hwid, см. DeviceId).
 *
 * Цифра в схеме — версия формата: новый ключ или шифр — новая версия.
 */
public final class LinkCrypt {

    public static final String PREFIX = "vpnrouting://crypt1/";
    private static final int BLOCK = 512; // RSA-4096

    private LinkCrypt() {
    }

    public static boolean isCryptLink(String text) {
        return text != null && text.trim().toLowerCase(Locale.ROOT).startsWith(PREFIX);
    }

    /** Есть ли в этой сборке ключ (сборка без него зашифрованные ссылки не читает). */
    public static boolean available() {
        return !BuildConfig.LINK_PRIVATE_KEY.isEmpty();
    }

    /** Адрес подписки из зашифрованной ссылки ключом этой сборки. */
    public static String decrypt(String link) throws IOException {
        if (!available()) {
            throw new IOException("эта сборка приложения не умеет зашифрованные ссылки — обновите приложение");
        }
        return decrypt(link, BuildConfig.LINK_PRIVATE_KEY);
    }

    /** keyBase64 — закрытый ключ PKCS#8 (DER) в base64. */
    static String decrypt(String link, String keyBase64) throws IOException {
        if (!isCryptLink(link)) throw new IOException("это не зашифрованная ссылка");
        byte[] data = decodeBase64(link.trim().substring(PREFIX.length()));
        if (data == null || data.length == 0 || data.length % BLOCK != 0) {
            throw new IOException("ссылка повреждена: скопируйте её из бота целиком");
        }
        String url;
        try {
            byte[] der = decodeBase64(keyBase64);
            if (der == null) throw new IOException("ключ сборки повреждён");
            PrivateKey key = KeyFactory.getInstance("RSA").generatePrivate(new PKCS8EncodedKeySpec(der));
            if (((RSAPrivateKey) key).getModulus().bitLength() != BLOCK * 8) {
                throw new IOException("ключ сборки не RSA-4096");
            }
            Cipher cipher = Cipher.getInstance("RSA/ECB/PKCS1Padding");
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            for (int off = 0; off < data.length; off += BLOCK) {
                cipher.init(Cipher.DECRYPT_MODE, key);
                out.write(cipher.doFinal(data, off, BLOCK));
            }
            url = new String(out.toByteArray(), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw e;
        } catch (Exception e) {
            // Чужой ключ или испорченная ссылка: PKCS#1 не сходится
            throw new IOException("ссылка не подходит к этому приложению или повреждена");
        }
        if (!SubscriptionFetcher.isSubscriptionUrl(url)) {
            throw new IOException("внутри ссылки не адрес подписки");
        }
        return url;
    }

    /** Base64 (обычный и URL-safe, с отступами и без) в байты. null, если это не base64. */
    static byte[] decodeBase64(String text) {
        String s = text.replaceAll("\\s+", "").replace('-', '+').replace('_', '/');
        while (s.endsWith("=")) s = s.substring(0, s.length() - 1);
        if (s.isEmpty() || !s.matches("[A-Za-z0-9+/]+") || s.length() % 4 == 1) return null;
        ByteArrayOutputStream out = new ByteArrayOutputStream(s.length() * 3 / 4);
        int buffer = 0, bits = 0;
        for (int i = 0; i < s.length(); i++) {
            char ch = s.charAt(i);
            int v = ch >= 'A' && ch <= 'Z' ? ch - 'A' : ch >= 'a' && ch <= 'z' ? ch - 'a' + 26
                    : ch >= '0' && ch <= '9' ? ch - '0' + 52 : ch == '+' ? 62 : 63;
            buffer = ((buffer << 6) | v) & 0xFFFFFF;
            bits += 6;
            if (bits >= 8) {
                bits -= 8;
                out.write((buffer >> bits) & 0xFF);
            }
        }
        return out.toByteArray();
    }
}
