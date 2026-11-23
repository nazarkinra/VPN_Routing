package com.example.vpn_routing;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

import java.util.ArrayList;
import java.util.List;

/** Переход на тот же сервер с новым ключом после плановой смены ключей на сервере. */
public class KeyRotationTest {

    private static final String OLD = "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443"
            + "?type=ws&security=tls&path=%2Fws#NL";
    private static final String NEW = "vless://22222222-2222-2222-2222-222222222222@vpn.example.com:443"
            + "?type=ws&security=tls&path=%2Fws#NL";
    private static final String DE = "vless://33333333-3333-3333-3333-333333333333@de.example.com:443"
            + "?type=ws&security=tls&path=%2Fws#DE";
    private static final String DUP = "vless://44444444-4444-4444-4444-444444444444@vpn.example.com:443"
            + "?type=ws&security=tls&path=%2Fws#NL";

    private static List<ServerStore.Server> list(String... urls) {
        List<ServerStore.Server> out = new ArrayList<>();
        for (int i = 0; i < urls.length; i++) out.add(new ServerStore.Server("s" + i, urls[i], "sub"));
        return out;
    }

    @Test
    public void findsSameServerWithNewKey() {
        assertEquals(NEW, KeyRotation.replacementFor(OLD, list(DE, NEW)));
    }

    @Test
    public void nothingToDo() {
        assertNull(KeyRotation.replacementFor(OLD, list(OLD, NEW))); // ссылка на месте
        assertNull(KeyRotation.replacementFor(OLD, list(DE))); // такого сервера больше нет
        assertNull(KeyRotation.replacementFor(OLD, list(NEW, DUP))); // неоднозначно — не угадываем
    }
}
