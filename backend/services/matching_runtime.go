package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"fan-web/services/matching"
)

// MatchingRuntime 不透明：模式、不可变 catalog、Bangumi 客户端。
// S0 仅端口骨架；catalog/provider 行为由 S4 填充。
type MatchingRuntime struct {
	engine    matching.Engine
	bangumi   *BangumiService
	aliasFile string
}

// NewMatchingRuntime 构造运行时。空 aliasFile 表示只用内置；显式路径必须存在且合法。
// S0 不装载 catalog 内容（S2/S4），仅校验路径并保存引擎。
func NewMatchingRuntime(b *BangumiService, engine matching.Engine, aliasFile string) (*MatchingRuntime, error) {
	if aliasFile != "" {
		info, err := os.Stat(aliasFile)
		if err != nil {
			return nil, fmt.Errorf("匹配别名文件不可用: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("匹配别名文件不能是目录: %s", aliasFile)
		}
	}
	return &MatchingRuntime{
		engine:    engine,
		bangumi:   b,
		aliasFile: aliasFile,
	}, nil
}

// MatchEngineFromEnv 解析 MATCH_ENGINE：空/v2→V2；legacy→Legacy；其他→Legacy 并警告。
func MatchEngineFromEnv(raw string) matching.Engine {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "v2":
		return matching.EngineV2
	case "legacy":
		return matching.EngineLegacy
	default:
		log.Printf("[Matching] 未知引擎 %q，回退 legacy", raw)
		return matching.EngineLegacy
	}
}

// Engine 返回当前运行时引擎模式。
func (r *MatchingRuntime) Engine() matching.Engine {
	if r == nil {
		return matching.EngineLegacy
	}
	return r.engine
}

// NewSession 创建本次操作独享的 MatchingSession。S0 骨架不注入缓存/预算（S5）。
func (r *MatchingRuntime) NewSession(ctx context.Context) *MatchingSession {
	return &MatchingSession{runtime: r, ctx: ctx}
}
