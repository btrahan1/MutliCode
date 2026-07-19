import { useState, useEffect, useRef } from 'react';
import './App.css';
import { 
  SelectWorkspace, 
  GetDirectoryTree, 
  GetFileContent, 
  SaveFileContent, 
  LoadSettings, 
  SaveSettings,
  CreateFile,
  CreateDirectory,
  DeletePath,
  RenamePath,
  StartAgent,
  OpenPathInExplorer,
  StopAgent,
  CreateNewProject,
  GetProjectSettings,
  SaveProjectSettings,
  ApprovePlan,
  RejectPlan,
  RunProject,
  StopProject,
  OpenBrowserURL,
  GetProjectSourceString
} from "../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../wailsjs/runtime/runtime";
import { main } from "../wailsjs/go/models";
import Editor from '@monaco-editor/react';

interface FileNode {
  name: string;
  path: string;
  isDir: boolean;
  children?: FileNode[];
}

interface TaskItem {
  id: string;
  description: string;
  status: 'pending' | 'in_progress' | 'completed' | 'failed';
}

interface AgentPlan {
  description: string;
  tasks: TaskItem[];
}

interface ProjectTab {
  id: string;
  name: string;
  path: string;
  fileTree: FileNode | null;
  activeFile: string | null;
  fileContent: string;
  isDirty: boolean;
  model: string;
  messages: main.ChatMessage[];
  agentStatus: 'idle' | 'running' | 'completed' | 'waiting_for_approval' | 'waiting_for_command_approval';
  agentGoal: string;
  chatInput: string;
  techStack?: string[];
  activeView?: 'editor' | 'plan';
  agentPlan?: AgentPlan | null;
  pendingCommand?: string | null;
  projectStatus?: 'idle' | 'starting' | 'running' | 'error';
  projectUrl?: string | null;
}

const TECH_STACKS = ["Wails", "Go", "React", "TypeScript", "HTML", ".NET", "Blazor", "Winforms"];

const MODELS = [
  "big-pickle",
  "DeepSeek Flash Free",
  "Gemini 2.5 Flash",
  "gemma4:26b"
];

const getLanguageFromFilename = (filename: string): string => {
  if (!filename) return "plaintext";
  const ext = filename.split('.').pop()?.toLowerCase();
  switch (ext) {
    case 'js':
    case 'jsx':
      return 'javascript';
    case 'ts':
    case 'tsx':
      return 'typescript';
    case 'go':
      return 'go';
    case 'py':
      return 'python';
    case 'json':
      return 'json';
    case 'html':
    case 'htm':
      return 'html';
    case 'css':
      return 'css';
    case 'md':
      return 'markdown';
    case 'yaml':
    case 'yml':
      return 'yaml';
    case 'sql':
      return 'sql';
    case 'cs':
      return 'csharp';
    case 'xml':
    case 'csproj':
    case 'config':
      return 'xml';
    case 'cpp':
    case 'h':
    case 'hpp':
    case 'cc':
      return 'cpp';
    case 'c':
      return 'c';
    case 'rs':
      return 'rust';
    case 'java':
      return 'java';
    case 'sh':
    case 'bash':
      return 'shell';
    default:
      return 'plaintext';
  }
};

