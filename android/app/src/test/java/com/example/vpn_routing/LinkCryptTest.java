package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import org.junit.Test;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

/**
 * Зашифрованные ссылки vpnrouting://crypt1/. Вектор linkcrypt_vector.txt зашифрован сервером подписок
 * (vpn_corp, subs/happ_crypt.build_app_link) тестовым ключом — проверяется совместимость с ним,
 * а не только сама с собой.
 */
public class LinkCryptTest {

    private static List<String> vector() throws IOException {
        InputStream in = LinkCryptTest.class.getClassLoader().getResourceAsStream("linkcrypt_vector.txt");
        List<String> lines = new ArrayList<>();
        try (BufferedReader r = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8))) {
            String line;
            while ((line = r.readLine()) != null) if (!line.startsWith("#") && !line.isEmpty()) lines.add(line);
        }
        return lines;
    }

    @Test
    public void decryptsServerLinks() throws IOException {
        List<String> v = vector();
        String key = v.get(0);
        assertEquals(v.get(1), LinkCrypt.decrypt(v.get(2), key));
        assertEquals(v.get(3), LinkCrypt.decrypt(v.get(4), key)); // несколько RSA-блоков
    }

    @Test
    public void recognizesScheme() throws IOException {
        List<String> v = vector();
        assertTrue(LinkCrypt.isCryptLink(v.get(2)));
        assertTrue(LinkCrypt.isCryptLink("  " + v.get(2) + "\n"));
        assertFalse(LinkCrypt.isCryptLink("https://example.com/sub"));
        assertFalse(LinkCrypt.isCryptLink("happ://crypt4/abc"));
        assertFalse(SubscriptionFetcher.isSubscriptionUrl(v.get(2)));
    }

    @Test
    public void damagedLinkIsRejected() throws IOException {
        List<String> v = vector();
        String link = v.get(2);
        expectFailure(link.substring(0, link.length() - 10), v.get(0)); // обрезана при копировании
        char last = link.charAt(link.length() - 5);
        String flipped = link.substring(0, link.length() - 5) + (last == 'A' ? 'B' : 'A') + link.substring(link.length() - 4);
        expectFailure(flipped, v.get(0));
    }

    @Test
    public void foreignKeyIsRejected() throws Exception {
        java.security.KeyPairGenerator g = java.security.KeyPairGenerator.getInstance("RSA");
        g.initialize(4096);
        String other = java.util.Base64.getEncoder().encodeToString(g.generateKeyPair().getPrivate().getEncoded());
        expectFailure(vector().get(2), other);
    }

    @Test
    public void deviceIdIsStableAndAcceptedByServer() {
        String a = DeviceId.fromPlatformId("9774d56d682e549c");
        assertEquals(a, DeviceId.fromPlatformId("9774d56d682e549c"));
        assertFalse(a.equals(DeviceId.fromPlatformId("9774d56d682e549d")));
        // Формат X-Hwid, который принимает сервер подписок: [A-Za-z0-9=-]{10,64}
        assertTrue(a.matches("[0-9a-f]{32}"));
    }

    private static void expectFailure(String link, String key) {
        try {
            LinkCrypt.decrypt(link, key);
            fail("ожидалась ошибка");
        } catch (IOException expected) {
            // ok
        }
    }
}
