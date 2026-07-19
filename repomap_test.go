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

	// Test C# parsing
	csCode := `
using System;
namespace App.Services {
    public interface IAuthService {
        bool ValidateToken(string token);
    }
    public class AuthService : IAuthService {
        public async Task<bool> ValidateToken(string token) {
            return true;
        }
    }
}
`
	csPath := filepath.Join(tmpDir, "auth.cs")
	if err := os.WriteFile(csPath, []byte(csCode), 0644); err != nil {
		t.Fatalf("failed to write cs file: %v", err)
	}

	csDefs, _, err := ParseRegexFile(csPath, "auth.cs")
	if err != nil {
		t.Fatalf("ParseRegexFile for C# failed: %v", err)
	}

	expectedCsDefs := map[string]bool{
		"IAuthService":  true,
		"ValidateToken": true,
		"AuthService":   true,
		"App":           true,
	}

	for _, def := range csDefs {
		if !expectedCsDefs[def.SymbolName] {
			t.Errorf("unexpected C# symbol: %s", def.SymbolName)
		}
	}

	// Test C++ parsing
	cppCode := `
#include <iostream>
#define MAX_BUFFER 1024
namespace Engine {
    struct Vertex {
        float x, y, z;
    };
    class Renderer {
        void RenderScene() {
            std::cout << MAX_BUFFER << std::endl;
        }
    };
}
`
	cppPath := filepath.Join(tmpDir, "render.cpp")
	if err := os.WriteFile(cppPath, []byte(cppCode), 0644); err != nil {
		t.Fatalf("failed to write cpp file: %v", err)
	}

	cppDefs, _, err := ParseRegexFile(cppPath, "render.cpp")
	if err != nil {
		t.Fatalf("ParseRegexFile for C++ failed: %v", err)
	}

	expectedCppDefs := map[string]bool{
		"MAX_BUFFER":  true,
		"Vertex":       true,
		"Renderer":     true,
		"RenderScene":  true,
		"Engine":       true,
	}

	for _, def := range cppDefs {
		if !expectedCppDefs[def.SymbolName] {
			t.Errorf("unexpected C++ symbol: %s", def.SymbolName)
		}
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

func TestParseFallbackPlan(t *testing.T) {
	// Test markdown table extraction
	markdownTableText := `
I'll propose a plan.

| Task | Description |
|------|-------------|
| **1. Scaffold solution** | Create new hosted Blazor WASM project using dotnet new blazorwasm --hosted. |
| **2. Add EF Core + SQLite** | Install Microsoft.EntityFrameworkCore.Sqlite. |
`
	plan := parseFallbackPlan(markdownTableText)
	if plan == nil {
		t.Fatalf("expected plan, got nil")
	}
	if len(plan.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Description != "1. Scaffold solution: Create new hosted Blazor WASM project using dotnet new blazorwasm --hosted." {
		t.Errorf("unexpected description: %s", plan.Tasks[0].Description)
	}

	// Test numbered list extraction
	listText := `
Here is my plan:
1. Setup DB - Configure connection string.
2. Build UI - Write HTML and CSS.
`
	plan2 := parseFallbackPlan(listText)
	if plan2 == nil {
		t.Fatalf("expected plan2, got nil")
	}
	if len(plan2.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(plan2.Tasks))
	}
	if plan2.Tasks[0].Description != "Setup DB: Configure connection string." {
		t.Errorf("unexpected description: %s", plan2.Tasks[0].Description)
	}
}

func TestGetProjectSourceString(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "project-source-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy source file
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	// Create an ignored file
	ignoredDir := filepath.Join(tmpDir, "node_modules")
	if err := os.MkdirAll(ignoredDir, 0755); err != nil {
		t.Fatalf("failed to create ignored dir: %v", err)
	}
	ignoredPath := filepath.Join(ignoredDir, "foo.js")
	if err := os.WriteFile(ignoredPath, []byte("console.log('foo')"), 0644); err != nil {
		t.Fatalf("failed to write ignored file: %v", err)
	}

	app := NewApp()
	source, err := app.GetProjectSourceString(tmpDir)
	if err != nil {
		t.Fatalf("GetProjectSourceString failed: %v", err)
	}

	if !strings.Contains(source, "main.go") {
		t.Errorf("expected source to contain main.go: %s", source)
	}
	if strings.Contains(source, "node_modules") || strings.Contains(source, "foo.js") {
		t.Errorf("expected source NOT to contain node_modules or foo.js: %s", source)
	}
}
