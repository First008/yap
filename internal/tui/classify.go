package tui

import "strings"

type responseClass int

const (
	responseEmpty   responseClass = iota // empty/blank → STT failed, retry
	responseSimple                       // "next", "looks good" → auto-approve
	responseSkip                         // "skip", "pass" → move on without marking
	responseStop                         // "stop", "done" → end batch
	responseComplex                      // anything else → interrupt, return to Claude
)

var simplePatterns = map[string]bool{
	"next": true, "continue": true, "looks good": true, "lgtm": true,
	"approve": true, "approved": true, "good": true, "okay": true,
	"ok": true, "fine": true, "sure": true, "yes": true, "yep": true,
	"yeah": true, "go ahead": true, "move on": true, "looks good to me": true,
	"that looks fine": true, "that looks good": true, "all good": true,
	"no issues": true, "looks fine": true,
}

var skipPatterns = map[string]bool{
	"skip": true, "pass": true,
}

var stopPatterns = map[string]bool{
	"stop": true, "done": true, "end": true, "quit": true,
	"finish": true, "that's all": true, "thats all": true,
}

func classifyResponse(text string) responseClass {
	normalized := strings.ToLower(strings.TrimSpace(text))

	// Remove trailing periods and commas from whisper output
	normalized = strings.TrimRight(normalized, ".,!?")
	normalized = strings.TrimSpace(normalized)

	if normalized == "" {
		return responseEmpty // STT returned nothing — retry, don't auto-approve
	}

	if simplePatterns[normalized] {
		return responseSimple
	}
	if skipPatterns[normalized] {
		return responseSkip
	}
	if stopPatterns[normalized] {
		return responseStop
	}

	return responseComplex
}
