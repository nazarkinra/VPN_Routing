package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.util.Base64;

/** Разбор тела и заголовков подписки. */
public class SubscriptionTest {

    private static final String A = "vless://11111111-2222-3333-4444-555555555555@a.example.com:443"
            + "?security=reality&type=tcp&pbk=KEY&sni=x.com#%F0%9F%87%A9%F0%9F%87%AA%20DE";
    private static final String B = "vless://11111111-2222-3333-4444-555555555555@b.example.com:443?type=ws&security=tls#NL";

    @Test
    public void plainList() {
        SubscriptionFetcher.Result r = SubscriptionFetcher.parseBody(A + "\n" + B + "\nvmess://abc\n\n# comment\n" + A);
        assertEquals(2, r.links.size());
        assertEquals(1, r.unsupported);
    }

    @Test
    public void base64Body() {
        String body = Base64.getEncoder().encodeToString((A + "\r\n" + B).getBytes(StandardCharsets.UTF_8));
        SubscriptionFetcher.Result r = SubscriptionFetcher.parseBody(body);
        assertEquals(2, r.links.size());
        assertEquals(A, r.links.get(0));
    }

    @Test
    public void urlSafeBase64WithoutPadding() {
        String body = Base64.getUrlEncoder().withoutPadding().encodeToString(B.getBytes(StandardCharsets.UTF_8));
        assertEquals(B, SubscriptionFetcher.decodeBase64(body));
        assertNull(SubscriptionFetcher.decodeBase64("не base64!"));
    }

    @Test
    public void headers() {
        SubscriptionFetcher.Result r = new SubscriptionFetcher.Result();
        String title = "base64:" + Base64.getEncoder().encodeToString("Мой VPN".getBytes(StandardCharsets.UTF_8));
        SubscriptionFetcher.applyHeaders(r, title,
                "upload=1024; download=2048; total=107374182400; expire=1798675200", "12", null);
        assertEquals("Мой VPN", r.title);
        assertEquals(1024, r.upload);
        assertEquals(2048, r.download);
        assertEquals(107374182400L, r.total);
        assertEquals(1798675200L, r.expire);
        assertEquals(12, r.updateIntervalHours);
    }
}
