package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;

import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/** Дополнительные туннели (2.1): выходы и порядок правил в конфиге Xray. */
public class TunnelsConfigTest {

    private static final String MAIN = "vless://11111111-2222-3333-4444-555555555555@main.example.com:443"
            + "?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.microsoft.com"
            + "&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&fp=chrome";
    private static final String SECOND = "vless://11111111-2222-3333-4444-555555555555@nl.example.com:443"
            + "?security=tls&type=ws&path=%2Fws&sni=nl.example.com";
    private static final String THIRD = "hysteria2://pass@us.example.com:443?sni=us.example.com";

    @Test
    public void tunnelsGetOwnOutboundsAndRules() throws Exception {
        List<XrayConfig.TunnelRoute> extra = new ArrayList<>();
        extra.add(new XrayConfig.TunnelRoute("tunnel-1", ProxyLink.parse(SECOND), "5.6.7.8",
                Arrays.asList(10123, 10124), Arrays.asList("youtube.com", "kinopoisk.ru")));
        extra.add(new XrayConfig.TunnelRoute("tunnel-2", ProxyLink.parse(THIRD), "9.9.9.9",
                Collections.singletonList(10200), Collections.singletonList("10.8.0.0/16")));
        JSONObject c = build(extra, "tunnels.json");

        // Основной — первый (маршрут по умолчанию), туннели — следом
        JSONArray outs = c.getJSONArray("outbounds");
        assertEquals("proxy", outs.getJSONObject(0).getString("tag"));
        assertEquals("tunnel-1", outs.getJSONObject(1).getString("tag"));
        assertEquals("5.6.7.8", outs.getJSONObject(1).getJSONObject("settings").getJSONArray("vnext")
                .getJSONObject(0).getString("address"));
        assertEquals("tunnel-2", outs.getJSONObject(2).getString("tag"));
        assertEquals("hysteria", outs.getJSONObject(2).getString("protocol"));

        JSONArray rules = c.getJSONObject("routing").getJSONArray("rules");
        int domainRule = -1, ruRule = -1, quicRule = -1, processRule = -1, cidrRule = -1;
        for (int i = 0; i < rules.length(); i++) {
            JSONObject r = rules.getJSONObject(i);
            String tag = r.optString("outboundTag");
            if ("tunnel-1".equals(tag) && r.has("domain")) domainRule = i;
            if ("tunnel-1".equals(tag) && r.has("process")) {
                processRule = i;
                assertEquals("[\"10123\",\"10124\"]", r.getJSONArray("process").toString());
            }
            if ("tunnel-2".equals(tag) && r.has("ip")) cidrRule = i;
            if (r.has("domain") && r.getJSONArray("domain").toString().contains("\"domain:ru\"")) ruRule = i;
            if ("block".equals(tag) && !r.has("domain") && "443".equals(r.optString("port"))) quicRule = i;
        }
        assertTrue("домен туннеля важнее «Напрямую» (kinopoisk.ru через туннель)", domainRule >= 0 && domainRule < ruRule);
        assertTrue(rules.getJSONObject(domainRule).getJSONArray("domain").toString().contains("domain:kinopoisk.ru"));
        assertTrue("подсеть туннеля", cidrRule >= 0 && cidrRule < ruRule);
        assertTrue("приложения туннеля — после «Напрямую» и блокировки QUIC", processRule > ruRule
                && processRule > quicRule);
    }

    @Test
    public void tunnelsIgnoredWithoutMainVpn() throws Exception {
        List<XrayConfig.TunnelRoute> extra = new ArrayList<>();
        extra.add(new XrayConfig.TunnelRoute("tunnel-1", ProxyLink.parse(SECOND), "5.6.7.8",
                Collections.singletonList(10123), Collections.singletonList("youtube.com")));
        String json = XrayConfig.build(null, null, "192.168.0.1", new ArrayList<>(),
                new XrayConfig.BmstuRoute(41234, "u", "p"), RoutingRules.DIRECT_DOMAINS,
                RoutingRules.DIRECT_TUNNEL_DOMAINS, extra);
        assertFalse(json.contains("tunnel-1"));
    }

    @Test
    public void manyTunnels() throws Exception {
        // Ограничения на число туннелей нет: проверяется конфиг с 30
        int count = 30;
        List<XrayConfig.TunnelRoute> extra = new ArrayList<>();
        for (int i = 1; i <= count; i++) {
            extra.add(new XrayConfig.TunnelRoute("tunnel-" + i, ProxyLink.parse(SECOND), "5.6.7." + i,
                    Collections.singletonList(10000 + i), Collections.singletonList("site" + i + ".com")));
        }
        JSONObject c = build(extra, "tunnels-30.json");
        assertEquals(count + 1 + 3, c.getJSONArray("outbounds").length());
    }

    private static JSONObject build(List<XrayConfig.TunnelRoute> extra, String fileName) throws Exception {
        String json = XrayConfig.build(ProxyLink.parse(MAIN), "1.2.3.4", "192.168.0.1",
                Arrays.asList("bmstu.example", "195.19.0.0/16"), null, RoutingRules.DIRECT_DOMAINS,
                RoutingRules.DIRECT_TUNNEL_DOMAINS, extra);
        File dir = new File("build/xray-configs");
        //noinspection ResultOfMethodCallIgnored
        dir.mkdirs();
        try (FileOutputStream out = new FileOutputStream(new File(dir, fileName))) {
            out.write(json.getBytes(StandardCharsets.UTF_8));
        }
        return new JSONObject(json);
    }
}
