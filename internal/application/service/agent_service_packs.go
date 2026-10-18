package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Domain packs (docs/domain-pack-standard.md §6) are kept in this file so
// the agent service itself only gains a few call sites.

// domainPackNow is the clock of the overdue-review line; tests replace it.
var domainPackNow = types.PromptNow

// SetDomainPacks installs the packs shipped in this build, loaded once at
// startup by the container.
func (s *agentService) SetDomainPacks(registry *domainpacks.Registry) {
	s.domainPacks = registry
}

// agentDomainPacks resolves the agent's configured pack names. Names this
// build does not ship are reported when warn is set and skipped: an agent
// saved against another build still runs.
func (s *agentService) agentDomainPacks(ctx context.Context, config *types.AgentConfig, warn bool) []*domainpacks.Pack {
	if config == nil || len(config.DomainPacks) == 0 {
		return nil
	}
	packs, unknown := s.domainPacks.Resolve(config.DomainPacks)
	if warn && len(unknown) > 0 {
		logger.Warnf(ctx, "Agent names domain packs this build does not ship; skipped: %v", unknown)
	}
	return packs
}

// attachDomainPacks gives the skills manager the agent's packs. They need no
// sandbox and do not follow SkillsEnabled.
func (s *agentService) attachDomainPacks(ctx context.Context, manager *skills.Manager, config *types.AgentConfig) {
	packs := s.agentDomainPacks(ctx, config, true)
	if len(packs) == 0 {
		return
	}
	manager.WithPackSource(domainpacks.NewSource(packs))
	logger.Infof(ctx, "Attached %d domain pack(s) to the skills manager", len(packs))
}

// setDomainFooter makes the engine end each answer with the packs' footer.
func (s *agentService) setDomainFooter(ctx context.Context, engine *agent.AgentEngine, config *types.AgentConfig) {
	if config.SkillInstallMode() {
		return
	}
	packs := s.agentDomainPacks(ctx, config, false)
	if len(packs) == 0 {
		return
	}
	source := domainpacks.NewSource(packs)
	engine.SetDomainFooter(func(context.Context) string {
		return source.Footer(domainPackNow())
	})
}
