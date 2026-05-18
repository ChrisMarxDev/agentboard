package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Column struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Card struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Column    string   `json:"column"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
	Priority  int      `json:"priority"`
	Order     float64  `json:"order"`
}

type Taskboard struct {
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Columns []Column `json:"columns"`
	Cards   []Card   `json:"cards"`
}

const tmpl = `<!doctype html>
<title>{{.Title}}</title>
<style>
  :root { color-scheme: light dark; }
  h1 { letter-spacing: -.015em; font-size: 1.6rem; margin: 0 0 .25rem; }
  .ab-muted { color: var(--text-secondary); font-size: .9rem; margin-bottom: 1.5rem; }
  .kanban {
    display: grid;
    grid-template-columns: repeat({{len .Columns}}, minmax(220px, 1fr));
    gap: 1rem;
    overflow-x: auto;
    padding-bottom: 1rem;
  }
  .lane {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--ab-radius, 8px);
    padding: .75rem;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    gap: .6rem;
  }
  .lane h2 {
    font-size: .8rem;
    text-transform: uppercase;
    letter-spacing: .06em;
    color: var(--text-secondary);
    margin: 0 0 .35rem;
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .lane h2 .count {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 9999px;
    padding: .05rem .5rem;
    font-size: .7rem;
    font-weight: 400;
  }
  .card {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: .6rem .75rem;
    font-size: .85rem;
  }
  .card .title { font-weight: 500; color: var(--text); line-height: 1.35; }
  .card .body { color: var(--text-secondary); margin-top: .35rem; font-size: .82rem; }
  .card .meta { display: flex; flex-wrap: wrap; gap: .3rem; margin-top: .5rem; align-items: center; }
  .pill {
    display: inline-block;
    padding: .05rem .5rem;
    border-radius: 9999px;
    font-size: .7rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    color: var(--text-secondary);
  }
  .assignee {
    font-size: .75rem;
    color: var(--accent);
  }
  .prio-1 .title::before { content: "● "; color: var(--error); font-size: .8em; }
  .prio-2 .title::before { content: "● "; color: var(--warning); font-size: .8em; }
  .prio-3 .title::before { content: "● "; color: var(--text-secondary); font-size: .8em; }
  .empty-lane { font-size: .8rem; color: var(--text-secondary); font-style: italic; padding: .5rem 0; }
  @media (max-width: 900px) {
    .kanban { grid-auto-flow: column; }
  }
</style>

<h1>{{.Title}}</h1>
<p class="ab-muted">Kanban authored as HTML — the typed-view renderer was retired in the wiki pivot;
CSS grid + one section per column does the same job with fewer rules to teach.
Edit this page directly in the dashboard or push a new commit.</p>

<div class="kanban">
{{range $col := .Columns}}
  <div class="lane">
    <h2>{{$col.Label}} <span class="count">{{laneCount $.Cards $col.ID}}</span></h2>
    {{range $card := lane $.Cards $col.ID}}
      <div class="card prio-{{$card.Priority}}">
        <div class="title">{{$card.Title}}</div>
        {{if $card.Body}}<div class="body">{{$card.Body}}</div>{{end}}
        <div class="meta">
          {{range $card.Labels}}<span class="pill">{{.}}</span>{{end}}
          {{if $card.Assignees}}<span class="assignee">{{join $card.Assignees " · "}}</span>{{end}}
        </div>
      </div>
    {{else}}
      <p class="empty-lane">—</p>
    {{end}}
  </div>
{{end}}
</div>
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: render-kanban <file.json>")
		os.Exit(1)
	}
	src := os.Args[1]
	b, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var tb Taskboard
	if err := json.Unmarshal(b, &tb); err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	t := template.Must(template.New("k").Funcs(template.FuncMap{
		"laneCount": func(cards []Card, colID string) int {
			n := 0
			for _, c := range cards {
				if c.Column == colID {
					n++
				}
			}
			return n
		},
		"lane": func(cards []Card, colID string) []Card {
			out := []Card{}
			for _, c := range cards {
				if c.Column == colID {
					out = append(out, c)
				}
			}
			sort.SliceStable(out, func(i, j int) bool {
				if out[i].Order != out[j].Order {
					return out[i].Order < out[j].Order
				}
				return out[i].ID < out[j].ID
			})
			return out
		},
		"join": strings.Join,
	}).Parse(tmpl))
	outPath := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)) + ".html"
	out, err := os.Create("/tmp/" + outPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer out.Close()
	if err := t.Execute(out, tb); err != nil {
		fmt.Fprintln(os.Stderr, "execute:", err)
		os.Exit(1)
	}
	fmt.Println("wrote /tmp/" + outPath)
}
