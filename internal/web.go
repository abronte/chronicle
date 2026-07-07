package internal

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultWebAddress = ":12345"

//go:embed web/templates/*.html web/assets/*
var webFS embed.FS

type webApp struct {
	templates *template.Template
}

type homePageData struct {
	Title       string
	CurrentPath string
	Directories []directoryView
	Error       string
}

type directoryPageData struct {
	Title     string
	Directory string
	Files     []recentChangeView
	Changes   []fileChangeView
	Error     string
}

type filePageData struct {
	Title      string
	Directory  string
	FilePath   string
	Changes    []fileChangeView
	SelectedID int64
	ShowFull   bool
	DiffData   *diffRenderData
	FullLines  []fileLine
	Error      string
}

type directoryView struct {
	Path string
}

type recentChangeView struct {
	DirectoryPath string
	FilePath      string
	AbsolutePath  string
	ChangeType    string
	CreatedAt     int64
}

type fileChangeView struct {
	ID            int64
	DirectoryPath string
	FilePath      string
	AbsolutePath  string
	SHA           string
	Previous      string
	ChangeType    string
	CreatedAt     int64
}

type diffLine struct {
	Kind    string
	Text    string
	OldLine string
	NewLine string
	Marker  string
	Code    string
}

type fileLine struct {
	Number string
	Code   string
}

type diffRenderData struct {
	OldFile diffFileData `json:"oldFile"`
	NewFile diffFileData `json:"newFile"`
}

type diffFileData struct {
	Name     string `json:"name"`
	Contents string `json:"contents"`
}

func ServeWeb(addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = DefaultWebAddress
	}
	if err := InitializeCentralDB(); err != nil {
		return err
	}
	if _, err := LoadConfig(); err != nil {
		return err
	}
	return http.ListenAndServe(addr, NewWebHandler())
}

func NewWebHandler() http.Handler {
	app := &webApp{
		templates: template.Must(template.New("").Funcs(template.FuncMap{
			"base":       filepath.Base,
			"formatTime": formatWebTime,
			"json":       jsonForTemplate,
			"shortSHA":   shortSHA,
		}).ParseFS(webFS, "web/templates/*.html")),
	}

	mux := http.NewServeMux()
	if assets, err := fs.Sub(webFS, "web/assets"); err == nil {
		mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	}
	mux.HandleFunc("/", app.handleHome)
	mux.HandleFunc("/directories", app.handleAddDirectory)
	mux.HandleFunc("/directories/delete", app.handleDeleteDirectory)
	mux.HandleFunc("/history", app.handleDirectoryHistory)
	mux.HandleFunc("/file", app.handleFileHistory)
	return mux
}

func (app *webApp) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	app.renderHome(w, http.StatusOK, "")
}

