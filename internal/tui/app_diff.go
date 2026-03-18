package tui

// getDiff returns the diff for a file, using the cache if available.
func (a *App) getDiff(filePath string) (string, error) {
	if diff, ok := a.diffCache[filePath]; ok {
		return diff, nil
	}
	return a.git.FileDiff(filePath)
}
