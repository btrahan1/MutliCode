package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGoFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repomap-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goCode := `package testpkg

type DBClient struct {
	connStr string
}

func NewDBClient(conn string) *DBClient {
	return &DBClient{connStr: conn}
}

func (c *DBClient) Connect() error {
	return nil
}
`
	filePath := filepath.Join(tmpDir, "db.go")
	if err := os.WriteFile(filePath, []byte(goCode), 0644); err != nil {
		t.Fatalf("failed to write go file: %v", err)
	}

	defs, refs, err := ParseGoFile(filePath, "db.go")
	if err != nil {
		t.Fatalf("ParseGoFile failed: %v", err)
	}

	if len(defs) != 3 {
		t.Errorf("expected 3 definitions, got %d", len(defs))
	}

	expectedDefs := map[string]string{
		"DBClient":    "type DBClient struct",
		"NewDBClient": "func NewDBClient(...)",
		"Connect":     "func (*DBClient) Connect(...)",
	}

	for _, def := range defs {
		sig, ok := expectedDefs[def.SymbolName]
		if !ok {
			t.Errorf("unexpected symbol name: %s", def.SymbolName)
		} else if sig != def.Signature {
			t.Errorf("expected signature %q, got %q", sig, def.Signature)
		}
	}

	// Verify references list
	foundConn := false
	for _, ref := range refs {
		if ref == "conn" {
			foundConn = true
		}
	}
	if !foundConn {
		t.Logf("references found: %v", refs)
	}
}

func TestParseRegexFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repomap-regex-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pyCode := `class UserAuth:
    def login(self, username, password):
        return True

def hash_password(plain):
    return plain + "hashed"
`
	filePath := filepath.Join(tmpDir, "auth.py")
	if err := os.WriteFile(filePath, []byte(pyCode), 0644); err != nil {
		t.Fatalf("failed to write py file: %v", err)
	}

	defs, refs, err := ParseRegexFile(filePath, "auth.py")
	if err != nil {
		t.Fatalf("ParseRegexFile failed: %v", err)
	}

	expectedDefs := map[string]bool{
		"UserAuth":      true,
		"login":         true,
		"hash_password": true,
	}

	if len(defs) != 3 {
		t.Errorf("expected 3 definitions, got %d: %v", len(defs), defs)
	}

	for _, def := range defs {
		if !expectedDefs[def.SymbolName] {
			t.Errorf("unexpected symbol name: %s", def.SymbolName)
		}
	}

	// Verify references contain 'plain'
	foundPlain := false
	for _, ref := range refs {
		if ref == "plain" {
			foundPlain = true
		}
	}
	if !foundPlain {
		t.Errorf("expected to find reference 'plain'")
	}
}

func TestBuildRepoMap(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repomap-graph-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// File A defines DBClient
	dbCode := `package db
type DBClient struct {}
func NewDBClient() *DBClient { return &DBClient{} }
`
	if err := os.WriteFile(filepath.Join(tmpDir, "db.go"), []byte(dbCode), 0644); err != nil {
		t.Fatalf("failed to write db.go: %v", err)
	}

	// File B uses DBClient
	appCode := `package app
import "db"
func Run() {
	client := db.NewDBClient()
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "app.go"), []byte(appCode), 0644); err != nil {
		t.Fatalf("failed to write app.go: %v", err)
	}

	rme := NewRepoMapEngine(tmpDir)
	isIgnored := func(path string) bool {
		return strings.Contains(path, "ignored")
	}

	// Build map with 'app.go' as active file
	output, err := rme.BuildRepoMap([]string{"app.go"}, 500, isIgnored)
	if err != nil {
		t.Fatalf("BuildRepoMap failed: %v", err)
	}

	if !strings.Contains(output, "db.go:") {
		t.Errorf("expected output to mention db.go: %s", output)
	}
	if !strings.Contains(output, "app.go:") {
		t.Errorf("expected output to mention app.go: %s", output)
	}
	if !strings.Contains(output, "NewDBClient") {
		t.Errorf("expected output to contain NewDBClient: %s", output)
	}
}
