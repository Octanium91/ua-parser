package core

import (
	"testing"
)

func TestParser(t *testing.T) {
	cfg := Config{
		DisableAutoUpdate: true,
		LRUCacheSize:      10,
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	// Test standard UA (Windows 10)
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	res := p.Parse(ua, nil)
	// Base parser usually identifies this as Windows 10 or Windows
	if res.OS.Name != "Windows" && res.OS.Name != "Windows 10" {
		t.Errorf("Expected OS Windows or Windows 10, got %s", res.OS.Name)
	}
	if res.Category != "desktop" {
		t.Errorf("Expected Category desktop, got %s", res.Category)
	}
	if res.Engine.Name != "Blink" {
		t.Errorf("Expected Engine Blink, got %s", res.Engine.Name)
	}

	// Test Client Hints for Windows 11
	headers := map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"15.0.0"`,
	}
	res = p.Parse(ua, headers)
	if res.OS.Name != "Windows" {
		t.Errorf("Expected OS Windows from Client Hints, got %s", res.OS.Name)
	}
	if res.OS.Version != "11" {
		t.Errorf("Expected OS Version 11 from Client Hints, got %s", res.OS.Version)
	}

	// Test Client Hints for Model and Arch
	headers["Sec-CH-UA-Model"] = `"Pixel 5"`
	headers["Sec-CH-UA-Arch"] = `"arm64"`
	headers["Sec-CH-UA-Mobile"] = `?1`
	res = p.Parse(ua, headers)
	if res.Device.Model != "Pixel 5" {
		t.Errorf("Expected Model Pixel 5 from Client Hints, got %s", res.Device.Model)
	}
	if res.CPU.Architecture != "arm64" {
		t.Errorf("Expected CPU arm64, got %s", res.CPU.Architecture)
	}
	if res.Device.Type != "mobile" {
		t.Errorf("Expected Device Type mobile, got %s", res.Device.Type)
	}
	if res.Category != "mobile" {
		t.Errorf("Expected Category mobile, got %s", res.Category)
	}

	// Test Client Hints overriding UA for Architecture
	uaAMD64 := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	headersArch := map[string]string{
		"Sec-CH-UA-Arch": `"arm64"`,
	}
	res = p.Parse(uaAMD64, headersArch)
	if res.CPU.Architecture != "arm64" {
		t.Errorf("Expected CPU arm64 from Client Hints overriding UA, got %s", res.CPU.Architecture)
	}

	// Test Client Hints case-insensitivity
	headersCase := map[string]string{
		"sec-ch-ua-platform":         `"Windows"`,
		"sec-ch-ua-platform-version": `"13.0.0"`,
	}
	res = p.Parse(ua, headersCase)
	if res.OS.Name != "Windows" || res.OS.Version != "11" {
		t.Errorf("Expected Windows 11 from lowercase Client Hints, got %s %s", res.OS.Name, res.OS.Version)
	}

	// Test Bot Detection
	botUA := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	res = p.Parse(botUA, nil)
	if !res.IsBot {
		t.Errorf("Expected IsBot to be true for Googlebot")
	}
	if res.Category != "bot" {
		t.Errorf("Expected Category bot, got %s", res.Category)
	}

	// Test AI Crawler
	aiUA := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.0; +https://openai.com/gptbot)"
	res = p.Parse(aiUA, nil)
	if !res.IsAICrawler {
		t.Errorf("Expected IsAICrawler to be true for GPTBot")
	}
	if !res.IsBot {
		t.Errorf("Expected IsBot to be true for GPTBot")
	}
}

func TestClientHintsAccuracy(t *testing.T) {
	cfg := Config{
		DisableAutoUpdate: true,
		LRUCacheSize:      0, // Disable cache for this test to ensure fresh results
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	headers := map[string]string{
		"Sec-CH-UA-Platform":          "\"Windows\"",
		"Sec-CH-UA-Platform-Version":  "\"13.0.0\"",
		"Sec-CH-UA-Full-Version-List": "\"Chromium\";v=\"144.0.7559.97\", \"Google Chrome\";v=\"144.0.7559.97\"",
	}

	res := p.Parse(ua, headers)

	// OS Verification
	if res.OS.Name != "Windows" {
		t.Errorf("Expected OS Name 'Windows', got '%s'", res.OS.Name)
	}
	if res.OS.Version != "11" {
		t.Errorf("Expected OS Version '11', got '%s'", res.OS.Version)
	}

	// Browser Verification
	if res.Browser.Name != "Chrome" {
		t.Errorf("Expected Browser Name 'Chrome', got '%s'", res.Browser.Name)
	}
	if res.Browser.Version != "144.0.7559.97" {
		t.Errorf("Expected Browser Version '144.0.7559.97', got '%s'", res.Browser.Version)
	}

	// Engine Verification
	if res.Engine.Name != "Blink" {
		t.Errorf("Expected Engine Name 'Blink', got '%s'", res.Engine.Name)
	}
	if res.Engine.Version != "144.0.7559.97" {
		t.Errorf("Expected Engine Version '144.0.7559.97', got '%s'", res.Engine.Version)
	}
}