func (app *webApp) handleAddDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.renderHome(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := AddMonitoredDirectory(r.FormValue("path")); err != nil {
		app.renderHome(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (app *webApp) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		app.renderHome(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := RemoveMonitoredDirectory(r.FormValue("path")); err != nil {
		app.renderHome(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (app *webApp) handleDirectoryHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dir := r.URL.Query().Get("dir")
	if strings.TrimSpace(dir) == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	files, err := GetChangedFilesForDirectory(dir, 200)
	if err != nil {
		app.renderDirectory(w, http.StatusInternalServerError, dir, nil, nil, err.Error())
		return
	}
	changes, err := GetDirectoryChanges(dir, 200)
	if err != nil {
		app.renderDirectory(w, http.StatusInternalServerError, dir, nil, nil, err.Error())
		return
	}
	app.renderDirectory(w, http.StatusOK, dir, toRecentViews(files), toFileChangeViews(changes), "")
}

func (app *webApp) handleFileHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dir := r.URL.Query().Get("dir")
	filePath := r.URL.Query().Get("file")
	showFull := r.URL.Query().Get("view") == "full"
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(filePath) == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	changes, err := GetFileHistoryForDirectory(dir, filePath, 200)
	if err != nil {
		app.renderFile(w, http.StatusInternalServerError, dir, filePath, nil, 0, showFull, nil, nil, err.Error())
		return
	}

	var selected FileChange
	if len(changes) > 0 {
		selected = changes[0]
	}
	if changeID := r.URL.Query().Get("change"); changeID != "" {
		id, err := strconv.ParseInt(changeID, 10, 64)
		if err != nil {
			app.renderFile(w, http.StatusBadRequest, dir, filePath, toFileChangeViews(changes), selected.ID, showFull, nil, nil, "invalid change id")
			return
		}
		for _, change := range changes {
			if change.ID == id {
				selected = change
				break
			}
		}
	}

	var diffData *diffRenderData
	var full []fileLine
	if selected.ID != 0 {
		if showFull {
			full = fullFileLines(selected.Data)
		} else {
			diffData, err = diffDataForChange(selected)
			if err != nil {
				app.renderFile(w, http.StatusInternalServerError, dir, filePath, toFileChangeViews(changes), selected.ID, showFull, nil, nil, err.Error())
				return
			}
		}
	}

	app.renderFile(w, http.StatusOK, dir, filePath, toFileChangeViews(changes), selected.ID, showFull, diffData, full, "")
}

func (app *webApp) renderHome(w http.ResponseWriter, status int, message string) {
	cfg, err := LoadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var dirs []directoryView
	for _, dir := range cfg.Directories {
		dirs = append(dirs, directoryView{Path: dir})
	}
	currentPath, _ := filepath.Abs(".")
	app.render(w, status, "home", homePageData{
		Title:       "Chronicle",
		CurrentPath: currentPath,
		Directories: dirs,
		Error:       message,
	})
}

func (app *webApp) renderDirectory(w http.ResponseWriter, status int, dir string, files []recentChangeView, changes []fileChangeView, message string) {
	app.render(w, status, "directory", directoryPageData{
		Title:     "Directory History",
		Directory: dir,
		Files:     files,
		Changes:   changes,
		Error:     message,
	})
}

func (app *webApp) renderFile(w http.ResponseWriter, status int, dir, filePath string, changes []fileChangeView, selectedID int64, showFull bool, diffData *diffRenderData, full []fileLine, message string) {
	app.render(w, status, "file", filePageData{
		Title:      "File History",
		Directory:  dir,
		FilePath:   filePath,
		Changes:    changes,
		SelectedID: selectedID,
		ShowFull:   showFull,
		DiffData:   diffData,
		FullLines:  full,
		Error:      message,
	})
}

func (app *webApp) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := app.templates.ExecuteTemplate(w, name, data); err != nil {
		fmt.Fprintf(w, "render template: %v", err)
	}
}

func jsonForTemplate(value any) (template.JS, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return template.JS(data), nil
}

func diffDataForChange(change FileChange) (*diffRenderData, error) {
	if change.ChangeType == ChangeTypeDelete {
		return newDiffRenderData(change.FilePath, change.Data, ""), nil
	}

	previous, ok, err := GetPreviousChange(change)
	if err != nil {
		return nil, err
	}
	if !ok || previous.ChangeType == ChangeTypeDelete {
		return newDiffRenderData(change.FilePath, "", change.Data), nil
	}
	return newDiffRenderData(change.FilePath, previous.Data, change.Data), nil
}

func newDiffRenderData(filePath, oldContents, newContents string) *diffRenderData {
	return &diffRenderData{
		OldFile: diffFileData{
			Name:     filePath,
			Contents: oldContents,
		},
		NewFile: diffFileData{
			Name:     filePath,
			Contents: newContents,
		},
	}
}

func diffForChange(change FileChange) ([]diffLine, error) {
	if change.ChangeType == ChangeTypeDelete {
		return formatDiff(deletionDiff(change.Data)), nil
	}

	previous, ok, err := GetPreviousChange(change)
	if err != nil {
		return nil, err
	}
	if !ok {
		return formatDiff(initialDiff(change.Data)), nil
	}
	if previous.ChangeType == ChangeTypeDelete {
		return formatDiff(initialDiff(change.Data)), nil
	}
	return formatDiff(CreateDiff(previous.Data, change.Data)), nil
}

func initialDiff(data string) string {
	if data == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("@@ -0,0 +1,")
	sb.WriteString(strconv.Itoa(len(splitLines(data))))
	sb.WriteString(" @@\n")
	for _, line := range splitLines(data) {
		sb.WriteString("+")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func deletionDiff(data string) string {
	if data == "" {
		return ""
	}
	var sb strings.Builder
	lines := splitLines(data)
	sb.WriteString("@@ -1,")
	sb.WriteString(strconv.Itoa(len(lines)))
	sb.WriteString(" +0,0 @@\n")
	for _, line := range lines {
		sb.WriteString("-")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatDiff(diff string) []diffLine {
	diff = strings.TrimSuffix(diff, "\n")
	if diff == "" {
		return nil
	}

	oldLine := 0
	newLine := 0
	var lines []diffLine
	for _, text := range strings.Split(diff, "\n") {
		kind := "context"
		switch {
		case strings.HasPrefix(text, "@@"):
			kind = "hunk"
		case strings.HasPrefix(text, "+"):
			kind = "add"
		case strings.HasPrefix(text, "-"):
			kind = "delete"
		}

		line := diffLine{Kind: kind, Text: text}
		switch kind {
		case "hunk":
			oldStart, newStart, ok := parseHunkLineStarts(text)
			if ok {
				oldLine = oldStart
				newLine = newStart
			}
			line.Code = text
		case "add":
			line.NewLine = lineNumberText(newLine)
			line.Marker = "+"
			line.Code = strings.TrimPrefix(text, "+")
			if newLine > 0 {
				newLine++
			}
		case "delete":
			line.OldLine = lineNumberText(oldLine)
			line.Marker = "-"
			line.Code = strings.TrimPrefix(text, "-")
			if oldLine > 0 {
				oldLine++
			}
		default:
			line.OldLine = lineNumberText(oldLine)
			line.NewLine = lineNumberText(newLine)
			line.Marker = " "
			line.Code = strings.TrimPrefix(text, " ")
			if oldLine > 0 {
				oldLine++
			}
			if newLine > 0 {
				newLine++
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func fullFileLines(data string) []fileLine {
	data = strings.TrimSuffix(data, "\n")
	if data == "" {
		return nil
	}

	lines := strings.Split(data, "\n")
	result := make([]fileLine, 0, len(lines))
	for i, line := range lines {
		result = append(result, fileLine{
			Number: strconv.Itoa(i + 1),
			Code:   line,
		})
	}
	return result
}

func parseHunkLineStarts(text string) (int, int, bool) {
	var oldStart, oldCount, newStart, newCount int
	n, err := fmt.Sscanf(text, "@@ -%d,%d +%d,%d @@", &oldStart, &oldCount, &newStart, &newCount)
	return oldStart, newStart, err == nil && n == 4
}

func lineNumberText(line int) string {
	if line <= 0 {
		return ""
	}
	return strconv.Itoa(line)
}

func toRecentViews(changes []RecentChange) []recentChangeView {
	var views []recentChangeView
	for _, change := range changes {
		views = append(views, recentChangeView{
			DirectoryPath: change.DirectoryPath,
			FilePath:      change.FilePath,
			AbsolutePath:  change.AbsolutePath,
			ChangeType:    change.ChangeType,
			CreatedAt:     change.CreatedAt,
		})
	}
	return views
}

func toFileChangeViews(changes []FileChange) []fileChangeView {
	var views []fileChangeView
	for _, change := range changes {
		views = append(views, fileChangeView{
			ID:            change.ID,
			DirectoryPath: change.DirectoryPath,
			FilePath:      change.FilePath,
			AbsolutePath:  change.AbsolutePath,
			SHA:           change.SHA,
			Previous:      change.Previous,
			ChangeType:    change.ChangeType,
			CreatedAt:     change.CreatedAt,
		})
	}
	return views
}

func formatWebTime(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04:05")
}

func shortSHA(sha string) string {
	if len(sha) <= 8 {
		return sha
	}
	return sha[:8]
}
