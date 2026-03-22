package tui

// getDiff returns the diff for a file, using the cache if available.
func (a *App) getDiff(filePath string) (string, error) {
	if diff, ok := a.diffCache[filePath]; ok {
		return diff, nil
	}
	if a.staged {
		return a.git.StagedFileDiff(filePath)
	}
	return a.git.FileDiff(filePath)
}
