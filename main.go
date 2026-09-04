// Command hyperliquid-mcp-go runs the Hyperliquid MCP server over stdio.
// stdout carries only MCP protocol frames; all logging goes to stderr.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/config"
	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
	"github.com/edkdev/hyperliquid-mcp-go/internal/tools"
)

func main() {
	// stdout is the MCP stdio channel; never write anything else to it.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := run(); err != nil {
		slog.Error(fmt.Sprintf("Server failed: %v", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.AccountAddress == cfg.WalletAddress {
		slog.Info(fmt.Sprintf("Using wallet address as account: %s", cfg.AccountAddress))
	} else {
		slog.Info(fmt.Sprintf("Agent mode: API wallet %s signing for account %s", cfg.WalletAddress, cfg.AccountAddress))
	}
	slog.Info(fmt.Sprintf("Connecting to: %s", cfg.BaseURL))

	ctx := context.Background()
	client, err := hl.New(ctx, cfg)
	if err != nil {
		return err
	}

	// Verify wallet registration (best-effort, mirrors Python).
	if accountValue, err := client.VerifyWallet(ctx); err != nil {
		slog.Warn(fmt.Sprintf("Could not verify wallet: %v", err))
		slog.Warn("Make sure your wallet is registered on Hyperliquid (deposit funds to register)")
	} else {
		slog.Info(fmt.Sprintf("Wallet verified! Account value: $%s", accountValue))
	}

	s := tools.NewServer(client)
	slog.Info("Hyperliquid MCP Server started")
	return server.ServeStdio(s)
}
