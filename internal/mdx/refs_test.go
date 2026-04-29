package mdx

import (
	"database/sql"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func openRefsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestExtractRefs_Literals(t *testing.T) {
	src := `
# Page

<Metric source="welcome.users" />
<Status source='dev.status' />
<Kanban source="team.tasks" columns={["todo","done"]} />
<Image src="/api/files/banner.svg" />
<File src='/api/files/report.pdf' />
`
	got := ExtractRefs(src, "")
	wantData := []string{"dev.status", "team.tasks", "welcome.users"}
	wantFiles := []string{"/api/files/banner.svg", "/api/files/report.pdf"}
	if !reflect.DeepEqual(got.Data, wantData) {
		t.Errorf("Data = %v, want %v", got.Data, wantData)
	}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Errorf("Files = %v, want %v", got.Files, wantFiles)
	}
}

func TestExtractRefs_IgnoresDynamicExpressions(t *testing.T) {
	src := `<Metric source={computed} />
<Metric source="literal.key" />`
	got := ExtractRefs(src, "")
	if len(got.Data) != 1 || got.Data[0] != "literal.key" {
		t.Errorf("expected only literal.key, got %v", got.Data)
	}
}

func TestExtractRefs_IgnoresNonFileSrc(t *testing.T) {
	src := `<img src="https://example.com/x.png" />
<Image src="/api/files/banner.svg" />`
	got := ExtractRefs(src, "")
	if len(got.Files) != 1 || got.Files[0] != "/api/files/banner.svg" {
		t.Errorf("expected only the /api/files/ src, got %v", got.Files)
	}
}

func TestExtractRefs_DedupesAndSorts(t *testing.T) {
	src := `<Metric source="b" /><Metric source="a" /><Metric source="b" />`
	got := ExtractRefs(src, "")
	if !reflect.DeepEqual(got.Data, []string{"a", "b"}) {
		t.Errorf("got %v", got.Data)
	}
}

func TestExtractRefs_KanbanAutoAttach(t *testing.T) {
	// <Kanban> with no source on page "tasks" auto-attaches to "tasks/"
	// AND adds the implicit `columns` key so frontmatter.columns reaches
	// the bundle (used for inline lane rename / + new lane writes).
	got := ExtractRefs(`<Kanban groupBy="col" />`, "tasks")
	if !reflect.DeepEqual(got.Data, []string{"columns", "tasks/"}) {
		t.Errorf("autowire Data = %v, want [columns tasks/]", got.Data)
	}

	// Explicit source disables auto-attach to the page's own folder
	// but the columns implicit key still rides along — column config
	// is per-page regardless of where the cards come from.
	got2 := ExtractRefs(`<Kanban source="other.tasks" groupBy="col" />`, "tasks")
	if !reflect.DeepEqual(got2.Data, []string{"columns", "other.tasks"}) {
		t.Errorf("explicit source Data = %v, want [columns other.tasks]", got2.Data)
	}

	// Empty pagePath disables auto-attach.
	got3 := ExtractRefs(`<Kanban groupBy="col" />`, "")
	if len(got3.Data) != 0 {
		t.Errorf("empty pagePath should disable autowire; got %v", got3.Data)
	}

	// <List> is also autowire-eligible.
	got4 := ExtractRefs(`<List />`, "skills")
	if !reflect.DeepEqual(got4.Data, []string{"skills/"}) {
		t.Errorf("List autowire Data = %v, want [skills/]", got4.Data)
	}
}

func TestRefStore_RecordAndGet(t *testing.T) {
	s, err := NewRefStore(openRefsDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Record("handbook", RefSet{Data: []string{"x.a", "x.b"}, Files: []string{"/api/files/y.svg"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetForPage("handbook")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Data, []string{"x.a", "x.b"}) {
		t.Errorf("Data = %v", got.Data)
	}
	if !reflect.DeepEqual(got.Files, []string{"/api/files/y.svg"}) {
		t.Errorf("Files = %v", got.Files)
	}
}

func TestRefStore_RecordReplaces(t *testing.T) {
	s, _ := NewRefStore(openRefsDB(t))
	_ = s.Record("p", RefSet{Data: []string{"old"}})
	_ = s.Record("p", RefSet{Data: []string{"new"}})
	got, _ := s.GetForPage("p")
	if len(got.Data) != 1 || got.Data[0] != "new" {
		t.Errorf("second Record should replace: got %v", got.Data)
	}
}

func TestRefStore_Delete(t *testing.T) {
	s, _ := NewRefStore(openRefsDB(t))
	_ = s.Record("p", RefSet{Data: []string{"x"}})
	_ = s.Delete("p")
	got, _ := s.GetForPage("p")
	if !got.Empty() {
		t.Errorf("refs survived Delete: %+v", got)
	}
}

func TestRefStore_GetForSubtree(t *testing.T) {
	s, _ := NewRefStore(openRefsDB(t))
	_ = s.Record("handbook", RefSet{Data: []string{"hb.main"}})
	_ = s.Record("handbook/faq", RefSet{Data: []string{"hb.faq"}})
	_ = s.Record("handbook/onboarding/day-1", RefSet{Data: []string{"hb.onb"}, Files: []string{"/api/files/x.svg"}})
	_ = s.Record("other", RefSet{Data: []string{"other.key"}})

	refs, pages, err := s.GetForSubtree("handbook")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refs.Data, []string{"hb.faq", "hb.main", "hb.onb"}) {
		t.Errorf("subtree Data = %v", refs.Data)
	}
	if !reflect.DeepEqual(refs.Files, []string{"/api/files/x.svg"}) {
		t.Errorf("subtree Files = %v", refs.Files)
	}
	wantPages := []string{"handbook", "handbook/faq", "handbook/onboarding/day-1"}
	if !reflect.DeepEqual(pages, wantPages) {
		t.Errorf("subtree pages = %v, want %v", pages, wantPages)
	}
}
