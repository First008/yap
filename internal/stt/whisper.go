package stt

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type WhisperAdapter struct {
	modelPath      string
	vadModelPath   string
	silenceTimeout string

	mu  sync.Mutex
	cmd *exec.Cmd
}

func NewWhisperAdapter(model, silenceTimeout string) *WhisperAdapter {
	if model == "" {
		model = "medium.en"
	}
	if silenceTimeout == "" {
		silenceTimeout = "3.0"
	}

	modelPath := resolveModelPath(model)
	vadModelPath := resolveVADModelPath()

	return &WhisperAdapter{
		modelPath:      modelPath,
		vadModelPath:   vadModelPath,
		silenceTimeout: silenceTimeout,
	}
}

func resolveModelPath(model string) string {
	if filepath.IsAbs(model) {
		return model
	}

	candidates := []string{
		filepath.Join("/usr/local/share/whisper-cpp/models", "ggml-"+model+".bin"),
		filepath.Join("/opt/homebrew/share/whisper-cpp/models", "ggml-"+model+".bin"),
		filepath.Join(os.Getenv("HOME"), ".local/share/whisper-cpp/models", "ggml-"+model+".bin"),
		filepath.Join(os.Getenv("HOME"), "whisper-models", "ggml-"+model+".bin"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return "ggml-" + model + ".bin"
}

func resolveVADModelPath() string {
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), ".local/share/whisper-cpp/models", "silero-vad.ggml.bin"),
		filepath.Join("/opt/homebrew/share/whisper-cpp/models", "silero-vad.ggml.bin"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Listen records with silence detection.
func (w *WhisperAdapter) Listen(ctx context.Context) (string, error) {
	tmpDir, err := os.MkdirTemp("", "review-stt-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	wavFile := filepath.Join(tmpDir, "recording.wav")
	if err := w.recordSilence(ctx, wavFile); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}

	text, err := w.transcribe(ctx, wavFile)
	if err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}

	return filterHallucination(text), nil
}

// ListenPTT records until stopCh is signaled (push-to-talk mode).
func (w *WhisperAdapter) ListenPTT(ctx context.Context, stopCh <-chan struct{}) (string, error) {
	tmpDir, err := os.MkdirTemp("", "review-stt-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	wavFile := filepath.Join(tmpDir, "recording.wav")
	if err := w.recordUntilStop(ctx, wavFile, stopCh); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}

	text, err := w.transcribe(ctx, wavFile)
	if err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}

	return filterHallucination(text), nil
}

// --- Audio validation ---

const (
	minWavSize = 100

	// whisper-cli uses a 30-second window mapped to 1500 context tokens.
	// We scale proportionally to the actual audio duration plus a small buffer.
	whisperWindowSec  = 30.0
	whisperMaxCtx     = 1500
	whisperCtxPadding = 128
)

// getAudioDuration returns duration in seconds using sox.
func getAudioDuration(wavPath string) (float64, error) {
	out, err := exec.Command("sox", "--info", "-D", wavPath).Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// getAudioRMS returns the RMS amplitude using sox.
func getAudioRMS(wavPath string) (float64, error) {
	// sox stat writes to stderr, so we need CombinedOutput
	out, execErr := exec.Command("sox", wavPath, "-n", "stat").CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "RMS     amplitude") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return strconv.ParseFloat(parts[len(parts)-1], 64)
			}
		}
	}
	if execErr != nil {
		return 0, fmt.Errorf("sox stat: %w", execErr)
	}
	return 0, fmt.Errorf("could not parse RMS from sox stat output")
}

// validateAudio checks if the recording has enough speech to transcribe.
func validateAudio(wavPath string) error {
	info, err := os.Stat(wavPath)
	if err != nil || info.Size() < minWavSize {
		return fmt.Errorf("empty recording")
	}

	dur, err := getAudioDuration(wavPath)
	if err != nil || dur < 0.3 {
		return fmt.Errorf("audio too short: %.2fs", dur)
	}

	rms, err := getAudioRMS(wavPath)
	if err == nil && rms < 0.005 {
		return fmt.Errorf("audio is silence (RMS: %.4f)", rms)
	}

	return nil
}

// --- Recording ---

func (w *WhisperAdapter) recordSilence(ctx context.Context, outputPath string) error {
	args := []string{
		"-d", "-t", "wav", "-r", "16000", "-c", "1", "-b", "16",
		outputPath,
		"silence", "1", "0.1", "1%", "1", w.silenceTimeout, "1%",
	}

	w.mu.Lock()
	w.cmd = exec.CommandContext(ctx, "sox", args...)
	cmd := w.cmd
	w.mu.Unlock()

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("sox: %w", err)
	}

	w.mu.Lock()
	w.cmd = nil
	w.mu.Unlock()
	return nil
}

