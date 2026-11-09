package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

/** Очистка .ovpn — те же правила, что в routing/scripts/bmstu-config.py. */
public class BmstuConfigTest {

    private static final String OVPN = "#Not Before: May 21 02:36:52 2025 GMT\n"
            + "#Not After : May 21 00:00:00 2027 GMT\n"
            + "dev tun\nport 2100\nclient\nremote-cert-tls server\nremote openvpn.bmstu.ru\n"
            + "reneg-sec 28800\nauth-user-pass\n# tun-mtu 1500\ntun-mtu 1492\nmssfix 1400\n"
            + "up /bin/sh\nplugin evil.so\n"
            + "<ca>\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n</ca>\n";

    @Test
    public void keepsWhitelistAndDropsCommands() {
        BmstuConfig c = BmstuConfig.parse(OVPN);
        assertEquals("openvpn.bmstu.ru", c.remoteHost);
        assertEquals("May 21 00:00:00 2027 GMT", c.notAfter);
        assertTrue(c.text.contains("port 2100\n"));
        assertTrue(c.text.contains("<ca>\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n</ca>"));
        assertFalse(c.text.contains("up /bin/sh"));
        assertFalse(c.text.contains("plugin"));
        assertFalse(c.text.contains("dev tun"));
        assertTrue(c.skipped.contains("plugin"));
        assertFalse("up заменяется приложением, о нём не предупреждаем", c.skipped.contains("up"));
    }

    @Test(expected = IllegalArgumentException.class)
    public void requiresCa() {
        BmstuConfig.parse("client\nremote host 1194\n");
    }

    @Test(expected = IllegalArgumentException.class)
    public void rejectsExternalFiles() {
        BmstuConfig.parse("remote host 1194\nca /etc/ca.crt\n");
    }

    @Test(expected = IllegalArgumentException.class)
    public void rejectsTlsCrypt() {
        BmstuConfig.parse("remote host 1194\n<ca>\nX\n</ca>\n<tls-crypt>\nY\n</tls-crypt>\n");
    }
}
