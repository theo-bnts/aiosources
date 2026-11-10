package aiosource

import (
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type bookFileChoice struct{ Files []model.AIOFile }

var auxiliaryBookFile = regexp.MustCompile(`(?i)^(?:read[ ._-]?me(?:[ ._-]?first)?|read[ ._-]?first|licen[cs]e|changelog|release[ ._-]?notes|file[ ._-]?list|info|cover|sample|preview|extrait)$`)

func (e *bookFileChoice) Error() string { return "choose a file" }
func (e *bookFileChoice) Unwrap() error { return ErrPending }
func fileSelectionError(files []model.AIOFile, r Resource) error {
	if (r.Type == "book" || r.Type == "torrent" || r.Type == "game") && r.File == "" {
		valid := resourceDownloadFiles(files, r)
		if len(valid) > 1 {
			return &bookFileChoice{Files: valid}
		}
	}
	if r.Type != "book" && r.Type != "torrent" && r.Type != "game" {
		return errors.New("no unambiguous matching video")
	}
	return newProviderError("not_found", "")
}

func resourceDownloadFiles(files []model.AIOFile, r Resource) []model.AIOFile {
	if r.Type != "torrent" && r.Type != "game" {
		return bookFiles(files)
	}
	result := []model.AIOFile{}
	for _, file := range files {
		path := strings.ReplaceAll(file.Path, `\`, "/")
		if file.Path != "" && len(file.Path) <= 4096 && file.Size >= 0 && !sampleRE.MatchString(path) && !auxiliaryBookFile.MatchString(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))) {
			result = append(result, file)
		}
		if len(result) >= 10000 {
			break
		}
	}
	return result
}

func selectDownloadFile(files []model.AIOFile, r Resource) (model.AIOFile, bool) {
	valid := resourceDownloadFiles(files, r)
	if r.File != "" {
		var selected model.AIOFile
		count := 0
		for _, file := range valid {
			if file.Path == r.File {
				selected = file
				count++
			}
		}
		return selected, count == 1
	}
	if len(valid) == 1 {
		return valid[0], true
	}
	return model.AIOFile{}, false
}

func bookFiles(files []model.AIOFile) []model.AIOFile {
	out := []model.AIOFile{}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Path))
		if auxiliaryBookFile.MatchString(strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))) {
			continue
		}
		if slices.Contains([]string{".epub", ".pdf", ".mobi", ".azw", ".azw3", ".fb2", ".djvu", ".cbz", ".cbr", ".cb7", ".cba", ".cbt", ".txt", ".rtf", ".doc", ".docx", ".odt", ".lit", ".prc", ".zip", ".rar", ".7z", ".aax", ".aaxc", ".m4b", ".mp3", ".flac", ".m4a", ".ogg", ".opus", ".aac", ".wav"}, ext) && !sampleRE.MatchString(f.Path) && len(f.Path) <= 4096 && len(out) < 10000 {
			out = append(out, f)
		}
	}
	return out
}
func selectBookFile(files []model.AIOFile, path string) (model.AIOFile, bool) {
	valid := bookFiles(files)
	if path != "" {
		for _, f := range valid {
			if f.Path == path {
				return f, true
			}
		}
		return model.AIOFile{}, false
	}
	if len(valid) == 1 {
		return valid[0], true
	}
	return model.AIOFile{}, false
}
