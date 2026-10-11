export namespace alerts {
	
	export class Chat {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Chat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}

}

export namespace cloudflare {
	
	export class Record {
	    name: string;
	    want: string;
	    status: string;
	    problem?: string;
	
	    static createFrom(source: any = {}) {
	        return new Record(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.want = source["want"];
	        this.status = source["status"];
	        this.problem = source["problem"];
	    }
	}

}

export namespace main {
	
	export class AlertsBot {
	    bot: string;
	    chats: alerts.Chat[];
	
	    static createFrom(source: any = {}) {
	        return new AlertsBot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bot = source["bot"];
	        this.chats = this.convertValues(source["chats"], alerts.Chat);
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
	export class AlertsView {
	    configured: boolean;
	    chats: string;
	    via: string;
	
	    static createFrom(source: any = {}) {
	        return new AlertsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.chats = source["chats"];
	        this.via = source["via"];
	    }
	}
	export class BackupFile {
	    path: string;
	    name: string;
	    size: number;
	    at: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.at = source["at"];
	    }
	}
	export class BackupReport {
	    file: BackupFile;
	    files: number;
	    modules: string[];
	    missing: string[];
	
	    static createFrom(source: any = {}) {
	        return new BackupReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = this.convertValues(source["file"], BackupFile);
	        this.files = source["files"];
	        this.modules = source["modules"];
	        this.missing = source["missing"];
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
	export class CardView {
	    kind: string;
	    node: string;
	    name: string;
	    title: string;
	    qr: string;
	    body: string;
	    hint: string;
	    extra: string;
	    base: string;
	    qrImage: string;
	
	    static createFrom(source: any = {}) {
	        return new CardView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.node = source["node"];
	        this.name = source["name"];
	        this.title = source["title"];
	        this.qr = source["qr"];
	        this.body = source["body"];
	        this.hint = source["hint"];
	        this.extra = source["extra"];
	        this.base = source["base"];
	        this.qrImage = source["qrImage"];
	    }
	}
	export class ClientList {
	    awg: string[];
	    sub: string[];
	    tg: string[];
	    errors: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ClientList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.awg = source["awg"];
	        this.sub = source["sub"];
	        this.tg = source["tg"];
	        this.errors = source["errors"];
	    }
	}
	export class ModuleView {
	    name: string;
	    catalog?: modules.Catalog;
	    choice: wizard.Choice;
	    installed: boolean;
	    missing: string[];
	    busy: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModuleView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.catalog = this.convertValues(source["catalog"], modules.Catalog);
	        this.choice = this.convertValues(source["choice"], wizard.Choice);
	        this.installed = source["installed"];
	        this.missing = source["missing"];
	        this.busy = source["busy"];
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
	export class NodeView {
	    name: string;
	    role: string;
	    host: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.role = source["role"];
	        this.host = source["host"];
	    }
	}
	export class State {
	    inventory: string;
	    nodes: NodeView[];
	    vault: string;
	    error: string;
	    open: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inventory = source["inventory"];
	        this.nodes = this.convertValues(source["nodes"], NodeView);
	        this.vault = source["vault"];
	        this.error = source["error"];
	        this.open = source["open"];
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
	export class VersionsView {
	    nodes: string[];
	    rows: profile.Row[];
	    errors: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new VersionsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodes = source["nodes"];
	        this.rows = this.convertValues(source["rows"], profile.Row);
	        this.errors = source["errors"];
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
	export class WCatalogView {
	    groups: modules.Group[];
	    modules: ModuleView[];
	
