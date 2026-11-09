package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

/** Разбор ключа и подсетей (без Android-классов, запускается на ПК). */
public class ExampleUnitTest {

    @Test
    public void parsesRealityLink() {
        ProxyLink l = ProxyLink.parse("vless://11111111-2222-3333-4444-555555555555@vpn.example.com:443"
                + "?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.microsoft.com&pbk=KEY&sid=ab&fp=firefox#My%20VPS");
        assertEquals("11111111-2222-3333-4444-555555555555", l.uuid);
        assertEquals("vpn.example.com", l.server);
        assertEquals(443, l.port);
        assertEquals("reality", l.security);
        assertEquals("tcp", l.network);
        assertEquals("xtls-rprx-vision", l.flow);
        assertEquals("www.microsoft.com", l.sni);
        assertEquals("KEY", l.pbk);
        assertEquals("ab", l.sid);
        assertEquals("firefox", l.fp);
        assertEquals("My VPS", l.name);
    }

    @Test
    public void parsesWsLinkWithDefaults() {
        ProxyLink l = ProxyLink.parse(" vless://id@[2001:db8::1]:8443?type=ws&security=tls&path=%2Fws%3Fed%3D2048&host=cdn.example.com \n");
        assertEquals("2001:db8::1", l.server);
        assertEquals(8443, l.port);
        assertEquals("/ws?ed=2048", l.path);
        assertEquals("cdn.example.com", l.host);
        assertEquals("chrome", l.fp);
        assertEquals("", l.flow);
    }

    @Test(expected = IllegalArgumentException.class)
    public void rejectsMissingPort() {
        ProxyLink.parse("vless://id@server?security=none");
    }

    @Test
    public void normalizesCidr() {
        assertEquals("195.19.0.0/16", Exceptions.parseCidr("195.19.12.7/16"));
        assertEquals("93.184.216.34/32", Exceptions.parseCidr("93.184.216.34"));
        assertEquals("2001:db8::/32", Exceptions.parseCidr("2001:db8:1::5/32"));
        assertNull(Exceptions.parseCidr("example.com"));
        assertNull(Exceptions.parseCidr("300.1.1.1"));
        assertNull(Exceptions.parseCidr("1.2.3.4/33"));
    }

    @Test
    public void builtinDirectDomains() {
        assertEquals(true, RoutingRules.isBuiltinDirect("bmstu.ru"));
        assertEquals(true, RoutingRules.isBuiltinDirect("music.vk.com"));
        assertEquals(false, RoutingRules.isBuiltinDirect("example.com"));
    }
}
