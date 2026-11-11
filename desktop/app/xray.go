package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	xcore "github.com/xtls/xray-core/core"
	xserial "github.com/xtls/xray-core/infra/conf/serial"
	"github.com/xtls/xray-core/transport/internet"

	// Все протоколы и транспорты Xray, включая inbound tun
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Запуск ядра Xray и замер задержки — то, что на Android делает AndroidLibXrayLite.

func startXray(config string) (*xcore.Instance, error) {
	cfg, err := xserial.LoadJSONConfig(strings.NewReader(config))
	if err != nil {
		return nil, fmt.Errorf("ошибка конфига: %w", err)
	}
	inst, err := xcore.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("ядро не создано: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return nil, fmt.Errorf("ядро не запустилось: %w", err)
	}
	return inst, nil
}

// resetDialerControllers снимает привязку сокетов Xray к физическому интерфейсу, которую TUN
// регистрирует глобально при каждом запуске: после остановки VPN она не нужна и копилась бы.
func resetDialerControllers() {
	internet.ControllersLock.Lock()
	internet.Controllers = nil
	internet.ControllersLock.Unlock()
}

// measureOutboundDelay — замер сервера отдельным экземпляром ядра только с выходом на сервер.
func measureOutboundDelay(config, url string) (int64, error) {
	cfg, err := xserial.LoadJSONConfig(strings.NewReader(config))
	if err != nil {
		return -1, fmt.Errorf("config: %w", err)
	}
	cfg.Inbound = nil
	var apps []*serial.TypedMessage
	for _, a := range cfg.App {
		if a.Type == "xray.app.proxyman.OutboundConfig" || a.Type == "xray.app.dispatcher.Config" ||
			a.Type == "xray.app.log.Config" {
			apps = append(apps, a)
		}
	}
	cfg.App = apps
	inst, err := xcore.New(cfg)
	if err != nil {
		return -1, err
	}
	if err := inst.Start(); err != nil {
		return -1, err
	}
	defer inst.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return measureInstDelay(ctx, inst, url)
}

// measureInstDelay — лучшая из двух попыток HTTP-запроса через экземпляр ядра, мс.
func measureInstDelay(ctx context.Context, inst *xcore.Instance, url string) (int64, error) {
	if inst == nil {
		return -1, errors.New("ядро не запущено")
	}
	tr := &http.Transport{
		TLSHandshakeTimeout: 6 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return xcore.Dial(ctx, inst, dest)
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second}
	best := int64(-1)
	var lastErr error
	for i := 0; i < 2; i++ {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return -1, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			lastErr = fmt.Errorf("ответ %s", resp.Status)
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}
		if d := time.Since(start).Milliseconds(); best < 0 || d < best {
			best = d
		}
	}
	if best < 0 {
		if lastErr == nil {
			lastErr = ctx.Err()
		}
		return -1, lastErr
	}
	if best == 0 {
		best = 1
	}
	return best, nil
}
