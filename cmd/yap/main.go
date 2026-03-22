package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/First008/yap/internal/claude"
	"github.com/First008/yap/internal/config"
	"github.com/First008/yap/internal/mcp"
	"github.com/First008/yap/internal/stt"
	"github.com/First008/yap/internal/tts"
	"github.com/First008/yap/internal/tui"
)

var version = "0.1.0"

func main() {
	// Load .env file if present (for API keys etc.)
	godotenv.Load(resolveConfigPath(".env"))

	mcpMode := flag.Bool("mcp", false, "Run as MCP stdio server (used by Claude)")
	prNum := flag.String("pr", "", "Review a GitHub PR by number")
	prompt := flag.String("p", "", "Custom review prompt")
	preset := flag.String("preset", "", "Review preset: security, performance, onboarding")
	staged := flag.Bool("staged", false, "Review staged changes instead of unstaged")
	noResume := flag.Bool("no-resume", false, "Don't resume Claude session on exit")
	configPath := flag.String("config", "yap.yaml", "Config file path")
	testTTS := flag.String("test-tts", "", "Test TTS with the given text")
	testSTT := flag.Bool("test-stt", false, "Test STT: listen and print transcription")
	testPTT := flag.Bool("test-ptt", false, "Test push-to-talk STT")
	flag.Parse()

	// Accept positional argument as prompt
	if *prompt == "" && flag.NArg() > 0 {
		p := flag.Arg(0)
		prompt = &p
	}

	cfg, err := config.Load(resolveConfigPath(*configPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch {
	case *testTTS != "":
		runTestTTS(ctx, cfg, *testTTS)
	case *testSTT:
		runTestSTT(ctx, cfg)
	case *testPTT:
		runTestPTT(ctx, cfg)
	case *mcpMode:
		runMCP(ctx, cfg)
	default:
		runReview(ctx, cfg, *prNum, *prompt, *preset, *staged, *noResume)
	}
}

func runMCP(ctx context.Context, cfg *config.Config) {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting working directory: %v\n", err)
		os.Exit(1)
	}

	sockPath := filepath.Join(dir, ".yap.sock")
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "yap: waiting for TUI — run ./review in another terminal\n")
	}

	server, err := mcp.NewServer(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating MCP server: %v\n", err)
		os.Exit(1)
	}

	if err := server.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func runReview(ctx context.Context, cfg *config.Config, prNum, customPrompt, preset string, reviewStaged, noResume bool) {
	ttsAdapter := buildTTS(cfg)
	sttAdapter := buildSTT(cfg)

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Start the TUI + IPC server
	app, err := tui.NewApp(dir, ttsAdapter, sttAdapter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating app: %v\n", err)
		os.Exit(1)
	}

	// Pre-fetch file list and diffs (populates TUI + diff cache)
	filesSummary := app.ChangedFilesSummary(reviewStaged)

	// Ensure MCP server is registered globally and permissions are set
	ensureMCPServer()
	ensureMCPPermissions(dir)

	// Write a temporary MCP config for the headless Claude session
	mcpConfigPath := writeMCPConfig(dir)
	if mcpConfigPath != "" {
		defer os.Remove(mcpConfigPath)
	}

	// Load persona if available
	persona, personaErr := config.LoadPersona(resolveConfigPath("yap.persona.yaml"))
	if personaErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", personaErr)
	}

	// Build the Claude prompt with pre-fetched file info
	reviewPrompt := buildReviewPrompt(prNum, customPrompt, preset, filesSummary, persona)

	// Launch Claude headlessly in the background
	session, err := claude.Launch(reviewPrompt, mcpConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not launch Claude: %v\n", err)
		fmt.Fprintf(os.Stderr, "Running TUI without Claude. Use /review in Claude Code to start.\n")
		// Run TUI without Claude — user can connect manually
		if err := app.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Run TUI (blocks until user quits)
	if err := app.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	// TUI exited — stop the headless Claude process
	session.Stop()

	// Give Claude a moment to flush output (session ID)
	time.Sleep(500 * time.Millisecond)

	// Resume Claude interactively, or print resume command
	if !noResume && session.SessionID() != "" {
		if err := session.ExecResume(); err != nil {
			// ExecResume failed — print the resume command instead
			fmt.Printf("\nResume with: %s\n", session.ResumeCommand())
		}
	} else if session.SessionID() != "" {
		fmt.Printf("\nResume with: %s\n", session.ResumeCommand())
	}
}

