# yap

> The AI that yaps at your code so your colleagues don't have to.

**yap** is a voice-driven code review TUI. It talks you through every diff, listens to your feedback, and fixes things on the spot. Think: a senior engineer sitting next to you, except it never gets tired and doesn't judge your variable names. Much.

> **Stop vibe coding. Start reviewing.** AI generates code faster than ever. But shipping code you've never actually looked at? That's how bugs, security holes, and tech debt sneak in. yap makes you look at every diff before it hits your repo — with an AI colleague walking you through it so you actually understand what changed.

> **Early stage.** The first review takes ~30-45s to start (Claude needs to analyze all diffs and generate explanations). After that, file transitions are instant thanks to batch mode. We're actively working on making the cold start faster.

## How it works

```
$ ./yap
```

That's it. yap opens a three-panel TUI, spawns Claude headlessly, and starts reviewing your changes:

```
┌──────────────┬────────────────────────────┬─────────────────┐
│ Files 3/12   │ internal/tui/model.go      │ Conversation    │
│              │                            │                 │
│ PTT support  │  45 + ptt pttState         │ Claude:         │
│  ✓ adapter.go│  46 + pttCh *PTTChannel    │ The reconnect   │
│  · model.go  │  47                        │ loop retries    │
│  · app.go    │  48   pendingG bool        │ for 30s with no │
│              │ ▸49 + mu sync.Mutex        │ backoff. Under  │
│ Config       │  50 }                      │ load this could │
│  · config.go │                            │ hammer the sock │
│              │                            │                 │
│ Other files  │                            │ You:            │
│  ✓ go.mod    │                            │ Fix that.       │
│  · keys.go   │                            │                 │
└──────────────┴────────────────────────────┴─────────────────┘
 [3/12] PTT support > model.go   5/12 reviewed  ⎵ TALK  n:NEXT
```

Claude explains each file aloud, you respond by voice (or press `n` to approve silently). When Claude finds something sketchy, you say "fix that" and it fixes it. When you're done, Ctrl+C hands you an interactive Claude session to continue the conversation.

## Features

- **Voice-driven** — Claude speaks, you respond. Push-to-talk with space bar.
- **Batch review** — All files reviewed in one MCP call. Zero latency between files.
- **Smart grouping** — Related changes grouped together (Claude figures out the connections).
- **Real reviews** — Finds bugs, security issues, race conditions. Not "looks good LGTM."
- **Quick next** — Press `n` to approve without speaking. For when Claude is right and you know it.
- **Edit in place** — Press `e` to open the file in your `$EDITOR`. Fix it yourself.
- **Auto-staging** — Approved files get `git add`-ed. Claude's fixes get staged too.
- **Syntax highlighting** — Chroma-powered, Nord theme. Supports all major languages.
- **File browser** — Tab to switch to file list, j/k to navigate, Enter to view any file.
- **Scroll-to-line** — Claude points you at the exact line that matters.
- **Resume** — Ctrl+C exits the TUI and drops you into Claude with full context.
- **Personalization** — Customize review style, focus areas, and voice via `yap.persona.yaml`.
- **Presets** — `--security`, `--performance`, `--onboarding` for different review types.
- **OpenAI TTS/STT** — Natural voice via OpenAI, or free local via macOS `say` + whisper.cpp.

## Install

```bash
# Prerequisites
brew install sox whisper-cpp
go install github.com/anthropics/claude-code@latest  # Claude Code CLI

# Build
git clone https://github.com/First008/yap
cd yap
make build

# Make yap available from anywhere
echo 'alias yap="$(pwd)/yap"' >> ~/.zshrc  # or ~/.bashrc
source ~/.zshrc

# Add /yap slash command to Claude Code (global)
cp .claude/commands/yap.md ~/.claude/commands/yap.md
```

Now you can run `yap` from any repo, or `/yap` inside Claude Code.

To use yap's MCP tools from Claude Code in any project, add to `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "yap": {
      "type": "stdio",
      "command": "/absolute/path/to/yap",
      "args": ["--mcp"]
    }
  }
}
```

### Whisper model (for local STT)

```bash
mkdir -p ~/.local/share/whisper-cpp/models
curl -L -o ~/.local/share/whisper-cpp/models/ggml-medium.en.bin \
  "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin"
```

### VAD model (reduces hallucination)

```bash
curl -L -o ~/.local/share/whisper-cpp/models/silero-vad.ggml.bin \
  "https://huggingface.co/ggml-org/whisper-vad/resolve/main/ggml-silero-v6.2.0.bin?download=true"
```

## Usage

```bash
# Review local changes
./yap

# Review with a focus
./yap --preset security
./yap --preset performance
./yap --preset onboarding

# Custom prompt
./yap -p "Focus on error handling in the auth package"

# PR review (coming soon)
./yap --pr 123
```

## Configuration

Copy the example configs:

```bash
cp yap.example.yaml yap.yaml
cp yap.persona.example.yaml yap.persona.yaml
```

### Voice (yap.yaml)

```yaml
# Free (macOS built-in)
tts:
  adapter: say
  voice: Daniel
  rate: 195

# Natural (OpenAI, ~$0.015/1K chars)
tts:
  adapter: openai
  voice: nova    # alloy, ash, ballad, cedar, coral, echo, fable, marin, nova, onyx, sage, shimmer

# STT: local (free) or cloud (accurate)
stt:
  adapter: whisper  # free, local
  # adapter: openai  # paid, cloud, much better for short phrases
```

For OpenAI adapters, create a `.env` file:

```
OPENAI_API_KEY=sk-...
```

### Persona (yap.persona.yaml)

```yaml
voice_style: |
  Speak casually, like a senior engineer colleague.
  Be direct. No fluff. If it's wrong, say it.

review_focus: |
  Focus on correctness and error handling.
  Flag missing nil checks and race conditions.

ignore: |
  Don't comment on import ordering.
  Skip test files unless they have obvious gaps.
```

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Space` | Push to talk (start/stop recording) |
| `n` | Quick next — approve without speaking |
| `e` | Open current file in `$EDITOR` |
| `Tab` | Switch focus between file list and diff |
| `j/k` | Scroll diff or navigate file list |
| `Enter` | Select file from list |
| `Ctrl+U/D` | Half page up/down |
| `gg/G` | Top/bottom of diff |
| `q` | Quit |

## Architecture

```
./yap
  ├── Bubble Tea TUI (your terminal)
  │   ├── File list panel (left)
  │   ├── Diff panel with syntax highlighting (center)
  │   └── Conversation panel (right)
  │
  ├── IPC server (.yap.sock)
  │   └── Unix socket for MCP ↔ TUI communication
  │
  └── Claude (headless, background)
      ├── Analyzes diffs, groups related files
      ├── Calls batch_review MCP tool (one call for all files)
      ├── TUI processes locally (zero latency between files)
      └── Interrupts return to Claude for complex feedback
```

## License

MIT
