package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Domain packs (docs/domain-pack-standard.md §6) are kept in this file so
// the agent and session services themselves only gain a few call sites.

// domainPackNow is the clock of the overdue-review line; tests replace it.
var domainPackNow = types.PromptNow

// SetDomainPacks installs the packs shipped in this build: a copied agent
// keeps only the packs that still ship.
func (s *customAgentService) SetDomainPacks(registry *domainpacks.Registry) {
	s.domainPacks = registry
}

// shippedDomainPacks keeps the names this build ships, in order.
func (s *customAgentService) shippedDomainPacks(ctx context.Context, names []string) []string {
	if len(names) == 0 {
		return nil
	}
	packs, unknown := s.domainPacks.Resolve(names)
	if len(unknown) > 0 {
		logger.Warnf(ctx, "Copied agent drops domain packs this build does not ship: %v", unknown)
	}
	kept := make([]string, 0, len(packs))
	for _, pack := range packs {
		kept = append(kept, pack.Name())
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// SetDomainPacks installs the packs shipped in this build, loaded once at
// startup by the container.
func (s *sessionService) SetDomainPacks(registry *domainpacks.Registry) {
	s.domainPacks = registry
}

// resolveAgentDomainPacks turns the agent's stored pack names into the list
// this turn uses everywhere (the @-pin check, the skills manager, the
// footer): trimmed, without duplicates, shipped in this build, and only for
// a smart-reasoning agent. What is dropped is reported here, once. An agent
// saved against another build still runs.
func (s *sessionService) resolveAgentDomainPacks(ctx context.Context, customAgent *types.CustomAgent) []string {
	if customAgent == nil || len(customAgent.Config.DomainPacks) == 0 {
		return nil
	}
	if !customAgent.IsAgentMode() {
		logger.Warnf(ctx, "Agent %s is not smart-reasoning; its domain packs are ignored: %v",
			customAgent.ID, customAgent.Config.DomainPacks)
		return nil
	}
	packs, unknown := s.domainPacks.Resolve(customAgent.Config.DomainPacks)
	if len(unknown) > 0 {
		logger.Warnf(ctx, "Agent %s names domain packs this build does not ship; skipped: %v", customAgent.ID, unknown)
		common.PipelineWarn(ctx, "Agent", "domain_pack_unknown", map[string]interface{}{
			"agent_id": customAgent.ID,
			"packs":    unknown,
		})
	}
	names := make([]string, 0, len(packs))
	for _, pack := range packs {
		names = append(names, pack.Name())
	}
	return names
}

// SetDomainPacks installs the packs shipped in this build, loaded once at
// startup by the container.
func (s *agentService) SetDomainPacks(registry *domainpacks.Registry) {
	s.domainPacks = registry
}

// usableDomainPacks are the agent's packs whose required tools this run
// actually registered. The registry is the effective tool set: the
// allowlist (or the defaults) after registerTools stripped what the turn
// cannot use, e.g. search_knowledge without a knowledge base. A pack
// without its tools would steer the model to calls it cannot make.
//
// It runs before read_file, the pinned MCP tools and the browser tools are
// registered. That is safe only because validate.go requires
// required_tools ⊆ AvailableToolDefinitions, which registerTools has
// already handled by the time this runs.
func (s *agentService) usableDomainPacks(
	ctx context.Context, config *types.AgentConfig, registry *tools.ToolRegistry, warn bool,
) []*domainpacks.Pack {
	if config == nil || len(config.DomainPacks) == 0 || config.SkillInstallMode() {
		return nil
	}
	packs, unknown := s.domainPacks.Resolve(config.DomainPacks)
	if warn && len(unknown) > 0 {
		logger.Warnf(ctx, "Domain packs this build does not ship; skipped: %v", unknown)
	}
	usable := packs[:0]
	for _, pack := range packs {
		var missing []string
		for _, name := range pack.Manifest.Metadata.RequiredTools {
			if registry == nil {
				missing = append(missing, name)
			} else if _, err := registry.GetTool(name); err != nil {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			if warn {
				logger.Warnf(ctx, "Domain pack %s skipped: this run lacks its required tools %v", pack.Name(), missing)
				common.PipelineWarn(ctx, "Agent", "domain_pack_skipped", map[string]interface{}{
					"pack":          pack.Name(),
					"missing_tools": missing,
				})
			}
			continue
		}
		usable = append(usable, pack)
	}
	return usable
}

// attachDomainPacks gives the skills manager the agent's packs. They need no
// sandbox and do not follow SkillsEnabled.
func (s *agentService) attachDomainPacks(
	ctx context.Context, manager *skills.Manager, config *types.AgentConfig, registry *tools.ToolRegistry,
) {
	packs := s.usableDomainPacks(ctx, config, registry, true)
	if len(packs) == 0 {
		return
	}
	manager.WithPackSource(domainpacks.NewSource(packs))
	logger.Infof(ctx, "Attached %d domain pack(s) to the skills manager", len(packs))
}

// setDomainFooter makes the engine end each answer with the footer of the
// same packs the skills manager offers.
func (s *agentService) setDomainFooter(
	ctx context.Context, engine *agent.AgentEngine, config *types.AgentConfig, registry *tools.ToolRegistry,
) {
	packs := s.usableDomainPacks(ctx, config, registry, false)
	if len(packs) == 0 {
		return
	}
	source := domainpacks.NewSource(packs)
	engine.SetDomainFooter(func(_ context.Context, answer string) string {
		return source.Footer(domainPackNow(), answer)
	})
	engine.SetDomainPackPrompts(domainPackPrompts(ctx, packs))
}

// domainPackPrompts is the SKILL.md body of each pack for the system
// prompt, at most agent.MaxDomainPackPrompts of them.
func domainPackPrompts(ctx context.Context, packs []*domainpacks.Pack) []agent.DomainPackPrompt {
	if len(packs) > agent.MaxDomainPackPrompts {
		dropped := make([]string, 0, len(packs)-agent.MaxDomainPackPrompts)
		for _, p := range packs[agent.MaxDomainPackPrompts:] {
			dropped = append(dropped, p.Name())
		}
		logger.Warnf(ctx, "Only %d domain pack bodies fit the system prompt; listed but not inlined: %v",
			agent.MaxDomainPackPrompts, dropped)
		common.PipelineWarn(ctx, "Agent", "domain_pack_prompt_cap", map[string]interface{}{"packs": dropped})
		packs = packs[:agent.MaxDomainPackPrompts]
	}
	out := make([]agent.DomainPackPrompt, 0, len(packs))
	for _, p := range packs {
		out = append(out, agent.DomainPackPrompt{
			Name: p.Name(), Version: p.Manifest.Metadata.Version, Body: p.Skill.Instructions,
		})
	}
	return out
}