	    static createFrom(source: any = {}) {
	        return new WCatalogView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = this.convertValues(source["groups"], modules.Group);
	        this.modules = this.convertValues(source["modules"], ModuleView);
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
	export class WPreviewView {
	    steps: wizard.Step[];
	    problems: string[];
	
	    static createFrom(source: any = {}) {
	        return new WPreviewView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.steps = this.convertValues(source["steps"], wizard.Step);
	        this.problems = source["problems"];
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
	export class WServer {
	    name: string;
	    host: string;
	    role: string;
	    rootPassword: string;
	    via: string;
	    viaName: string;
	
	    static createFrom(source: any = {}) {
	        return new WServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.host = source["host"];
	        this.role = source["role"];
	        this.rootPassword = source["rootPassword"];
	        this.via = source["via"];
	        this.viaName = source["viaName"];
	    }
	}
	export class WSetup {
	    topology: string;
	    entry: WServer;
	    exit: WServer;
	    domain: string;
	    tunnelName: string;
	    subName: string;
	    exitName: string;
	    choices: Record<string, boolean>;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new WSetup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.topology = source["topology"];
	        this.entry = this.convertValues(source["entry"], WServer);
	        this.exit = this.convertValues(source["exit"], WServer);
	        this.domain = source["domain"];
	        this.tunnelName = source["tunnelName"];
	        this.subName = source["subName"];
	        this.exitName = source["exitName"];
	        this.choices = source["choices"];
	        this.dir = source["dir"];
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

}

export namespace modules {
	
	export class Catalog {
	    group: string;
	    order: number;
	    required: boolean;
	    auto: boolean;
	    default: boolean;
	    domain: boolean;
	    place: Record<string, Array<string>>;
	    title: string;
	    what: string;
	    why: string;
	    cost: string;
	
	    static createFrom(source: any = {}) {
	        return new Catalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group = source["group"];
	        this.order = source["order"];
	        this.required = source["required"];
	        this.auto = source["auto"];
	        this.default = source["default"];
	        this.domain = source["domain"];
	        this.place = source["place"];
	        this.title = source["title"];
	        this.what = source["what"];
	        this.why = source["why"];
	        this.cost = source["cost"];
	    }
	}
	export class Group {
	    id: string;
	    title: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.note = source["note"];
	    }
	}

}

export namespace profile {
	
	export class Row {
	    name: string;
	    ref: string;
	    repo: string;
	    nodes: Record<string, string>;
	    latest: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new Row(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ref = source["ref"];
	        this.repo = source["repo"];
	        this.nodes = source["nodes"];
	        this.latest = source["latest"];
	        this.note = source["note"];
	    }
	}

}

export namespace reach {
	
	export class DNSAnswer {
	    resolver: string;
	    addrs: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DNSAnswer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.resolver = source["resolver"];
	        this.addrs = source["addrs"];
	        this.error = source["error"];
	    }
	}
	export class DNSResult {
	    name: string;
	    want: string;
	    ok: boolean;
	    answers: DNSAnswer[];
	    problem: string;
	
	    static createFrom(source: any = {}) {
	        return new DNSResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.want = source["want"];
	        this.ok = source["ok"];
	        this.answers = this.convertValues(source["answers"], DNSAnswer);
	        this.problem = source["problem"];
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
	export class Verdict {
	    host: string;
	    port: number;
	    ok: boolean;
	    banner: string;
	    problem: string;
	    internet: boolean;
	    myIp: string;
	    viaOwn: string;
	    ms: number;
	
	    static createFrom(source: any = {}) {
	        return new Verdict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.ok = source["ok"];
	        this.banner = source["banner"];
	        this.problem = source["problem"];
	        this.internet = source["internet"];
	        this.myIp = source["myIp"];
	        this.viaOwn = source["viaOwn"];
	        this.ms = source["ms"];
	    }
	}

}

export namespace status {
	
	export class Module {
	    name: string;
	    installed: string;
	    code: string;
	
	    static createFrom(source: any = {}) {
	        return new Module(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.installed = source["installed"];
	        this.code = source["code"];
	    }
	}
	export class Status {
	    node: string;
	    role: string;
	    host: string;
	    reachable: boolean;
	    error?: string;
	    os: string;
	    kernel: string;
	    uptime: string;
	    disk: string;
	    mem: string;
	    modules: Module[];
	    watch: boolean;
	    checks: number;
	    failed: string[];
	    at: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node = source["node"];
	        this.role = source["role"];
	        this.host = source["host"];
	        this.reachable = source["reachable"];
	        this.error = source["error"];
	        this.os = source["os"];
	        this.kernel = source["kernel"];
	        this.uptime = source["uptime"];
	        this.disk = source["disk"];
	        this.mem = source["mem"];
	        this.modules = this.convertValues(source["modules"], Module);
	        this.watch = source["watch"];
	        this.checks = source["checks"];
	        this.failed = source["failed"];
	        this.at = source["at"];
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

}

export namespace wizard {
	
	export class Choice {
	    on: boolean;
	    why: string;
	
	    static createFrom(source: any = {}) {
	        return new Choice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.on = source["on"];
	        this.why = source["why"];
	    }
	}
	export class Finding {
	    level: string;
	    what: string;
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.what = source["what"];
	    }
	}
	export class ServerCheck {
	    findings: Finding[];
	    worst: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.findings = this.convertValues(source["findings"], Finding);
	        this.worst = source["worst"];
	        this.error = source["error"];
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
	export class Step {
	    node: string;
	    role: string;
	    module: string;
	    title: string;
	    done: boolean;
	    reason: string;
	    skip: boolean;
	    from?: string;
	    to?: string;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node = source["node"];
	        this.role = source["role"];
	        this.module = source["module"];
	        this.title = source["title"];
	        this.done = source["done"];
	        this.reason = source["reason"];
	        this.skip = source["skip"];
	        this.from = source["from"];
	        this.to = source["to"];
	    }
	}

}

