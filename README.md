# 🤖 MultiCode

MultiCode is a lightweight, high-performance, and feature-complete AI-agentic desktop IDE. It is designed to manage and work simultaneously across multiple project workspaces, leveraging an autonomous AI agent to read, edit, execute, and connect external database systems and web tools via the Model Context Protocol (MCP).

Built with **Go (Golang)**, **React**, **TypeScript**, and **Wails**, the entire application is written in **under 6,000 lines of clean code**—maximizing architectural density and minimizing dependency bloat.

---

## ✨ Features

*   📂 **Multi-Workspace Operations:** Open, manage, and toggle between multiple directory trees simultaneously.
*   🧠 **Flexible LLM Orchestration:** Routing client supporting Gemini, OpenCode, Ollama (local models), and OpenRouter wildcards (e.g. `openrouter/free` auto-router) with custom model starring.
*   🤖 **Autonomous Agent Executor:** An XML-based tool-calling engine that reads/writes files, plans task lists, updates checklists, and executes terminal commands.
*   🔍 **Surgical Diff Approval Gate:** Side-by-side diff viewer. Every code edit proposed by the agent goes through a local YOLO-prevention gate where you can review, approve, or reject changes before they hit your disk.
*   📊 **Repo Mapping & Token Optimization:** Implements a PageRank-based repository skeleton generator. The agent builds a condensed codebase layout representation to maximize prompt efficiency.
*   🔌 **Model Context Protocol (MCP):** Connects to external MCP servers (such as SQLite, PostgreSQL, Puppeteer, GitHub, Brave Search) over stdin/stdout pipes, translating custom tools dynamically into the agent's prompt environment.
*   🗜️ **Context Window Compression:** Auto-compresses history logs and token payloads to prevent prompt bloat.

---

## 🛠️ Architecture

MultiCode is designed as a secure, local-first application splitting frontend interactions and OS-level control:

```
┌──────────────────────────────────────────────────────────┐
│                      React Frontend                      │
│      (Workspace Explorer, Terminal xterm, Diff View)     │
└────────────────────────────┬─────────────────────────────┘
                             │ (Wails IPC Bindings)
┌────────────────────────────▼─────────────────────────────┐
│                        Go Backend                        │
│ (Agent Loop, LLM Client, Settings Manager, PageRank Graph)│
└────────────────────────────┬─────────────────────────────┘
                             │ (Stdio Pipes / JSON-RPC 2.0)
┌────────────────────────────▼─────────────────────────────┐
│                    MCP Server Processes                  │
│       (Local SQLite DB, Puppeteer Headless Chrome)       │
└──────────────────────────────────────────────────────────┘
```

*   **Frontend:** Built with React, TypeScript, Monaco Editor (for diffing and coding), and `xterm.js` for full interactive terminal emulation.
*   **Backend:** Powered by Go. Manages settings, runs the asynchronous agent loop, executes shell commands, and acts as the JSON-RPC coordinator for MCP.
*   **Security:** MultiCode hides background console windows on Windows (`syscall.SysProcAttr{HideWindow: true}`) for silent, background execution.

---

## 🚀 Getting Started

### Prerequisites
*   [Go](https://go.dev/dl/) (v1.21 or higher)
*   [Node.js & npm](https://nodejs.org/en) (v18 or higher)
*   [Wails CLI](https://wails.io/docs/gettingstarted/installation) (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

### Build & Run
1.  Clone the repository:
    ```bash
    git clone https://github.com/btrahan1/MutliCode.git
    cd MutliCode
    ```
2.  Run in development mode:
    ```bash
    wails dev
    ```
3.  Build production-ready desktop executable:
    ```bash
    wails build
    ```
    The compiled binary will be placed in `build/bin/MultiCode.exe`.

---

## 🔌 Model Context Protocol (MCP) Setup

MultiCode allows you to register MCP servers directly from the **Application Settings** panel. Here are verified configurations you can paste directly:

### 💾 Local SQLite Database Server
Allows the AI agent to query, inspect schemas, and manage database files locally:
```json
{
  "sqlite": {
    "command": "npx",
    "args": [
      "-y",
      "mcp-sqlite",
      "C:/Users/your-username/my-database.db"
    ]
  }
}
```

### 🔍 Brave Web Search
Allows the AI agent to search the web dynamically for documentation or answers (requires a Brave Search API Key):
```json
{
  "brave-search": {
    "command": "npx",
    "args": [
      "-y",
      "@modelcontextprotocol/server-brave-search"
    ],
    "env": {
      "BRAVE_API_KEY": "YOUR_BRAVE_API_KEY_HERE"
    }
  }
}
```

---

## 📈 System Code Statistics

*   **Total Codebase Size:** ~5,780 Lines of Code.
*   **Backend (Go):** ~4,000 Lines (Concurrence-safe agent handlers, terminal shells, settings engines, and custom JSON-RPC implementations).
*   **Frontend (TSX/CSS):** ~1,780 Lines (Monaco diff editor, tabs manager, dynamic modal dialogs, and auto-polling indicators).
