package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"fan-web/services"
	"fan-web/services/matching"
)

func TestLoadMatchingRuntimeDefaultV2Log(t *testing.T) {
	t.Setenv("FAN_WEB_MATCH_ENGINE", "")
	t.Setenv("FAN_WEB_MATCH_ALIASES_FILE", "")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	rt, err := loadMatchingRuntime(services.NewBangumiService())
	if err != nil {
		t.Fatal(err)
	}
	if rt.Engine() != matching.EngineV2 {
		t.Fatalf("default engine=%s", rt.Engine())
	}
	if !strings.Contains(buf.String(), "引擎=v2") {
		t.Fatalf("启动日志应区分 v2, got %q", buf.String())
	}
}

func TestLoadMatchingRuntimeLegacyLog(t *testing.T) {
	t.Setenv("FAN_WEB_MATCH_ENGINE", "legacy")
	// legacy 即使别名路径无效也不应读取
	t.Setenv("FAN_WEB_MATCH_ALIASES_FILE", "/no/such/aliases.json")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	rt, err := loadMatchingRuntime(services.NewBangumiService())
	if err != nil {
		t.Fatal(err)
	}
	if rt.Engine() != matching.EngineLegacy {
		t.Fatalf("legacy engine=%s", rt.Engine())
	}
	if !strings.Contains(buf.String(), "引擎=legacy") {
		t.Fatalf("启动日志应区分 legacy, got %q", buf.String())
	}
}
