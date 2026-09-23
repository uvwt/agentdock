package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillbundle "github.com/uvwt/agentdock/internal/skill/bundle"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func runSkillCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("用法：agentdock skill bootstrap --bundle <目录>")
	}
	switch args[0] {
	case "bootstrap":
		return runSkillBootstrapCommand(ctx, args[1:], stdout, stderr)
	case "finalize-migration":
		if len(args) != 1 {
			return errors.New("用法：agentdock skill finalize-migration")
		}
		return runSkillFinalizeMigrationCommand(stdout)
	default:
		return errors.New("用法：agentdock skill bootstrap --bundle <目录>")
	}
}

func runSkillBootstrapCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock skill bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bundleDir := flags.String("bundle", "", "Release 随附 Skill Bundle 目录")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*bundleDir) == "" {
		return errors.New("用法：agentdock skill bootstrap --bundle <目录>")
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	state, err := skillstate.New(config.SkillDir(cfg))
	if err != nil {
		return err
	}
	manager, err := skills.New(state)
	if err != nil {
		return err
	}
	if _, err := skills.MigrateLegacyLayoutForUpdate(ctx, cfg.AgentDockHome, manager); err != nil {
		return err
	}
	result, err := skillbundle.Bootstrap(ctx, state, manager, *bundleDir)
	if err != nil {
		return err
	}
	for _, item := range result.Skills {
		fmt.Fprintf(stdout, "bundled skill installed: %s %s\n", item.Name, item.ContentDigest)
	}
	return nil
}

func runSkillFinalizeMigrationCommand(stdout io.Writer) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	backup, err := skills.FinalizeLegacyMigration(cfg.AgentDockHome)
	if err != nil {
		return err
	}
	if backup != "" {
		fmt.Fprintf(stdout, "legacy Skill migration finalized: %s\n", backup)
	}
	return nil
}