var presetPrompts = map[string]string{
	"security": "Focus on SECURITY issues: authentication, authorization, injection attacks, " +
		"secrets in code, input validation, CSRF, XSS, SQL injection, path traversal, " +
		"insecure deserialization, and missing access controls. Flag anything suspicious.",
	"performance": "Focus on PERFORMANCE issues: unnecessary allocations, N+1 queries, " +
		"missing caching, unbounded loops, large memory copies, sync operations that could " +
		"be async, missing indexes, and O(n²) algorithms. Suggest optimizations.",
	"onboarding": "This is an ONBOARDING review for a new team member. Explain each file " +
		"in detail: what it does, how it fits in the architecture, key patterns used, and " +
		"any gotchas. Be thorough and educational. Longer explanations are fine here.",
}

func buildReviewPrompt(prNum, customPrompt, preset, filesSummary string, persona *config.Persona) string {
	if customPrompt != "" {
		return customPrompt
	}

	presetSuffix := ""
	if p, ok := presetPrompts[preset]; ok {
		presetSuffix = "\n\nREVIEW FOCUS: " + p
	}

	if prNum != "" {
		return fmt.Sprintf(
			"Review GitHub PR #%s. Use the review MCP tools. "+
				"Call get_changed_files first, then batch_review for all files. "+
				"Follow CLAUDE.md.%s",
			prNum, presetSuffix,
		)
	}

	personaSuffix := ""
	if persona != nil {
		if persona.VoiceStyle != "" {
			personaSuffix += "\n\nSPEAKING STYLE: " + strings.TrimSpace(persona.VoiceStyle)
		}
		if persona.ReviewFocus != "" {
			personaSuffix += "\n\nREVIEW PRIORITIES: " + strings.TrimSpace(persona.ReviewFocus)
		}
		if persona.Ignore != "" {
			personaSuffix += "\n\nIGNORE: " + strings.TrimSpace(persona.Ignore)
		}
	}

	return fmt.Sprintf(
		"Voice-driven code review. File list is pre-loaded in the TUI. "+
			"Analyze ALL diffs, group related files, and call batch_review with everything in ONE call. "+
			"DO NOT call review_file individually. Follow CLAUDE.md.%s%s\n\n"+
			"Files to review:\n%s",
		presetSuffix, personaSuffix, filesSummary,
	)
}

