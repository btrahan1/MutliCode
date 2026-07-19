package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Tag represents a symbol definition in a file.
type Tag struct {
	RelPath    string
	Line       int
	SymbolName string
	Signature  string
	Kind       string // "def" or "ref"
}

// RankedTag represents a Tag paired with its PageRank score.
type RankedTag struct {
	Tag   Tag
	Score float64
}

// ParseGoFile parses a Go file and extracts defined symbols and referenced identifiers.
func ParseGoFile(fullPath, relPath string) ([]Tag, []string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	var defs []Tag
	var refsMap = make(map[string]bool)

	// Filter out Go keywords and common types
	ignoredWords := map[string]bool{
		"error": true, "nil": true, "string": true, "int": true, "bool": true,
		"err": true, "ctx": true, "context": true, "make": true, "append": true,
		"len": true, "true": true, "false": true, "close": true, "panic": true,
	}

	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			// Extract function signature
			line := fset.Position(x.Pos()).Line
			sig := ""
			if x.Recv != nil && len(x.Recv.List) > 0 {
				// Method receiver
				recvType := ""
				if x.Recv.List[0].Type != nil {
					switch r := x.Recv.List[0].Type.(type) {
					case *ast.Ident:
						recvType = r.Name
					case *ast.StarExpr:
						if ident, ok := r.X.(*ast.Ident); ok {
							recvType = "*" + ident.Name
						}
					}
				}
				sig = fmt.Sprintf("func (%s) %s(...)", recvType, x.Name.Name)
			} else {
				sig = fmt.Sprintf("func %s(...)", x.Name.Name)
			}

			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       line,
				SymbolName: x.Name.Name,
				Signature:  sig,
				Kind:       "def",
			})

		case *ast.TypeSpec:
			// Struct / Interface / Type definition
			line := fset.Position(x.Pos()).Line
			var kindStr string
			switch x.Type.(type) {
			case *ast.StructType:
				kindStr = "struct"
			case *ast.InterfaceType:
				kindStr = "interface"
			default:
				kindStr = "type"
			}
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       line,
				SymbolName: x.Name.Name,
				Signature:  fmt.Sprintf("type %s %s", x.Name.Name, kindStr),
				Kind:       "def",
			})

		case *ast.Ident:
			// Reference tracking
			if x.Obj == nil && !ignoredWords[x.Name] && len(x.Name) > 2 {
				refsMap[x.Name] = true
			}
		}
		return true
	})

	var refs []string
	for r := range refsMap {
		refs = append(refs, r)
	}

	return defs, refs, nil
}

