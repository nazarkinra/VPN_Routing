package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.json.JSONObject;
import org.junit.Test;

import java.util.Collections;

/** Hysteria2: разбор ссылок и выход Xray в том же виде, что отдаёт панель. */
public class HysteriaTest {

    @Test
    public void parsesPanelLink() {
        ProxyLink l = ProxyLink.parse("hysteria2://secret-auth@cdn.example.com:443/?sni=cdn.example.com"
                + "&fm=%7B%22quicParams%22%3A%7B%22keepAlivePeriod%22%3A10%7D%7D#%F0%9F%87%B3%F0%9F%87%B1%20Gaming");
        assertTrue(l.isHysteria());
        assertEquals("secret-auth", l.uuid);
        assertEquals("cdn.example.com", l.server);
        assertEquals(443, l.port);
        assertEquals("{\"quicParams\":{\"keepAlivePeriod\":10}}", l.finalmask);
        assertEquals("🇳🇱 Gaming", l.name);
    }

    @Test
    public void parsesShortSchemeWithDefaultPortAndObfs() {
        ProxyLink l = ProxyLink.parse("hy2://user:pass@1.2.3.4?obfs=salamander&obfs-password=xyz&pinSHA256=AB:CD:EF#X");
        assertEquals("user:pass", l.uuid);
        assertEquals(443, l.port);
        assertEquals("salamander", l.obfs);
        assertEquals("abcdef", l.pinnedCert);
    }

    @Test(expected = IllegalArgumentException.class)
    public void rejectsPortHopping() {
        ProxyLink.parse("hysteria2://auth@example.com:20000-30000/?sni=example.com");
    }

    @Test
    public void buildsOutboundLikePanel() throws Exception {
        ProxyLink l = ProxyLink.parse("hysteria2://AUTH@cdn.example.com:443/?sni=cdn.example.com&obfs=salamander"
                + "&obfs-password=pw&pinSHA256=abcd&fm=%7B%22quicParams%22%3A%7B%22keepAlivePeriod%22%3A10%7D%7D#G");
        JSONObject out = new JSONObject(XrayConfig.build(l, "93.184.216.34", null, Collections.emptyList(), null))
                .getJSONArray("outbounds").getJSONObject(0);
        assertEquals("hysteria", out.getString("protocol"));
        assertFalse("Mux поверх QUIC не нужен", out.has("mux"));
        JSONObject settings = out.getJSONObject("settings");
        assertEquals("93.184.216.34", settings.getString("address"));
        assertEquals(2, settings.getInt("version"));

        JSONObject stream = out.getJSONObject("streamSettings");
        assertEquals("hysteria", stream.getString("network"));
        assertEquals("AUTH", stream.getJSONObject("hysteriaSettings").getString("auth"));
        JSONObject tls = stream.getJSONObject("tlsSettings");
        assertEquals("cdn.example.com", tls.getString("serverName"));
        assertEquals("h3", tls.getJSONArray("alpn").getString(0));
        assertEquals("abcd", tls.getString("pinnedPeerCertSha256"));
        JSONObject mask = stream.getJSONObject("finalmask");
        assertEquals(10, mask.getJSONObject("quicParams").getInt("keepAlivePeriod"));
        assertEquals("salamander", mask.getJSONArray("udp").getJSONObject(0).getString("type"));
        assertEquals("pw", mask.getJSONArray("udp").getJSONObject(0).getJSONObject("settings").getString("password"));
    }

    @Test
    public void subscriptionAcceptsHysteria() {
        SubscriptionFetcher.Result r = SubscriptionFetcher.parseBody(
                "vless://id@a.example.com:443?security=tls&type=ws#A\n"
                        + "hysteria2://auth@b.example.com:443/?sni=b.example.com#B\n"
                        + "trojan://x@c.example.com:443#C");
        assertEquals(2, r.links.size());
        assertEquals(1, r.unsupported);
    }
}
