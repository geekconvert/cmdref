package main

import (
	"bufio"
	"bytes"
	"commandref/api"
	"commandref/auth"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Item struct {
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	Command   string   `json:"command"`
	Tags      []string `json:"tags"`
	Notes     string   `json:"notes"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

type DB struct {
	NextID int    `json:"nextId"`
	Items  []Item `json:"items"`
}

func dbPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".commandref", "commands.json"), nil
}

func ensureDir() error {
	p, err := dbPath()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(p), 0755)
}

func loadDB() (DB, error) {
	if err := ensureDir(); err != nil {
		return DB{}, err
	}
	p, err := dbPath()
	if err != nil {
		return DB{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DB{NextID: 1, Items: []Item{}}, nil
		}
		return DB{}, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return DB{}, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return DB{NextID: 1, Items: []Item{}}, nil
	}

	var db DB
	if err := json.Unmarshal(b, &db); err != nil {
		return DB{}, err
	}
	if db.NextID < 1 {
		db.NextID = 1
	}
	return db, nil
}

func saveDB(db DB) error {
	p, err := dbPath()
	if err != nil {
		return err
	}
	tmp := p + ".tmp"

	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func parseTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		t := strings.ToLower(strings.TrimSpace(p))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func findByID(db *DB, id int) (*Item, int) {
	for i := range db.Items {
		if db.Items[i].ID == id {
			return &db.Items[i], i
		}
	}
	return nil, -1
}

func usage() {
	fmt.Print(`commandref - save and recall important terminal commands

Usage:
  commandref add    --title "..." --cmd "..." [--tags t1,t2] [--notes "..."]
  commandref list
  commandref search <query>
  commandref show <id>
  commandref copy <id>     (macOS clipboard via pbcopy)
  commandref run  <id>     (executes using: /bin/zsh -lc "<command>")
  commandref rm   <id>

Examples:
  commandref add --title "List files" --cmd "ls -la" --tags shell,mac
  commandref search adb
  commandref copy 2
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "login":
		if err := auth.Login(); err != nil {
			fmt.Println("Login failed:", err)
			os.Exit(1)
		}
		fmt.Println("Login successful")

	case "whoami":
		s, err := auth.LoadSession()
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(2)
		}
		if s == nil {
			fmt.Println("Not logged in. Run: commandref login")
			return
		}
		fmt.Println("Logged in as:", s.Email)

	case "logout":
		if err := auth.ClearSession(); err != nil {
			fmt.Println("error:", err)
			os.Exit(2)
		}
		fmt.Println("Logged out")

	case "add":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		title := fs.String("title", "", "title for the command")
		command := fs.String("cmd", "", "the command to save")
		tags := fs.String("tags", "", "comma-separated tags")
		notes := fs.String("notes", "", "optional notes")
		_ = fs.Parse(os.Args[2:])

		// ✅ interactive fallback (only if required fields missing)
		if err := addInteractiveFallback(title, command, tags, notes); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		if strings.TrimSpace(*title) == "" || strings.TrimSpace(*command) == "" {
			fmt.Fprintln(os.Stderr, "error: title and command are required")
			os.Exit(2)
		}

		c := api.New()

		var created Item
		err := c.DoJSON("POST", "/v1/commands", map[string]any{
			"title":   strings.TrimSpace(*title),
			"command": strings.TrimSpace(*command),
			"tags":    parseTags(*tags),
			"notes":   strings.TrimSpace(*notes),
		}, &created)

		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		fmt.Printf("Saved #%d: %s\n", created.ID, created.Title)

		
	case "edit":
		id, err := requireID(os.Args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		c := api.New()

		// 1) Fetch existing item
		var existing Item
		if err := c.DoJSON("GET", fmt.Sprintf("/v1/commands/%d", id), nil, &existing); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				fmt.Fprintln(os.Stderr, "not found")
				os.Exit(3)
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		// 2) Open in editor (pre-filled)
		edited, err := editItemWithEditor(existing)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		// 3) PUT update (backend must support this)
		var updated Item
		err = c.DoJSON("PUT", fmt.Sprintf("/v1/commands/%d", id), map[string]any{
			"title":   edited.Title,
			"command": edited.Command,
			"tags":    parseTags(edited.TagsRaw),
			"notes":   edited.Notes,
		}, &updated)

		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		fmt.Printf("Updated #%d: %s\n", updated.ID, updated.Title)

	case "list":
		c := api.New()
		var items []Item
		if err := c.DoJSON("GET", "/v1/commands", nil, &items); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		if len(items) == 0 {
			fmt.Println("(empty) add one with: commandref add --title ... --cmd ...")
			return
		}
		// stable order by ID (backend already does it, but safe)
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

		for _, it := range items {
			fmt.Printf("\033[32m%d)\033[0m \033[36m%s\033[0m      (\033[33m%s\033[0m)\n", it.ID, it.Command, it.Title)
		}

	case "search":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: search requires a query")
			os.Exit(2)
		}

		query := strings.TrimSpace(strings.Join(os.Args[2:], " "))
		q := url.QueryEscape(query)

		c := api.New()

		var items []Item
		if err := c.DoJSON("GET", "/v1/commands?q="+q, nil, &items); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		if len(items) == 0 {
			fmt.Println("(no matches)")
			return
		}

		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

		for _, it := range items {
			tagStr := ""
			if len(it.Tags) > 0 {
				tagStr = " [" + strings.Join(it.Tags, ",") + "]"
			}
			fmt.Printf("%d) %s%s\n", it.ID, it.Title, tagStr)
		}

	case "show":
		id, err := requireID(os.Args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		c := api.New()

		var it Item

		if err := c.DoJSON("GET", fmt.Sprintf("/v1/commands/%d", id), nil, &it); err != nil {
			if strings.Contains(err.Error(), "not found") {
				fmt.Fprintln(os.Stderr, "not found")
				os.Exit(3)
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		fmt.Printf("#%d %s\n", it.ID, it.Title)
		if len(it.Tags) > 0 {
			fmt.Printf("Tags: %s\n", strings.Join(it.Tags, ", "))
		}
		if it.Notes != "" {
			fmt.Printf("Notes: %s\n", it.Notes)
		}
		fmt.Printf("Command:\n%s\n", it.Command)

	case "copy":
		id, err := requireID(os.Args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		c := api.New()
		var it Item

		if err := c.DoJSON("GET", fmt.Sprintf("/v1/commands/%d", id), nil, &it); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				fmt.Fprintln(os.Stderr, "not found")
				os.Exit(3)
			}
			fmt.Fprintf(os.Stderr, "error:", err)
			os.Exit(2)
		}

		if err := pbcopy(it.Command); err != nil {
			fmt.Fprintln(os.Stderr, "error copying:", err)
			os.Exit(4)
		}
		fmt.Printf("Copied #%d to clipboard\n", it.ID)

	case "run":
		id, err := requireID(os.Args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		c := api.New()
		var it Item

		if err := c.DoJSON("GET", fmt.Sprintf("/v1/commands/%d", id), nil, &it); err != nil {
			if strings.Contains(err.Error(), "not found") {
				fmt.Fprintln(os.Stderr, "not found")
				os.Exit(3)
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		// Use login shell so user's PATH etc works.
		cmdExec := exec.Command("/bin/zsh", "-lc", it.Command)
		cmdExec.Stdout = os.Stdout
		cmdExec.Stderr = os.Stderr
		cmdExec.Stdin = os.Stdin

		if err := cmdExec.Run(); err != nil {
			// return underlying exit code if any
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				os.Exit(ee.ExitCode())
			}
			fmt.Fprintln(os.Stderr, "run error:", err)
			os.Exit(5)
		}

	case "rm":
		id, err := requireID(os.Args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		c := api.New()

		if err := c.DoJSON("DELETE", fmt.Sprintf("/v1/commands/%d", id), nil, nil); err != nil {
			if strings.Contains(err.Error(), "not found") {
				fmt.Fprintln(os.Stderr, "not found")
				os.Exit(3)
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		fmt.Printf("Removed #%d\n", id)

	default:
		usage()
		os.Exit(1)
	}
}

func requireID(args []string) (int, error) {
	if len(args) < 3 {
		return 0, fmt.Errorf("missing <id>")
	}
	id, err := strconv.Atoi(args[2])
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id: %s", args[2])
	}
	return id, nil
}

func pbcopy(text string) error {
	// macOS only; later we’ll make Linux fallback (xclip/wl-copy)
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func promptLine(r *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func promptMultiline(r *bufio.Reader, label string) (string, error) {
	fmt.Println(label)
	fmt.Println("(end with empty line)")

	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")

		// empty line ends input (also ends on EOF)
		if line == "" {
			break
		}
		lines = append(lines, line)

		if err == io.EOF {
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func addInteractiveFallback(title, command, tags, notes *string) error {
	// Only run interactive prompts if required fields are missing.
	if strings.TrimSpace(*title) != "" && strings.TrimSpace(*command) != "" {
		return nil
	}

	r := bufio.NewReader(os.Stdin)

	if strings.TrimSpace(*title) == "" {
		t, err := promptLine(r, "Title: ")
		if err != nil {
			return err
		}
		*title = t
	}

	if strings.TrimSpace(*command) == "" {
		cmd, err := promptMultiline(r, "Command:")
		if err != nil {
			return err
		}
		*command = cmd
	}

	// Optional fields: only prompt if empty and user is in interactive mode.
	if strings.TrimSpace(*tags) == "" {
		t, err := promptLine(r, "Tags (comma separated, optional): ")
		if err != nil {
			return err
		}
		*tags = t
	}

	if strings.TrimSpace(*notes) == "" {
		n, err := promptLine(r, "Notes (optional): ")
		if err != nil {
			return err
		}
		*notes = n
	}

	return nil
}

/* ------------------ EDIT MODE HELPERS ------------------ */

type editedAdd struct {
	Title   string
	TagsRaw string
	Notes   string
	Command string
}

func chooseEditor() string {
	if ed := strings.TrimSpace(os.Getenv("EDITOR")); ed != "" {
		return ed
	}
	return "vi"
}

func runEditorWithFile(path string) error {
	editor := chooseEditor()
	parts := strings.Fields(editor)
	bin := parts[0]
	args := []string{}
	if len(parts) > 1 {
		args = append(args, parts[1:]...)
	}
	args = append(args, path)

	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func editTemplateForItem(it Item) string {
	now := time.Now().Format(time.RFC3339)
	tagLine := strings.Join(it.Tags, ",")

	return fmt.Sprintf(`# commandref edit %d
# Save and quit the editor to apply changes.
# Lines starting with # are ignored.
# Required: Title and Command
# Timestamp: %s

Title: %s
Tags: %s

Notes:
%s

Command:
%s
`, it.ID, now,
		it.Title,
		tagLine,
		it.Notes,
		it.Command,
	)
}

func parseEditFile(content string) (editedAdd, error) {
	// Strip comment lines but preserve blank lines.
	lines := []string{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return editedAdd{}, err
	}

	text := strings.Join(lines, "\n")

	var out editedAdd
	var section string // "", "notes", "command"

	sc2 := bufio.NewScanner(strings.NewReader(text))
	var notesLines []string
	var cmdLines []string

	for sc2.Scan() {
		line := sc2.Text()

		if strings.HasPrefix(line, "Title:") {
			out.Title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
			section = ""
			continue
		}
		if strings.HasPrefix(line, "Tags:") {
			out.TagsRaw = strings.TrimSpace(strings.TrimPrefix(line, "Tags:"))
			section = ""
			continue
		}
		if strings.TrimSpace(line) == "Notes:" {
			section = "notes"
			continue
		}
		if strings.TrimSpace(line) == "Command:" {
			section = "command"
			continue
		}

		switch section {
		case "notes":
			notesLines = append(notesLines, line)
		case "command":
			cmdLines = append(cmdLines, line)
		}
	}
	if err := sc2.Err(); err != nil {
		return editedAdd{}, err
	}

	out.Title = strings.TrimSpace(out.Title)
	out.Notes = strings.TrimSpace(strings.Join(notesLines, "\n"))
	out.Command = strings.TrimSpace(strings.Join(cmdLines, "\n"))

	if out.Title == "" {
		return editedAdd{}, fmt.Errorf("missing Title (line like: Title: ...)")
	}
	if out.Command == "" {
		return editedAdd{}, fmt.Errorf("missing Command section (add lines under Command:)")
	}

	return out, nil
}

func editItemWithEditor(existing Item) (editedAdd, error) {

	tmp, err := os.CreateTemp("", "commandref-edit-*.txt")
	if err != nil {
		return editedAdd{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(editTemplateForItem(existing)); err != nil {
		tmp.Close()
		return editedAdd{}, err
	}
	if err := tmp.Close(); err != nil {
		return editedAdd{}, err
	}

	if err := runEditorWithFile(tmpPath); err != nil {
		return editedAdd{}, fmt.Errorf("editor failed: %w", err)
	}

	b, err := os.ReadFile(tmpPath)
	if err != nil {
		return editedAdd{}, err
	}
	return parseEditFile(string(b))
}
