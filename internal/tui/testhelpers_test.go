package tui

import (
	"testing"

	"github.com/GovnocoderAI/kern/internal/agent"
	appcore "github.com/GovnocoderAI/kern/internal/app"
	"github.com/GovnocoderAI/kern/internal/config"
	"github.com/GovnocoderAI/kern/internal/permission"
	"github.com/GovnocoderAI/kern/internal/session"
	"github.com/GovnocoderAI/kern/internal/tools"
)

// newTestModel builds a Model against a minimal in-memory App so layout and
// rendering can be tested without touching the network or the real config.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	initTheme("amber")

	workdir := t.TempDir()
	cfg := &config.Config{
		Workdir:   workdir,
		Model:     "test/model",
		Theme:     "amber",
		Providers: map[string]*config.Provider{},
		Agents:    map[string]*config.Agent{},
	}
	sess := session.New(workdir, "test/model")
	perms := permission.New(nil, true, nil)
	registry := tools.NewRegistry(workdir, "")
	ag := agent.New(nil, "test", "model", "system", registry, perms, nil)
	ag.History = sess.Messages

	app := &appcore.App{
		Config:     cfg,
		Session:    sess,
		Agent:      ag,
		Store:      session.NewStore(),
		Workdir:    workdir,
		ProviderID: "test",
		ModelID:    "model",
	}
	return New(app, NewObserver(), newPermBridge())
}
