package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"
)

// Client — вызов команд: внутри процесса (окно на Windows) или через сокет (окно на Linux, командная строка).
type Client struct {
	local http.Handler
	hc    *http.Client
}

// Local — команды обработчиком этого же процесса, без сокета.
func Local(h http.Handler) *Client { return &Client{local: h} }

// Dial — команды через сокет управления. candidates — пути сокетов, перебираются при каждом подключении
// (именованный канал на Windows, сокет на Linux); берётся первый, что отвечает.
func Dial(candidates func() []string) *Client {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			err := ErrUnavailable
			for _, p := range candidates() {
				c, e := dialSocket(ctx, p)
				if e == nil {
					return c, nil
				}
				err = e
			}
			return nil, err
		},
		DisableKeepAlives: true,
	}
	return &Client{hc: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
}

// ErrUnavailable — приложение (служба) не отвечает.
var ErrUnavailable = errors.New("служба VPN routing не запущена")

// Call выполняет команду: GET, если body == nil, иначе POST с JSON. out — куда разобрать ответ (может быть nil).
func (c *Client) Call(path string, body any, out any) error {
	method := http.MethodGet
	var rd io.Reader
	if body != nil {
		method = http.MethodPost
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://vpnrouting"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	var status int
	var data []byte
	if c.local != nil {
		rec := httptest.NewRecorder()
		c.local.ServeHTTP(rec, req)
		status, data = rec.Code, rec.Body.Bytes()
	} else {
		resp, err := c.hc.Do(req)
		if err != nil {
			return ErrUnavailable
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		status = resp.StatusCode
	}
	if status != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("ответ %d", status)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Do — команда с разбором стандартного ответа.
func (c *Client) Do(path string, body any) (*Result, error) {
	var r Result
	if body == nil {
		body = struct{}{}
	}
	err := c.Call(path, body, &r)
	return &r, err
}

// State — текущее состояние.
func (c *Client) State() (*State, error) {
	var st State
	if err := c.Call("/api/state", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
