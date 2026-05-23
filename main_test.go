package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizedRequiresHeaderWhenAKConfigured(t *testing.T) {
	server := &Server{cfg: Config{RokidAuthAK: "secret"}}
	req := httptest.NewRequest(http.MethodPost, "/rokid/sse", bytes.NewReader([]byte(`{"text":"开灯"}`)))
	req.RemoteAddr = "127.0.0.1:12345"
	if server.authorized(req) {
		t.Fatal("expected unauthorized without token")
	}
	req.Header.Set("Authorization", "Bearer secret")
	if !server.authorized(req) {
		t.Fatal("expected authorized with bearer token")
	}
}

func TestExtractTextPrefersFirstNonEmptyField(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/rokid/sse", bytes.NewReader([]byte(`{"content":"  关灯  "}`)))
	_, text, err := extractText(req)
	if err != nil {
		t.Fatal(err)
	}
	if text != "关灯" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestPublishHermesRejectsMissingMQTTConfig(t *testing.T) {
	server := &Server{cfg: Config{Mode: "hermes-mqtt", HermesTopic: "hermes/intent/RokidCommand", HermesSite: "rokid-glasses"}}
	err := server.publishHermes(t.Context(), HermesPayload{Input: "打开客厅灯", SiteID: "rokid-glasses", SessionID: "debug", Intent: HermesIntent{IntentName: "RokidCommand"}, Slots: []interface{}{}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHermesPayloadShape(t *testing.T) {
	server := &Server{cfg: Config{HermesSite: "rokid-glasses"}}
	payload := server.hermesPayload(RokidRequest{SessionID: "debug"}, "打开客厅灯")
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"intentName":"RokidCommand"`)) {
		t.Fatalf("unexpected payload: %s", data)
	}
}

func TestMQTTBrokerURL(t *testing.T) {
	server := &Server{cfg: Config{MQTTHost: "127.0.0.1"}}
	if got := server.mqttBrokerURL(1883); got != "tcp://127.0.0.1:1883" {
		t.Fatalf("unexpected broker url: %s", got)
	}
	server.cfg.MQTTHost = "tls://mqtt.example.com:8883"
	if got := server.mqttBrokerURL(1883); got != "tls://mqtt.example.com:8883" {
		t.Fatalf("unexpected broker url: %s", got)
	}
}
