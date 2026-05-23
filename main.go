package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	Port         string
	Mode         string
	HAURL        string
	HAToken      string
	Language     string
	RokidAuthAK  string
	HermesTopic  string
	HermesSite   string
	MQTTHost     string
	MQTTPort     string
	MQTTUsername string
	MQTTPassword string
}

type Server struct {
	cfg    Config
	client *HAClient
}

type HAClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type RokidRequest struct {
	Text      string `json:"text"`
	Content   string `json:"content"`
	Query     string `json:"query"`
	Input     string `json:"input"`
	SessionID string `json:"sessionId"`
}

type HermesPayload struct {
	Input     string        `json:"input"`
	SiteID    string        `json:"siteId"`
	SessionID string        `json:"sessionId"`
	Intent    HermesIntent  `json:"intent"`
	Slots     []interface{} `json:"slots"`
}

type HermesIntent struct {
	IntentName string `json:"intentName"`
}

func main() {
	cfg := Config{
		Port:         env("PORT", "8081"),
		Mode:         env("MODE", "ha-direct"),
		HAURL:        strings.TrimRight(os.Getenv("HA_URL"), "/"),
		HAToken:      os.Getenv("HA_TOKEN"),
		Language:     env("LANGUAGE", "zh-cn"),
		RokidAuthAK:  os.Getenv("ROKID_AUTH_AK"),
		HermesTopic:  env("HERMES_TOPIC", "hermes/intent/RokidCommand"),
		HermesSite:   env("HERMES_SITE_ID", "rokid-glasses"),
		MQTTHost:     os.Getenv("MQTT_HOST"),
		MQTTPort:     env("MQTT_PORT", "1883"),
		MQTTUsername: os.Getenv("MQTT_USERNAME"),
		MQTTPassword: os.Getenv("MQTT_PASSWORD"),
	}

	s := &Server{
		cfg: cfg,
		client: &HAClient{
			baseURL: cfg.HAURL,
			token:   cfg.HAToken,
			http:    &http.Client{Timeout: 20 * time.Second},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /rokid/sse", s.rokidSSE)

	addr := ":" + cfg.Port
	log.Printf("rokid-hermes-connector listening on %s mode=%s", addr, cfg.Mode)
	if err := http.ListenAndServe(addr, logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": s.cfg.Mode})
}

func (s *Server) rokidSSE(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeSSE(w, "error", map[string]string{"error": "unauthorized"})
		return
	}

	rokidReq, text, err := extractText(r)
	if err != nil {
		writeSSE(w, "error", map[string]string{"error": err.Error()})
		return
	}

	writeSSE(w, "message", map[string]string{"content": "正在处理 Rokid 指令..."})
	message, err := s.handleInput(r.Context(), rokidReq, text)
	if err != nil {
		writeSSE(w, "error", map[string]string{"error": err.Error()})
		return
	}

	writeSSE(w, "message", map[string]string{"content": message})
	writeSSE(w, "done", map[string]bool{"ok": true})
}

func (s *Server) handleInput(ctx context.Context, req RokidRequest, text string) (string, error) {
	switch s.cfg.Mode {
	case "ha-direct":
		data, err := s.client.conversation(ctx, text, s.cfg.Language)
		if err != nil {
			return "", err
		}
		return summarizeHAResult(data), nil
	case "hermes-log":
		payload := s.hermesPayload(req, text)
		data, _ := json.Marshal(payload)
		log.Printf("hermes topic=%s payload=%s", s.cfg.HermesTopic, data)
		return fmt.Sprintf("已生成 Hermes intent：%s", string(data)), nil
	case "hermes-mqtt":
		payload := s.hermesPayload(req, text)
		if err := s.publishHermes(ctx, payload); err != nil {
			return "", err
		}
		data, _ := json.Marshal(payload)
		return fmt.Sprintf("已发布 Hermes intent：%s", string(data)), nil
	default:
		return "", fmt.Errorf("unsupported MODE: %s", s.cfg.Mode)
	}
}

func (s *Server) hermesPayload(req RokidRequest, text string) HermesPayload {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("rokid-%d", time.Now().UnixNano())
	}
	return HermesPayload{
		Input:     text,
		SiteID:    s.cfg.HermesSite,
		SessionID: sessionID,
		Intent:    HermesIntent{IntentName: "RokidCommand"},
		Slots:     []interface{}{},
	}
}

func (s *Server) publishHermes(ctx context.Context, payload HermesPayload) error {
	if s.cfg.MQTTHost == "" {
		return errors.New("MQTT_HOST must be configured for hermes-mqtt mode")
	}
	port, err := strconv.Atoi(s.cfg.MQTTPort)
	if err != nil || port <= 0 {
		return fmt.Errorf("invalid MQTT_PORT: %s", s.cfg.MQTTPort)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	broker := s.mqttBrokerURL(port)
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(fmt.Sprintf("rokid-hermes-%d", time.Now().UnixNano())).
		SetKeepAlive(30 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetCleanSession(true).
		SetOrderMatters(false)
	if s.cfg.MQTTUsername != "" {
		opts.SetUsername(s.cfg.MQTTUsername)
		opts.SetPassword(s.cfg.MQTTPassword)
	}
	if strings.HasPrefix(broker, "ssl://") || strings.HasPrefix(broker, "tls://") {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		client.Disconnect(250)
		return errors.New("mqtt connect timeout")
	}
	if token.Error() != nil {
		client.Disconnect(250)
		return token.Error()
	}
	defer client.Disconnect(250)

	pub := client.Publish(s.cfg.HermesTopic, 1, false, data)
	if !pub.WaitTimeout(10 * time.Second) {
		return errors.New("mqtt publish timeout")
	}
	return pub.Error()
}

func (s *Server) mqttBrokerURL(port int) string {
	host := strings.TrimSpace(s.cfg.MQTTHost)
	if strings.Contains(host, "://") {
		return host
	}
	return fmt.Sprintf("tcp://%s:%d", host, port)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.RokidAuthAK == "" {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		return host == "127.0.0.1" || host == "::1" || host == ""
	}
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == s.cfg.RokidAuthAK {
		return true
	}
	if r.Header.Get("X-Auth-AK") == s.cfg.RokidAuthAK {
		return true
	}
	return false
}

func (c *HAClient) conversation(ctx context.Context, text, language string) ([]byte, error) {
	if c.baseURL == "" || c.token == "" {
		return nil, errors.New("HA_URL and HA_TOKEN must be configured for ha-direct mode")
	}
	payload := map[string]interface{}{"text": text, "language": language}
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/conversation/process", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return data, fmt.Errorf("home assistant returned %d: %s", res.StatusCode, string(data))
	}
	return data, nil
}

func extractText(r *http.Request) (RokidRequest, string, error) {
	defer r.Body.Close()
	var req RokidRequest
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		return req, "", err
	}
	for _, value := range []string{req.Text, req.Content, req.Query, req.Input} {
		if strings.TrimSpace(value) != "" {
			return req, strings.TrimSpace(value), nil
		}
	}
	return req, "", errors.New("text/content/query/input is required")
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	data, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeSSE(w http.ResponseWriter, event string, payload interface{}) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func summarizeHAResult(data []byte) string {
	if len(data) == 0 || string(data) == "null" {
		return "指令已发送到 Home Assistant。"
	}
	if len(data) > 500 {
		return "Home Assistant 已处理指令并返回较长结果。"
	}
	return "Home Assistant 返回：" + string(data)
}