func (w *WhisperAdapter) recordUntilStop(ctx context.Context, outputPath string, stopCh <-chan struct{}) error {
	time.Sleep(150 * time.Millisecond) // Bluetooth mic wake-up

	args := []string{
		"-d", "-t", "wav", "-r", "16000", "-c", "1", "-b", "16",
		outputPath,
	}

	w.mu.Lock()
	w.cmd = exec.CommandContext(ctx, "sox", args...)
	cmd := w.cmd
	w.mu.Unlock()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sox start: %w", err)
	}

	select {
	case <-stopCh:
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
		}
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return nil
	}

	cmd.Wait()
	w.mu.Lock()
	w.cmd = nil
	w.mu.Unlock()
	return nil
}

// --- Transcription ---

func (w *WhisperAdapter) transcribe(ctx context.Context, wavPath string) (string, error) {
	// Pre-transcription validation
	if err := validateAudio(wavPath); err != nil {
		return "", nil // silent/empty — not an error, just no speech
	}

	// Preprocess: normalize + trim trailing silence
	processedPath := wavPath + ".processed.wav"
	preprocess := exec.CommandContext(ctx, "sox", wavPath, processedPath,
		"highpass", "80",      // remove low-frequency rumble
		"gain", "-n", "-3",    // normalize to -3dB
		"reverse",
		"silence", "1", "0.3", "1%", // trim trailing silence
		"reverse",
	)
	if preprocess.Run() == nil {
		if pi, err := os.Stat(processedPath); err == nil && pi.Size() > minWavSize {
			wavPath = processedPath
		}
	}

	// Scale audio context proportionally to actual duration
	dur, _ := getAudioDuration(wavPath)
	audioCtx := int(math.Ceil((dur/whisperWindowSec)*float64(whisperMaxCtx))) + whisperCtxPadding
	if audioCtx > whisperMaxCtx {
		audioCtx = whisperMaxCtx
	}
	if audioCtx < whisperCtxPadding {
		audioCtx = whisperCtxPadding
	}

	// Build whisper-cli args
	args := []string{
		"--model", w.modelPath,
		"--no-timestamps",
		"--no-prints",
		"--language", "en",
		"--max-context", "0",    // no decoder context carry-over (prevents hallucination loops)
		"--suppress-nst",        // suppress non-speech tokens
		"--entropy-thold", "2.0",   // stricter than default 2.4
		"--logprob-thold", "-0.8",  // stricter than default -1.0
		"--no-speech-thold", "0.5", // more aggressive silence detection
		"--audio-ctx", fmt.Sprintf("%d", audioCtx),
	}

	// NO --prompt (causes prompt leaking on short audio)

	// Enable VAD if model is available
	if w.vadModelPath != "" {
		args = append(args,
			"--vad",
			"--vad-model", w.vadModelPath,
			"--vad-threshold", "0.5",
			"--vad-min-speech-duration-ms", "250",
		)
	}

	args = append(args, "--file", wavPath)

	cmd := exec.CommandContext(ctx, "whisper-cli", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", nil
		}
		return "", fmt.Errorf("whisper-cli: %s: %w", string(output), err)
	}

	return validateTranscription(string(output), dur)
}

// --- Post-transcription validation ---

func validateTranscription(text string, audioDurationSec float64) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}

	lower := strings.ToLower(text)

	// Known hallucination patterns
	hallucinations := []string{
		"thanks for watching", "thank you for watching",
		"subscribe", "subtitles by", "transcribed by",
		"copyright", "amara.org", "please subscribe",
		"like and subscribe", "see you next time",
	}
	for _, h := range hallucinations {
		if strings.Contains(lower, h) {
			return "", nil
		}
	}

	// Repetition detection: same trigram 3+ times
	words := strings.Fields(text)
	if len(words) > 6 {
		seen := make(map[string]int)
		for i := 0; i+2 < len(words); i++ {
			trigram := strings.ToLower(words[i] + " " + words[i+1] + " " + words[i+2])
			seen[trigram]++
			if seen[trigram] >= 3 {
				return "", nil // repetitive output = hallucination
			}
		}
	}

	// Word rate check: too many words for audio duration = hallucination
	// Average speaking rate ~150 wpm = 2.5 words/sec, allow up to 4 w/s
	if audioDurationSec > 0 {
		maxWords := int(audioDurationSec*4) + 5
		if len(words) > maxWords {
			return "", nil
		}
	}

	return text, nil
}

// --- Hallucination filter (applied after validateTranscription) ---

var knownHallucinations = map[string]bool{
	"you": true, "you.": true,
	"thank you.": true, "thanks for watching!": true,
	"thanks for watching.": true, "[blank_audio]": true,
	"[silence]": true, "(silence)": true, "the end.": true,
	"": true,
}

func filterHallucination(text string) string {
	cleaned := strings.ToLower(strings.TrimSpace(text))
	if knownHallucinations[cleaned] {
		return ""
	}
	return strings.TrimSpace(text)
}

func (w *WhisperAdapter) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		return w.cmd.Process.Kill()
	}
	return nil
}