function App() {
  const [tabs, setTabs] = useState<ProjectTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [collapsedPaths, setCollapsedPaths] = useState<Set<string>>(new Set());
  const [isLoaded, setIsLoaded] = useState(false);
  const [sidebarWidth, setSidebarWidth] = useState(260);
  const [chatWidth, setChatWidth] = useState(320);
  const [theme, setTheme] = useState<'dark' | 'light'>('dark');
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);

  const [isNewProjectOpen, setIsNewProjectOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  const [newProjectParentDir, setNewProjectParentDir] = useState('');
  const [newProjectTechStack, setNewProjectTechStack] = useState<string[]>([]);

  const [isProjectSettingsOpen, setIsProjectSettingsOpen] = useState(false);
  const [currentProjectTechStack, setCurrentProjectTechStack] = useState<string[]>([]);

  const [apiKeys, setApiKeys] = useState({
    geminiApiKey: '',
    openCodeApiKey: '',
    openRouterApiKey: '',
    ollamaEndpoint: 'http://localhost:11434'
  });

  const [toggles, setToggles] = useState({
    enableSearchCode: true,
    enableContextCompression: true,
    useRepoMap: false,
    repoMapTokens: 1024,
    enforcePlanning: true
  });

  const [contextMenu, setContextMenu] = useState<{
    visible: boolean;
    x: number;
    y: number;
    node: FileNode | null;
  }>({ visible: false, x: 0, y: 0, node: null });

  const [toast, setToast] = useState<{ message: string; type: 'success' | 'info' | 'error' } | null>(null);

  const showToast = (message: string, type: 'success' | 'info' | 'error' = 'success') => {
    setToast({ message, type });
  };

  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 2500);
    return () => clearTimeout(timer);
  }, [toast]);

  const activeTab = tabs.find(t => t.id === activeTabId) || null;
  const messagesEndRef = useRef<HTMLDivElement | null>(null);

  // Load settings and setup event listeners on startup
  useEffect(() => {
    // Listen to agent messages
    EventsOn("agent:message", (event: { tabId: string; message: main.ChatMessage }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          const lastMsg = t.messages[t.messages.length - 1];
          if (lastMsg && lastMsg.role === event.message.role && lastMsg.content === event.message.content) {
            return t;
          }
          return {
            ...t,
            messages: [...t.messages, event.message]
          };
        }
        return t;
      }));
    });

    // Listen to agent plan updates
    EventsOn("agent:plan", (event: { tabId: string; status: 'idle' | 'running' | 'completed' | 'waiting_for_approval' | 'waiting_for_command_approval'; plan: AgentPlan | null }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          return {
            ...t,
            agentStatus: event.status,
            agentPlan: event.plan,
            activeView: event.plan ? 'plan' : t.activeView,
            pendingCommand: null
          };
        }
        return t;
      }));
    });

    // Listen to agent command approval requests
    EventsOn("agent:command_approval", (event: { tabId: string; status: 'idle' | 'running' | 'completed' | 'waiting_for_command_approval'; command: string }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          return {
            ...t,
            agentStatus: event.status,
            pendingCommand: event.command
          };
        }
        return t;
      }));
    });

    // Listen to project status events
    EventsOn("project:status", (event: { tabId: string; status: 'idle' | 'starting' | 'running' | 'error' }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          return {
            ...t,
            projectStatus: event.status,
            projectUrl: event.status === 'idle' || event.status === 'error' ? null : t.projectUrl
          };
        }
        return t;
      }));
    });

    // Listen to project url extraction events
    EventsOn("project:url", (event: { tabId: string; url: string }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          return {
            ...t,
            projectStatus: 'running',
            projectUrl: event.url
          };
        }
        return t;
      }));
    });

    // Listen to agent history compression updates
    EventsOn("agent:history_update", (event: { tabId: string; messages: main.ChatMessage[] }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          return {
            ...t,
            messages: event.messages
          };
        }
        return t;
      }));
    });

    // Listen to agent status updates
    EventsOn("agent:status", (event: { tabId: string; status: 'idle' | 'running' | 'completed' | 'waiting_for_approval' | 'waiting_for_command_approval' }) => {
      setTabs(prev => prev.map(t => {
        if (t.id === event.tabId) {
          const wasRunning = t.agentStatus === "running";
          const isCompleted = event.status === "completed";

          // If transition from running to completed, refresh the file tree
          if (wasRunning && isCompleted) {
            GetDirectoryTree(t.path).then(tree => {
              setTabs(current => current.map(curr => curr.id === t.id ? { ...curr, fileTree: tree } : curr));
            }).catch(err => console.error("Failed to refresh tree on complete:", err));
          }

          return {
            ...t,
            agentStatus: event.status
          };
        }
        return t;
      }));
    });

    async function initSettings() {
      try {
        const loaded = await LoadSettings();
        if (loaded) {
          setApiKeys({
            geminiApiKey: loaded.geminiApiKey || '',
            openCodeApiKey: loaded.openCodeApiKey || '',
            openRouterApiKey: loaded.openRouterApiKey || '',
            ollamaEndpoint: loaded.ollamaEndpoint || 'http://localhost:11434'
          });
          setToggles({
            enableSearchCode: loaded.enableSearchCode !== false,
            enableContextCompression: loaded.enableContextCompression !== false,
            useRepoMap: loaded.useRepoMap === true,
            repoMapTokens: loaded.repoMapTokens || 1024,
            enforcePlanning: loaded.enforcePlanning !== false
          });
          setSidebarWidth(loaded.sidebarWidth || 260);
          setChatWidth(loaded.chatWidth || 320);
          setTheme((loaded.theme as 'dark' | 'light') || 'dark');
        }
        if (loaded && loaded.openWorkspaces && loaded.openWorkspaces.length > 0) {
          const restoredTabs: ProjectTab[] = [];
          for (const path of loaded.openWorkspaces) {
            try {
              const tree = await GetDirectoryTree(path);
              const folderName = path.split(/[/\\]/).pop() || path;
              const savedModel = loaded.workspaceModels?.[path] || "big-pickle";
              let savedHistory = loaded.workspaceHistory?.[path];
              
              // Handle Wails type-conversion bug where map[string][]Struct becomes nested [[ChatMessage]]
              if (savedHistory && Array.isArray(savedHistory)) {
                if (savedHistory.length === 1 && Array.isArray(savedHistory[0])) {
                  savedHistory = savedHistory[0];
                }
              }
              
              if (!savedHistory || savedHistory.length === 0) {
                savedHistory = [
                  new main.ChatMessage({ role: 'assistant', content: `Restored workspace: ${folderName}\nPath: ${path}` })
                ];
              }
              
              const projSettings = await GetProjectSettings(path).catch(() => ({ techStack: [] }));
              restoredTabs.push({
                id: Math.random().toString(36).substring(2, 9),
                name: folderName,
                path: path,
                fileTree: tree,
                activeFile: null,
                fileContent: '',
                isDirty: false,
                model: savedModel,
                messages: savedHistory,
                agentStatus: 'idle',
                agentGoal: '',
                chatInput: '',
                techStack: projSettings.techStack || [],
                activeView: 'editor',
                agentPlan: null,
                pendingCommand: null,
                projectStatus: 'idle',
                projectUrl: null
              });
            } catch (treeErr) {
              console.error(`Failed to restore workspace at ${path}:`, treeErr);
            }
          }

          if (restoredTabs.length > 0) {
            setTabs(restoredTabs);
            // Match active workspace if possible
            const match = restoredTabs.find(t => t.path === loaded.activeWorkspace);
            setActiveTabId(match ? match.id : restoredTabs[0].id);
          } else {
            showWelcomeTab();
          }
        } else {
          showWelcomeTab();
        }
      } catch (err) {
        console.error("Failed to load settings:", err);
        showWelcomeTab();
      } finally {
        setIsLoaded(true);
      }
    }

    initSettings();

    // Context Menu Global Click Dismissal
    const hideMenu = () => setContextMenu(prev => ({ ...prev, visible: false }));
    window.addEventListener('click', hideMenu);

    return () => {
      EventsOff("agent:message");
      EventsOff("agent:plan");
      EventsOff("agent:command_approval");
      EventsOff("agent:status");
      EventsOff("agent:history_update");
      EventsOff("project:status");
      EventsOff("project:url");
      window.removeEventListener('click', hideMenu);
    };
  }, []);

  // Save settings whenever tabs, active tab, width, theme, or models change
  useEffect(() => {
    if (!isLoaded) return;
    
    const activeWorkspacePath = tabs.find(t => t.id === activeTabId)?.path || "";
    const openWorkspaces = tabs.filter(t => t.path !== "").map(t => t.path);
    const workspaceModels: Record<string, string> = {};
    const workspaceHistory: Record<string, main.ChatMessage[]> = {};

    tabs.forEach(t => {
      if (t.path) {
        workspaceModels[t.path] = t.model;
        workspaceHistory[t.path] = t.messages;
      }
    });

    const settings = {
      openWorkspaces,
      activeWorkspace: activeWorkspacePath,
      geminiApiKey: apiKeys.geminiApiKey,
      openCodeApiKey: apiKeys.openCodeApiKey,
      openRouterApiKey: apiKeys.openRouterApiKey,
      ollamaEndpoint: apiKeys.ollamaEndpoint,
      workspaceModels,
      workspaceHistory,
      sidebarWidth,
      chatWidth,
      theme,
      enableSearchCode: toggles.enableSearchCode,
      enableContextCompression: toggles.enableContextCompression,
      useRepoMap: toggles.useRepoMap,
      repoMapTokens: toggles.repoMapTokens,
      enforcePlanning: toggles.enforcePlanning
    };

    SaveSettings(settings as any).catch(err => console.error("Failed to save settings:", err));
  }, [tabs, activeTabId, isLoaded, sidebarWidth, chatWidth, theme, apiKeys, toggles]);

  // Autoscroll chat history
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [activeTab?.messages, activeTab?.agentStatus]);

  const toggleTheme = () => {
    setTheme(prev => prev === 'dark' ? 'light' : 'dark');
  };

  const showWelcomeTab = () => {
    const welcome: ProjectTab = {
      id: 'welcome',
      name: 'Welcome to MultiCode',
      path: '',
      fileTree: null,
      activeFile: null,
      fileContent: '',
      isDirty: false,
      model: 'big-pickle',
      messages: [
        new main.ChatMessage({ role: 'assistant', content: 'Welcome to MultiCode! Open a project folder to start working simultaneously across multiple workspaces.' })
      ],
      agentStatus: 'idle',
      agentGoal: '',
      chatInput: '',
      activeView: 'editor',
      agentPlan: null,
      pendingCommand: null,
      projectStatus: 'idle',
      projectUrl: null
    };
    setTabs([welcome]);
    setActiveTabId('welcome');
  };

  // Open directory selector
  const handleOpenProject = async () => {
    try {
      const path = await SelectWorkspace();
      if (!path) return;

      const folderName = path.split(/[/\\]/).pop() || path;
      const tree = await GetDirectoryTree(path);

      // Check if project already open
      const existing = tabs.find(t => t.path === path);
      if (existing) {
        setActiveTabId(existing.id);
        return;
      }

      const projSettings = await GetProjectSettings(path).catch(() => ({ techStack: [] }));
      const newTab: ProjectTab = {
        id: Math.random().toString(36).substring(2, 9),
        name: folderName,
        path: path,
        fileTree: tree,
        activeFile: null,
        fileContent: '',
        isDirty: false,
        model: 'big-pickle',
        messages: [
          new main.ChatMessage({ role: 'assistant', content: `Opened project: ${folderName}\nPath: ${path}\nTech Stack: ${(projSettings.techStack || []).join(", ") || "None selected"}` })
        ],
        agentStatus: 'idle',
        agentGoal: '',
        chatInput: '',
        techStack: projSettings.techStack || [],
        activeView: 'editor',
        agentPlan: null,
        pendingCommand: null,
        projectStatus: 'idle',
        projectUrl: null
      };

      setTabs(prev => {
        const filtered = prev.filter(t => t.id !== 'welcome');
        return [...filtered, newTab];
      });
      setActiveTabId(newTab.id);
    } catch (err) {
      console.error("Failed to open project:", err);
    }
  };

  const handleCreateNewProject = async () => {
    if (!newProjectName.trim()) {
      showToast("Please enter a project name", "error");
      return;
    }
    if (!newProjectParentDir.trim()) {
      showToast("Please select a parent directory", "error");
      return;
    }
    try {
      const createdPath = await CreateNewProject(newProjectParentDir, newProjectName.trim(), newProjectTechStack);
      showToast(`Project ${newProjectName} created!`, "success");
      setIsNewProjectOpen(false);
      setNewProjectName('');
      setNewProjectTechStack([]);

      const folderName = newProjectName.trim();
      const tree = await GetDirectoryTree(createdPath);

      const newTab: ProjectTab = {
        id: Math.random().toString(36).substring(2, 9),
        name: folderName,
        path: createdPath,
        fileTree: tree,
        activeFile: null,
        fileContent: '',
        isDirty: false,
        model: 'big-pickle',
        messages: [
          new main.ChatMessage({ role: 'assistant', content: `Created and opened project: ${folderName}\nPath: ${createdPath}\nTech Stack: ${newProjectTechStack.join(", ") || "None"}` })
        ],
        agentStatus: 'idle',
        agentGoal: '',
        chatInput: '',
        techStack: newProjectTechStack,
        activeView: 'editor',
        agentPlan: null,
        pendingCommand: null,
        projectStatus: 'idle',
        projectUrl: null
      };

      setTabs(prev => {
        const filtered = prev.filter(t => t.id !== 'welcome');
        return [...filtered, newTab];
      });
      setActiveTabId(newTab.id);
    } catch (err: any) {
      showToast(`Error creating project: ${err}`, "error");
    }
  };

  const handleBrowseParentDir = async () => {
    try {
      const dir = await SelectWorkspace();
      if (dir) {
        setNewProjectParentDir(dir);
      }
    } catch (err) {
      console.error("Failed to select folder", err);
    }
  };

  const handleOpenProjectSettings = async () => {
    if (!activeTab || !activeTab.path) return;
    try {
      const settings = await GetProjectSettings(activeTab.path);
      setCurrentProjectTechStack(settings.techStack || []);
      setIsProjectSettingsOpen(true);
    } catch (err) {
      showToast("Failed to load project settings", "error");
    }
  };

  const handleSaveProjectSettings = async () => {
    if (!activeTab || !activeTab.path) return;
    try {
      await SaveProjectSettings(activeTab.path, { techStack: currentProjectTechStack });
      showToast("Project settings saved!", "success");
      
      setTabs(prev => prev.map(t => {
        if (t.id === activeTab.id) {
          return {
            ...t,
            techStack: currentProjectTechStack,
            messages: [
              ...t.messages,
              new main.ChatMessage({ role: 'assistant', content: `Updated project tech stack: ${currentProjectTechStack.join(", ") || "None"}` })
            ]
          };
        }
        return t;
      }));
      setIsProjectSettingsOpen(false);
    } catch (err) {
      showToast("Failed to save project settings", "error");
    }
  };

  const handleCloseTab = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setTabs(prev => {
      const nextTabs = prev.filter(t => t.id !== id);
      if (activeTabId === id) {
        setActiveTabId(nextTabs[nextTabs.length - 1]?.id || null);
      }
      return nextTabs;
    });
  };

  const refreshFileTree = async () => {
    if (!activeTab || !activeTab.path) return;
    try {
      const tree = await GetDirectoryTree(activeTab.path);
      setTabs(prev => prev.map(t => {
        if (t.id === activeTab.id) {
          return { ...t, fileTree: tree };
        }
        return t;
      }));
    } catch (err) {
      console.error("Failed to refresh file tree:", err);
    }
  };

  const handleSelectFile = async (node: FileNode) => {
    if (!activeTab || node.isDir) return;
    try {
      const content = await GetFileContent(activeTab.path, node.path);
      setTabs(prev => prev.map(t => {
        if (t.id === activeTab.id) {
          return {
            ...t,
            activeFile: node.path,
            fileContent: content,
            isDirty: false
          };
        }
        return t;
      }));
    } catch (err) {
      console.error("Failed to read file:", err);
    }
  };

  const handleSaveFile = async () => {
    if (!activeTab || !activeTab.activeFile) return;
    try {
      await SaveFileContent(activeTab.path, activeTab.activeFile, activeTab.fileContent);
      setTabs(prev => prev.map(t => {
        if (t.id === activeTab.id) {
          return { ...t, isDirty: false };
        }
        return t;
      }));
    } catch (err) {
      console.error("Failed to save file:", err);
    }
  };

  const handleCreateFile = async () => {
    if (!activeTab || !activeTab.path) return;
    const name = prompt("Enter file name (relative path):");
    if (!name) return;
    try {
      await CreateFile(activeTab.path, name);
      await refreshFileTree();
    } catch (err) {
      alert("Failed to create file: " + err);
    }
  };

  const handleCreateDirectory = async () => {
    if (!activeTab || !activeTab.path) return;
    const name = prompt("Enter folder name (relative path):");
    if (!name) return;
    try {
      await CreateDirectory(activeTab.path, name);
      await refreshFileTree();
    } catch (err) {
      alert("Failed to create folder: " + err);
    }
  };

  const handleDeletePath = async (node: FileNode, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!activeTab || !activeTab.path) return;
    if (!confirm(`Are you sure you want to delete ${node.name}?`)) return;
    try {
      await DeletePath(activeTab.path, node.path);
      if (activeTab.activeFile === node.path) {
        setTabs(prev => prev.map(t => {
          if (t.id === activeTab.id) {
            return { ...t, activeFile: null, fileContent: "", isDirty: false };
          }
          return t;
        }));
      }
      await refreshFileTree();
    } catch (err) {
      alert("Failed to delete: " + err);
    }
  };

  const handleRenamePath = async (node: FileNode, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!activeTab || !activeTab.path) return;
    const newName = prompt(`Rename ${node.name} to:`, node.name);
    if (!newName || newName === node.name) return;
    
    const parts = node.path.split('/');
    parts[parts.length - 1] = newName;
    const newPath = parts.join('/');

    try {
      await RenamePath(activeTab.path, node.path, newPath);
      if (activeTab.activeFile === node.path) {
        setTabs(prev => prev.map(t => {
          if (t.id === activeTab.id) {
            return { ...t, activeFile: newPath };
          }
          return t;
        }));
      }
      await refreshFileTree();
    } catch (err) {
      alert("Failed to rename: " + err);
    }
  };

  const handleEditorChange = (val: string) => {
    if (!activeTab) return;
    setTabs(prev => prev.map(t => {
      if (t.id === activeTab.id) {
        return {
          ...t,
          fileContent: val,
          isDirty: true
        };
      }
      return t;
    }));
  };

  const handleModelChange = (model: string) => {
    if (!activeTab) return;
    setTabs(prev => prev.map(t => {
      if (t.id === activeTab.id) {
        return { ...t, model };
      }
      return t;
    }));
  };

  const handleSendMessage = async () => {
    if (!activeTab || !activeTab.chatInput || !activeTab.chatInput.trim()) return;

    const promptText = activeTab.chatInput;
    const userMsg = new main.ChatMessage({ role: 'user', content: promptText });

    const history = activeTab.messages.map(m => ({ role: m.role, content: m.content }));

    setTabs(prev => prev.map(t => {
      if (t.id === activeTab.id) {
        return {
          ...t,
          chatInput: "",
          messages: [...t.messages, userMsg],
          agentStatus: 'running',
          agentGoal: promptText
        };
      }
      return t;
    }));

    try {
      await StartAgent(activeTab.id, activeTab.path, activeTab.model, promptText, history);
    } catch (err) {
      setTabs(prev => prev.map(t => {
        if (t.id === activeTab.id) {
          return {
            ...t,
            agentStatus: 'idle',
            messages: [
              ...t.messages,
              new main.ChatMessage({ role: 'assistant', content: `[Error starting agent]: ${err}` })
            ]
          };
        }
        return t;
      }));
    }
  };

  const handleStopAgent = async () => {
    if (!activeTab) return;
    try {
      await StopAgent(activeTab.id);
    } catch (err) {
      console.error("Failed to stop agent:", err);
    }
  };

  const toggleFolder = (path: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setCollapsedPaths(prev => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  };

  const startResizeSidebar = (e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const startWidth = sidebarWidth;

    const doResize = (moveEvent: MouseEvent) => {
      const newWidth = startWidth + (moveEvent.clientX - startX);
      if (newWidth > 180 && newWidth < 600) {
        setSidebarWidth(newWidth);
      }
    };

    const stopResize = () => {
      document.removeEventListener('mousemove', doResize);
      document.removeEventListener('mouseup', stopResize);
    };

    document.addEventListener('mousemove', doResize);
    document.addEventListener('mouseup', stopResize);
  };

  const startResizeChat = (e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const startWidth = chatWidth;

    const doResize = (moveEvent: MouseEvent) => {
      const newWidth = startWidth - (moveEvent.clientX - startX);
      if (newWidth > 200 && newWidth < 800) {
        setChatWidth(newWidth);
      }
    };

    const stopResize = () => {
      document.removeEventListener('mousemove', doResize);
      document.removeEventListener('mouseup', stopResize);
    };

    document.addEventListener('mousemove', doResize);
    document.addEventListener('mouseup', stopResize);
  };

  const handleChatInputChange = (val: string) => {
    if (!activeTab) return;
    setTabs(prev => prev.map(t => {
      if (t.id === activeTab.id) {
        return { ...t, chatInput: val };
      }
      return t;
    }));
  };

  const handleContextMenu = (e: React.MouseEvent, node: FileNode) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      node: node
    });
  };

  const handleClearChat = () => {
    if (!activeTab) return;
    if (!confirm("Are you sure you want to clear the conversation history for this workspace?")) return;
    setTabs(prev => prev.map(t => {
      if (t.id === activeTab.id) {
        return {
          ...t,
          messages: [
            new main.ChatMessage({ role: 'assistant', content: `Cleared chat history for: ${t.name}` })
          ]
        };
      }
      return t;
    }));
  };

  const handleManualCompress = () => {
    if (!activeTab) return;
    const messages = activeTab.messages;
    if (messages.length <= 4) {
      showToast("No older logs to compress", "info");
      return;
    }

    let compressedCount = 0;
    const compressed = messages.map((msg, idx) => {
      // Preserve the last 3 messages
      if (idx >= messages.length - 3) {
        return msg;
      }
      if (msg.role === 'user' && msg.content.startsWith('### TOOL OUTPUT:\n')) {
        // Skip if already compressed
        if (msg.content.includes("compressed to save context space")) {
          return msg;
        }

        const lines = msg.content.split('\n');
        let toolName = "tool output";
        if (lines.length > 1) {
          toolName = lines[1].trim();
          if (toolName.startsWith('[') && toolName.endsWith(']')) {
            toolName = toolName.slice(1, -1);
          }
        }
        compressedCount++;
        return new main.ChatMessage({
          role: msg.role,
          content: `### TOOL OUTPUT:\n${toolName} (content manually compressed to save context space)`
        });
      }
      return msg;
    });

    if (compressedCount === 0) {
      showToast("Context already compressed", "info");
      return;
    }

    setTabs(prev => prev.map(t => {
      if (t.id === activeTab.id) {
        return {
          ...t,
          messages: compressed
        };
      }
      return t;
    }));

    showToast(`Compressed ${compressedCount} tool output logs!`, "success");
  };

  // Custom markdown/code-block formatter
  const renderMessageContent = (content: string) => {
    if (!content) return null;
    const parts = content.split(/(```[\s\S]*?```)/g);

    return parts.map((part, index) => {
      if (part.startsWith('```')) {
        const match = part.match(/```(\w*)\n([\s\S]*?)```/);
        const lang = match ? match[1] : '';
        const code = match ? match[2] : part.slice(3, -3);

        const copyToClipboard = () => {
          navigator.clipboard.writeText(code);
        };

        return (
          <div key={index} className="code-block-container">
            <div className="code-block-header">
              <span>{lang || 'code'}</span>
              <button onClick={copyToClipboard} className="copy-code-btn">Copy</button>
            </div>
            <pre className="code-block">
              <code>{code.trim()}</code>
            </pre>
          </div>
        );
      } else {
        const inlineParts = part.split(/(`[^`\n]+`)/g);
        return (
          <span key={index}>
            {inlineParts.map((subPart, subIdx) => {
              if (subPart.startsWith('`') && subPart.endsWith('`')) {
                return <code key={subIdx} className="inline-code">{subPart.slice(1, -1)}</code>;
              }
              return subPart.split('\n').map((line, lineIdx, array) => (
                <span key={lineIdx}>
                  {line}
                  {lineIdx < array.length - 1 && <br />}
                </span>
              ));
            })}
          </span>
        );
      }
    });
  };

  // Helper to render tree nodes recursively
  const renderTree = (node: FileNode) => {
    const isCollapsed = collapsedPaths.has(node.path);
    return (
      <div key={node.path} className="tree-node" style={{ paddingLeft: '8px' }}>
        <div 
          className={`tree-label-wrapper ${activeTab?.activeFile === node.path ? 'active-file' : ''}`}
          onClick={() => node.isDir ? null : handleSelectFile(node)}
          onContextMenu={(e) => handleContextMenu(e, node)}
        >
          <div className="tree-label-info" onClick={(e) => node.isDir ? toggleFolder(node.path, e) : null}>
            {node.isDir ? (
              <>
                <span className={`folder-arrow ${isCollapsed ? 'collapsed' : ''}`}>▼</span>
                <svg className="node-icon" viewBox="0 0 24 24"><path fill="currentColor" d="M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/></svg>
              </>
            ) : (
              <svg className="node-icon file-icon" viewBox="0 0 24 24"><path fill="currentColor" d="M6 2c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6H6zm7 7V3.5L18.5 9H13z"/></svg>
            )}
            <span className="node-name">{node.name}</span>
          </div>
          
          {activeTab?.path && (
            <div className="node-actions">
              <button title="Rename" onClick={(e) => handleRenamePath(node, e)}>✎</button>
              <button title="Delete" onClick={(e) => handleDeletePath(node, e)}>✕</button>
            </div>
          )}
        </div>
        {node.isDir && !isCollapsed && node.children && (
          <div className="tree-children">
            {node.children.map(child => renderTree(child))}
          </div>
        )}
      </div>
    );
  };

  const handleCopySource = () => {
    if (!activeTab || !activeTab.path) return;
    GetProjectSourceString(activeTab.path)
      .then((sourceText) => {
        navigator.clipboard.writeText(sourceText)
          .then(() => showToast("Project source copied to clipboard!", "success"))
          .catch(err => showToast(`Failed to copy to clipboard: ${err}`, "error"));
      })
      .catch((err) => showToast(`Failed to harvest source: ${err}`, "error"));
  };

  const handleSwitchView = (view: 'editor' | 'plan') => {
    if (!activeTab) return;
    setTabs(prev => prev.map(t => t.id === activeTab.id ? { ...t, activeView: view } : t));
  };

  const handleApprovePlan = () => {
    if (!activeTab) return;
    ApprovePlan(activeTab.id)
      .then(() => {
        showToast("Plan approved! Executing tasks...", "success");
      })
      .catch(err => showToast(`Error approving plan: ${err}`, "error"));
  };

  const handleApproveCommand = () => {
    if (!activeTab) return;
    ApprovePlan(activeTab.id)
      .then(() => {
        showToast("Command allowed", "success");
      })
      .catch(err => showToast(`Error: ${err}`, "error"));
  };

  const handleRejectCommand = () => {
    if (!activeTab) return;
    RejectPlan(activeTab.id, "Command execution denied by user.")
      .then(() => {
        showToast("Command blocked", "info");
      })
      .catch(err => showToast(`Error: ${err}`, "error"));
  };

  const handleRunProject = () => {
    if (!activeTab || !activeTab.path) return;
    setTabs(prev => prev.map(t => t.id === activeTab.id ? { ...t, projectStatus: 'starting', projectUrl: null } : t));
    RunProject(activeTab.id, activeTab.path)
      .catch(err => {
        showToast(`Failed to run project: ${err}`, "error");
        setTabs(prev => prev.map(t => t.id === activeTab.id ? { ...t, projectStatus: 'error' } : t));
      });
  };

  const handleStopProject = () => {
    if (!activeTab) return;
    StopProject(activeTab.id)
      .catch(err => showToast(`Failed to stop project: ${err}`, "error"));
  };

  const handleOpenBrowser = () => {
    if (!activeTab || !activeTab.projectUrl) return;
    OpenBrowserURL(activeTab.projectUrl);
  };

  const handleRejectPlanClick = () => {
    if (!activeTab) return;
    const feedback = prompt("Please provide instructions on what to change in the plan:");
    if (feedback === null) return;
    if (!feedback.trim()) {
      showToast("Feedback cannot be empty", "error");
      return;
    }
    RejectPlan(activeTab.id, feedback.trim())
      .then(() => {
        showToast("Plan rejected. Sending feedback to agent.", "info");
      })
      .catch(err => showToast(`Error rejecting plan: ${err}`, "error"));
  };

  const handleRefreshExplorer = async () => {
    if (!activeTab || !activeTab.path) return;
    try {
      const tree = await GetDirectoryTree(activeTab.path);
      setTabs(prev => prev.map(t => t.id === activeTab.id ? { ...t, fileTree: tree } : t));
    } catch (err) {
      showToast(`Failed to refresh explorer: ${err}`, "error");
    }
  };

  const renderPlanView = () => {
    if (!activeTab || !activeTab.agentPlan) return null;
    const plan = activeTab.agentPlan;
    const status = activeTab.agentStatus;

    return (
      <div className="plan-view-container">
        <div className="plan-header-card">
          <h2>Execution Plan Description</h2>
          <p className="plan-description">{plan.description}</p>
        </div>

        <div className="plan-tasks-section">
          <h3>Checklist / Task Progress</h3>
          <div className="tasks-list">
            {plan.tasks && plan.tasks.map(task => {
              let icon = "⚪";
              let className = "task-pending";
              if (task.status === "in_progress") {
                icon = "🔵";
                className = "task-in-progress pulsing";
              } else if (task.status === "completed") {
                icon = "✅";
                className = "task-completed";
              } else if (task.status === "failed") {
                icon = "❌";
                className = "task-failed";
              }

              return (
                <div key={task.id} className={`task-item ${className}`}>
                  <span className="task-icon">{icon}</span>
                  <span className="task-desc">{task.description}</span>
                </div>
              );
            })}
          </div>
        </div>

        {status === "waiting_for_approval" && (
          <div className="plan-approval-gate">
            <div className="approval-warning">
              <h3>⚠️ User Approval Required</h3>
              <p>Review the plan checklist above. Do you want to proceed with this plan?</p>
            </div>
            <div className="approval-actions">
              <button className="approve-plan-btn" onClick={handleApprovePlan}>
                ✓ Approve Plan
              </button>
              <button className="reject-plan-btn" onClick={handleRejectPlanClick}>
                ✕ Request Changes
              </button>
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className={`multicode-app ${theme === 'light' ? 'light-theme' : ''}`}>
      {/* Top Header / Project Tab bar */}
      <header className="app-header">
        <div className="logo-section">
          <span className="logo-glowing-dot"></span>
          <h1>MultiCode</h1>
        </div>

        <div className="project-tabs-container">
          <div className="tabs-list">
            {tabs.map(tab => {
              const isActive = tab.id === activeTabId;
              const isRunning = tab.agentStatus === 'running';
              return (
                <div 
                  key={tab.id} 
                  className={`project-tab ${isActive ? 'active' : ''} ${isRunning ? 'glowing-border' : ''}`}
                  onClick={() => setActiveTabId(tab.id)}
                >
                  {isRunning && <span className="tab-pulse-indicator"></span>}
                  <span className="tab-title">{tab.name}</span>
                  {tab.isDirty && <span className="tab-dirty-indicator">●</span>}
                  {tab.id !== 'welcome' && (
                    <button className="close-tab-btn" onClick={(e) => handleCloseTab(tab.id, e)}>×</button>
                  )}
                </div>
              );
            })}
          </div>
          <div className="header-controls">
            <button className="header-icon-btn" onClick={toggleTheme} title="Toggle Day/Night Theme">
              {theme === 'dark' ? '🌙' : '☀️'}
            </button>
            <button className="header-icon-btn" onClick={() => setIsSettingsOpen(true)} title="Settings">
              ⚙️
            </button>
            {activeTab && activeTab.path && (
              <button className="header-icon-btn" onClick={handleOpenProjectSettings} title="Project Settings">
                🛠️
              </button>
            )}
            <button className="new-project-btn secondary-new-btn" style={{ marginRight: '8px' }} onClick={() => setIsNewProjectOpen(true)} title="Create New Project">
              + New Project
            </button>
            <button className="new-project-btn" onClick={handleOpenProject} title="Open Project Folder">
              + Open Folder
            </button>
          </div>
        </div>
      </header>

      {/* Main Workspace Layout */}
      <div className="workspace-layout">
        {activeTab ? (
          <>
            {/* Left Explorer Sidebar */}
            <aside className="explorer-sidebar" style={{ width: sidebarWidth }}>
              <div className="sidebar-header">
                <div className="sidebar-title-row">
                  <h2>Explorer</h2>
                  {activeTab.path && (
                    <div className="project-runner-controls">
                      {(!activeTab.projectStatus || activeTab.projectStatus === 'idle' || activeTab.projectStatus === 'error') ? (
                        <button className="runner-btn play-btn" title="Run Project" onClick={handleRunProject}>▶️</button>
                      ) : (
                        <button className="runner-btn stop-btn" title="Stop Project" onClick={handleStopProject}>⏹️</button>
                      )}
                      {activeTab.projectStatus === 'starting' && <span className="runner-status starting">Starting...</span>}
                      {activeTab.projectStatus === 'running' && (
                        <span className="runner-status running" onClick={handleOpenBrowser} title="Open in browser">
                          🌐 {activeTab.projectUrl ? "Open" : "Running"}
                        </span>
                      )}
                    </div>
                  )}
                </div>
                {activeTab.path && (
                  <div className="explorer-quick-actions">
                    <button title="Copy Project Source" onClick={handleCopySource}>📋</button>
                    <button title="Refresh Explorer" onClick={handleRefreshExplorer}>🔄</button>
                    <button title="New File" onClick={handleCreateFile}>+📄</button>
                    <button title="New Folder" onClick={handleCreateDirectory}>+📁</button>
                  </div>
                )}
              </div>
              <div className="file-tree-container">
                {activeTab.fileTree ? (
                  renderTree(activeTab.fileTree)
                ) : (
                  <div className="empty-tree-state">
                    <p>No project directory loaded.</p>
                    <button className="secondary-btn" onClick={handleOpenProject}>Select Directory</button>
                  </div>
                )}
              </div>
            </aside>

            {/* Splitter 1 */}
            <div className="resizer-bar" onMouseDown={startResizeSidebar}></div>

            {/* Center Code Editor */}
            <main className="editor-area">
              <div className="editor-header">
                <div className="editor-tabs-switcher">
                  <button 
                    className={`editor-tab-btn ${(!activeTab.activeView || activeTab.activeView === 'editor') ? 'active' : ''}`}
                    onClick={() => handleSwitchView('editor')}
                  >
                    📄 Code Editor
                  </button>
                  {activeTab.agentPlan && (
                    <button 
                      className={`editor-tab-btn ${(activeTab.activeView === 'plan') ? 'active' : ''}`}
                      onClick={() => handleSwitchView('plan')}
                    >
                      📋 Execution Plan
                    </button>
                  )}
                </div>
                {(!activeTab.activeView || activeTab.activeView === 'editor') && (
                  <>
                    <span className="active-filepath">
                      {activeTab.activeFile ? activeTab.activeFile : "Select a file to edit"}
                    </span>
                    {activeTab.activeFile && (
                      <button 
                        className={`save-btn ${activeTab.isDirty ? 'dirty' : ''}`} 
                        onClick={handleSaveFile}
                        disabled={!activeTab.isDirty}
                      >
                        Save Changes
                      </button>
                    )}
                  </>
                )}
              </div>
              <div className="editor-wrapper">
                {activeTab.activeView === 'plan' && activeTab.agentPlan ? (
                  renderPlanView()
                ) : activeTab.activeFile ? (
                  <Editor
                    height="100%"
                    language={getLanguageFromFilename(activeTab.activeFile)}
                    theme={theme === 'dark' ? 'vs-dark' : 'light'}
                    value={activeTab.fileContent}
                    onChange={(value) => handleEditorChange(value || '')}
                    options={{
                      minimap: { enabled: true },
                      fontSize: 14,
                      lineNumbers: 'on',
                      automaticLayout: true,
                      scrollbar: {
                        vertical: 'visible',
                        horizontal: 'visible'
                      }
                    }}
                  />
                ) : (
                  <div className="editor-empty-state">
                    <svg className="splash-icon" viewBox="0 0 24 24"><path fill="currentColor" d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z"/></svg>
                    <h3>Simultaneous Workspaces</h3>
                    <p>Select a file from the explorer or type an instruction for the AI agent.</p>
                  </div>
                )}
              </div>
            </main>

            {/* Splitter 2 */}
            <div className="resizer-bar" onMouseDown={startResizeChat}></div>

            {/* Right Chat Assistant */}
            <aside className="chat-sidebar" style={{ width: chatWidth }}>
              <div className="chat-header">
                <div className="model-selector-wrapper">
                  <select 
                    value={activeTab.model} 
                    onChange={(e) => handleModelChange(e.target.value)}
                    className="model-select"
                  >
                    {MODELS.map(m => <option key={m} value={m}>{m}</option>)}
                  </select>
                </div>
                <div className="chat-header-actions">
                  <button className="clear-chat-btn" onClick={handleManualCompress} title="Compress Context History (Manual)">
                    🗜️
                  </button>
                  <button className="clear-chat-btn" onClick={handleClearChat} title="Clear Chat History">
                    🗑️
                  </button>
                  <div className="agent-status-badge">
                    <span className={`status-dot ${activeTab.agentStatus}`}></span>
                    <span className="status-text">{activeTab.agentStatus}</span>
                  </div>
                </div>
              </div>

              <div className="chat-messages-container">
                {activeTab.messages.map((m, idx) => (
                  <div key={idx} className={`message-bubble ${m.role}`}>
                    <div className="message-content">{renderMessageContent(m.content)}</div>
                  </div>
                ))}
                {activeTab.agentStatus === 'running' && (
                  <div className="message-bubble assistant running-bubble">
                    <div className="agent-spinner"></div>
                    <span>Agent is working in background...</span>
                  </div>
                )}
                <div ref={messagesEndRef} />
              </div>

              {activeTab.agentStatus === 'waiting_for_command_approval' && activeTab.pendingCommand && (
                <div className="command-approval-gate">
                  <div className="command-warning-header">
                    <span>⚠️ Dangerous Command Blocked</span>
                  </div>
                  <pre className="command-warning-code">
                    <code>{activeTab.pendingCommand}</code>
                  </pre>
                  <div className="command-warning-actions">
                    <button className="approve-command-btn" onClick={handleApproveCommand}>Allow</button>
                    <button className="reject-command-btn" onClick={handleRejectCommand}>Deny</button>
                  </div>
                </div>
              )}

              <div className="chat-input-area">
                <input
                  type="text"
                  placeholder={(activeTab.agentStatus === 'running' || activeTab.agentStatus === 'waiting_for_command_approval' || activeTab.agentStatus === 'waiting_for_approval') ? "Agent is working..." : "Instruct the agent..."}
                  value={activeTab.chatInput || ""}
                  onChange={(e) => handleChatInputChange(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && activeTab.agentStatus !== 'running' && activeTab.agentStatus !== 'waiting_for_command_approval' && handleSendMessage()}
                  className="chat-input"
                  disabled={activeTab.agentStatus === 'running' || activeTab.agentStatus === 'waiting_for_command_approval' || activeTab.agentStatus === 'waiting_for_approval'}
                />
                {activeTab.agentStatus === 'running' ? (
                  <button className="send-btn stop" onClick={handleStopAgent} title="Stop Agent">
                    <svg viewBox="0 0 24 24"><path fill="currentColor" d="M6 19h12V5H6v14z"/></svg>
                  </button>
                ) : (
                  <button className="send-btn" onClick={handleSendMessage} title="Send Message">
                    <svg viewBox="0 0 24 24"><path fill="currentColor" d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>
                  </button>
                )}
              </div>
              {toast && (
                <div className={`toast-notification ${toast.type}`}>
                  {toast.type === 'success' ? '✓ ' : 'ℹ '}
                  {toast.message}
                </div>
              )}
            </aside>
          </>
        ) : (
          <div className="no-active-tab-state">
            <h2>No active workspace. Open a folder to start.</h2>
          </div>
        )}
      </div>

      {/* Floating Context Menu */}
      {contextMenu.visible && contextMenu.node && (
        <div 
          className="context-menu" 
          style={{ top: contextMenu.y, left: contextMenu.x }}
          onClick={(e) => e.stopPropagation()}
        >
          {!contextMenu.node.isDir && (
            <button onClick={() => { handleSelectFile(contextMenu.node!); setContextMenu(prev => ({ ...prev, visible: false })); }}>
              Open File
            </button>
          )}
          <button onClick={(e) => { handleRenamePath(contextMenu.node!, e); setContextMenu(prev => ({ ...prev, visible: false })); }}>
            Rename
          </button>
          <button onClick={(e) => { handleDeletePath(contextMenu.node!, e); setContextMenu(prev => ({ ...prev, visible: false })); }}>
            Delete
          </button>
          <button onClick={() => { 
            if (activeTab) {
              OpenPathInExplorer(activeTab.path, contextMenu.node!.path);
            }
            setContextMenu(prev => ({ ...prev, visible: false }));
          }}>
            Reveal in Explorer
          </button>
        </div>
      )}

      {/* Settings Modal Overlay */}
      {isSettingsOpen && (
        <div className="settings-overlay" onClick={() => setIsSettingsOpen(false)}>
          <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
            <div className="settings-modal-header">
              <h2>Application Settings</h2>
              <button className="close-modal-btn" onClick={() => setIsSettingsOpen(false)}>×</button>
            </div>
            <div className="settings-modal-body">
              <div className="form-group">
                <label>Gemini API Key</label>
                <input 
                  type="password" 
                  value={apiKeys.geminiApiKey} 
                  onChange={(e) => setApiKeys(prev => ({ ...prev, geminiApiKey: e.target.value }))}
                  placeholder="Enter Gemini API key"
                />
              </div>
              <div className="form-group">
                <label>OpenCode API Key</label>
                <input 
                  type="password" 
                  value={apiKeys.openCodeApiKey} 
                  onChange={(e) => setApiKeys(prev => ({ ...prev, openCodeApiKey: e.target.value }))}
                  placeholder="Enter OpenCode API key"
                />
              </div>
              <div className="form-group">
                <label>OpenRouter API Key</label>
                <input 
                  type="password" 
                  value={apiKeys.openRouterApiKey} 
                  onChange={(e) => setApiKeys(prev => ({ ...prev, openRouterApiKey: e.target.value }))}
                  placeholder="Enter OpenRouter API key"
                />
              </div>
              <div className="form-group">
                <label>Ollama Endpoint URL</label>
                <input 
                  type="text" 
                  value={apiKeys.ollamaEndpoint} 
                  onChange={(e) => setApiKeys(prev => ({ ...prev, ollamaEndpoint: e.target.value }))}
                  placeholder="http://localhost:11434"
                />
              </div>
              <div className="form-group checkbox-group">
                <label className="checkbox-label">
                  <input 
                    type="checkbox" 
                    checked={toggles.enableSearchCode} 
                    onChange={(e) => setToggles(prev => ({ ...prev, enableSearchCode: e.target.checked }))}
                  />
                  Enable Context Search Tool (search_code)
                </label>
              </div>
              <div className="form-group checkbox-group">
                <label className="checkbox-label">
                  <input 
                    type="checkbox" 
                    checked={toggles.enableContextCompression} 
                    onChange={(e) => setToggles(prev => ({ ...prev, enableContextCompression: e.target.checked }))}
                  />
                  Enable Conversation Log Compression (Sliding Window)
                </label>
              </div>
              <div className="form-group checkbox-group">
                <label className="checkbox-label">
                  <input 
                    type="checkbox" 
                    checked={toggles.enforcePlanning} 
                    onChange={(e) => setToggles(prev => ({ ...prev, enforcePlanning: e.target.checked }))}
                  />
                  Enforce Planning Mode (Pause for approval and display checklists)
                </label>
              </div>
              <div className="form-group checkbox-group">
                <label className="checkbox-label">
                  <input 
                    type="checkbox" 
                    checked={toggles.useRepoMap} 
                    onChange={(e) => setToggles(prev => ({ ...prev, useRepoMap: e.target.checked }))}
                  />
                  Use Repository Map (PageRank Skeleton prompt compression)
                </label>
              </div>
              {toggles.useRepoMap && (
                <div className="form-group" style={{ paddingLeft: '24px' }}>
                  <label>Repo Map Token Limit</label>
                  <input 
                    type="number" 
                    value={toggles.repoMapTokens} 
                    onChange={(e) => setToggles(prev => ({ ...prev, repoMapTokens: parseInt(e.target.value) || 0 }))}
                    placeholder="1024"
                    style={{ width: '100px' }}
                  />
                </div>
              )}
            </div>
            <div className="settings-modal-footer">
              <button className="secondary-btn" onClick={() => setIsSettingsOpen(false)}>Close</button>
            </div>
          </div>
        </div>
      )}

      {/* New Project Modal Overlay */}
      {isNewProjectOpen && (
        <div className="settings-overlay" onClick={() => setIsNewProjectOpen(false)}>
          <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
            <div className="settings-modal-header">
              <h2>Create New Project</h2>
              <button className="close-modal-btn" onClick={() => setIsNewProjectOpen(false)}>×</button>
            </div>
            <div className="settings-modal-body">
              <div className="form-group">
                <label>Project Name</label>
                <input 
                  type="text" 
                  value={newProjectName} 
                  onChange={(e) => setNewProjectName(e.target.value)}
                  placeholder="e.g. MyAwesomeApp"
                />
              </div>
              <div className="form-group">
                <label>Parent Directory</label>
                <div style={{ display: 'flex', gap: '8px' }}>
                  <input 
                    type="text" 
                    value={newProjectParentDir} 
                    onChange={(e) => setNewProjectParentDir(e.target.value)}
                    placeholder="Select folder..."
                    style={{ flex: 1 }}
                  />
                  <button className="secondary-btn" onClick={handleBrowseParentDir}>Browse...</button>
                </div>
              </div>
              <div className="form-group">
                <label>Tech Stack Checklist</label>
                <div className="tech-stack-grid">
                  {TECH_STACKS.map(tech => (
                    <label key={tech} className="checkbox-label tech-checkbox">
                      <input 
                        type="checkbox" 
                        checked={newProjectTechStack.includes(tech)}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setNewProjectTechStack(prev => [...prev, tech]);
                          } else {
                            setNewProjectTechStack(prev => prev.filter(t => t !== tech));
                          }
                        }}
                      />
                      {tech}
                    </label>
                  ))}
                </div>
              </div>
            </div>
            <div className="settings-modal-footer">
              <button className="secondary-btn" onClick={() => setIsNewProjectOpen(false)}>Cancel</button>
              <button className="save-btn dirty" onClick={handleCreateNewProject}>Create Project</button>
            </div>
          </div>
        </div>
      )}

      {/* Project Settings Modal Overlay */}
      {isProjectSettingsOpen && (
        <div className="settings-overlay" onClick={() => setIsProjectSettingsOpen(false)}>
          <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
            <div className="settings-modal-header">
              <h2>Project Settings - {activeTab?.name}</h2>
              <button className="close-modal-btn" onClick={() => setIsProjectSettingsOpen(false)}>×</button>
            </div>
            <div className="settings-modal-body">
              <p style={{ marginBottom: '12px', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                Configure the tech stack for this project. The AI agent will use this context to tailor its solutions.
              </p>
              <div className="form-group">
                <label>Tech Stack Checklist</label>
                <div className="tech-stack-grid">
                  {TECH_STACKS.map(tech => (
                    <label key={tech} className="checkbox-label tech-checkbox">
                      <input 
                        type="checkbox" 
                        checked={currentProjectTechStack.includes(tech)}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setCurrentProjectTechStack(prev => [...prev, tech]);
                          } else {
                            setCurrentProjectTechStack(prev => prev.filter(t => t !== tech));
                          }
                        }}
                      />
                      {tech}
                    </label>
                  ))}
                </div>
              </div>
            </div>
            <div className="settings-modal-footer">
              <button className="secondary-btn" onClick={() => setIsProjectSettingsOpen(false)}>Cancel</button>
              <button className="save-btn dirty" onClick={handleSaveProjectSettings}>Save Settings</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default App;
