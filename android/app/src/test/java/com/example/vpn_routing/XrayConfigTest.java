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
import java.util.Arrays;

/**
 * Собирает конфиги Xray и сохраняет их в app/build/xray-configs,
 * чтобы их можно было проверить командой xray run -test -config <файл>.
 */
public class XrayConfigTest {

    private static final String REALITY = "vless://11111111-2222-3333-4444-555555555555@vpn.example.com:443"
            + "?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.microsoft.com"
            + "&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&fp=chrome";
    private static final String WS_TLS = "vless://11111111-2222-3333-4444-555555555555@cdn.example.com:443"
            + "?security=tls&type=ws&path=%2Fws&host=cdn.example.com&sni=cdn.example.com";
    private static final String GRPC = "vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443"
            + "?security=tls&type=grpc&serviceName=grpc&sni=example.com";

    @Test
    public void realityConfig() throws Exception {
        JSONObject c = build(REALITY, "reality.json");
        JSONObject proxy = c.getJSONArray("outbounds").getJSONObject(0);
        assertEquals("proxy", proxy.getString("tag"));
        assertEquals("93.184.216.34", proxy.getJSONObject("settings").getJSONArray("vnext")
                .getJSONObject(0).getString("address"));
        assertFalse("mux несовместим с Vision", proxy.has("mux"));
        assertEquals("www.microsoft.com", proxy.getJSONObject("streamSettings")
                .getJSONObject("realitySettings").getString("serverName"));

        String routing = c.getJSONObject("routing").toString();
        assertTrue(routing.contains("domain:ru"));
        assertTrue(routing.contains("domain:bmstu.example"));
        assertTrue(routing.contains("195.19.0.0/16"));
    }

    @Test
    public void wsConfig() throws Exception {
        JSONObject c = build(WS_TLS, "ws.json");
        JSONObject proxy = c.getJSONArray("outbounds").getJSONObject(0);
        assertTrue(proxy.has("mux"));
        assertEquals("/ws", proxy.getJSONObject("streamSettings").getJSONObject("wsSettings").getString("path"));
    }

    @Test
    public void xhttpExtraAlpnAndFinalmask() throws Exception {
        String url = "vless://11111111-2222-3333-4444-555555555555@x.example.com:443?encryption=none&type=xhttp"
                + "&security=tls&sni=x.example.com&alpn=h2%2Chttp%2F1.1&mode=auto&path=%2Fx"
                + "&extra=%7B%22xmux%22%3A%7B%22maxConcurrency%22%3A%226-12%22%7D%7D"
                + "&fm=%7B%22tcp%22%3A%5B%7B%22type%22%3A%22fragment%22%7D%5D%7D#X";
        JSONObject proxy = build(url, "xhttp.json").getJSONArray("outbounds").getJSONObject(0);
        assertFalse("XHTTP мультиплексирует сам, Mux поверх него сервер обрывает", proxy.has("mux"));
        JSONObject stream = proxy.getJSONObject("streamSettings");
        assertEquals("h2", stream.getJSONObject("tlsSettings").getJSONArray("alpn").getString(0));
        assertEquals("6-12", stream.getJSONObject("xhttpSettings").getJSONObject("extra")
                .getJSONObject("xmux").getString("maxConcurrency"));
        assertEquals("fragment", stream.getJSONObject("finalmask").getJSONArray("tcp")
                .getJSONObject(0).getString("type"));
    }

    @Test
    public void grpcConfig() throws Exception {
        build(GRPC, "grpc.json");
    }

    @Test
    public void bmstuRouteBeforeDirectRu() throws Exception {
        JSONObject c = build(REALITY, "reality-bmstu.json", new XrayConfig.BmstuRoute(41234, "u", "p"));
        JSONObject bmstu = c.getJSONArray("outbounds").getJSONObject(4);
        assertEquals("bmstu", bmstu.getString("tag"));
        assertEquals(41234, bmstu.getJSONObject("settings").getJSONArray("servers").getJSONObject(0).getInt("port"));

        JSONArray rules = c.getJSONObject("routing").getJSONArray("rules");
        int bmstuIndex = -1, ruIndex = -1;
        for (int i = 0; i < rules.length(); i++) {
            JSONObject r = rules.getJSONObject(i);
            if ("bmstu".equals(r.optString("outboundTag"))) bmstuIndex = i;
            if (r.has("domain") && r.getJSONArray("domain").toString().contains("\"domain:ru\"")) ruIndex = i;
        }
        assertTrue("bmstu.ru должен проверяться раньше правила .ru", bmstuIndex >= 0 && bmstuIndex < ruIndex);
    }

    @Test
    public void noBmstuRouteWhenDisabled() throws Exception {
        JSONObject c = build(REALITY, "reality.json");
        assertFalse(c.toString().contains("\"bmstu\""));
    }

    private static JSONObject build(String url, String fileName) throws Exception {
        return build(url, fileName, null);
    }

    private static JSONObject build(String url, String fileName, XrayConfig.BmstuRoute bmstu) throws Exception {
        String json = XrayConfig.build(ProxyLink.parse(url), "93.184.216.34", "192.168.0.1",
                Arrays.asList("bmstu.example", "195.19.0.0/16"), bmstu);
        File dir = new File("build/xray-configs");
        //noinspection ResultOfMethodCallIgnored
        dir.mkdirs();
        try (FileOutputStream out = new FileOutputStream(new File(dir, fileName))) {
            out.write(json.getBytes(StandardCharsets.UTF_8));
        }
        JSONObject config = new JSONObject(json);
        JSONArray rules = config.getJSONObject("routing").getJSONArray("rules");
        assertTrue(rules.length() > 5);
        return config;
    }
}
