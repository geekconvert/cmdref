package main

import (
	"bytes"
	"cmdref/api"
	"cmdref/auth"
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
	case "login":
		if err := auth.Login();err != nil {
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
			fmt.Println("Not logged in. Run: cmdref login")
			return
		}
		fmt.Println("Logged in as:", s.Email)

	case "logout":
		if err:= auth.ClearSession(); err != nil {
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

		if strings.TrimSpace(*title) == "" || strings.TrimSpace(*command) == "" {
			fmt.Fprintln(os.Stderr, "error: --title and --cmd are required")
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

	case "list":
		c := api.New()
		var items []Item
		if err := c.DoJSON("GET", "/v1/commands", nil, &items); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		if len(items) == 0 {
			fmt.Println("(empty) add one with: cmdref add --title ... --cmd ...")
			return
		}
		// stable order by ID (backend already does it, but safe)
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

		for _, it := range items{
			tagStr  := ""
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

		for _, it := range items{
			tagStr  := ""
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
		
		if  err := c.DoJSON("GET", fmt.Sprintf("/v1/commands/%d", id), nil, &it); err != nil {
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
