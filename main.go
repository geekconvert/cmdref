package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	return filepath.Join(home, ".cmdref", "commands.json"), nil
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
	fmt.Print(`cmdref - save and recall important terminal commands

Usage:
  cmdref add    --title "..." --cmd "..." [--tags t1,t2] [--notes "..."]
  cmdref list
  cmdref search <query>
  cmdref show <id>
  cmdref copy <id>     (macOS clipboard via pbcopy)
  cmdref run  <id>     (executes using: /bin/zsh -lc "<command>")
  cmdref rm   <id>

Examples:
  cmdref add --title "List files" --cmd "ls -la" --tags shell,mac
  cmdref search adb
  cmdref copy 2
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "add":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		title := fs.String("title", "", "title for the command")
		command := fs.String("cmd", "", "the command to save")
		tags := fs.String("tags", "", "comma-separated tags")
		notes := fs.String("notes", "", "optional notes")
		_ = fs.Parse(os.Args[2:])

		if strings.TrimSpace(*title) == "" || strings.TrimSpace(*command) == "" {
			fmt.Fprintln(os.Stderr, "error: --title and --cmd are required")
			os.Exit(2)
		}

		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}

		now := time.Now().Format(time.RFC3339)
		item := Item{
			ID:        db.NextID,
			Title:     strings.TrimSpace(*title),
			Command:   strings.TrimSpace(*command),
			Tags:      parseTags(*tags),
			Notes:     strings.TrimSpace(*notes),
			CreatedAt: now,
			UpdatedAt: now,
		}

		db.NextID++
		db.Items = append(db.Items, item)

		if err := saveDB(db); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		fmt.Printf("Saved #%d: %s\n", item.ID, item.Title)

	case "list":
		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		if len(db.Items) == 0 {
			fmt.Println("(empty) add one with: cmdref add --title ... --cmd ...")
			return
		}
		// stable order by ID
		sort.Slice(db.Items, func(i, j int) bool { return db.Items[i].ID < db.Items[j].ID })
		for _, it := range db.Items {
			tagStr := ""
			if len(it.Tags) > 0 {
				tagStr = " [" + strings.Join(it.Tags, ",") + "]"
			}
			fmt.Printf("%d) %s%s\n", it.ID, it.Title, tagStr)
		}

	case "search":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: search requires a query")
			os.Exit(2)
		}
		q := strings.ToLower(strings.Join(os.Args[2:], " "))
		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		hits := 0
		for _, it := range db.Items {
			hay := strings.ToLower(it.Title + " " + it.Command + " " + strings.Join(it.Tags, " ") + " " + it.Notes)
			if strings.Contains(hay, q) {
				hits++
				tagStr := ""
				if len(it.Tags) > 0 {
					tagStr = " [" + strings.Join(it.Tags, ",") + "]"
				}
				fmt.Printf("%d) %s%s\n", it.ID, it.Title, tagStr)
			}
		}
		if hits == 0 {
			fmt.Println("(no matches)")
		}

	case "show":
		id, err := requireID(os.Args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		it, _ := findByID(&db, id)
		if it == nil {
			fmt.Fprintln(os.Stderr, "not found")
			os.Exit(3)
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
		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		it, _ := findByID(&db, id)
		if it == nil {
			fmt.Fprintln(os.Stderr, "not found")
			os.Exit(3)
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
		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		it, _ := findByID(&db, id)
		if it == nil {
			fmt.Fprintln(os.Stderr, "not found")
			os.Exit(3)
		}
		// Use login shell so user's PATH etc works.
		c := exec.Command("/bin/zsh", "-lc", it.Command)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		if err := c.Run(); err != nil {
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
		db, err := loadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		_, idx := findByID(&db, id)
		if idx < 0 {
			fmt.Fprintln(os.Stderr, "not found")
			os.Exit(3)
		}
		db.Items = append(db.Items[:idx], db.Items[idx+1:]...)
		if err := saveDB(db); err != nil {
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