func runTestTTS(ctx context.Context, cfg *config.Config, text string) {
	adapter := buildTTS(cfg)
	fmt.Println("Speaking:", text)
	if err := adapter.Speak(ctx, text); err != nil {
		fmt.Fprintf(os.Stderr, "TTS error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Done.")
}

func runTestSTT(ctx context.Context, cfg *config.Config) {
	adapter := buildSTT(cfg)
	fmt.Println("Listening... speak now.")
	text, err := adapter.Listen(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "STT error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("You said:", text)
}

func runTestPTT(ctx context.Context, cfg *config.Config) {
	adapter := buildSTT(cfg)
	fmt.Println("=== Push-to-Talk STT Test ===")
	fmt.Println("Same code path as the review app.")
	fmt.Println("")

	for {
		fmt.Print(">> Press Enter to START recording... ")
		fmt.Scanln()

		stopCh := make(chan struct{}, 1)
		go func() {
			fmt.Print(">> Press Enter to STOP recording... ")
			fmt.Scanln()
			stopCh <- struct{}{}
		}()

		fmt.Println("   Recording...")
		text, err := adapter.ListenPTT(ctx, stopCh)
		if err != nil {
			fmt.Fprintf(os.Stderr, "   STT error: %v\n", err)
			continue
		}

		if text == "" {
			fmt.Println("   Result: (empty — hallucination filtered)")
		} else {
			fmt.Printf("   Result: %q\n", text)
		}
		fmt.Println("")
	}
}

func buildTTS(cfg *config.Config) tts.TTSAdapter {
	switch cfg.TTS.Adapter {
	case "openai":
		apiKey := cfg.TTS.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return tts.NewOpenAIAdapter(apiKey, cfg.TTS.Voice, cfg.TTS.Model, cfg.TTS.Speed)
	default:
		return tts.NewSayAdapter(cfg.TTS.Voice, cfg.TTS.Rate)
	}
}

func buildSTT(cfg *config.Config) stt.STTAdapter {
	switch cfg.STT.Adapter {
	case "openai":
		apiKey := cfg.STT.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return stt.NewOpenAIAdapter(apiKey)
	default:
		return stt.NewWhisperAdapter(cfg.STT.Model, cfg.STT.SilenceTimeout)
	}
}

// resolveConfigPath looks for a config file in cwd first, then next to the yap binary.
// This allows project-local configs to override, while global configs in the yap
// directory work when running yap from any repo via alias.
func resolveConfigPath(name string) string {
	// If it's an absolute path, use as-is
	if filepath.IsAbs(name) {
		return name
	}

	// Check cwd first
	if _, err := os.Stat(name); err == nil {
		return name
	}

	// Fall back to binary's directory
	if bin, err := os.Executable(); err == nil {
		if bin, err = filepath.EvalSymlinks(bin); err == nil {
			binDir := filepath.Dir(bin)
			candidate := filepath.Join(binDir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	// Return original (will trigger default config)
	return name
}

// writeMCPConfig creates a temporary MCP config file for the headless Claude session.
// This is needed because `claude -p` (non-interactive) doesn't load ~/.claude/.mcp.json.
func writeMCPConfig(projectDir string) string {
	yapBin, err := os.Executable()
	if err != nil {
		return ""
	}
	yapBin, err = filepath.EvalSymlinks(yapBin)
	if err != nil {
		return ""
	}

	config := map[string]any{
		"mcpServers": map[string]any{
			"yap": map[string]any{
				"type":    "stdio",
				"command": yapBin,
				"args":    []string{"--mcp"},
			},
		},
	}

	f, err := os.CreateTemp("", "yap-mcp-*.json")
	if err != nil {
		return ""
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	f.Write(data)
	f.Close()
	return f.Name()
}

// ensureMCPServer adds yap as a global MCP server in ~/.claude/.mcp.json
// so Claude Code can use yap's review tools from any project.
func ensureMCPServer() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	// Resolve the absolute path of the running yap binary
	yapBin, err := os.Executable()
	if err != nil {
		return
	}
	yapBin, err = filepath.EvalSymlinks(yapBin)
	if err != nil {
		return
	}

	mcpPath := filepath.Join(homeDir, ".claude", ".mcp.json")

	var mcpConfig map[string]any
	if data, err := os.ReadFile(mcpPath); err == nil {
		json.Unmarshal(data, &mcpConfig)
	}
	if mcpConfig == nil {
		mcpConfig = make(map[string]any)
	}

	servers, _ := mcpConfig["mcpServers"].(map[string]any)
	if servers == nil {
		servers = make(map[string]any)
	}

	// Check if yap entry already exists with the correct command
	if existing, ok := servers["yap"].(map[string]any); ok {
		if cmd, _ := existing["command"].(string); cmd == yapBin {
			return // already configured correctly
		}
	}

	servers["yap"] = map[string]any{
		"type":    "stdio",
		"command": yapBin,
		"args":    []string{"--mcp"},
	}
	mcpConfig["mcpServers"] = servers

	os.MkdirAll(filepath.Join(homeDir, ".claude"), 0755)
	data, _ := json.MarshalIndent(mcpConfig, "", "  ")
	os.WriteFile(mcpPath, data, 0644)
	fmt.Fprintf(os.Stderr, "yap: registered MCP server in ~/.claude/.mcp.json\n")
}

// ensureMCPPermissions creates/updates .claude/settings.json to pre-approve
// all review MCP tools so Claude doesn't block on permission prompts.
func ensureMCPPermissions(projectDir string) {
	settingsDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(settingsDir, 0755)

	settingsPath := filepath.Join(settingsDir, "settings.json")

	tools := []string{
		// MCP review tools
		"mcp__yap__batch_review",
		"mcp__yap__review_file",
		"mcp__yap__get_changed_files",
		"mcp__yap__mark_reviewed",
		"mcp__yap__speak",
		"mcp__yap__listen",
		"mcp__yap__get_review_status",
		"mcp__yap__finish_review",
		"mcp__yap__show_diff",
		"mcp__yap__show_message",
		// Standard tools needed to read/fix code during review
		"Bash(git *)",
		"Bash(go *)",
		"Read",
		"Edit",
		"Write",
		"Glob",
		"Grep",
	}

	// Read existing settings or start fresh
	var settings map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	// Get or create permissions.allow
	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = make(map[string]any)
	}

	// Merge with existing allow list
	existing := make(map[string]bool)
	if allowList, ok := perms["allow"].([]any); ok {
		for _, v := range allowList {
			if s, ok := v.(string); ok {
				existing[s] = true
			}
		}
	}

	changed := false
	for _, tool := range tools {
		if !existing[tool] {
			existing[tool] = true
			changed = true
		}
	}

	if changed {
		var allow []string
		for k := range existing {
			allow = append(allow, k)
		}
		perms["allow"] = allow
		settings["permissions"] = perms

		data, _ := json.MarshalIndent(settings, "", "  ")
		os.WriteFile(settingsPath, data, 0644)
		fmt.Fprintf(os.Stderr, "yap: auto-approved MCP tool permissions in .claude/settings.json\n")
	}
}
