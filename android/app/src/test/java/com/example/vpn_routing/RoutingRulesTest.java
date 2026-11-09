package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.util.Arrays;
import java.util.List;

/** Список прямых доменов без повторов. */
public class RoutingRulesTest {

    @Test
    public void builtinListHasNoRedundantEntries() {
        assertEquals("встроенный список уже без повторов",
                RoutingRules.DIRECT_DOMAINS, RoutingRules.compact(RoutingRules.DIRECT_DOMAINS));
        for (String d : RoutingRules.DIRECT_DOMAINS) assertFalse(d, d.endsWith(".ru"));
    }

    @Test
    public void compactRemovesChildrenAndSpellings() {
        List<String> out = RoutingRules.compact(Arrays.asList(
                ".ru", "ya.ru", "рф", "xn--p1ai", "госуслуги.рф", "Vk.com", "m.vk.com", "vk.com."));
        assertEquals(Arrays.asList("ru", "xn--p1ai", "vk.com"), out);
    }

    @Test
    public void builtinCoverage() {
        assertTrue(RoutingRules.isBuiltinDirect("ya.ru"));
        assertTrue(RoutingRules.isBuiltinDirect("gosuslugi.ru"));
        assertTrue(RoutingRules.isBuiltinDirect("госуслуги.рф"));
        assertTrue(RoutingRules.isBuiltinDirect("sun9-1.userapi.com"));
        assertFalse(RoutingRules.isBuiltinDirect("example.com"));
    }

    @Test
    public void xrayConfigHasNoDuplicateDomains() throws Exception {
        String json = XrayConfig.build(ProxyLink.parse("vless://id@a.example.com:443?security=tls&type=ws#A"),
                "1.2.3.4", null, Arrays.asList("ya.ru", "example.com", "sub.example.com"), null);
        assertFalse("ya.ru покрыт зоной ru", json.contains("domain:ya.ru"));
        assertFalse("sub.example.com покрыт example.com", json.contains("domain:sub.example.com"));
        assertTrue(json.contains("domain:example.com"));
    }
}
