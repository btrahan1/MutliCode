export namespace main {
	
	export class AppSettings {
	    openWorkspaces: string[];
	    activeWorkspace: string;
	    geminiApiKey: string;
	    openCodeApiKey: string;
	    openRouterApiKey: string;
	    ollamaEndpoint: string;
	    workspaceModels: Record<string, string>;
	    sidebarWidth: number;
	    chatWidth: number;
	    workspaceHistory: Record<string, Array<ChatMessage>>;
	    theme: string;
	    enableSearchCode: boolean;
	    enableContextCompression: boolean;
	    useRepoMap: boolean;
	    repoMapTokens: number;
	    enforcePlanning: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openWorkspaces = source["openWorkspaces"];
	        this.activeWorkspace = source["activeWorkspace"];
	        this.geminiApiKey = source["geminiApiKey"];
	        this.openCodeApiKey = source["openCodeApiKey"];
	        this.openRouterApiKey = source["openRouterApiKey"];
	        this.ollamaEndpoint = source["ollamaEndpoint"];
	        this.workspaceModels = source["workspaceModels"];
	        this.sidebarWidth = source["sidebarWidth"];
	        this.chatWidth = source["chatWidth"];
	        this.workspaceHistory = this.convertValues(source["workspaceHistory"], Array<ChatMessage>, true);
	        this.theme = source["theme"];
	        this.enableSearchCode = source["enableSearchCode"];
	        this.enableContextCompression = source["enableContextCompression"];
	        this.useRepoMap = source["useRepoMap"];
	        this.repoMapTokens = source["repoMapTokens"];
	        this.enforcePlanning = source["enforcePlanning"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChatMessage {
	    role: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	    }
	}
	export class FileNode {
	    name: string;
	    path: string;
	    isDir: boolean;
	    children: FileNode[];
	
	    static createFrom(source: any = {}) {
	        return new FileNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.children = this.convertValues(source["children"], FileNode);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectSettings {
	    techStack: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProjectSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.techStack = source["techStack"];
	    }
	}

}