// Regex definitions for other languages
var (
	pyDefRegex   = regexp.MustCompile(`^\s*(def|class)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	jsDefRegex   = regexp.MustCompile(`^\s*(class|interface|type|function)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	arrowRegex   = regexp.MustCompile(`^\s*(const|let|var)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*(async\s*)?(\([^)]*\)|[a-zA-Z_][a-zA-Z0-9_]*)\s*=>`)
	csTypeDefRegex = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|static|sealed|abstract|partial)\s+)*(class|interface|struct|enum|namespace|record)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	csMethodDefRegex = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|static|async|override|virtual|new|partial)\s+)+([a-zA-Z0-9_<>\\[\\]?]+)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	cppTypeDefRegex = regexp.MustCompile(`^\s*(class|struct|union|enum|namespace)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	cppMethodDefRegex = regexp.MustCompile(`^\s*(?:(?:inline|static|virtual|explicit|friend|const|constexpr)\s+)*([a-zA-Z0-9_<>\*&]+)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	cppMacroDefRegex = regexp.MustCompile(`^\s*#\s*define\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	wordRegex    = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)
)

var commonKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "return": true, "import": true, "export": true,
	"from": true, "const": true, "let": true, "var": true, "class": true, "function": true,
	"interface": true, "type": true, "def": true, "self": true, "this": true, "nil": true,
	"null": true, "true": true, "false": true, "async": true, "await": true, "default": true,
	"using": true, "namespace": true, "public": true, "private": true, "protected": true,
	"internal": true, "void": true, "int": true, "string": true, "bool": true, "float": true,
	"double": true, "struct": true, "enum": true, "new": true, "static": true, "virtual": true,
	"override": true, "switch": true, "case": true, "break": true, "continue": true,
	"define": true, "include": true,
}

// ParseRegexFile parses JS/TS/Py/CS/CPP files using regular expressions.
func ParseRegexFile(fullPath, relPath string) ([]Tag, []string, error) {
	bytes, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, nil, err
	}

	lines := strings.Split(strings.ReplaceAll(string(bytes), "\r\n", "\n"), "\n")
	var defs []Tag
	var refsMap = make(map[string]bool)

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// 1. Definition matching
		if match := pyDefRegex.FindStringSubmatch(trimmed); match != nil {
			kind := match[1]
			sym := match[2]
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       lineNum,
				SymbolName: sym,
				Signature:  fmt.Sprintf("%s %s", kind, sym),
				Kind:       "def",
			})
		} else if match := jsDefRegex.FindStringSubmatch(trimmed); match != nil {
			kind := match[1]
			sym := match[2]
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       lineNum,
				SymbolName: sym,
				Signature:  fmt.Sprintf("%s %s", kind, sym),
				Kind:       "def",
			})
		} else if match := arrowRegex.FindStringSubmatch(trimmed); match != nil {
			sym := match[2]
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       lineNum,
				SymbolName: sym,
				Signature:  fmt.Sprintf("const %s = =>", sym),
				Kind:       "def",
			})
		} else if match := csTypeDefRegex.FindStringSubmatch(trimmed); match != nil {
			kind := match[1]
			sym := match[2]
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       lineNum,
				SymbolName: sym,
				Signature:  fmt.Sprintf("%s %s", kind, sym),
				Kind:       "def",
			})
		} else if match := csMethodDefRegex.FindStringSubmatch(trimmed); match != nil {
			sym := match[2]
			if !commonKeywords[sym] {
				defs = append(defs, Tag{
					RelPath:    relPath,
					Line:       lineNum,
					SymbolName: sym,
					Signature:  fmt.Sprintf("method %s()", sym),
					Kind:       "def",
				})
			}
		} else if match := cppTypeDefRegex.FindStringSubmatch(trimmed); match != nil {
			kind := match[1]
			sym := match[2]
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       lineNum,
				SymbolName: sym,
				Signature:  fmt.Sprintf("%s %s", kind, sym),
				Kind:       "def",
			})
		} else if match := cppMethodDefRegex.FindStringSubmatch(trimmed); match != nil {
			sym := match[2]
			if !commonKeywords[sym] {
				defs = append(defs, Tag{
					RelPath:    relPath,
					Line:       lineNum,
					SymbolName: sym,
					Signature:  fmt.Sprintf("function %s()", sym),
					Kind:       "def",
				})
			}
		} else if match := cppMacroDefRegex.FindStringSubmatch(trimmed); match != nil {
			sym := match[1]
			defs = append(defs, Tag{
				RelPath:    relPath,
				Line:       lineNum,
				SymbolName: sym,
				Signature:  fmt.Sprintf("#define %s", sym),
				Kind:       "def",
			})
		}

		// 2. Reference extraction
		words := wordRegex.FindAllString(line, -1)
		for _, w := range words {
			if len(w) > 2 && !commonKeywords[w] {
				refsMap[w] = true
			}
		}
	}

	// Remove self-definitions from reference map to keep graph clean
	for _, def := range defs {
		delete(refsMap, def.SymbolName)
	}

	var refs []string
	for r := range refsMap {
		refs = append(refs, r)
	}

	return defs, refs, nil
}

// RepoMapEngine coordinates AST parsing, PageRank, and packing.
type RepoMapEngine struct {
	WorkspacePath string
}

func NewRepoMapEngine(workspacePath string) *RepoMapEngine {
	return &RepoMapEngine{WorkspacePath: workspacePath}
}

type GraphEdge struct {
	To     string
	Weight float64
}

// BuildRepoMap generates the packed repository skeleton layout.
func (rme *RepoMapEngine) BuildRepoMap(activeFiles []string, tokenBudget int, isIgnored func(string) bool) (string, error) {
	// 1. Walk directory and collect all defs and refs
	allDefs := make(map[string][]Tag)         // file -> defs
	symbolToDefFile := make(map[string]string) // symbolName -> file
	allRefs := make(map[string][]string)      // file -> refs

	err := filepath.Walk(rme.WorkspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if isIgnored(path) {
			return nil
		}

		relPath, err := filepath.Rel(rme.WorkspacePath, path)
		if err != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		var defs []Tag
		var refs []string

		if ext == ".go" {
			defs, refs, _ = ParseGoFile(path, relPath)
		} else if ext == ".py" || ext == ".js" || ext == ".ts" || ext == ".tsx" || ext == ".cs" || ext == ".cpp" || ext == ".hpp" || ext == ".c" || ext == ".h" {
			defs, refs, _ = ParseRegexFile(path, relPath)
		} else {
			return nil
		}

		allDefs[relPath] = defs
		allRefs[relPath] = refs
		for _, def := range defs {
			symbolToDefFile[def.SymbolName] = relPath
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	// 2. Build dependency graph
	nodes := make(map[string]bool)
	edges := make(map[string]map[string]float64) // from -> to -> weight

	for file := range allDefs {
		nodes[file] = true
	}

	for file, refs := range allRefs {
		for _, ref := range refs {
			if targetFile, exists := symbolToDefFile[ref]; exists && targetFile != file {
				if _, ok := edges[file]; !ok {
					edges[file] = make(map[string]float64)
				}
				edges[file][targetFile] += 1.0
			}
		}
	}

	// 3. Compute Personalized PageRank
	damping := 0.85
	maxIterations := 20
	ranks := computePageRank(nodes, edges, activeFiles, damping, maxIterations)

	// Sort symbols globally by their file rank score
	var rankedTags []RankedTag
	for file, defs := range allDefs {
		score := ranks[file]
		for _, def := range defs {
			rankedTags = append(rankedTags, RankedTag{Tag: def, Score: score})
		}
	}

	// Sort descending by score, then ascending by file name and line number
	sort.Slice(rankedTags, func(i, j int) bool {
		if rankedTags[i].Score != rankedTags[j].Score {
			return rankedTags[i].Score > rankedTags[j].Score
		}
		if rankedTags[i].Tag.RelPath != rankedTags[j].Tag.RelPath {
			return rankedTags[i].Tag.RelPath < rankedTags[j].Tag.RelPath
		}
		return rankedTags[i].Tag.Line < rankedTags[j].Tag.Line
	})

	// 4. Binary search packing
	charBudget := tokenBudget * 4
	low := 0
	high := len(rankedTags)
	bestOutput := ""

	for low <= high {
		mid := (low + high) / 2
		candidateTags := rankedTags[:mid]

		formatted := formatSkeleton(candidateTags)
		if len(formatted) <= charBudget {
			bestOutput = formatted
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	if bestOutput == "" && len(rankedTags) > 0 {
		limit := 5
		if len(rankedTags) < limit {
			limit = len(rankedTags)
		}
		bestOutput = formatSkeleton(rankedTags[:limit])
	}

	return bestOutput, nil
}

func computePageRank(nodes map[string]bool, edges map[string]map[string]float64, activeFiles []string, damping float64, maxIterations int) map[string]float64 {
	rank := make(map[string]float64)
	numNodes := len(nodes)
	if numNodes == 0 {
		return rank
	}

	teleport := make(map[string]float64)
	activeCount := 0
	for _, file := range activeFiles {
		if nodes[file] {
			teleport[file] = 1.0
			activeCount++
		}
	}

	if activeCount > 0 {
		for file := range teleport {
			teleport[file] = 1.0 / float64(activeCount)
		}
	} else {
		for file := range nodes {
			teleport[file] = 1.0 / float64(numNodes)
		}
	}

	for file := range nodes {
		rank[file] = 1.0 / float64(numNodes)
	}

	outSum := make(map[string]float64)
	for from, toMap := range edges {
		sum := 0.0
		for _, w := range toMap {
			sum += w
		}
		outSum[from] = sum
	}

	for iter := 0; iter < maxIterations; iter++ {
		nextRank := make(map[string]float64)
		danglingSum := 0.0

		for node, currentRank := range rank {
			toMap, hasEdges := edges[node]
			if !hasEdges || len(toMap) == 0 {
				danglingSum += currentRank
				continue
			}

			totalWeight := outSum[node]
			for to, weight := range toMap {
				nextRank[to] += damping * currentRank * (weight / totalWeight)
			}
		}

		for node := range nodes {
			teleportVal := teleport[node]
			nextRank[node] += (1.0 - damping) * teleportVal
			nextRank[node] += damping * danglingSum * teleportVal
		}

		rank = nextRank
	}

	return rank
}

func formatSkeleton(ranked []RankedTag) string {
	grouped := make(map[string][]Tag)
	var files []string

	for _, rt := range ranked {
		file := rt.Tag.RelPath
		if _, ok := grouped[file]; !ok {
			files = append(files, file)
		}
		grouped[file] = append(grouped[file], rt.Tag)
	}

	sort.Strings(files)

	var sb strings.Builder
	sb.WriteString("REPOSITORY MAP (Active Skeletons):\n")

	for _, file := range files {
		tags := grouped[file]
		sort.Slice(tags, func(i, j int) bool {
			return tags[i].Line < tags[j].Line
		})

		sb.WriteString(fmt.Sprintf("%s:\n", file))
		for _, tag := range tags {
			sb.WriteString(fmt.Sprintf("  │ %d: %s\n", tag.Line, tag.Signature))
		}
		sb.WriteString("  \u22ee...\n")
	}

	return sb.String()
}
