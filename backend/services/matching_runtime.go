package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"fan-web/services/matching"
)

// ErrMatchReviewRequired 身份/编号仍需人工确认；单番扫描应零写入。
var ErrMatchReviewRequired = errors.New("匹配需要人工确认")

// ErrMatchUpstream 网络/限流/预算/上游坏响应等基础设施错误。
var ErrMatchUpstream = errors.New("匹配上游不可用")

// MatchingRuntime 不透明：模式、不可变 catalog、Bangumi 客户端。
type MatchingRuntime struct {
	engine    matching.Engine
	bangumi   *BangumiService
	catalog   matching.Catalog
	aliasFile string
}

// NewMatchingRuntime 构造运行时并装载不可变 catalog。
// 空 aliasFile 表示只用内置；显式路径必须存在且合法，否则返回 error。
func NewMatchingRuntime(b *BangumiService, engine matching.Engine, aliasFile string) (*MatchingRuntime, error) {
	var override []byte
	if aliasFile != "" {
		info, err := os.Stat(aliasFile)
		if err != nil {
			return nil, fmt.Errorf("匹配别名文件不可用: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("匹配别名文件不能是目录: %s", aliasFile)
		}
		raw, err := os.ReadFile(aliasFile)
		if err != nil {
			return nil, fmt.Errorf("读取匹配别名文件失败: %w", err)
		}
		override = raw
	}
	catalog, err := matching.LoadCatalog(matching.BuiltinTitleAliasesJSON, override)
	if err != nil {
		return nil, fmt.Errorf("装载匹配别名 catalog 失败: %w", err)
	}
	return &MatchingRuntime{
		engine:    engine,
		bangumi:   b,
		catalog:   catalog,
		aliasFile: aliasFile,
	}, nil
}

// MatchEngineFromEnv 解析 MATCH_ENGINE：空/legacy→Legacy；显式 v2→V2；未知→Legacy 并警告。
// 本次合入默认 legacy；v2 须环境变量显式开启。
func MatchEngineFromEnv(raw string) matching.Engine {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "v2":
		return matching.EngineV2
	case "", "legacy":
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

// Catalog 返回装载后的只读系列别名索引（供 Session / 测试）。
func (r *MatchingRuntime) Catalog() matching.Catalog {
	if r == nil {
		return matching.Catalog{}
	}
	return r.catalog
}

// NewSession 创建本次操作独享的 MatchingSession（含 Provider 预算/缓存/降级状态）。
func (r *MatchingRuntime) NewSession(ctx context.Context) *MatchingSession {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &MatchingSession{runtime: r, ctx: ctx}
	if r != nil && r.bangumi != nil {
		s.provider = newBangumiMatchingProvider(r.bangumi, realMatchingClock{})
	}
	return s
}

// newMatchingSessionForTest 包内测试 helper：注入 fake Provider（不暴露生产开关）。
func newMatchingSessionForTest(ctx context.Context, r *MatchingRuntime, p matchingProvider) *MatchingSession {
	if ctx == nil {
		ctx = context.Background()
	}
	return &MatchingSession{runtime: r, ctx: ctx, provider: p}
}
