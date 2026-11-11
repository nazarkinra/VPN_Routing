package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;

import java.io.IOException;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/** Файл версии для обновлений и отключение встроенных правил. */
public class UpdateAndBuiltinsTest {

    private static final String SHA = "07840c18f16a6540b1b498a51df7c65cdd3d90cb72124ee179d57057d2abb588";

    @Test
    public void parsesVersionJson() throws Exception {
        UpdateManager.Info info = UpdateManager.parse("{\"versionCode\": 999, \"versionName\": \"9.9\","
                + "\"apk\": \"VPN_routing.apk\", \"size\": 123, \"sha256\": \"" + SHA.toUpperCase() + "\","
                + "\"notes\": \" Новое \"}", "https://github.com/x/y/raw/main/");
        assertEquals(999, info.versionCode);
        assertEquals("https://github.com/x/y/raw/main/VPN_routing.apk", info.apkUrl);
        assertEquals(SHA, info.sha256);
        assertEquals("Новое", info.notes);
        assertTrue(info.isNewer());
    }

    @Test
    public void absoluteApkUrlAndOldVersion() throws Exception {
        UpdateManager.Info info = UpdateManager.parse("{\"versionCode\": 1, \"apk\": \"https://cdn/x.apk\","
                + "\"sha256\": \"" + SHA + "\"}", "https://base/");
        assertEquals("https://cdn/x.apk", info.apkUrl);
        assertFalse(info.isNewer());
    }

    @Test(expected = IOException.class)
    public void rejectsMissingChecksum() throws Exception {
        UpdateManager.parse("{\"versionCode\": 5}", "https://base/");
    }

    @Test
    public void withoutRemovesDisabledBuiltins() {
        List<String> left = RoutingRules.without(RoutingRules.DIRECT_DOMAINS, Arrays.asList(".ru", "рф", "VK.com"));
        assertFalse(left.contains("ru"));
        assertFalse(left.contains("xn--p1ai"));
        assertFalse(left.contains("vk.com"));
        assertTrue(left.contains("su"));
    }

    @Test
    public void configWithoutDisabledZone() throws Exception {
        ProxyLink link = ProxyLink.parse("vless://id@a.example.com:443?security=tls&type=ws#A");
        List<String> direct = RoutingRules.without(RoutingRules.DIRECT_DOMAINS, Collections.singletonList("ru"));
        String json = XrayConfig.build(link, "1.2.3.4", null, Collections.emptyList(), null,
                direct, RoutingRules.DIRECT_TUNNEL_DOMAINS);
        assertFalse(json.contains("\"domain:ru\""));
        assertTrue(json.contains("\"domain:su\""));
    }

    @Test
    public void configWithAllBuiltinsRemoved() throws Exception {
        ProxyLink link = ProxyLink.parse("vless://id@a.example.com:443?security=tls&type=ws#A");
        JSONObject c = new JSONObject(XrayConfig.build(link, "1.2.3.4", null, Collections.emptyList(), null,
                Collections.emptyList(), Collections.emptyList()));
        JSONArray rules = c.getJSONObject("routing").getJSONArray("rules");
        for (int i = 0; i < rules.length(); i++) {
            JSONArray d = rules.getJSONObject(i).optJSONArray("domain");
            assertTrue("нет правил с пустым списком доменов", d == null || d.length() > 0);
        }
        // Без доменов DNS сети не нужен: остаётся только DNS через VPN
        JSONArray servers = c.getJSONObject("dns").getJSONArray("servers");
        assertEquals(RoutingRules.PROXY_DNS.size(), servers.length());
        for (int i = 0; i < servers.length(); i++) {
            assertFalse(servers.getJSONObject(i).has("domains"));
        }
    }
}
