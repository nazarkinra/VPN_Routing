package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;

import java.util.Collections;

/** Требования корпоративной сети МГТУ: внешний DNS по UDP 53 закрыт, работает DNS сети, TCP и DoH. */
public class CorpNetworkTest {

    private static final String KEY = "vless://11111111-2222-3333-4444-555555555555@srv.example.com:443"
            + "?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.google.com&pbk=PUBKEY&sid=abcd&fp=chrome#R";

    private static JSONObject config(String localDns) throws Exception {
        return new JSONObject(XrayConfig.build(ProxyLink.parse(KEY), "203.0.113.10", localDns,
                Collections.emptyList(), null));
    }

    /** Ни один DNS-сервер не ходит на внешний адрес по UDP 53 — кроме DNS, выданного сетью. */
    @Test
    public void noPlainUdpDnsToExternalResolvers() throws Exception {
        for (String localDns : new String[]{"195.19.33.202", null}) {
            JSONArray servers = config(localDns).getJSONObject("dns").getJSONArray("servers");
            for (int i = 0; i < servers.length(); i++) {
                JSONObject s = servers.getJSONObject(i);
                String address = s.getString("address");
                boolean plain = !address.contains("://");
                if (plain) assertEquals("обычный DNS только от сети", "195.19.33.202", address);
                assertTrue("у каждого сервера тайм-аут", s.getInt("timeoutMs") <= 3000);
            }
        }
    }

    /** DNS сети — первым для российских доменов, затем запасные TCP и DoH в обход туннеля. */
    @Test
    public void networkDnsFirstThenTcpAndDoh() throws Exception {
        JSONArray servers = config("195.19.33.202").getJSONObject("dns").getJSONArray("servers");
        assertEquals("195.19.33.202", servers.getJSONObject(0).getString("address"));
        assertEquals("tcp+local://77.88.8.8", servers.getJSONObject(1).getString("address"));
        assertEquals("https+local://1.1.1.1/dns-query", servers.getJSONObject(2).getString("address"));
        // Остальные домены — через VPN
        assertEquals("tcp://8.8.8.8", servers.getJSONObject(3).getString("address"));
        assertEquals("https://1.1.1.1/dns-query", servers.getJSONObject(4).getString("address"));
        assertFalse(servers.getJSONObject(3).has("domains"));
    }

    /** Без DNS сети нет и правила «DNS-модуль → напрямую» с пустым списком адресов. */
    @Test
    public void noEmptyDirectDnsRule() throws Exception {
        JSONArray rules = config(null).getJSONObject("routing").getJSONArray("rules");
        for (int i = 0; i < rules.length(); i++) {
            JSONArray ip = rules.getJSONObject(i).optJSONArray("ip");
            assertTrue(ip == null || ip.length() > 0);
        }
    }

    @Test
    public void tunMtuIs1400() throws Exception {
        JSONObject tun = config(null).getJSONArray("inbounds").getJSONObject(0);
        assertEquals(1400, tun.getJSONObject("settings").getInt("mtu"));
    }

    /** Локальные сети, в том числе 172.16.0.0/12 (Wi-Fi МГТУ), идут напрямую. */
    @Test
    public void privateNetworksDirect() throws Exception {
        String json = config(null).getJSONObject("routing").toString();
        assertTrue(json.contains("172.16.0.0/12"));
        assertTrue(json.contains("10.0.0.0/8"));
    }

    @Test
    public void ipLiteralNeedsNoDns() throws Exception {
        assertEquals("203.0.113.10", ServerResolver.resolve("203.0.113.10", null).ip);
        assertTrue(ServerResolver.isIpLiteral("2001:db8::1"));
        assertFalse(ServerResolver.isIpLiteral("srv.example.com"));
    }

    @Test
    public void parsesDohJson() throws Exception {
        String body = "{\"Status\":0,\"Answer\":[{\"name\":\"a.example.\",\"type\":5,\"data\":\"b.example.\"},"
                + "{\"name\":\"b.example.\",\"type\":1,\"TTL\":60,\"data\":\"203.0.113.10\"}]}";
        assertEquals("203.0.113.10", ServerResolver.parseDohJson(body));
        assertNull(ServerResolver.parseDohJson("{\"Status\":3}"));
    }

    @Test
    public void tcpDnsPacket() {
        byte[] q = ServerResolver.dnsQuery("api.example.ru", 0x1234);
        // заголовок 12 байт + 3api7example2ru0 (16) + тип и класс (4)
        assertEquals(32, q.length);
        java.io.ByteArrayOutputStream r = new java.io.ByteArrayOutputStream();
        r.write(0x12); r.write(0x34); r.write(0x81); r.write(0x80); // ответ, RCODE 0
        r.write(0); r.write(1); r.write(0); r.write(2);             // 1 вопрос, 2 ответа
        r.write(0); r.write(0); r.write(0); r.write(0);
        r.write(q, 12, q.length - 12);                              // вопрос
        // CNAME (тип 5) по ссылке на имя, затем A
        r.write(0xc0); r.write(12); r.write(0); r.write(5); r.write(0); r.write(1);
        r.write(0); r.write(0); r.write(0); r.write(60); r.write(0); r.write(2); r.write(0xc0); r.write(12);
        r.write(0xc0); r.write(12); r.write(0); r.write(1); r.write(0); r.write(1);
        r.write(0); r.write(0); r.write(0); r.write(60); r.write(0); r.write(4);
        r.write(203); r.write(0); r.write(113); r.write(10);
        assertEquals("203.0.113.10", ServerResolver.parseDnsResponse(r.toByteArray()));
        byte[] nx = r.toByteArray();
        nx[3] = (byte) 0x83; // NXDOMAIN
        assertNull(ServerResolver.parseDnsResponse(nx));
    }

    @Test
    public void dumpHidesSecrets() throws Exception {
        String masked = ConfigDump.mask(XrayConfig.build(ProxyLink.parse(KEY), "203.0.113.10", null,
                Collections.emptyList(), new XrayConfig.BmstuRoute(1080, "u-secret", "p-secret")));
        assertFalse(masked.contains("11111111-2222"));
        assertFalse(masked.contains("PUBKEY"));
        assertFalse(masked.contains("abcd"));
        assertFalse(masked.contains("p-secret"));
        assertFalse(masked.contains("u-secret"));
        assertTrue(masked.contains("203.0.113.10"));
    }
}
